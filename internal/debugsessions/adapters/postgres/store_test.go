package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/debugsessions/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

func store(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("requires disposable PostgreSQL gate")
	}
	s, err := New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func initial(t *testing.T) domain.Event {
	t.Helper()
	e, err := domain.New("dsr_"+strings.ToLower(rand.Text()), domain.Target{Environment: "env", Namespace: "app", Profile: "profile"}, domain.Identity{Actor: "actor", Owner: "owner"}, time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestMigrationIdentityIsStableAndIndependent(t *testing.T) {
	m := Migrations()
	if err := pgmigrate.Validate(Component, m); err != nil {
		t.Fatal(err)
	}
	if Component != "debug-session-journal" || len(m) != 1 || m[0].Name != "0001-immutable-session-events" {
		t.Fatal(m)
	}
	m[0].Name = "modified"
	if Migrations()[0].Name != "0001-immutable-session-events" {
		t.Fatal("migration list alias")
	}
}
func TestIndependentPoolsSerializeOneTransitionAndRetainAudit(t *testing.T) {
	s := store(t)
	peer := store(t)
	e := initial(t)
	if err := s.Append(t.Context(), 0, e); err != nil {
		t.Fatal(err)
	}
	reopened := store(t)
	history, err := reopened.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 1 || history[0] != e {
		t.Fatal(history, err)
	}
	next, _ := e.Transition(domain.Revoked, "admin", e.CreatedAt.Add(time.Minute))
	var success, conflict atomic.Int32
	var workers sync.WaitGroup
	for i := range 32 {
		writer := s
		if i%2 == 1 {
			writer = peer
		}
		workers.Go(func() {
			err := writer.Append(t.Context(), 1, next)
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
	if success.Load() != 1 || conflict.Load() != 31 {
		t.Fatal(success.Load(), conflict.Load())
	}
	history, err = peer.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 2 || history[1] != next {
		t.Fatal(history, err)
	}
	var count int
	if err := s.db.QueryRowContext(t.Context(), `SELECT count(*) FROM conduit_debug_session_events WHERE session_id=$1`, e.SessionID).Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	history[0].Owner = "intruder"
	again, err := peer.History(t.Context(), e.SessionID)
	if err != nil || again[0] != e {
		t.Fatal(again, err)
	}
}
func TestInvalidAndCancelledTransactionsLeaveHistoryUnchanged(t *testing.T) {
	s := store(t)
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
	next.Target.Namespace = "other"
	if err := s.Append(t.Context(), 1, next); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
	history, err := s.History(t.Context(), e.SessionID)
	if err != nil || len(history) != 1 {
		t.Fatal(history, err)
	}
	if _, err := s.IDs(t.Context(), "../escape", 1); !errors.Is(err, domain.ErrInvalid) {
		t.Fatal(err)
	}
}
func TestStoredCorruptionAndMissingPredecessorsFailClosed(t *testing.T) {
	s := store(t)
	e := initial(t)
	next, _ := e.Transition(domain.Revoked, "admin", e.CreatedAt.Add(time.Minute))
	document, _ := domain.EncodeEvent(next)
	if _, err := s.db.ExecContext(t.Context(), `INSERT INTO conduit_debug_session_events VALUES($1,2,$2)`, e.SessionID, document); err != nil {
		t.Fatal(err)
	}
	if _, err := s.History(t.Context(), e.SessionID); !errors.Is(err, domain.ErrCorrupt) {
		t.Fatal(err)
	}
	if err := s.Append(t.Context(), 2, next); !errors.Is(err, domain.ErrCorrupt) {
		t.Fatal(err)
	}
}
