package pgmigrate_test

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Verify against a real server, with real drift planted in it.
//
// Every test here builds a database with the probe migrations below, then makes
// exactly one hand alteration and asserts Verify names it. The alteration is the
// point: a verifier can only be trusted to the extent that somebody has broken a
// database on purpose and watched it complain, and the failure modes it has to
// catch are the ones an operator produces at a psql prompt during an incident --
// a column dropped, a type widened, an index removed because it looked unused.
//
// The probe schema is not the Hub's, deliberately. It carries every shape the
// four real migration sets use -- a composite primary key, a partial index, an
// inline CHECK, a UNIQUE, constant defaults, an identity column, and the
// three-step add-column idiom of docs/operations/schema-migrations.md -- while
// leaving this package free of an import of the adapters that depend on it.
// cmd/platform-delivery/schema_verification_test.go runs the same verifier
// against the Hub's own four components, where those imports already exist.

func probeCore() []pgmigrate.Migration {
	return []pgmigrate.Migration{
		{
			Name: "0001-probe-core",
			Statements: `
CREATE TABLE verify_probe_work (
    environment text NOT NULL,
    installation text NOT NULL,
    available_at timestamptz NOT NULL,
    lease_token text,
    attempt integer NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    exhausted boolean NOT NULL DEFAULT false,
    PRIMARY KEY (environment, installation)
);
CREATE INDEX verify_probe_work_available
    ON verify_probe_work (available_at, environment, installation)
    WHERE exhausted = false;
CREATE TABLE verify_probe_history (
    sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    recorded_at timestamptz NOT NULL,
    environment text NOT NULL,
    document bytea NOT NULL
);`,
		},
		{
			// The three-step idiom, so the shadow build is exercised on a
			// migration that alters what an earlier one created rather than only
			// on migrations that create.
			Name: "0002-probe-last-error",
			Statements: `
ALTER TABLE verify_probe_work ADD COLUMN IF NOT EXISTS last_error text;
UPDATE verify_probe_work SET last_error = '' WHERE last_error IS NULL;
ALTER TABLE verify_probe_work ALTER COLUMN last_error SET NOT NULL;`,
		},
	}
}

func probeBlobs() []pgmigrate.Migration {
	return []pgmigrate.Migration{{
		Name: "0001-probe-blobs",
		Statements: `
CREATE TABLE verify_probe_blobs (
    digest text PRIMARY KEY,
    descriptor jsonb NOT NULL,
    contents bytea NOT NULL,
    CHECK (octet_length(contents) >= 0)
);`,
	}}
}

const (
	coreComponent  = "verify-probe-core"
	blobsComponent = "verify-probe-blobs"
)

func probeSets() []pgmigrate.Set {
	return []pgmigrate.Set{
		{Component: coreComponent, Migrations: probeCore()},
		{Component: blobsComponent, Migrations: probeBlobs()},
	}
}

// migrated builds a database from the probe sets and returns a handle on it.
func migrated(t *testing.T) *sql.DB {
	t.Helper()
	database := freshDatabase(t, sharedDatabase(t))
	for _, set := range probeSets() {
		if err := pgmigrate.Apply(t.Context(), database, set.Component, set.Migrations); err != nil {
			t.Fatal(err)
		}
	}
	return database
}

