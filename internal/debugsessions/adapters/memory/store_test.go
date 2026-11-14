package memory

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func initial(t *testing.T, id string) domain.Event {
	t.Helper()
	e, err := domain.New(id, domain.Target{Environment: "env", Namespace: "app", Profile: "profile"}, domain.Identity{Actor: "actor", Owner: "owner"}, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestConcurrentRevisionHasOneTransitionAndAuditFact(t *testing.T) {
	s := New()
	e := initial(t, "dsr_abcdefghijklmnopqrstuvwxyz")
	if err := s.Append(t.Context(), 0, e); err != nil {
		t.Fatal(err)
	}
	revoked, err := e.Transition(domain.Revoked, "admin", e.CreatedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	var success, conflict atomic.Int32
	var workers sync.WaitGroup
	for range 40 {
		workers.Go(func() {
			err := s.Append(t.Context(), 1, revoked)
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
	if success.Load() != 1 || conflict.Load() != 39 {
		t.Fatal(success.Load(), conflict.Load())
	}
	history, err := s.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 2 || history[0] != e || history[1] != revoked {
		t.Fatal(history, err)
	}
	history[0].Owner = "attacker"
	again, err := s.History(t.Context(), e.SessionID)
	if err != nil || again[0] != e {
		t.Fatal("return alias changed audit", again, err)
	}
}
func TestInvalidScopeReplayAndCancellationDoNotChangeJournal(t *testing.T) {
	s := New()
	e := initial(t, "dsr_abcdefghijklmnopqrstuvwxyz")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.Append(cancelled, 0, e); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.History(t.Context(), e.SessionID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Append(t.Context(), 0, e); err != nil {
		t.Fatal(err)
	}
	if err := s.Append(t.Context(), 0, e); !errors.Is(err, domain.ErrConflict) {
		t.Fatal(err)
	}
	revoked, _ := e.Transition(domain.Revoked, "admin", e.CreatedAt.Add(time.Minute))
	revoked.Target.Namespace = "other"
	if err := s.Append(t.Context(), 1, revoked); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	revoked.Target = e.Target
	if err := s.Append(cancelled, 1, revoked); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	history, err := s.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 1 {
		t.Fatal(history, err)
	}
	if _, err := s.History(cancelled, e.SessionID); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.IDs(cancelled, "", 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestJournalIDsAreBoundedAndOrdered(t *testing.T) {
	s := New()
	ids := []string{"dsr_bcdefghijklmnopqrstuvwxyz2", "dsr_abcdefghijklmnopqrstuvwxyz"}
	for _, id := range ids {
		if err := s.Append(t.Context(), 0, initial(t, id)); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.IDs(t.Context(), "", 1)
	if err != nil || len(first) != 1 || first[0] != ids[1] {
		t.Fatal(first, err)
	}
	second, err := s.IDs(t.Context(), first[0], 256)
	if err != nil || len(second) != 1 || second[0] != ids[0] {
		t.Fatal(second, err)
	}
	for _, limit := range []int{-1, 0, 257} {
		if _, err := s.IDs(t.Context(), "", limit); !errors.Is(err, domain.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, err := s.IDs(t.Context(), "../escape", 1); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	if _, err := s.History(t.Context(), "../escape"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
}
