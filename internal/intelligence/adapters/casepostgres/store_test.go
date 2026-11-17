package casepostgres

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/jonathanagustin/conduit/internal/intelligence/adapters/casetest"
	"github.com/jonathanagustin/conduit/internal/intelligence/application"
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

// Every reopen is a new pool on the same database, which is what a restarted
// Hub or a second replica is.
func TestPostgresStoreConformsToTheCaseStorePort(t *testing.T) {
	if os.Getenv(pgtest.DataSourceNameEnvironment) == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
	}
	casetest.Run(t, func(t *testing.T) (func() application.CaseStore, bool) {
		dsn := freshDSN(t)
		return func() application.CaseStore {
			store, err := New(context.Background(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store
		}, true
	})
}

// The Service on top of the store: a case added through one Hub process is
// listed by the next one.
func TestAnAddedCaseSurvivesAHubRestart(t *testing.T) {
	dsn, ctx := freshDSN(t), context.Background()
	first, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	before := application.New("", "")
	before.Stored = first
	added, err := before.AddCase(ctx, "operator rejected 2Gi", "sample-app-1", "2Gi", "FAIL_REQUIRED")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	after := application.New("", "")
	after.Stored = second
	cases, err := after.Cases(ctx)
	if err != nil || len(cases) != 4 || cases[3] != added {
		t.Fatalf("cases after a restart = %+v, %v", cases, err)
	}
}

// The database refuses what the port would never write.
func TestSchemaRefusesAnEmptyScenario(t *testing.T) {
	dsn, ctx := freshDSN(t), context.Background()
	store, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.ExecContext(ctx, `INSERT INTO conduit_intelligence_eval_cases
		(case_id, scenario, bound_object, value, expected_outcome, source) VALUES ('x', '', '', '', 'PASS', '')`); err == nil {
		t.Fatal("an empty scenario was stored")
	}
}
