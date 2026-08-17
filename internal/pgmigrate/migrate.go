// Package pgmigrate applies an ordered, forward-only set of schema migrations
// to PostgreSQL exactly once per database, and records what it applied.
//
// Conduit had a migration idiom before this package and it worked: one string
// constant of conditional DDL, re-executed on every start. Every statement was
// idempotent, so re-running it was harmless, and a real migration -- two
// `ADD COLUMN IF NOT EXISTS`, a guarded backfill, then `SET NOT NULL` -- had
// already shipped through it. `docs/operations/PRODUCTION-READINESS.md` records
// what was missing around it, and it is worth being precise, because "we should
// have migrations" is not a finding:
//
//   - Nothing knew which migrations a database had seen, so the only safe
//     migration was an idempotent one. That is a constraint on every future
//     change, forever, enforced by nothing.
//   - The backfill `UPDATE` re-ran on every start, bounded only by its `IS NULL`
//     predicate. On a large outbox that is a full scan per replica per restart.
//   - Ordering was textual position inside one constant. Moving a statement
//     while editing was a schema change nobody would review as one.
//   - Two replicas starting together both ran the DDL. Postgres serialises the
//     locks, so this was survivable rather than broken -- but it is luck, and it
//     is the exact scenario a migration that is not idempotent turns into
//     corruption.
//
// # What this does instead
//
// Migrations are a Go slice. Position in the slice is the order; the name is
// identity and never changes. Each one runs in its own transaction together
// with the row that records it, so a migration and the claim that it ran commit
// or roll back as one -- there is no state in which the ledger is wrong.
//
// Two session advisory locks, in this order. A fixed one shared by every
// caller, held only long enough to create the ledger table; then the caller's
// own, held for the migrations. That ordering is deliberate and the split is
// the correction of a real defect: concurrent `CREATE TABLE IF NOT EXISTS` is
// not actually safe in PostgreSQL -- it can raise a duplicate-key error on its
// catalogue rather than skipping -- so the lock has to be older than the table
// it protects, which advisory locks can be because they need no table. The
// first version used only the per-component lock and claimed that was enough.
// It was not: different components take different keys and exclude each other
// not at all, so a fresh database raced by two owners failed exactly as
// described. See createLedger.
//
// # Forward-only
//
// There is no down path and that is a decision, not an omission. A down
// migration is a data-loss instruction that runs, by definition, during an
// incident, having been tested less than the up path it reverses. Rolling
// forward with a new migration is slower to write and does not destroy the
// evidence of what went wrong.
package pgmigrate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"
)

// Migration is one forward-only step.
//
// Statements may contain several statements separated by semicolons; they are
// sent as one command string and therefore share the transaction.
type Migration struct {
	// Name identifies this migration in the ledger forever. Renaming one makes
	// every database that has run it appear not to have, so names are treated
	// as immutable once merged -- Validate cannot enforce that, but the tests
	// over each caller's list can and do.
	Name string
	// Statements is the DDL. Order within a migration is textual; order between
	// migrations is slice position.
	Statements string
}

// ledger is the table every component's migration history shares. One table
// with a component column rather than one table per component: several owners
// can be pointed at the same database, and a single place to look is worth more
// during an incident than a tidier separation nobody queries.
const ledger = `
CREATE TABLE IF NOT EXISTS conduit_schema_migrations (
    component text NOT NULL,
    name text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (component, name)
);`

// Validate refuses a migration list that cannot mean what it says.
//
// Exported and called by Apply, so a caller can also assert it in a unit test
// without a database -- which is the only way the ordering and uniqueness rules
// get checked on a machine with no PostgreSQL.
func Validate(component string, migrations []Migration) error {
	if strings.TrimSpace(component) == "" {
		return errors.New("a migration component name is required; it is the ledger key and cannot be empty")
	}
	if len(migrations) == 0 {
		return errors.New("no migrations to apply; an empty list would silently leave the database without a schema")
	}
	seen := make(map[string]int, len(migrations))
	for index, migration := range migrations {
		if strings.TrimSpace(migration.Name) == "" {
			return fmt.Errorf("migration %d has no name; the ledger is keyed on the name", index)
		}
		if strings.TrimSpace(migration.Statements) == "" {
			return fmt.Errorf("migration %q has no statements; recording an empty migration claims work that never happened", migration.Name)
		}
		if first, duplicate := seen[migration.Name]; duplicate {
			return fmt.Errorf(
				"migrations %d and %d are both named %q; the ledger would record the first and silently skip the second",
				first, index, migration.Name,
			)
		}
		seen[migration.Name] = index
	}
	return nil
}