func verified(t *testing.T, database *sql.DB) pgmigrate.Report {
	t.Helper()
	report, err := pgmigrate.Verify(t.Context(), database, probeSets()...)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func alter(t *testing.T, database *sql.DB, statement string) {
	t.Helper()
	if _, err := database.ExecContext(t.Context(), statement); err != nil {
		t.Fatal(err)
	}
}

// requireFault asserts one fault names the object and reads the way an operator
// needs it to. Matching on the rendered line rather than on struct fields is
// deliberate: the line is the deliverable -- a report that knows something and
// does not say it is the failure this package exists to fix.
func requireFault(t *testing.T, report pgmigrate.Report, fragments ...string) pgmigrate.Difference {
	t.Helper()
	for _, fault := range report.Faults() {
		matched := true
		for _, fragment := range fragments {
			if !strings.Contains(fault.String(), fragment) {
				matched = false
				break
			}
		}
		if matched {
			t.Logf("reported: %s", fault)
			return fault
		}
	}
	t.Fatalf("no fault mentions all of %v. The report was:\n%s", fragments, report)
	return pgmigrate.Difference{}
}

// The negative control, and the assertion every mutation below leans on: an
// untouched database produces nothing at all, not even an advisory. Without
// this, a verifier that reported a difference on every run would pass every
// mutation test in this file.
func TestVerifyIsSilentOnADatabaseTheMigrationsBuilt(t *testing.T) {
	report := verified(t, migrated(t))
	if !report.Sound() || len(report.Differences) != 0 {
		t.Fatalf("a database built by these migrations is not clean:\n%s", report)
	}
}

// The gap this package was written for, in the words
// docs/operations/PRODUCTION-READINESS.md used: a ledger row asserts a migration
// ran and does not detect a column dropped by hand.
func TestVerifyFindsAColumnDroppedByHand(t *testing.T) {
	database := migrated(t)
	alter(t, database, `ALTER TABLE verify_probe_work DROP COLUMN attempt`)

	report := verified(t, database)
	requireFault(t, report, "column verify_probe_work.attempt", coreComponent, "is missing", "integer NOT NULL DEFAULT 0")
	if report.Sound() {
		t.Fatal("a dropped column left the report sound")
	}
}

// A column nothing built is a fault rather than an advisory, and the split is
// the reason: an operator adding an index changes no shape the application
// depends on, while a column on a table these migrations own arrived from
// somewhere no migration records -- so the next migration to touch that table
// is being written against a schema its author cannot see.
func TestVerifyFindsAColumnNoMigrationBuilt(t *testing.T) {
	database := migrated(t)
	alter(t, database, `ALTER TABLE verify_probe_work ADD COLUMN operator_note text`)

	report := verified(t, database)
	requireFault(t, report, "column verify_probe_work.operator_note", "in no migration")
}

func TestVerifyFindsARetypedColumn(t *testing.T) {
	database := migrated(t)
	alter(t, database, `ALTER TABLE verify_probe_work ALTER COLUMN attempt TYPE bigint`)

	report := verified(t, database)
	fault := requireFault(t, report, "column verify_probe_work.attempt", "differs")
	// Both definitions, because "differs" without them sends an operator to
	// psql to find out how.
	if !strings.Contains(fault.Expected, "integer") || !strings.Contains(fault.Live, "bigint") {
		t.Errorf("the fault does not carry both types: %s", fault)
	}
}

// A widened column that no longer rejects what it used to. The type is
// unchanged, so nothing but the attribute comparison can catch this.
func TestVerifyFindsADroppedNotNull(t *testing.T) {
	database := migrated(t)
	alter(t, database, `ALTER TABLE verify_probe_work ALTER COLUMN last_error DROP NOT NULL`)

	report := verified(t, database)
	fault := requireFault(t, report, "column verify_probe_work.last_error", "differs")
	if !strings.Contains(fault.Expected, "NOT NULL") || strings.Contains(fault.Live, "NOT NULL") {
		t.Errorf("the fault does not say the constraint is gone: %s", fault)
	}
}

// Dropping an index the application's queries were planned against does not
// break a query, it makes one scan a table -- which is why it is a fault and
// why it is the drift nobody notices until load arrives.
func TestVerifyFindsADroppedIndex(t *testing.T) {
	database := migrated(t)
	alter(t, database, `DROP INDEX verify_probe_work_available`)

	report := verified(t, database)
	requireFault(t, report, "index verify_probe_work.verify_probe_work_available", "is missing", "WHERE")
}

func TestVerifyFindsADroppedConstraint(t *testing.T) {
	database := migrated(t)
	alter(t, database, `ALTER TABLE verify_probe_work DROP CONSTRAINT verify_probe_work_attempt_check`)

	report := verified(t, database)
	requireFault(t, report, "constraint verify_probe_work.verify_probe_work_attempt_check", "is missing", "attempt >= 0")
}

// A primary key is read from pg_constraint rather than from pg_index, so that
// dropping one is reported once and as the thing it is.
func TestVerifyFindsADroppedPrimaryKey(t *testing.T) {
	database := migrated(t)
	alter(t, database, `ALTER TABLE verify_probe_blobs DROP CONSTRAINT verify_probe_blobs_pkey`)

	report := verified(t, database)
	fault := requireFault(t, report, "constraint verify_probe_blobs.verify_probe_blobs_pkey", "PRIMARY KEY")
	if fault.Component != blobsComponent {
		t.Errorf("the fault is attributed to %q rather than to the component whose DDL created the table", fault.Component)
	}
}

// The ledger is a table in this database like any other, and a verifier that
// exempted it would leave the one table the whole mechanism trusts unchecked.
func TestVerifyFindsDriftInTheLedgerItself(t *testing.T) {
	database := migrated(t)
	alter(t, database, `ALTER TABLE conduit_schema_migrations DROP COLUMN applied_at`)

	report := verified(t, database)
	fault := requireFault(t, report, "column conduit_schema_migrations.applied_at", "is missing")
	// Attributed to nobody. Every component shares this table, so naming one of
	// them as its owner would be a claim the shadow build cannot support.
	if fault.Component != "" {
		t.Errorf("the ledger's own drift is attributed to component %q", fault.Component)
	}
}

// A table only the database has. It is an advisory rather than a fault, and the
// reason is stated where it can be argued with: nothing in the catalogue records
// which component created which table, so a caller that enumerated three of the
// four components sharing a database would see the fourth's tables here. Naming
// it is what the operator needs; deciding for them is what this cannot do.
func TestVerifyReportsATableOnlyTheDatabaseHas(t *testing.T) {
	database := migrated(t)
	alter(t, database, `CREATE TABLE verify_probe_stowaway (id text PRIMARY KEY)`)

	report := verified(t, database)
	if !report.Sound() {
		t.Errorf("a table nothing built was reported as a fault, which would refuse a database shared with "+
			"a component this caller did not enumerate:\n%s", report)
	}
	found := false
	for _, advisory := range report.Advisories() {
		if strings.Contains(advisory.String(), "table verify_probe_stowaway") {
			t.Logf("reported: %s", advisory)
			found = true
		}
	}
	if !found {
		t.Fatalf("a table only the database has was not reported at all:\n%s", report)
	}
}

// The benign control the severity split exists for. An operator who adds an
// index for their own query load has not damaged anything, and a check that
// called that a fault would train its readers to ignore it.
func TestVerifyTreatsAnOperatorsExtraIndexAsAnAdvisory(t *testing.T) {
	database := migrated(t)
	alter(t, database, `CREATE INDEX verify_probe_work_by_environment ON verify_probe_work (environment)`)

	report := verified(t, database)
	if !report.Sound() {
		t.Errorf("an operator's own index was reported as a fault:\n%s", report)
	}
	if len(report.Advisories()) != 1 ||
		!strings.Contains(report.Advisories()[0].String(), "verify_probe_work_by_environment") {
		t.Fatalf("an operator's own index was not reported as an advisory:\n%s", report)
	}
	t.Logf("reported: %s", report.Advisories()[0])
}

// An index that still exists under its name and indexes something else is the
// difference a name-only comparison cannot see, and it is the one an operator
// produces by dropping an index and recreating it from memory.
func TestVerifyFindsARedefinedIndex(t *testing.T) {
	database := migrated(t)
	alter(t, database, `DROP INDEX verify_probe_work_available`)
	alter(t, database, `CREATE INDEX verify_probe_work_available ON verify_probe_work (available_at)`)

	report := verified(t, database)
	fault := requireFault(t, report, "index verify_probe_work.verify_probe_work_available", "differs")
	if !strings.Contains(fault.Expected, "WHERE") || strings.Contains(fault.Live, "WHERE") {
		t.Errorf("the fault does not show that the partial predicate is gone: %s", fault)
	}
}

// Verify answers for the ledger's claim, not for the binary's migration list. A
// database one migration behind is a Pending answer; reporting the objects that
// migration would add as missing would make an ordinary rolling upgrade look
// like drift, and a check that cries wolf during every deploy is not run twice.
func TestVerifyExpectsOnlyWhatTheLedgerRecords(t *testing.T) {
	database := freshDatabase(t, sharedDatabase(t))
	core := probeCore()
	if err := pgmigrate.Apply(t.Context(), database, coreComponent, core[:1]); err != nil {
		t.Fatal(err)
	}
	if err := pgmigrate.Apply(t.Context(), database, blobsComponent, probeBlobs()); err != nil {
		t.Fatal(err)
	}
	report := verified(t, database)
	if !report.Sound() || len(report.Differences) != 0 {
		t.Fatalf("a database that is one migration behind was reported as drifted:\n%s", report)
	}
}

// A component that has never touched this database has no schema here to be
// wrong, and saying so is not the same as saying the database is clean.
func TestVerifySaysWhenAComponentHasNeverMigratedThisDatabase(t *testing.T) {
	database := freshDatabase(t, sharedDatabase(t))
	if err := pgmigrate.Apply(t.Context(), database, coreComponent, probeCore()); err != nil {
		t.Fatal(err)
	}
	report := verified(t, database)
	if !report.Sound() {
		t.Fatalf("an absent component's tables were reported as missing:\n%s", report)
	}
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], blobsComponent) {
		t.Fatalf("the report does not say that %s has never migrated this database:\n%s", blobsComponent, report)
	}
	t.Logf("reported: %s", report.Notes[0])
}

