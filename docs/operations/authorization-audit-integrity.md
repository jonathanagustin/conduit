# Authorization audit integrity

Issue #261 and ADR 0012 move production authorization audit events into the
`authorization-audit` migration component. `conduit_authorization_audit_events`
holds the event bytes and predecessor; `conduit_authorization_audit_head` holds
the current sequence and digest. Governance owns the adapter. The legacy
control-plane repository only supplies the transaction and compatibility view.

## Guarantees and limits

Appends serialize on the head row and commit with the metadata decision. A
failed outer transaction rolls back both; a caught inner failure also rolls
back both through a SQL savepoint and an isolated metadata copy. Duplicate IDs
return the original event. No update/delete API exists. PostgreSQL triggers
refuse ordinary UPDATE, DELETE and TRUNCATE, including accidental direct SQL.

Records are retained **indefinitely, with no automatic pruning**. Past event
timestamps do not expire records. Audit history is no longer decoded or encoded
as part of ordinary PostgreSQL metadata snapshots after adoption. Listing the
log still reads the requested events, and full verification walks the chain.
Deployments must plan capacity and backup retention; this change provides no
bounded archive or destructive retention command.

A database owner can disable the guards, rewrite events, recompute digests and
replace the head. A chain checked against itself cannot detect that. Keep
checkpoint receipts under credentials or immutable storage independent of the
database administrator. Verification detects an inconsistent chain, a changed
trusted prefix, or a rollback behind that checkpoint. It cannot authenticate an
uncheckpointed suffix, prove an event's claim true, or detect history already
rewritten before the first independently trusted checkpoint.

The HTTP audit read remains administrator-only. The verifier is an operator
command requiring database access, not a new HTTP authorization bypass.
Memory/file development repositories remain trusted-store logs; these at-rest
checks apply to PostgreSQL.

`GET /v1/authorization-audit` is cursor-paginated and time-bounded (issue
#841): `limit` (default 100, maximum 200), `cursor`, `since`, `until` and
`resourceRid`, under `docs/ui/contracts/pagination.md`. On PostgreSQL the limit,
the seek and the time window are part of the SQL statement and rows come back
in chain `sequence` order, so the Hub holds one page however long the log is.
The time window has no index — the table has no time column and its rows are
immutable — so a windowed read or count still scans the rows it filters.
`view=chain` remains a projection folded from genesis in read order, not the
stored chain. The verifier below does not use this read: it walks every row
inside one snapshot and is unaffected by paging. Other in-process readers
(timeline, live events, administrator status) still use the unpaginated
repository read. Export sinks, archival, pruning and scheduled checkpoints are
not provided.

## Verification and checkpoint custody

Use the Hub's shipped binary with the deployment's mounted `POSTGRES_DSN_FILE`.
It opens a read-only repeatable-read transaction, checks for pending legacy
adoption, and walks all records and the head. It never starts the Hub, creates a
repository, applies migrations or repairs damage. A missing table, bad digest,
missing row, wrong checkpoint or pending adoption exits nonzero without a receipt.
Database credentials and event payloads are not printed.

Establish the first checkpoint only at an explicitly trusted point:

```bash
platform-delivery verify-authorization-audit --bootstrap-checkpoint > initial-audit-checkpoint.json
```

The receipt's `verification` is `BOOTSTRAP_INTERNAL_CONSISTENCY_ONLY`. Retain it
independently; this is the trust-establishment step, not proof of earlier
history. An empty-log receipt cannot serve as a trusted event prefix. After at
least one event, verify against the retained receipt and capture the new tip:

```bash
platform-delivery verify-authorization-audit --checkpoint-file=/trusted/audit-checkpoint.json > next-audit-checkpoint.json
```

A successful receipt says `CHECKPOINT_PREFIX_MATCHED`; it includes `version`,
`sequence` and `digest`. Compare/store the new receipt under the same independent
custody before relying on its longer prefix. Choose a cadence appropriate to
the maximum unanchored suffix the deployment accepts. Do not overwrite the last
trusted receipt when verification fails. The default command deadline is two
minutes; `--timeout=5m` can bound a larger verification run. Receipts are bounded
to 64 KiB on input and contain no audit event bodies or credentials.

## Upgrade and restore

The first new Hub imports existing snapshot records in one metadata transaction,
ordered by event time and ID because the old map did not retain append order.
Imported records carry `LEGACY_IMPORT`; new appends carry `LIVE`. The chain
attests the imported bytes at cutover, not their authenticity before cutover.
The native table wins on a duplicate ID, and imported records are removed from
the snapshot before commit.

During a mixed Hub rollout, an older writer can still add records to its legacy
map. New readers include them, the next new writer adopts them, and verification
refuses to call the chain complete while any remain. Older audit readers cannot
see chained-only events. Upgrade every Hub replica before declaring the audit
read path complete and establishing a post-upgrade checkpoint. This is not a
claim of complete old/new Hub audit-read compatibility.

A consistent `pg_dump` includes the audit tables, head, guards and migration
ledger with metadata. After restoring a post-upgrade database, check migration
state and the separately retained checkpoint **before opening a repository**.
Repository construction can adopt legacy records, so doing it first changes the
evidence being inspected. See `backup-and-restore.md` for the full restore order.
Keep checkpoint custody and deployment secrets independent of the dump's data.

## Digest format and acceptance

`conduit.authorization-audit-chain/v1` hashes UTF-8 version bytes, a big-endian
64-bit sequence, then five length-framed byte strings: predecessor digest, event
ID, resource RID, origin and exact JSON payload. Every length is a big-endian
64-bit byte count. Digests are lowercase SHA-256 hex. The genesis digest hashes
the version followed by `:genesis`. Verification uses stored bytes rather than
re-serializing an event. This is a native integrity format, not an Apollo wire
contract or a signature format.

Domain tests include an independently computed Python digest vector and altered
payload/identity/origin/predecessor cases. PostgreSQL tests cover independent
pools, duplicate IDs, old-event retention, direct SQL guard refusals, failed
transactions, coherent DBA rewrites and rollback against an external checkpoint.
Control-plane tests exercise nested errors/panics, metadata-write failures,
legacy adoption and the absence of audit history in snapshots. Command tests
cover strict input bounds, credential redaction, read-only behavior and pending
legacy refusal. The real online dump/restore drill verifies the saved checkpoint
before repository construction and compares the restored audit event afterward.
