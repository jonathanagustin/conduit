package pgmigrate_test

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

// Pending against a real server. The unit tests beside this one cover what can
// be decided without a database; everything here is about what the database
// actually contains, and the first of them is why Pending has the shape it does.
//
// These run wherever CONDUIT_TEST_POSTGRES_DSN is set, and
// scripts/test-postgres-repository.sh now runs this package for exactly that
// reason. It did not before, and a database-backed test added here would have
// skipped locally for want of a DSN and been reached by nothing in CI -- which
// is the trap #146 removed from that script and would have re-created here.

// The property the dry run exists for, and the one easiest to lose.
//
// "What would a start apply?" is asked most often of a database that has never
// been migrated, and that is precisely the database with no ledger table. An
// implementation that created the ledger in order to read it would answer
// correctly and leave the schema changed -- so asking would not be free, and
// the first thing an operator does before touching production would itself
// touch production.
func TestPendingDoesNotCreateTheLedgerItReads(t *testing.T) {
	shared := sharedDatabase(t)
	fresh := freshDatabase(t, shared)

	pending, err := pgmigrate.Pending(t.Context(), fresh, "pending-probe", list("0001-a", "0002-b"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Errorf("a database that has never been migrated reports %d pending, want 2", len(pending))
	}

	var resolved sql.NullString
	if err := fresh.QueryRowContext(t.Context(),
		`SELECT to_regclass('conduit_schema_migrations')::text`).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	if resolved.Valid {
		t.Fatal("asking what would be applied created the ledger table; a dry run that writes is not one")
	}
}

// Order is what a dry run is read for. Reported in any other order it tells an
// operator the wrong thing about what runs first, which after "what runs at
// all" is the only thing about a migration list that matters.
func TestPendingReportsWhatApplyWouldRunInApplyOrder(t *testing.T) {
	shared := sharedDatabase(t)
	fresh := freshDatabase(t, shared)
	ctx := t.Context()

	full := list("0001-a", "0002-b", "0003-c")
	if err := pgmigrate.Apply(ctx, fresh, "pending-probe", full[:1]); err != nil {
		t.Fatal(err)
	}
	pending, err := pgmigrate.Pending(ctx, fresh, "pending-probe", full)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 || pending[0].Name != "0002-b" || pending[1].Name != "0003-c" {
		t.Fatalf("pending after applying the first migration = %v, want [0002-b 0003-c]", names(pending))
	}
	// The statements come with them: an operator asking what would run wants to
	// read the DDL, not a list of names to go and look up.
	if pending[0].Statements != full[1].Statements {
		t.Errorf("pending migration %s carries no statements", pending[0].Name)
	}
}

// The assertion the restore drill leans on, and the one holding the ledger's
// name in the DDL together with the name Pending resolves. If those two strings
// disagreed, a fully migrated database would report everything pending -- the
// most alarming direction for a dry run to be wrong in, and one no compiler can
// catch because both are string constants.
func TestPendingIsEmptyOnceEverythingIsApplied(t *testing.T) {
	shared := sharedDatabase(t)
	fresh := freshDatabase(t, shared)
	ctx := t.Context()

	full := list("0001-a", "0002-b")
	if err := pgmigrate.Apply(ctx, fresh, "pending-probe", full); err != nil {
		t.Fatal(err)
	}
	pending, err := pgmigrate.Pending(ctx, fresh, "pending-probe", full)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("a fully migrated database reports %v pending", names(pending))
	}
}

// One component's history must not answer for another's. They share a table,
// so a Pending that ignored the component column would report a Hub's second
// schema as already applied because its first one had been -- and the Hub opens
// two.
func TestPendingReadsOneComponentsHistory(t *testing.T) {
	shared := sharedDatabase(t)
	fresh := freshDatabase(t, shared)
	ctx := t.Context()

	full := list("0001-a")
	if err := pgmigrate.Apply(ctx, fresh, "pending-probe-alpha", full); err != nil {
		t.Fatal(err)
	}
	pending, err := pgmigrate.Pending(ctx, fresh, "pending-probe-beta", full)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("beta reports %v pending after alpha applied the same name; it must read its own history", names(pending))
	}
}

// Pending validates before touching anything, for the reason Apply does: a
// duplicated name means the second occurrence never runs, so a dry run that
// reported it as pending would promise work Apply silently skips forever. The
// nil database is how that is asserted without one -- validation first returns
// the error, validation second dereferences nil.
func TestPendingValidatesBeforeTouchingTheDatabase(t *testing.T) {
	t.Parallel()
	if _, err := pgmigrate.Pending(t.Context(), nil, "reconciliation", list("0001-a", "0001-a")); err == nil {
		t.Fatal("Pending accepted a duplicated migration name")
	}
}

func sharedDatabase(t *testing.T) *sql.DB {
	t.Helper()
	if os.Getenv(pgtest.DataSourceNameEnvironment) == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is required for the PostgreSQL migration tests")
	}
	opened, err := sql.Open("pgx", os.Getenv(pgtest.DataSourceNameEnvironment))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	if err := opened.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return opened
}

func freshDatabase(t *testing.T, shared *sql.DB) *sql.DB {
	t.Helper()
	dsn, drop, err := pgtest.Fresh(t.Context(), shared, os.Getenv(pgtest.DataSourceNameEnvironment), t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	opened, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { opened.Close() })
	if err := opened.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	return opened
}

func names(migrations []pgmigrate.Migration) []string {
	out := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		out = append(out, migration.Name)
	}
	return out
}
