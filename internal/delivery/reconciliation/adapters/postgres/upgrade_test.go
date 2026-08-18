package postgres

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Migrations now run once per database and are recorded, so most of what these
// tests used to guard is enforced by the ledger rather than by how the DDL
// happens to be written. What is left is the part the ledger cannot cover:
// adoption.
//
// Every database already in service contains the effects of both migrations and
// has no ledger row for either. The first start after this change therefore runs
// both against populated tables. Until every deployment has started once, these
// two migrations must remain exactly as safe as they were when they ran on every
// start -- which is what the two source-reading tests below hold down.
//
// That requirement expires. A migration added after adoption runs against a
// database whose ledger is accurate and may be as destructive as it needs to be;
// the tests are scoped to the adoption set by name so they do not quietly become
// a rule nobody chose.

// adoptionMigrations are the migrations that predate the ledger.
//
// Named explicitly rather than derived as "all of them", because the whole point
// is that this set stops growing. A new migration that is added to this list has
// to be added deliberately, and there is no reason to.
var adoptionMigrations = map[string]struct{}{
	"0001-reconciliation-core":        {},
	"0002-outbox-desired-coordinates": {},
}

// destructiveDDL is what would make re-applying a migration lose data.
//
// ALTER is deliberately NOT on this list wholesale, and the first draft of this
// test had it there and failed. Migration 0002 is two ADD COLUMN IF NOT EXISTS,
// a backfill UPDATE guarded by IS NULL, and two ALTER COLUMN SET NOT NULL, and
// every one of those is idempotent and forward-safe. Banning ALTER would have
// banned the one migration idiom this repository actually has. What is banned is
// the subset that destroys or rewrites: dropping, renaming, and retyping.
//
// Each is spelled separately rather than as one alternation so a failure names
// the specific thing that was written.
var destructiveDDL = map[string]*regexp.Regexp{
	"DROP TABLE":        regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`),
	"DROP INDEX":        regexp.MustCompile(`(?i)\bDROP\s+INDEX\b`),
	"DROP COLUMN":       regexp.MustCompile(`(?i)\bDROP\s+COLUMN\b`),
	"TRUNCATE":          regexp.MustCompile(`(?i)\bTRUNCATE\b`),
	"DELETE FROM":       regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`),
	"CREATE OR REPLACE": regexp.MustCompile(`(?i)\bCREATE\s+OR\s+REPLACE\b`),
	"RENAME":            regexp.MustCompile(`(?i)\bRENAME\b`),
	// Both spellings. PostgreSQL accepts ALTER COLUMN c TYPE and
	// ALTER COLUMN c SET DATA TYPE for the same operation, and matching only the
	// first left a destructive retype a keyword away from passing this gate.
	"column retype": regexp.MustCompile(`(?i)\bALTER\s+COLUMN\s+\w+\s+(?:SET\s+DATA\s+)?TYPE\b`),
}

