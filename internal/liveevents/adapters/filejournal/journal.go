// Package filejournal persists delivery state with fsync and an atomic rename.
// One Journal owns a file; it is not a multi-process database adapter.
package filejournal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/liveevents/domain"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const maxStateBytes = 512 * 1024 * 1024

type Journal struct {
	mu    sync.Mutex
	path  string
	state domain.State
}

func Open(path string) (*Journal, error) {
	if path == "" {
		return nil, domain.ErrInvalid
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	state := domain.NewState()
	info, err := os.Lstat(path)
	if err == nil {
		state = domain.State{}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > maxStateBytes {
			return nil, fmt.Errorf("%w: journal permissions/type/size", domain.ErrUnavailable)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(io.LimitReader(file, maxStateBytes+1))
		decoder.DisallowUnknownFields()
		decodeErr := decoder.Decode(&state)
		var extra any
		if decodeErr == nil && decoder.Decode(&extra) != io.EOF {
			decodeErr = domain.ErrInvalid
		}
		closeErr := file.Close()
		if decodeErr != nil || closeErr != nil || state.Version != domain.Version || state.Streams == nil || state.Producers == nil {
			return nil, fmt.Errorf("%w: journal state", domain.ErrUnavailable)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return &Journal{path: path, state: state}, nil
}
func (journal *Journal) Update(ctx context.Context, change func(*domain.State) error) error {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	working, err := domain.Clone(journal.state)
	if err != nil {
		return err
	}
	if err := change(&working); err != nil {
		return err
	}
	if working.Version != domain.Version || working.Streams == nil || working.Producers == nil {
		return domain.ErrInvalid
	}
	// Detach callback-owned maps and byte slices before retaining committed state.
	committed, err := domain.Clone(working)
	if err != nil {
		return err
	}
	body, err := json.Marshal(committed)
	if err != nil || len(body) > maxStateBytes {
		return domain.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(journal.path), ".live-events-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(body); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, journal.path); err != nil {
		return err
	}
	// Rename committed the visible state. Keep memory aligned even if directory
	// sync reports an uncertain durability outcome; retries remain idempotent.
	journal.state = committed
	directory, err := os.Open(filepath.Dir(journal.path))
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func (journal *Journal) Snapshot(ctx context.Context) (domain.State, error) {
	journal.mu.Lock()
	defer journal.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.State{}, err
	}
	return domain.Clone(journal.state)
}