// Apply runs every migration this database has not recorded, in slice order.
//
// It holds one connection for the whole run so the advisory locks stay in the
// session that took them. Taking each statement from the pool would let
// database/sql hand an unlock to a different connection, where it silently does
// nothing and leaves the lock held.
func Apply(ctx context.Context, database *sql.DB, component string, migrations []Migration) error {
	if err := Validate(component, migrations); err != nil {
		return err
	}
	connection, err := database.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Close()

	// The ledger table is created under a lock every component shares, and it is
	// taken before the per-component one.
	//
	// An earlier version created the table under the component's own lock and
	// claimed that solved the concurrent-CREATE hazard. It did not, and an
	// adversarial review proved it: two different components hash to two
	// different keys, so they do not exclude each other at all. Four components
	// racing a fresh database failed 36 times in 20 rounds with SQLSTATE 23505
	// on pg_type_typname_nsp_index -- precisely the failure the comment said was
	// prevented. The guarantee was per-component and the claim was general.
	//
	// No deadlock is possible from holding two: the ledger lock is always taken
	// and released before the component lock is requested, so no session ever
	// holds a component lock while waiting for the ledger one. A component whose
	// name happened to hash to the ledger key would simply take the same lock
	// twice, which PostgreSQL allows and which the paired unlocks balance.
	if err := createLedger(ctx, connection); err != nil {
		return err
	}

	key := lockKey(component)
	// Blocks rather than failing fast. A replica that starts while another is
	// migrating should wait for the schema it is about to depend on, not race
	// it or exit; the context bounds the wait.
	if _, err := connection.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
		return fmt.Errorf("take migration lock for %s: %w", component, err)
	}
	defer release(connection, ctx, key)
	applied, err := appliedNames(ctx, connection, component)
	if err != nil {
		return err
	}
	for _, migration := range migrations {
		if _, done := applied[migration.Name]; done {
			continue
		}
		if err := applyOne(ctx, connection, component, migration); err != nil {
			return err
		}
	}
	return nil
}

// ledgerLock is the name whose hash guards creating the ledger table. It is not
// a component and never appears in the table's component column; it exists only
// so that every caller, whatever it is migrating, contends for the same lock at
// the one moment they would otherwise collide.
const ledgerLock = "conduit_schema_migrations"

func createLedger(ctx context.Context, connection *sql.Conn) error {
	key := lockKey(ledgerLock)
	if _, err := connection.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, key); err != nil {
		return fmt.Errorf("take migration ledger lock: %w", err)
	}
	defer release(connection, ctx, key)
	if _, err := connection.ExecContext(ctx, ledger); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	return nil
}

// release drops an advisory lock, and destroys the connection if it cannot.
//
// The obvious version swallows the error on the reasoning that the lock goes
// when the session ends. That reasoning is wrong in a pooled world: closing a
// *sql.Conn returns a healthy connection to the pool rather than ending the
// PostgreSQL session, so an unlock that failed on an otherwise-healthy
// connection would leave the lock held by an idle pooled session for up to
// ConnMaxLifetime -- thirty minutes for both callers here -- and block every
// other replica's Apply for that long, with nothing reporting it.
//
// Returning driver.ErrBadConn from Raw marks the connection bad, so the pool
// destroys it and the session dies with the lock. The window is narrow, but the
// alternative is a fleet-wide startup stall whose cause is an ignored error.
//
// The context is detached because unlocking has to happen even when the caller's
// context is why we are unwinding.
func release(connection *sql.Conn, ctx context.Context, key int64) {
	if _, err := connection.ExecContext(
		context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, key,
	); err != nil {
		_ = connection.Raw(func(any) error { return driver.ErrBadConn })
	}
}

func appliedNames(ctx context.Context, connection *sql.Conn, component string) (map[string]struct{}, error) {
	rows, err := connection.QueryContext(ctx,
		`SELECT name FROM conduit_schema_migrations WHERE component = $1`, component)
	if err != nil {
		return nil, fmt.Errorf("read migration ledger for %s: %w", component, err)
	}
	defer rows.Close()
	applied := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read migration ledger for %s: %w", component, err)
		}
		applied[name] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migration ledger for %s: %w", component, err)
	}
	return applied, nil
}

// applyOne commits the DDL and the record of it together. PostgreSQL has
// transactional DDL, which is the whole reason this is possible; on a database
// without it the ledger would be a lie the moment a migration failed halfway.
func applyOne(ctx context.Context, connection *sql.Conn, component string, migration Migration) error {
	transaction, err := connection.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration %s/%s: %w", component, migration.Name, err)
	}
	defer func() { _ = transaction.Rollback() }()

	if _, err := transaction.ExecContext(ctx, migration.Statements); err != nil {
		return fmt.Errorf("apply migration %s/%s: %w", component, migration.Name, err)
	}
	// A plain insert, deliberately. The obvious defensive move is
	// ON CONFLICT DO NOTHING, and an earlier version of this had it, justified
	// by "a database restored from a backup taken mid-migration can arrive with
	// the row and not the DDL". That cannot happen: the row and the DDL commit
	// in this transaction, so any consistent backup has both or neither, and a
	// mutation test removing the clause survived because no case could be
	// written that needed it. Reaching here with the row already present would
	// mean the ledger read above missed it or something else is writing the
	// ledger -- neither is a state to continue from, and a primary-key violation
	// says so where a silent skip would not.
	if _, err := transaction.ExecContext(ctx,
		`INSERT INTO conduit_schema_migrations (component, name) VALUES ($1, $2)`,
		component, migration.Name); err != nil {
		return fmt.Errorf("record migration %s/%s: %w", component, migration.Name, err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migration %s/%s: %w", component, migration.Name, err)
	}
	return nil
}

// lockKey turns a component name into the bigint pg_advisory_lock takes.
//
// FNV-1a rather than anything cryptographic: this needs to be stable across
// processes and versions, not unguessable. Two components colliding would make
// one wait for the other's migrations, which is slow and correct, so a collision
// costs latency rather than safety.
func lockKey(component string) int64 {
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(component))
	return int64(digest.Sum64())
}
