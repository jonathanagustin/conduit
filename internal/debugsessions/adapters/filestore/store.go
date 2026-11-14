// Package filestore persists private immutable transition/audit events. Atomic
// hard-link creation provides the revision comparison across Store instances.
package filestore

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/debugsessions/ports"
)

type Store struct{ root *os.Root }

func New(directory string) (*Store, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, err
	}
	info, err := root.Stat(".")
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		_ = root.Close()
		if err != nil {
			return nil, err
		}
		return nil, errors.New("debug-session journal directory must be private")
	}
	return &Store{root: root}, nil
}
func (s *Store) Close() error                    { return s.root.Close() }
func filename(id string, revision uint64) string { return fmt.Sprintf("%s.%06d.json", id, revision) }
func (s *Store) Append(ctx context.Context, expected uint64, next domain.Event) error {
	if !domain.ValidID(next.SessionID) {
		return domain.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	history, err := s.History(ctx, next.SessionID)
	if errors.Is(err, domain.ErrNotFound) {
		history = nil
	} else if err != nil {
		return err
	}
	updated, err := domain.AppendHistory(history, expected, next)
	if err != nil {
		return err
	}
	next = updated[len(updated)-1]
	document, err := domain.EncodeEvent(next)
	if err != nil {
		return err
	}
	temporary := ".event-" + rand.Text() + ".tmp"
	file, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer s.root.Remove(temporary)
	if _, err := file.Write(document); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.root.Link(temporary, filename(next.SessionID, next.Revision)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return domain.ErrConflict
		}
		return err
	}
	// A failure from this point can be an ambiguous commit. Never unlink the
	// canonical record to pretend rollback: another writer may already see it.
	directory, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
func (s *Store) History(ctx context.Context, id string) ([]domain.Event, error) {
	if !domain.ValidID(id) {
		return nil, domain.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var maximum uint64
	err := s.walk(ctx, func(entry os.DirEntry) error {
		if !strings.HasPrefix(entry.Name(), id+".") {
			return nil
		}
		recordID, revision, err := parseName(entry.Name())
		if err != nil || recordID != id {
			return domain.ErrCorrupt
		}
		if revision > maximum {
			maximum = revision
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if maximum == 0 {
		return nil, domain.ErrNotFound
	}
	// Load every predecessor by its fixed key. Directory iteration under an
	// append need not list entries in revision order or in the same batch.
	history := make([]domain.Event, maximum)
	for i := range history {
		revision := uint64(i + 1)
		event, err := s.read(filename(id, revision), id, revision)
		if errors.Is(err, os.ErrNotExist) {
			return nil, domain.ErrCorrupt
		}
		if err != nil {
			return nil, err
		}
		history[i] = event
	}
	if _, err := domain.ValidateHistory(history); err != nil {
		return nil, domain.ErrCorrupt
	}
	return history, nil
}
func (s *Store) read(name, id string, revision uint64) (domain.Event, error) {
	before, err := s.root.Lstat(name)
	if err != nil {
		return domain.Event{}, err
	}
	if !before.Mode().IsRegular() || before.Size() > domain.MaximumDocumentBytes {
		return domain.Event{}, domain.ErrCorrupt
	}
	file, err := s.root.Open(name)
	if err != nil {
		return domain.Event{}, err
	}
	defer file.Close()
	after, err := file.Stat()
	if err != nil {
		return domain.Event{}, err
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return domain.Event{}, domain.ErrCorrupt
	}
	document, err := io.ReadAll(io.LimitReader(file, domain.MaximumDocumentBytes+1))
	if err != nil {
		return domain.Event{}, err
	}
	return domain.DecodeEvent(id, revision, document)
}
func (s *Store) IDs(ctx context.Context, after string, limit int) ([]string, error) {
	if (after != "" && !domain.ValidID(after)) || limit < 1 || limit > 256 {
		return nil, domain.ErrInvalid
	}
	result := []string{}
	err := s.walk(ctx, func(entry os.DirEntry) error {
		name := entry.Name()
		if strings.HasPrefix(name, ".event-") && strings.HasSuffix(name, ".tmp") {
			return nil
		}
		id, _, err := parseName(name)
		if err != nil || entry.Type()&os.ModeType != 0 {
			return domain.ErrCorrupt
		}
		if id <= after {
			return nil
		}
		index, found := slices.BinarySearch(result, id)
		if found || index >= limit {
			return nil
		}
		result = slices.Insert(result, index, id)
		if len(result) > limit {
			result = result[:limit]
		}
		return nil
	})
	return result, err
}
func parseName(name string) (string, uint64, error) {
	parts := strings.Split(name, ".")
	if len(parts) != 3 || !domain.ValidID(parts[0]) || parts[2] != "json" {
		return "", 0, domain.ErrCorrupt
	}
	for revision := uint64(1); revision <= 3; revision++ {
		if name == filename(parts[0], revision) {
			return parts[0], revision, nil
		}
	}
	return "", 0, domain.ErrCorrupt
}
func (s *Store) walk(ctx context.Context, visit func(os.DirEntry) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	directory, err := s.root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	for {
		entries, err := directory.ReadDir(128)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(entry); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

var _ ports.Journal = (*Store)(nil)