// The guard has to recognise the thing it forbids, and nothing checked that it
// did. Every pattern above matches zero characters of the current DDL -- which
// is the point, and also means a pattern that had stopped matching anything at
// all would look identical. It happened: the retype pattern knew
// `ALTER COLUMN c TYPE` and not PostgreSQL's equally valid
// `ALTER COLUMN c SET DATA TYPE`, so a destructive retype was one keyword away
// from passing, and changing the pattern back was a mutation nothing killed.
func TestTheDestructiveDDLGuardRecognisesWhatItForbids(t *testing.T) {
	t.Parallel()
	forbidden := map[string][]string{
		"DROP TABLE":        {"DROP TABLE conduit_reconciliation_work;", "drop table if exists t;"},
		"DROP INDEX":        {"DROP INDEX conduit_reconciliation_work_available;"},
		"DROP COLUMN":       {"ALTER TABLE t DROP COLUMN last_error;"},
		"TRUNCATE":          {"TRUNCATE conduit_reconciliation_audit;"},
		"DELETE FROM":       {"DELETE FROM conduit_reconciliation_work WHERE exhausted;"},
		"CREATE OR REPLACE": {"CREATE OR REPLACE VIEW v AS SELECT 1;"},
		"RENAME":            {"ALTER TABLE t RENAME TO u;", "ALTER TABLE t RENAME COLUMN a TO b;"},
		// Both spellings PostgreSQL accepts for the same operation.
		"column retype": {"ALTER TABLE t ALTER COLUMN c TYPE bigint;", "ALTER TABLE t ALTER COLUMN c SET DATA TYPE bigint;"},
	}
	if len(forbidden) != len(destructiveDDL) {
		t.Fatalf("%d patterns are guarded and %d have an example; every one needs one or it is unchecked",
			len(destructiveDDL), len(forbidden))
	}
	for name, examples := range forbidden {
		pattern, guarded := destructiveDDL[name]
		if !guarded {
			t.Errorf("no pattern named %q; the guard and this test have drifted apart", name)
			continue
		}
		for _, example := range examples {
			if !pattern.MatchString(example) {
				t.Errorf("the %s guard does not match %q", name, example)
			}
		}
	}
	// And it must not fire on the migrations that are meant to pass, which is
	// the other way a guard goes wrong: one broad enough to match everything
	// would pass this test and fail every future migration.
	safe := "ALTER TABLE t ADD COLUMN IF NOT EXISTS c text;\nUPDATE t SET c = 'x' WHERE c IS NULL;\n" +
		"ALTER TABLE t ALTER COLUMN c SET NOT NULL;\nCREATE INDEX IF NOT EXISTS i ON t (c);"
	for name, pattern := range destructiveDDL {
		if pattern.MatchString(safe) {
			t.Errorf("the %s guard fires on the additive idiom this repository uses", name)
		}
	}
}

// adoptionSet returns the pre-ledger migrations, and fails if one has gone
// missing. A test that silently checks nothing is worse than no test, and
// renaming a migration is exactly the mistake that would empty this.
func adoptionSet(t *testing.T) []pgmigrate.Migration {
	t.Helper()
	var set []pgmigrate.Migration
	for _, migration := range migrations {
		if _, adoption := adoptionMigrations[migration.Name]; adoption {
			set = append(set, migration)
		}
	}
	if len(set) != len(adoptionMigrations) {
		t.Fatalf(
			"expected %d adoption migrations, found %d. A migration that predates the ledger was renamed "+
				"or removed -- every database in service is keyed on those names and would run it again",
			len(adoptionMigrations), len(set),
		)
	}
	return set
}

// The adoption migrations run once more against every populated database in
// service. This reads the DDL and says so, needing no database -- which is the
// point: a contributor who adds a DROP finds out from `go test ./...` rather
// than from a gate that only runs where Docker does.
func TestAdoptionMigrationsAreSafeToReapplyByConstruction(t *testing.T) {
	t.Parallel()
	for _, migration := range adoptionSet(t) {
		for name, pattern := range destructiveDDL {
			if pattern.MatchString(migration.Statements) {
				t.Errorf(
					"migration %s contains %s. It predates the ledger, so every database already in "+
						"service will run it once against populated tables; a statement of this kind is a "+
						"data-loss event on the next start rather than a migration",
					migration.Name, name,
				)
			}
		}
	}
}

// Idempotence is what makes the above safe, and for CREATE it is carried
// entirely by IF NOT EXISTS. A CREATE without it fails on adoption rather than
// corrupting anything, which is a smaller problem than data loss but still one
// that takes the Hub down on upgrade rather than at review time.
func TestEveryCreateInAnAdoptionMigrationIsConditional(t *testing.T) {
	t.Parallel()
	creates := regexp.MustCompile(`(?i)\bCREATE\s+(?:UNIQUE\s+)?(TABLE|INDEX)\b(\s+IF\s+NOT\s+EXISTS\b)?`)
	found := 0
	for _, migration := range adoptionSet(t) {
		for _, match := range creates.FindAllStringSubmatch(migration.Statements, -1) {
			found++
			if strings.TrimSpace(match[2]) == "" {
				t.Errorf(
					"migration %s has a CREATE %s without IF NOT EXISTS; adoption will fail on it",
					migration.Name, strings.ToUpper(match[1]),
				)
			}
		}
	}
	// A pattern that matched nothing would pass the loop above silently, and
	// then keep passing after somebody rewrote the DDL into a shape it does not
	// recognise.
	if found == 0 {
		t.Fatal("no CREATE statements matched; the pattern no longer fits the DDL it is guarding")
	}
}