// A ledger row this build has no migration for means the database was migrated
// by a newer one. Everything that migration created is then reported as
// unexpected, which is true and, unexplained, alarming.
func TestVerifySaysWhenTheLedgerRecordsAMigrationThisBuildDoesNotCarry(t *testing.T) {
	database := migrated(t)
	alter(t, database,
		`INSERT INTO conduit_schema_migrations (component, name) VALUES ('`+coreComponent+`', '0003-from-the-future')`)

	report := verified(t, database)
	if len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "0003-from-the-future") {
		t.Fatalf("the report does not name the migration this build does not carry:\n%s", report)
	}
	t.Logf("reported: %s", report.Notes[0])
}

// A database nothing has migrated has made no claim, so there is nothing to
// verify. Reporting every table in it as unexpected would be true and useless;
// this is the database Pending exists for.
func TestVerifyOnADatabaseWithNoLedgerFindsNothingToVerify(t *testing.T) {
	database := freshDatabase(t, sharedDatabase(t))
	report := verified(t, database)
	if len(report.Differences) != 0 || len(report.Notes) != 1 {
		t.Fatalf("a database with no ledger produced a diff:\n%s", report)
	}
	if !strings.Contains(report.Notes[0], "no migration claims to have run") {
		t.Errorf("the note does not say why there was nothing to do: %s", report.Notes[0])
	}
}

