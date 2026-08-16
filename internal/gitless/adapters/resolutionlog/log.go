// Package resolutionlog records what a selector resolved to, append-only.
//
// It exists because a channel is the only mutable thing this design tolerates,
// and an unrecorded move is indistinguishable from a compromise after the fact.
// The application records before it deploys, so a failure here stops the
// deployment — which makes durability the whole contract, not a nice-to-have.
//
// Each record is one JSON object on one line, appended with O_APPEND and fsynced
// before the write is reported successful. O_APPEND makes concurrent writers
// interleave whole lines rather than corrupt each other; the fsync is what makes
// "recorded" mean "survived power loss" rather than "reached a buffer".
//
// Its bound, stated plainly: this is one file on one filesystem. A Hub running
// several replicas needs a shared durable store instead, and this adapter is
// then the wrong one — not misconfigured, wrong. Nothing here detects that, so
// it is a deployment-time decision.
package resolutionlog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

type record struct {
	RecordedAt  string `json:"recordedAt"`
	Environment string `json:"environment"`
	Selector    string `json:"selector"`
	SelectorKid string `json:"selectorKind"`
	Repository  string `json:"repository"`
	Digest      string `json:"digest"`
	Superseded  string `json:"superseded,omitempty"`
	Moved       bool   `json:"moved"`
	Publisher   string `json:"publisher"`
	// Failure withdraws the claim an earlier line made. A reader that ignores
	// it reads the log as it always did; a reader that honours it can tell a
	// move that landed from one that was recorded and then did not.
	Failure string `json:"failure,omitempty"`
}

// Log appends resolutions to a file.
type Log struct {
	mutex sync.Mutex
	path  string
	now   func() time.Time
}

func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create the directory for the resolution log: %w", err)
	}
	// Opened once here to fail at startup rather than at the first deployment.
	handle, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil, fmt.Errorf("open the resolution log: %w", err)
	}
	if err := handle.Close(); err != nil {
		return nil, fmt.Errorf("close the resolution log after checking it: %w", err)
	}
	return &Log{path: path, now: time.Now}, nil
}

func (log *Log) Record(_ context.Context, resolution domain.Resolution) error {
	if err := resolution.Validate(); err != nil {
		return fmt.Errorf("refusing to record an invalid resolution: %w", err)
	}
	kind, label := resolution.Selector.Kind, resolution.Selector.String()
	line, err := json.Marshal(record{
		RecordedAt:  log.now().UTC().Format(time.RFC3339Nano),
		Environment: resolution.Environment,
		Selector:    label,
		SelectorKid: string(kind),
		Repository:  resolution.Pin.Repository,
		Digest:      resolution.Pin.Digest,
		Superseded:  resolution.Superseded,
		// Recorded rather than left to be recomputed. A reader counting moves
		// should not have to reimplement what counts as one.
		Moved:     resolution.Moved(),
		Publisher: resolution.Publisher,
		Failure:   resolution.Failure,
	})
	if err != nil {
		return fmt.Errorf("encode resolution: %w", err)
	}

	log.mutex.Lock()
	defer log.mutex.Unlock()
	handle, err := os.OpenFile(log.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return fmt.Errorf("open the resolution log: %w", err)
	}
	defer handle.Close()
	if _, err := handle.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("append to the resolution log: %w", err)
	}
	// The deployment happens after this returns. Reporting success on a buffered
	// write would mean a crash could leave the deployment recorded nowhere.
	if err := handle.Sync(); err != nil {
		return fmt.Errorf("flush the resolution log to disk: %w", err)
	}
	return nil
}
