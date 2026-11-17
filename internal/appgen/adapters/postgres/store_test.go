package postgres

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/adapters/storetest"
	"github.com/jonathanagustin/conduit/internal/appgen/application"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	store, err := New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(t.Context(), `TRUNCATE conduit_app_generations`); err != nil {
		t.Fatal(err)
	}
	// Leave the table empty. Another package's test reads the same database,
	// and a deliberately corrupt row here would fail its list.
	t.Cleanup(func() {
		if cleaner, err := New(context.Background(), dsn); err == nil {
			_, _ = cleaner.db.ExecContext(context.Background(), `TRUNCATE conduit_app_generations`)
			_ = cleaner.Close()
		}
		_ = store.Close()
	})
	return store
}

func TestPostgresMeetsTheStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) application.Store { return newStore(t) })
}

// A second pool is what a restarted Hub, or another replica, has.
func TestResultsSurviveANewConnectionPool(t *testing.T) {
	first := newStore(t)
	ctx := context.Background()
	if err := first.Save(ctx, application.Result{ID: "gen_durable", Owner: "ada", Name: "hello-http"}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := New(ctx, os.Getenv("CONDUIT_TEST_POSTGRES_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, ok, err := second.Get(ctx, "gen_durable")
	if err != nil || !ok || got.Owner != "ada" || got.Name != "hello-http" {
		t.Fatalf("after reopening: %+v %v %v", got, ok, err)
	}
	listed, err := second.List(ctx, "ada")
	if err != nil || len(listed) != 1 {
		t.Fatalf("list after reopening: %+v %v", listed, err)
	}
	if other, err := second.List(ctx, "bea"); err != nil || len(other) != 0 {
		t.Fatalf("another owner lists %+v %v", other, err)
	}
}

// Two owners race to claim one id. Exactly one holds it afterwards, and the
// loser is told. Neither write is silently replaced.
func TestConcurrentOwnersCannotBothHoldOneID(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	errs := make([]error, 2)
	var group sync.WaitGroup
	for index, owner := range []string{"ada", "bea"} {
		group.Add(1)
		go func() {
			defer group.Done()
			errs[index] = store.Save(ctx, application.Result{ID: "gen_raced", Owner: owner})
		}()
	}
	group.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("saves = %v, want exactly one to succeed", errs)
	}
	got, ok, err := store.Get(ctx, "gen_raced")
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	winner := "ada"
	if errs[0] != nil {
		winner = "bea"
	}
	if got.Owner != winner {
		t.Fatalf("stored owner = %q, want the writer whose save succeeded, %q", got.Owner, winner)
	}
}

// A row whose document names another owner is corrupt. It is an error and is
// never served under the column's owner.
func TestARowThatDisagreesWithItsDocumentIsNotServed(t *testing.T) {
	store := newStore(t)
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO conduit_app_generations (id, owner, document) VALUES ('gen_bad', 'ada', $1)`,
		[]byte(`{"id":"gen_bad","owner":"bea","label":"x","provenance":{}}`)); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := store.Get(ctx, "gen_bad"); err == nil || ok {
		t.Fatalf("get = %v %v, want an error", ok, err)
	}
	if listed, err := store.List(ctx, "ada"); err == nil {
		t.Fatalf("list = %+v, want an error", listed)
	}
}