// The property that makes this safe to run against production. The shadow build
// executes every recorded migration's DDL, which is the most write-shaped thing
// this repository does on purpose; if any of it escaped the rollback, a check
// intended to detect damage would be causing it.
func TestVerifyChangesNothing(t *testing.T) {
	database := migrated(t)
	before := catalogue(t, database)
	if _, err := pgmigrate.Verify(t.Context(), database, probeSets()...); err != nil {
		t.Fatal(err)
	}
	if after := catalogue(t, database); after != before {
		t.Fatalf("verifying changed the schema.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	// Twice, because a temporary schema created by the first call survives the
	// rollback and a second call has to be able to build in it again.
	report := verified(t, database)
	if !report.Sound() || len(report.Differences) != 0 {
		t.Fatalf("a second verification of an unchanged database reported differences:\n%s", report)
	}
	if after := catalogue(t, database); after != before {
		t.Fatal("the second verification changed the schema")
	}
}

// catalogue is every table, column, index and constraint in the live schema, as
// one string, so a change of any kind fails the comparison above.
func catalogue(t *testing.T, database *sql.DB) string {
	t.Helper()
	var dump string
	if err := database.QueryRowContext(t.Context(), `
SELECT COALESCE(string_agg(line, E'\n' ORDER BY line), '') FROM (
    SELECT class.relname || ' ' || attribute.attname || ' ' ||
           format_type(attribute.atttypid, attribute.atttypmod) || ' ' || attribute.attnotnull AS line
    FROM pg_attribute AS attribute
    JOIN pg_class AS class ON class.oid = attribute.attrelid
    WHERE class.relnamespace = current_schema()::regnamespace AND class.relkind IN ('r', 'p')
      AND attribute.attnum > 0 AND NOT attribute.attisdropped
    UNION ALL
    SELECT class.relname || ' ' || pg_get_constraintdef(constraint_.oid)
    FROM pg_constraint AS constraint_
    JOIN pg_class AS class ON class.oid = constraint_.conrelid
    WHERE class.relnamespace = current_schema()::regnamespace
    UNION ALL
    SELECT pg_get_indexdef(index_.oid)
    FROM pg_index AS catalogued
    JOIN pg_class AS index_ ON index_.oid = catalogued.indexrelid
    WHERE index_.relnamespace = current_schema()::regnamespace
) AS catalogue`).Scan(&dump); err != nil {
		t.Fatal(err)
	}
	return dump
}

// Verify validates before it touches anything, for the reason Apply and Pending
// do. The nil database is how that is asserted without one.
func TestVerifyValidatesBeforeTouchingTheDatabase(t *testing.T) {
	t.Parallel()
	_, err := pgmigrate.Verify(t.Context(), nil, pgmigrate.Set{
		Component: coreComponent, Migrations: list("0001-a", "0001-a"),
	})
	if err == nil {
		t.Fatal("Verify accepted a duplicated migration name")
	}
	if !strings.Contains(err.Error(), "0001-a") {
		t.Errorf("Verify did not report the validation failure: %v", err)
	}
}

// Two sets naming one component would apply that component's DDL to the shadow
// schema twice, and the second run would fail on a CREATE TABLE or -- worse, for
// the idempotent ones -- succeed and hide the mistake.
func TestVerifyRefusesTheSameComponentTwice(t *testing.T) {
	t.Parallel()
	_, err := pgmigrate.Verify(t.Context(), nil,
		pgmigrate.Set{Component: coreComponent, Migrations: list("0001-a")},
		pgmigrate.Set{Component: coreComponent, Migrations: list("0001-a")})
	if err == nil {
		t.Fatal("Verify accepted the same component twice")
	}
}

// An empty call would report every table in the database as unexpected, which
// is the shape a refactor produces when a filter goes wrong -- and it would
// read as a clean bill of health, because unexpected objects are advisories.
func TestVerifyRefusesAnEmptySetList(t *testing.T) {
	t.Parallel()
	if _, err := pgmigrate.Verify(t.Context(), nil); err == nil {
		t.Fatal("Verify accepted no sets at all")
	}
}
