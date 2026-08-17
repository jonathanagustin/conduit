# Schema migrations

How Conduit changes a PostgreSQL schema without stopping, and why it changes it
that way. Written because the idiom existed and was recorded nowhere, so the
next contributor had to infer it from the DDL — which is a good way to infer the
wrong thing.

## The mechanism

`internal/pgmigrate` applies an ordered slice of named migrations exactly once
per database and records what it applied in `conduit_schema_migrations`.

```go
var migrations = []pgmigrate.Migration{
    {Name: "0001-reconciliation-core", Statements: `...`},
    {Name: "0002-outbox-desired-coordinates", Statements: `...`},
}

pgmigrate.Apply(ctx, database, "reconciliation", migrations)
```

Four properties, each of which is load-bearing:

**Position is the order. The name is identity.** A migration's position in the
slice decides when it runs; its name decides whether it has already run.
Renaming a merged migration makes every database that has run it appear not to
have, so names do not change once merged.

**A migration and the record of it commit together.** Each runs in its own
transaction alongside the `INSERT` into the ledger. PostgreSQL has transactional
DDL, which is the whole reason this works — there is no window in which the
schema moved and the ledger did not, or vice versa.

**One advisory lock, taken before anything else.** Two replicas starting
together would otherwise both run the DDL. The lock is taken before the ledger
table is created, because concurrent `CREATE TABLE IF NOT EXISTS` is *not* safe
in PostgreSQL — it can raise a duplicate-key error on the system catalogue
rather than skipping — so the lock has to be older than the table protecting it.
Advisory locks need no table, which is what makes that ordering possible.

**Forward-only.** There is no down path, and that is a decision. A down
migration runs, by definition, during an incident, having been tested less than
the up path it reverses, and its job is to destroy data. Roll forward with a new
migration instead: slower to write, and it does not delete the evidence of what
went wrong.

## Writing one

The idiom for adding a non-null column to a populated table is three steps, and
migration `0002-outbox-desired-coordinates` is the worked example:

```sql
-- 1. Add it nullable. Instant; no table rewrite, no lock held.
ALTER TABLE t ADD COLUMN IF NOT EXISTS c text;

-- 2. Backfill, guarded, so re-running costs nothing.
UPDATE t SET c = <derivation> WHERE c IS NULL;

-- 3. Constrain, once every row satisfies it.
ALTER TABLE t ALTER COLUMN c SET NOT NULL;
```

Each step is safe to interrupt and none requires the application to be stopped.
The order matters: constraining before backfilling fails, and backfilling
without the guard rewrites every row on a retry.

### Rules

- **Additive first.** A release that adds a column and a release that starts
  writing it are two releases. During a rolling upgrade both versions run at
  once, so the schema must satisfy the old code and the new code simultaneously.
- **No `DROP`, `RENAME`, or column retype in the same release as the code that
  stops using the thing.** Stop using it, ship, then remove it.
- **`SET NOT NULL` takes an `ACCESS EXCLUSIVE` lock.** It is brief on a
  backfilled table and it is not free on a large one. On a table big enough to
  matter, add a `NOT VALID` check constraint, `VALIDATE` it, and constrain after.
- **Do not put `CREATE INDEX CONCURRENTLY` in a migration.** It cannot run
  inside a transaction, and every migration here runs in one.

## Adoption

Databases that predate the ledger contain the effects of their migrations and
have no rows recording them. The first start after the ledger landed therefore
runs every existing migration once against a populated database, which is safe
only because every one of them is idempotent.

That requirement is a one-time property, not a rule for the future, and it is
scoped by name in
`internal/delivery/reconciliation/adapters/postgres/upgrade_test.go`. A
migration written after adoption runs against a database whose ledger is
accurate and does not have to be idempotent.

## Components

| Component | Owner | Tables |
| --- | --- | --- |
| `reconciliation` | `internal/delivery/reconciliation/adapters/postgres` | desired state, observations, leased work, decisions, audit, proposal outbox, plan receipts |
| `controlplane` | `internal/controlplane` | the versioned metadata snapshot and the artifact blob store |

One ledger table with a `component` column rather than one table per component:
several owners can be pointed at the same database, and a single place to look
is worth more during an incident than a separation nobody queries.

## What this still does not do

- **No dry run.** There is no way to ask what a start would apply without
  applying it.
- **No timeout on the advisory lock beyond the context.** A replica starting
  while another is midway through a long migration waits for it, which is
  correct, but the wait is bounded only by the caller's context.
- **Nothing verifies that a database's schema matches the migrations that
  claim to have built it.** A ledger row asserts a migration ran; it does not
  detect a column somebody dropped by hand.
- **Restore is not covered here.** See
  `docs/operations/PRODUCTION-READINESS.md`, which still records backup and
  restore as a Tier 1 gap. A migration and the row recording it commit together,
  so a consistent backup holds both or neither and a restore does not need
  special handling — but nothing here rehearses a restore, and no procedure says
  how to take one.