// The list this store applies has to satisfy the runner's own rules, and it has
// to do so without a database. Ordering and uniqueness are the two mistakes a
// migration list invites -- a duplicated name silently skips the second one --
// and neither shows up until a deployment runs.
func TestTheMigrationListIsWellFormed(t *testing.T) {
	t.Parallel()
	if err := pgmigrate.Validate(component, migrations); err != nil {
		t.Fatal(err)
	}
	// Position is the order, so the names are expected to sort with it. This is
	// not required by the runner -- it applies slice order regardless -- but a
	// list whose names no longer sort is one where a reader has to check the
	// slice to know what runs first, which is how an out-of-order migration gets
	// reviewed as fine.
	for index := 1; index < len(migrations); index++ {
		if migrations[index-1].Name >= migrations[index].Name {
			t.Errorf(
				"migration %q is positioned after %q but does not sort after it; renumber rather than "+
					"relying on slice position alone to convey the order",
				migrations[index].Name, migrations[index-1].Name,
			)
		}
	}
}

// The end-to-end half, against a real server: data written before a restart must
// still be there afterwards, and the second start must not re-run anything.
//
// A fresh Store is a restarted Hub or a newly scheduled replica: a new pool, a
// new call to Apply. That is the case an earlier commit removed on the theory
// that it deadlocked CI; it never did -- the run that supposedly hung reported
// TRUNK_STATE: success and failed publishing its commit status.
func TestPostgresRestartPreservesExistingRowsAndReappliesNothing(t *testing.T) {
	ctx := t.Context()
	first, _ := newStorePair(t, ctx)
	resetStore(t, ctx, first)

	key := domain.Key{Environment: "prod", Installation: "checkout"}
	recordedAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	if err := first.UpsertDesired(ctx, testDesired(key, "gen-1", "target-1", recordedAt)); err != nil {
		t.Fatal(err)
	}
	before := appliedAt(t, ctx, first)

	second := reopenStore(t, ctx)

	snapshot, found, err := second.CurrentDesired(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("restarting lost desired state written before it")
	}
	if snapshot.Generation != "gen-1" {
		t.Errorf("generation survived as %q, want gen-1", snapshot.Generation)
	}

	// The ledger's whole purpose. If the timestamps moved, the migrations ran
	// again -- which today is merely wasted work, and on the first migration
	// that is not idempotent is the corruption this exists to prevent.
	after := appliedAt(t, ctx, second)
	if len(after) != len(migrations) {
		t.Fatalf("ledger holds %d migrations, want %d: %v", len(after), len(migrations), after)
	}
	for name, when := range before {
		if !after[name].Equal(when) {
			t.Errorf("migration %s was applied again on restart (%v then %v)", name, when, after[name])
		}
	}

	// And the store still works afterwards, which is the half a row count would
	// miss: a restart that preserved rows but broke writes is no better.
	later := recordedAt.Add(time.Minute)
	if err := second.UpsertDesired(ctx, testDesired(key, "gen-2", "target-2", later)); err != nil {
		t.Fatalf("the store could not write after restarting: %v", err)
	}
	snapshot, found, err = second.CurrentDesired(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !found || snapshot.Generation != "gen-2" {
		t.Errorf("post-restart write did not take: found=%v generation=%q", found, snapshot.Generation)
	}
}

// Adoption in miniature: a database that already has every object and no ledger
// is what every deployment looks like on the first start after this change.
//
// The tables are left in place and only the ledger rows are removed, so Apply
// runs both migrations against populated tables exactly as it will in
// production. Nothing else in this suite exercises that path -- the other tests
// start from a ledger that is already correct.
func TestAdoptingTheLedgerOnAPopulatedDatabaseIsANoOp(t *testing.T) {
	ctx := t.Context()
	store, _ := newStorePair(t, ctx)
	resetStore(t, ctx, store)

	key := domain.Key{Environment: "prod", Installation: "orders"}
	recordedAt := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	if err := store.UpsertDesired(ctx, testDesired(key, "gen-1", "target-1", recordedAt)); err != nil {
		t.Fatal(err)
	}
	// The outbox has to have a row in it, and an earlier version of this test
	// forgot: it populated only the desired table, so migration 0002 -- the
	// backfill and the two SET NOT NULL, the only genuinely interesting
	// statements in the adoption set -- ran against zero rows and would have
	// passed however wrong it was.
	if err := store.UpsertObservation(ctx, testObservation(key, "gen-0", "target-0", recordedAt)); err != nil {
		t.Fatal(err)
	}
	lease, found, err := store.LeaseWork(ctx, recordedAt, time.Minute)
	if err != nil || !found {
		t.Fatalf("no work to lease: found=%v err=%v", found, err)
	}
	input, err := store.LoadInput(ctx, lease.Key)
	if err != nil {
		t.Fatal(err)
	}
	decision, proposal, err := domain.Decide(input, lease.Attempt, recordedAt, 10*time.Minute)
	if err != nil || proposal == nil {
		t.Fatalf("decision=%#v proposal=%#v err=%v", decision, proposal, err)
	}
	if err := store.CommitDecision(ctx, lease, decision, proposal); err != nil {
		t.Fatal(err)
	}
	outboxBefore := outboxCoordinates(t, ctx, store)
	if len(outboxBefore) == 0 {
		t.Fatal("nothing reached the proposal outbox; migration 0002 would be exercised against no rows")
	}

	if _, err := store.database.ExecContext(ctx,
		`DELETE FROM conduit_schema_migrations WHERE component = $1`, component); err != nil {
		t.Fatal(err)
	}

	if err := pgmigrate.Apply(ctx, store.database, component, migrations); err != nil {
		t.Fatalf("adoption failed against a populated database: %v", err)
	}

	snapshot, found, err := store.CurrentDesired(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if !found || snapshot.Generation != "gen-1" {
		t.Errorf("adoption lost rows: found=%v generation=%q", found, snapshot.Generation)
	}
	// The backfill's guard is `WHERE desired_generation IS NULL`, so a re-run
	// against populated NOT NULL columns must match nothing and change nothing.
	// A backfill that rewrote what was already there would show up here.
	outboxAfter := outboxCoordinates(t, ctx, store)
	if len(outboxAfter) != len(outboxBefore) {
		t.Fatalf("adoption changed the outbox row count: %d then %d", len(outboxBefore), len(outboxAfter))
	}
	for id, before := range outboxBefore {
		if outboxAfter[id] != before {
			t.Errorf("adoption rewrote outbox %s: %q then %q", id, before, outboxAfter[id])
		}
	}
	if applied := appliedAt(t, ctx, store); len(applied) != len(migrations) {
		t.Errorf("adoption recorded %d migrations, want %d", len(applied), len(migrations))
	}
}

// outboxCoordinates reads the two columns migration 0002 adds and backfills.
func outboxCoordinates(t *testing.T, ctx context.Context, store *Store) map[string]string {
	t.Helper()
	rows, err := store.database.QueryContext(ctx,
		`SELECT proposal_id, desired_generation || '/' || target_digest FROM conduit_reconciliation_proposal_outbox`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	coordinates := map[string]string{}
	for rows.Next() {
		var id, value string
		if err := rows.Scan(&id, &value); err != nil {
			t.Fatal(err)
		}
		coordinates[id] = value
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return coordinates
}

// The advisory lock is the claim the whole design rests on, and until this
// existed nothing tested it. A mutation replacing the lock with a no-op "died",
// but for the wrong reason -- the replacement could not be type-inferred by the
// driver, so it failed on an error rather than on the absence of mutual
// exclusion. A kill for the wrong reason is not a kill.
//
// This races the real thing: eight goroutines call Apply against a component
// with no ledger rows, all at once. With the lock, one applies and the rest wait
// and then find the work done. Without it, several reach the ledger insert for
// the same row and all but one fail on the primary key.
//
// Not perfectly deterministic -- an unlocked run could serialise by luck -- but
// the asymmetry is the right way round: the locked version always passes, and
// eight racers make the unlocked version fail nearly always.
func TestConcurrentStartsDoNotBothApplyTheSameMigration(t *testing.T) {
	ctx := t.Context()
	store, _ := newStorePair(t, ctx)

	// A component of its own so this cannot disturb, or be disturbed by, the
	// migrations the store itself has already applied.
	const racing = "pgmigrate-concurrency-test"
	steps := []pgmigrate.Migration{{
		Name:       "0001-probe",
		Statements: `CREATE TABLE IF NOT EXISTS conduit_pgmigrate_probe (id integer PRIMARY KEY);`,
	}}
	t.Cleanup(func() {
		_, _ = store.database.ExecContext(context.WithoutCancel(ctx),
			`DROP TABLE IF EXISTS conduit_pgmigrate_probe`)
		_, _ = store.database.ExecContext(context.WithoutCancel(ctx),
			`DELETE FROM conduit_schema_migrations WHERE component = $1`, racing)
	})
	if _, err := store.database.ExecContext(ctx,
		`DELETE FROM conduit_schema_migrations WHERE component = $1`, racing); err != nil {
		t.Fatal(err)
	}

	// Ten rounds, not one. A single round of eight racers let the no-lock
	// mutation survive about one run in five, which for a regression that only
	// has to slip through once is not a guard at all. Rounds compound: the
	// locked version passes every round deterministically, and the unlocked one
	// has to win ten coin flips in a row.
	const racers, rounds = 8, 10
	for round := range rounds {
		if _, err := store.database.ExecContext(ctx,
			`DELETE FROM conduit_schema_migrations WHERE component = $1`, racing); err != nil {
			t.Fatal(err)
		}
		if _, err := store.database.ExecContext(ctx, `DROP TABLE IF EXISTS conduit_pgmigrate_probe`); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		failures := make(chan error, racers)
		var group sync.WaitGroup
		for range racers {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				if err := pgmigrate.Apply(ctx, store.database, racing, steps); err != nil {
					failures <- err
				}
			}()
		}
		close(start)
		group.Wait()
		close(failures)
		for err := range failures {
			t.Errorf("round %d: a concurrent start failed to migrate: %v", round, err)
		}

		var recorded int
		if err := store.database.QueryRowContext(ctx,
			`SELECT count(*) FROM conduit_schema_migrations WHERE component = $1`, racing).
			Scan(&recorded); err != nil {
			t.Fatal(err)
		}
		if recorded != 1 {
			t.Errorf("round %d: %d starts recorded %d ledger rows, want 1", round, racers, recorded)
		}
		if t.Failed() {
			return
		}
	}
}

// appliedAt reads this component's ledger rows.
func appliedAt(t *testing.T, ctx context.Context, store *Store) map[string]time.Time {
	t.Helper()
	rows, err := store.database.QueryContext(ctx,
		`SELECT name, applied_at FROM conduit_schema_migrations WHERE component = $1`, component)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	applied := map[string]time.Time{}
	for rows.Next() {
		var name string
		var when time.Time
		if err := rows.Scan(&name, &when); err != nil {
			t.Fatal(err)
		}
		applied[name] = when
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return applied
}

func reopenStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	_, reopened := newStorePair(t, ctx)
	return reopened
}
