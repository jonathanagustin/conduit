# Authorization audit time indexes

The `0002-authorization-audit-time-indexes` migration adds two expression
indexes to the existing append-only ledger: `(createdAt(payload), sequence)`
and `(resource_rid, createdAt(payload), sequence)`. Page and count predicates
use that same extractor. No event row, JSON payload, predecessor, digest or
head is rewritten. There is no writable timestamp mirror or side table.

The existing resource/sequence index remains useful for unwindowed reads.
Selective time windows can use the new indexes; broad windows can still make a
sequential or sequence-index scan cheaper. The planner keeps that choice. Two
additional index entries and timestamp extractions are now part of each audit
insert; write-throughput impact has not been measured.

## Immutable conversion

`conduit_authorization_audit_created_at(bytea)` lives in the ledger's schema.
It is `STRICT`, `IMMUTABLE` and `PARALLEL SAFE`, with its search path fixed to
`pg_catalog`. It accepts the numeric RFC 3339 representation written by Go's
`time.Time.MarshalJSON`, with at most nine fractional digits. A missing or
malformed timestamp fails rather than acquiring a session-dependent meaning.

The function extracts numeric calendar/time fields, constructs a timestamp
without time zone, subtracts the numeric offset, and applies a fixed **interval**
zero offset. It never casts the raw timestamp text to `timestamptz`, consults a
named time zone or accepts relative values such as `now`. Its result therefore
does not depend on `TimeZone`, `DateStyle`, timezone abbreviations or daylight
saving rules. The ASCII regular expression uses the `C` collation.

Fractional seconds follow PostgreSQL's existing input behavior: parse the
fraction as double precision and round it to microseconds with ties to even.
Rounding happens before adding whole seconds, including carry across a second,
day or year. Adding the whole seconds before rounding can lose low fractional
bits. SQL query bounds retain the existing PostgreSQL/driver microsecond
semantics: `since` is inclusive and `until` exclusive.

All Go JSON years `0000` through `9999` are supported. Astronomical year `0000`
maps to PostgreSQL's `1 BC` constructor year, including its leap day. Numeric
offsets through `23:59` follow Go's JSON range. Year `0000` and offsets beyond
PostgreSQL's direct text-parser range previously made bounded reads fail; they
now derive a time without changing the stored evidence.

The immutability claim is based on these numeric operations, not on changing the
volatility label of PostgreSQL's general date parser. See PostgreSQL's
[volatility rules](https://www.postgresql.org/docs/18/xfunc-volatility.html),
[date/time types](https://www.postgresql.org/docs/18/datatype-datetime.html), and
the `ParseFractionalSecond` implementation in
[PostgreSQL 18.6](https://github.com/postgres/postgres/blob/REL_18_6/src/backend/utils/adt/datetime.c).

## Migration maintenance window

The migration uses ordinary transactional `CREATE INDEX`, with both indexes
and the migration ledger entry committed together. It scans existing payloads
and holds a table `SHARE` lock until commit. Audit appends wait behind that
lock; metadata decisions that must append an audit record can wait too. This
is a write-blocking migration, including when a newly starting Hub applies it.

Before rolling out the first upgraded Hub, schedule a maintenance window for
all writers, let in-flight decisions drain, retain the normal consistent
backup and independently held audit checkpoint, and budget disk for both new
indexes. Allow the migration to finish before resuming writes. Run
`ANALYZE conduit_authorization_audit_events` through the deployment's normal
database maintenance connection to populate statistics for the new expressions;
the planner evidence below does the same. Verify the ledger against the
retained checkpoint afterward, following
[`authorization-audit-integrity.md`](authorization-audit-integrity.md).
Malformed historical timestamps abort the migration transaction; investigate
the retained bytes rather than disabling guards or rewriting the ledger.

This migration is not a concurrent online index build. A later requirement for
online construction needs a separate migration protocol; running
`CREATE INDEX CONCURRENTLY` inside the current transaction is not an option.

One synthetic run on 2026-11-17 used PostgreSQL **18.6, aarch64 Linux musl** on
the shared disposable local test server: **16,384** valid chained events,
**8,765,440** payload bytes, **4.612 seconds** for the migration call. This is
one fixture measurement, not a production capacity or outage estimate. Real
history size, payload size, cache state, storage and competing transactions
determine the required window.

## Executable evidence

`internal/governance/adapters/auditpostgres/time_index_test.go` checks:

- 1,155 fixed and seeded random timestamps against the old predicate under four
  combinations of time zone, date/interval style, abbreviations, floating-point
  formatting and string-literal settings; ties, offsets and year boundaries are
  included.
- Explicit Go year-zero/leap-day and extreme-offset cases, malformed-input
  refusal, strict NULL handling and the declared function properties.
- Page and count equivalence around sub-microsecond inclusive/exclusive bounds.
- Schema-local objects, a year-zero indexed read, and the actual write-blocking
  lock retained until the index transaction commits.
- Exact before/after ledger bytes and head, independent checkpoint verification,
  retained UPDATE/DELETE/TRUNCATE guards, and a harmless repeat migration.
- Unforced `EXPLAIN (ANALYZE, FORMAT JSON)` on the populated fixture using each
  new index for both page and count queries.

Run with `CONDUIT_TEST_POSTGRES_DSN` pointing at a disposable server and
`go test -race ./internal/governance/adapters/auditpostgres`. Tests create and
drop their own isolated databases and schema; they do not migrate a deployment.
