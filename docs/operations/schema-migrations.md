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
schema moved and the ledger did not, or vice versa. This is the claim the
restore procedure leans on, and until recently nothing had interrupted a
migration to check it; `internal/pgmigrate/crash_postgres_test.go` now does, and
[what a crash test proves](#what-the-crash-test-proves) says how far that goes.

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
-- 1. Add it nullable. Instant; no table rewrite. It still takes a brief
--    ACCESS EXCLUSIVE lock, so it queues behind long-running queries and
--    everything else queues behind it — set a lock_timeout on a table that
--    sees long transactions.
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
- **A nullable column is the right answer when NULL is a fact.** The three-step
  idiom above exists for a column every row must eventually have. It is the
  wrong shape when the absence means something: `0002-notification-delivery`
  leaves `delivery_available_at` nullable because NULL records "this entry was
  never queued for a transport", and backfilling it would make the first start
  after an upgrade try to deliver every notification the Hub had ever recorded.
  A migration that gives an existing deployment new outbound behaviour is a
  migration that did too much.
- **`ADD COLUMN … NOT NULL DEFAULT <constant>` does not rewrite the table** on
  PostgreSQL 11 and later; the default is recorded in the catalogue. That is why
  the delivery migration constrains its counters in one step rather than three.
  A non-constant default still rewrites, so this is not a general licence.

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
| `authorization-audit` | `internal/governance/adapters/auditpostgres` | append-only authorization events, digest chain head and rewrite/truncate guards |
| `notifications` | `internal/notifications/adapters/postgres` | notification events, subscription preferences, per-recipient inbox entries and their delivery state, and one contact address per (owner, kind) |
| `gitless-resolution-log` | `internal/gitless/adapters/resolutionlogpostgres` | append-only source selector, digest, publisher, and failed-convergence history |
| `assistant-threads` | `internal/assistant/adapters/threadpostgres` | assistant conversation threads and their messages, keyed by owner; bounded per owner, per thread and per message by `CHECK` constraints |
| `appgen` | `internal/appgen/adapters/postgres` | app generation results, one row per generation, with the owning operator as a column |
| `operator-sessions` | `internal/identity/adapters/sessionpostgres` | signed-in browser sessions keyed by the SHA-256 of the session cookie, and sign-in attempts in flight; applied by every PostgreSQL Hub and used only when interactive sign-in is configured. See [`operator-sign-in.md`](operator-sign-in.md) |

One ledger table with a `component` column rather than one table per component:
several owners can be pointed at the same database, and a single place to look
is worth more during an incident than a separation nobody queries.

## Asking what a start would apply

`pgmigrate.Pending(ctx, database, component, migrations)` reports what `Apply`
would run, in the order it would run it, and applies nothing.

It also **creates** nothing, which is the requirement that shapes it. The
question is asked most often of a database that has never been migrated — and
that is exactly the database with no ledger table. An implementation that
created the ledger in order to read it would answer correctly and leave the
schema changed, so asking would not be free. The table's absence is read with
`to_regclass` and answered for what it means: nothing has been recorded, so
everything is pending.

The answer is a snapshot rather than a promise: no lock is taken, so a
concurrent `Apply` on another replica can make it stale. Locking instead would
make a read-only question block a starting replica, and the answer would still
be stale by the time anyone acted on it.

The restore procedure uses it as its acceptance check — a correctly restored
database has nothing pending for any component. See
[`backup-and-restore.md`](backup-and-restore.md).

## Asking whether the schema the ledger claims is still there

A ledger row says a migration ran. It cannot say the result survived, because
nothing about a `DROP COLUMN` typed at a `psql` prompt touches the ledger.

`pgmigrate.Verify(ctx, database, sets...)` answers the other half. It rebuilds
the expected catalogue by replaying the migrations the ledger records —
**recorded, not every migration the binary carries**, so a database that is one
migration behind is a `Pending` answer rather than drift — into this session's
temporary schema, inside a transaction that is always rolled back, and then
diffs the live schema against it object by object.

The operator command is `platform-delivery verify-schema`, which reads
`POSTGRES_DSN_FILE` exactly as a start does and passes every component that
shares that DSN. It writes a report either way and exits non-zero on a fault:

```text
fault: column conduit_reconciliation_proposal_outbox.attempt (reconciliation) is missing:
    the migrations build "integer NOT NULL DEFAULT 0" and the database has nothing
```

**A shadow schema rather than a fingerprint.** The cheaper design records a hash
of the schema when `Apply` commits and compares it later. It fails three ways: it
can only detect drift that happened after the fingerprint was taken; it is
computed by the same code that ran the DDL, so it agrees with a mistake in that
DDL; and a hash cannot say *what* differs, which is the whole operational value.
Replaying the migrations derives the expected schema from the DDL rather than
from a previous observation of the database, and produces two catalogues that
compare object by object.

**Faults and advisories are different findings.** A fault is the live schema
lacking or contradicting something the recorded migrations built: a missing
table; a column that is missing, retyped, or no longer `NOT NULL`; a missing or
redefined index; a missing or redefined constraint; a column on an owned table
that no migration adds. Nothing benign produces one. An advisory is the live
schema having something no recorded migration builds — an extra index, or an
extra table — and it is never a fault, because two legitimate causes are
indistinguishable from drift there: an operator's own index for their query load,
and a component sharing the database that the caller did not enumerate. Nothing
in the PostgreSQL catalogue records which component created which table, so
ownership here is an observation of what appeared while a set's DDL ran, not a
rule about names.

**Operator-invoked, not a start-time check.** Nothing calls `Verify` on a Hub
start, deliberately. A start that refused on drift would convert a schema anomaly
into a fleet-wide outage at the moment an operator most needs a Hub to look at,
and the check is not free — it executes every recorded migration's DDL on every
invocation, per replica. `Pending` already answers the start-time question, which
is whether the schema is up to date. Whether it is *intact* has a human in the
loop, because half the answer needs one.

**What it does not compare**: column ordinal position, storage and compression
settings, ownership and privileges, triggers, views, functions, sequences beyond
the ones an identity column implies, and row contents. Every migration here is
unqualified DDL, so `Verify` reads `current_schema()` on a connection opened from
the same DSN by the same role; a database whose objects were migrated under a
different `search_path` is outside what this checks.

## What the crash test proves

The atomicity claim above was, for its whole life, checked only by runs that
finished. Every test called `Apply` and let it return before looking at
anything, so a claim about what happens when the process *does not* return was
being carried by the fact that nobody had interrupted one.

`internal/pgmigrate/crash_postgres_test.go` interrupts one. It re-executes the
test binary as the migrating process and `SIGKILL`s it — no deferred
`Rollback`, no `Close`, the kernel closes the socket and PostgreSQL discovers
the client is gone. The parent waits for that exact database backend to exit
before reading the schema and ledger: PostgreSQL may finish an in-flight COMMIT
after the client dies, so reading sooner can compare two different committed
states. It then asserts that the schema and the ledger agree about every migration, and that re-running the migrations afterwards completes. The
kills land at four named points inside `applyOne` (after the DDL, either side
of the ledger `INSERT`, and immediately before `COMMIT`) and at uniformly
random instants across the whole of `Apply`. Measured on one run at sixty
trials apiece: 240 kills at the named points, and 43 of the 60 randomised
trials landing inside `Apply` rather than outrunning it — **283 crashes, no
disagreement**. CI runs the same thing at five and forty trials, about thirteen
seconds under `-race`.

**A pass bounds the risk; it does not prove atomicity for all interleavings.**
The named points are the windows a reader of `applyOne` would think of, which
is exactly their weakness, and the randomised trials sample the rest rather
than covering it. What the test does establish is that the structure carrying
the claim cannot be removed silently: with the ledger `INSERT` moved into a
second transaction — a mutant that compiles and passes every other test in the
package — ten of the twenty deterministic trials fail and six of forty
randomised ones do, each reporting a table in the schema with no row recording
it. Two of the named points survive that mutant, because they straddle its
split rather than landing in it; the randomised trials are what catch it there.

Two harder interruptions were run by hand rather than committed, because both
need a disposable PostgreSQL of their own:

- **`SIGKILL` of the server** (every process in the container's cgroup, which
  is an unclean stop and *not* a power cut — the writes that reached the host
  page cache survive it). Twenty kills during a migration and twelve
  immediately after one committed: no disagreement, and every migration the
  client had seen commit came back whole from WAL recovery, before any
  checkpoint had run.
- **A true power cut**, with the server inside a UML guest whose `PGDATA` is an
  ext4 image on a virtual block device, killed from outside so the guest's page
  cache dies with it. Eight cuts, no disagreement; `e2fsck` reported and
  repaired filesystem damage on every one and PostgreSQL recovered each time.
  Worth recording honestly: **that arm has almost no power against this
  particular defect**. Six cuts against the mutant above detected it zero
  times, because a cut lands uniformly across a migration cycle in which the
  vulnerable window is under one percent. It bounds WAL durability and
  recovery; the client-side campaign is what bounds this claim.

## What this still does not do

- **No timeout on the advisory lock beyond the context.** A replica starting
  while another is midway through a long migration waits for it, which is
  correct, but the wait is bounded only by the caller's context.
- **Nothing runs the drift check on a schedule.** `verify-schema` is a command
  an operator invokes; no alert fires when a database stops matching the
  migrations recorded against it, so the check finds drift when somebody thinks
  to look for it.
- **Restore is not covered here** — it is covered in
  [`backup-and-restore.md`](backup-and-restore.md), which now states the
  procedure and is held down by a drill. The invariant this document has always
  claimed is the reason that works: a migration and the row recording it commit
  together, so a consistent backup holds both or neither and a restore needs no
  special handling. What was missing was anyone having checked.
