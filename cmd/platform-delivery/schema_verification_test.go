package main

// The schema-drift check against the Hub's own persistence components.
//
// internal/pgmigrate's tests break a probe schema on purpose and prove the
// verifier names what changed. What they cannot prove is that the Hub's real
// DDL round-trips: that every statement in the migration sets can be replayed
// into a shadow schema and produce, object for object, what Apply produced in
// the live one. A single ordering assumption, a `public.` somewhere, or one
// migration that is not replayable would make the operator's command report
// differences on a database nobody has touched -- and a check that reports drift
// on a healthy database is worse than no check, because it is switched off.
//
// It lives here for the reason the restore drill does: this is the only package
// that opens every persistence component the Hub opens against one DSN, and
// Verify needs all of them at once.

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

// hubDatabase is a database of its own with every Hub component migrated into
// it. Its own, because the drift planted below is a DROP COLUMN on a real table
// and the rest of the suite shares one database.
func hubDatabase(t *testing.T) (handle *sql.DB, dataSourceName string) {
	t.Helper()
	shared := os.Getenv(pgtest.DataSourceNameEnvironment)
	if shared == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is required for the PostgreSQL integration gate")
	}
	sharedHandle, err := sql.Open("pgx", shared)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sharedHandle.Close() })
	fresh, drop, err := pgtest.Fresh(t.Context(), sharedHandle, shared, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	handle, err = sql.Open("pgx", fresh)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { handle.Close() })
	for _, set := range hubMigrationSets() {
		if err := pgmigrate.Apply(t.Context(), handle, set.Component, set.Migrations); err != nil {
			t.Fatalf("applying %s: %v", set.Component, err)
		}
	}
	return handle, fresh
}

// environment writes the DSN where the command reads it, exactly as a
// deployment mounts it.
func environment(t *testing.T, dataSourceName string) func(string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsn")
	if err := os.WriteFile(path, []byte(dataSourceName), 0o600); err != nil {
		t.Fatal(err)
	}
	return func(name string) string {
		if name == "POSTGRES_DSN_FILE" {
			return path
		}
		return ""
	}
}

// The control, and the assertion the planted-drift test below leans on.
func TestPostgresSchemaVerificationIsSilentOnADatabaseTheHubMigrated(t *testing.T) {
	_, dataSourceName := hubDatabase(t)
	var report strings.Builder
	if err := verifySchema(t.Context(), environment(t, dataSourceName), &report); err != nil {
		t.Fatalf("a database this binary's own migrations built reported faults: %v\n%s", err, report.String())
	}
	if !strings.Contains(report.String(), "matches the migrations") {
		t.Fatalf("the command reported something on an untouched database:\n%s", report.String())
	}
}

// The gap, on a real table: a ledger row asserts 0001-reconciliation-core ran,
// and cannot detect that somebody dropped a column out of what it built.
func TestPostgresSchemaVerificationFindsAColumnDroppedFromTheProposalOutbox(t *testing.T) {
	database, dataSourceName := hubDatabase(t)
	if _, err := database.ExecContext(t.Context(),
		`ALTER TABLE conduit_reconciliation_proposal_outbox DROP COLUMN attempt`); err != nil {
		t.Fatal(err)
	}
	// The ledger still says every migration ran, which is the whole point.
	pending, err := pgmigrate.Pending(t.Context(), database,
		hubMigrationSets()[1].Component, hubMigrationSets()[1].Migrations)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("the dropped column made a migration pending, so the ledger caught it and this test proves nothing")
	}

	var report strings.Builder
	err = verifySchema(t.Context(), environment(t, dataSourceName), &report)
	if err == nil {
		t.Fatalf("a column dropped by hand did not fail the check:\n%s", report.String())
	}
	for _, expected := range []string{
		"fault:", "conduit_reconciliation_proposal_outbox.attempt", "is missing", "reconciliation",
	} {
		if !strings.Contains(report.String(), expected) {
			t.Errorf("the report does not contain %q:\n%s", expected, report.String())
		}
	}
	t.Logf("exit error: %v\n%s", err, report.String())
}

// A component the Hub applies only when configured -- the Gitless resolution log
// under GITLESS_RESOLUTION_LOG_KIND=postgres -- must not make an unconfigured
// Hub's database look damaged. This is the false positive that would switch the
// check off in every deployment that does not run Gitless, which is most of them.
func TestPostgresSchemaVerificationAcceptsADatabaseWithoutTheOptionalComponents(t *testing.T) {
	shared := os.Getenv(pgtest.DataSourceNameEnvironment)
	if shared == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is required for the PostgreSQL integration gate")
	}
	sharedHandle, err := sql.Open("pgx", shared)
	if err != nil {
		t.Fatal(err)
	}
	defer sharedHandle.Close()
	fresh, drop, err := pgtest.Fresh(t.Context(), sharedHandle, shared, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer drop()
	handle, err := sql.Open("pgx", fresh)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	for _, set := range hubMigrationSets() {
		if set.Component == "gitless-resolution-log" {
			continue
		}
		if err := pgmigrate.Apply(t.Context(), handle, set.Component, set.Migrations); err != nil {
			t.Fatal(err)
		}
	}
	var report strings.Builder
	if err := verifySchema(t.Context(), environment(t, fresh), &report); err != nil {
		t.Fatalf("a Hub that does not run Gitless was reported as damaged: %v\n%s", err, report.String())
	}
	if !strings.Contains(report.String(), "gitless-resolution-log has never migrated this database") {
		t.Fatalf("the report does not say why the Gitless tables are absent:\n%s", report.String())
	}
	t.Logf("%s", report.String())
}
