package postgres

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/storetest"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

// freshDSN returns a throwaway database, or skips without a server.
func freshDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(pgtest.DataSourceNameEnvironment)
	if dsn == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
	}
	shared, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	fresh, drop, err := pgtest.Fresh(context.Background(), shared, dsn, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	return fresh
}

func open(t *testing.T) *Store {
	t.Helper()
	store, err := New(context.Background(), freshDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestPostgresStoresConform(t *testing.T) {
	if os.Getenv(pgtest.DataSourceNameEnvironment) == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
	}
	storetest.RunUsage(t, func(t *testing.T) modelgovernor.UsageStore { return open(t) })
	storetest.RunTranscripts(t, func(t *testing.T) modelgovernor.TranscriptStore { return open(t) })
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time                       { return clock.now }
func (clock fixedClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }

// Two governors over two pools on one database are two replicas. The budget
// one of them spends is the budget the other refuses on.
func TestABudgetIsSharedBetweenTwoReplicas(t *testing.T) {
	dsn := freshDSN(t)
	ctx := context.Background()
	first, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	clock := fixedClock{now: time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)}
	replica := func(store *Store) *modelgovernor.Governor {
		return modelgovernor.New(modelgovernor.Config{DailyCallBudget: 2}, modelgovernor.Dependencies{
			Usage: store, Clock: clock, Metrics: modelgovernor.NewMetrics(),
		})
	}
	call := func(governor *modelgovernor.Governor) error {
		scoped := modelgovernor.WithScope(ctx, modelgovernor.Scope{Governor: governor, Owner: func() string { return "ada" }})
		_, err := governor.Do(scoped, modelgovernor.Call{Capability: modelgovernor.CapabilityAssistant, Transport: modelgovernor.TransportHTTP},
			nil, func(context.Context) (modelgovernor.Result, error) {
				return modelgovernor.Result{Response: []byte("ok")}, nil
			})
		return err
	}
	a, b := replica(first), replica(second)
	if err := call(a); err != nil {
		t.Fatal(err)
	}
	if err := call(b); err != nil {
		t.Fatal(err)
	}
	if err := call(a); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("the third call across two replicas returned %v, want a budget refusal", err)
	}
}

func TestMigrationsAreWhatAStartApplies(t *testing.T) {
	dsn := freshDSN(t)
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	pending, err := pgmigrate.Pending(ctx, db, Component, Migrations())
	if err != nil || len(pending) != 2 {
		t.Fatalf("pending before a start = %v, err = %v", pending, err)
	}
	store, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	pending, err = pgmigrate.Pending(ctx, db, Component, Migrations())
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after a start = %v, err = %v", pending, err)
	}
	// The schema bound refuses a digest that is not sha256:<64 hex>.
	record := modelgovernor.TranscriptRecord{
		RunID: "airun_x", Round: 1, Owner: "ada", Capability: "assistant", SentPromptSHA256: "short",
		ReceivedResponseSHA256: modelgovernor.Digest(nil), StoredPromptSHA256: modelgovernor.Digest(nil),
		StoredResponseSHA256: modelgovernor.Digest(nil), CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.Put(ctx, record, 0); err == nil {
		t.Fatal("a record with a malformed digest was stored")
	}
}
