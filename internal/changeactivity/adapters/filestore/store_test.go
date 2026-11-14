package filestore

import (
	"encoding/json"
	"errors"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func comment() domain.Comment {
	return domain.Comment{ID: "cmt_" + strings.Repeat("1", 32), RequestID: "crq_thread", Author: "author", Actor: "carrier", Text: "original", CreatedAt: time.Now().UTC(), ValueTier: domain.ValueTier}
}
func TestDiscussionAppendIsImmutablePrivateAndAvailableAfterReopen(t *testing.T) {
	directory := t.TempDir()
	first, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	record := comment()
	replacement := record
	replacement.Text = "replacement"
	var successes atomic.Int32
	var group sync.WaitGroup
	for i := range 20 {
		group.Go(func() {
			store, record := first, record
			if i%2 != 0 {
				store, record = second, replacement
			}
			if err := store.Append(t.Context(), record); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrConflict) {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatal("duplicate append acknowledged", successes.Load())
	}
	reopened, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	rows, err := reopened.List(t.Context(), record.RequestID)
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	saved := rows[0]
	if err := first.Append(t.Context(), replacement); !errors.Is(err, domain.ErrConflict) {
		t.Fatal("discussion overwrite accepted", err)
	}
	again, err := second.List(t.Context(), record.RequestID)
	if err != nil || len(again) != 1 || again[0] != saved {
		t.Fatal("duplicate changed persisted record", again, err)
	}
	info, err := os.Stat(filepath.Join(directory, prefix(record.RequestID)+record.ID+".json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("discussion record not private", err)
	}
	other := record
	other.RequestID = "crq_other"
	if err := first.Append(t.Context(), other); err != nil {
		t.Fatal("comment identity is not scoped to its request", err)
	}
	rows, err = reopened.List(t.Context(), record.RequestID)
	if err != nil || len(rows) != 1 {
		t.Fatal("foreign discussion leaked", rows, err)
	}
}
func TestCorruptDiscussionDoesNotBecomeAnEmptyThread(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	record := comment()
	path := filepath.Join(directory, prefix(record.RequestID)+record.ID+".json")
	foreign := record
	foreign.RequestID = "crq_other"
	document, _ := json.Marshal(foreign)
	if err := os.WriteFile(path, document, 0o600); err != nil {
		t.Fatal(err)
	}
	if rows, err := store.List(t.Context(), record.RequestID); !errors.Is(err, domain.ErrCorrupt) || rows != nil {
		t.Fatal("corruption became observed absence", rows, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, document, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := store.List(t.Context(), record.RequestID); !errors.Is(err, domain.ErrCorrupt) {
		t.Fatal("discussion symlink followed", err)
	}
}
