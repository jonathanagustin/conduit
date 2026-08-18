package pgmigrate_test

import (
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

// Two different components racing a database with no ledger table at all.
//
// This is the case the design originally got wrong. The per-component advisory
// lock made concurrent starts of the same component safe and did nothing
// whatever for two different ones: they hash to different keys and do not
// exclude each other, so both ran CREATE TABLE IF NOT EXISTS on the ledger and
// PostgreSQL answered one of them with a duplicate key on
// pg_type_typname_nsp_index. Four components over twenty rounds failed
// thirty-six times. The fix is a fixed lock every caller shares, taken before
// the per-component one.
//
// It is reachable in production: a Hub opens the control-plane repository and
// the reconciliation store, and a fresh database has neither ledger nor tables.
//
// # Why this lives here and takes a database of its own
//
// It was written in the reconciliation store's package, where it reached the
// fresh-database case by dropping conduit_schema_migrations in the shared
// database and putting it back afterwards. That worked within one package and
// was a landmine outside it: `go test` runs packages in parallel, so a test in
// cmd/platform-delivery reading the ledger while this one had it dropped failed
// with `relation "conduit_schema_migrations" does not exist` -- a confusing
// error in a package that had done nothing wrong. Adding a third ledger
// component only widened the window.
//
// A throwaway database removes the hazard rather than documenting it, and the
// test belongs beside the code it exercises rather than beside a store that
// merely happened to have a connection.
func TestDifferentComponentsRacingAFreshDatabaseDoNotCollide(t *testing.T) {
	t.Parallel()
	database := freshDatabase(t, sharedDatabase(t))
	ctx := t.Context()

	components := []string{"race-alpha", "race-beta", "race-gamma", "race-delta"}
	steps := []pgmigrate.Migration{{Name: "0001-probe", Statements: `SELECT 1;`}}

	// Ten rounds. One round of four racers is a coin flip the unlocked version
	// can win; ten in a row it cannot. The locked version passes every round
	// deterministically, so the rounds cost nothing when the code is right.
	for round := range 10 {
		// Safe here in a way it was not before: this database belongs to this
		// test and is dropped with it, so nothing else can be reading the table
		// while it is gone.
		if _, err := database.ExecContext(ctx, `DROP TABLE IF EXISTS conduit_schema_migrations`); err != nil {
			t.Fatal(err)
		}
		failures := make(chan error, len(components))
		start := make(chan struct{})
		var group sync.WaitGroup
		for _, name := range components {
			group.Add(1)
			go func() {
				defer group.Done()
				<-start
				if err := pgmigrate.Apply(ctx, database, name, steps); err != nil {
					failures <- err
				}
			}()
		}
		close(start)
		group.Wait()
		close(failures)
		for err := range failures {
			t.Errorf("round %d: a component racing another for the ledger failed: %v", round, err)
		}
		if t.Failed() {
			return
		}
	}

	// Every racer's migration is recorded exactly once. A run that survived the
	// race but recorded a component twice, or dropped one, would otherwise pass.
	var recorded int
	if err := database.QueryRowContext(ctx,
		`SELECT count(*) FROM conduit_schema_migrations`).Scan(&recorded); err != nil {
		t.Fatal(err)
	}
	if recorded != len(components) {
		t.Errorf("the ledger holds %d rows after the race, want %d", recorded, len(components))
	}
}
