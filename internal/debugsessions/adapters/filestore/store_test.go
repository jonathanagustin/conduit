package filestore

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func initial(t *testing.T) domain.Event {
	t.Helper()
	e, err := domain.New("dsr_abcdefghijklmnopqrstuvwxyz", domain.Target{Environment: "env", Namespace: "app", Profile: "profile"}, domain.Identity{Actor: "actor", Owner: "owner"}, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func open(t *testing.T, directory string) *Store {
	t.Helper()
	s, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func TestDurableHistorySurvivesReopenAndConcurrentIndependentWriters(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "journal")
	s := open(t, directory)
	e := initial(t)
	if err := s.Append(t.Context(), 0, e); err != nil {
		t.Fatal(err)
	}
	reopened := open(t, directory)
	history, err := reopened.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 1 || history[0] != e {
		t.Fatal(history, err)
	}
	revoked, _ := e.Transition(domain.Revoked, "administrator", e.CreatedAt.Add(time.Minute))
	var success, conflict atomic.Int32
	var workers sync.WaitGroup
	for range 24 {
		independent := open(t, directory)
		workers.Go(func() {
			err := independent.Append(t.Context(), 1, revoked)
			switch {
			case err == nil:
				success.Add(1)
			case errors.Is(err, domain.ErrConflict):
				conflict.Add(1)
			default:
				t.Error(err)
			}
		})
	}
	workers.Wait()
	if success.Load() != 1 || conflict.Load() != 23 {
		t.Fatal(success.Load(), conflict.Load())
	}
	final := open(t, directory)
	history, err = final.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 2 || history[1] != revoked {
		t.Fatal(history, err)
	}
	history[0].Owner = "attacker"
	again, err := final.History(t.Context(), e.SessionID)
	if err != nil || again[0] != e {
		t.Fatal("return alias", again, err)
	}
	ids, err := final.IDs(t.Context(), "", 256)
	if err != nil || len(ids) != 1 || ids[0] != e.SessionID {
		t.Fatal(ids, err)
	}
}
func TestFailedAndCancelledAppendsDoNotCreateTransitions(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "journal"))
	e := initial(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Append(ctx, 0, e); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.History(t.Context(), e.SessionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Append(t.Context(), 0, e); err != nil {
		t.Fatal(err)
	}
	next, _ := e.Transition(domain.Revoked, "admin", e.CreatedAt.Add(time.Minute))
	next.Owner = "other"
	if err := s.Append(t.Context(), 1, next); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.Append(t.Context(), 0, e); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	if _, err := s.History(t.Context(), "../escape"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.IDs(t.Context(), "../escape", 1); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.IDs(ctx, "", 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	history, err := s.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 1 {
		t.Fatal(history, err)
	}
}
func TestCorruptAndUntrustedFilesystemRecordsFailClosed(t *testing.T) {
	for _, kind := range []string{"gap", "invalid-name", "symlink", "oversized", "scope-change"} {
		t.Run(kind, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "journal")
			s := open(t, directory)
			e := initial(t)
			next, _ := e.Transition(domain.Revoked, "admin", e.CreatedAt.Add(time.Minute))
			b, _ := domain.EncodeEvent(next)
			if kind != "gap" {
				if err := s.Append(t.Context(), 0, e); err != nil {
					t.Fatal(err)
				}
			}
			name := filename(e.SessionID, 2)
			switch kind {
			case "invalid-name":
				name = e.SessionID + ".4.json"
			case "symlink":
				if err := os.Symlink("/etc/passwd", filepath.Join(directory, name)); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				b = make([]byte, domain.MaximumDocumentBytes+1)
			case "scope-change":
				next.Target.Namespace = "other"
				b, _ = domain.EncodeEvent(next)
			}
			if kind != "symlink" {
				if err := os.WriteFile(filepath.Join(directory, name), b, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.History(t.Context(), e.SessionID); !errors.Is(err, domain.ErrCorrupt) {
				t.Fatal(kind, err)
			}
			if err := s.Append(t.Context(), 1, next); !errors.Is(err, domain.ErrCorrupt) {
				t.Fatal(kind, err)
			}
		})
	}
}
func TestPrivateDirectoryAndOrphanTemporaryFiles(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "journal")
	s := open(t, directory)
	if err := os.WriteFile(filepath.Join(directory, ".event-crash.tmp"), []byte("incomplete"), 0o600); err != nil {
		t.Fatal(err)
	}
	ids, err := s.IDs(t.Context(), "", 1)
	if err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if other, err := New(directory); err == nil {
		_ = other.Close()
		t.Fatal("accepted non-private journal directory")
	}
}

func TestWriterProcessExitsBeforeReaderOpensJournal(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "journal")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestJournalWriterChild$")
	command.Env = append(os.Environ(), "CONDUIT_DEBUG_JOURNAL_CHILD_DIRECTORY="+directory)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("writer child: %v: %s", err, output)
	}
	reader := open(t, directory)
	e := initial(t)
	history, err := reader.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 1 || history[0] != e {
		t.Fatal(history, err)
	}
}
func TestJournalWriterChild(t *testing.T) {
	directory := os.Getenv("CONDUIT_DEBUG_JOURNAL_CHILD_DIRECTORY")
	if directory == "" {
		return
	}
	s := open(t, directory)
	if err := s.Append(t.Context(), 0, initial(t)); err != nil {
		t.Fatal(err)
	}
}
func TestIDPagesAreSortedBoundedAndStrictlyAfterCursor(t *testing.T) {
	s := open(t, filepath.Join(t.TempDir(), "journal"))
	first := initial(t)
	second := first
	second.SessionID = "dsr_bcdefghijklmnopqrstuvwxyz2"
	for _, e := range []domain.Event{second, first} {
		if err := s.Append(t.Context(), 0, e); err != nil {
			t.Fatal(err)
		}
	}
	ids, err := s.IDs(t.Context(), "", 1)
	if err != nil || len(ids) != 1 || ids[0] != first.SessionID {
		t.Fatal(ids, err)
	}
	ids, err = s.IDs(t.Context(), first.SessionID, 1)
	if err != nil || len(ids) != 1 || ids[0] != second.SessionID {
		t.Fatal(ids, err)
	}
	ids, err = s.IDs(t.Context(), second.SessionID, 1)
	if err != nil || len(ids) != 0 {
		t.Fatal(ids, err)
	}
	for _, limit := range []int{-1, 0, 257} {
		if _, err := s.IDs(t.Context(), "", limit); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
}
