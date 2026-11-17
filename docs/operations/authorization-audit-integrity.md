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
Deployments must plan capacity and backup retention. The verified JSONL export
below archives the original chain one record at a time; it does not remove live
records. Destructive retention remains a separate decision under ADR 0012 and
issue #865.

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
Time windows use expression indexes derived directly from the immutable
payload bytes, without a writable timestamp column. The same extractor is
used for page and count predicates at PostgreSQL microsecond resolution. See
[`authorization-audit-time-index.md`](authorization-audit-time-index.md) for
conversion guarantees, query-planner evidence and the required write-blocking
migration window.
`view=chain` remains a projection folded from genesis in read order, not the
stored chain. The verifier below does not use this read: it walks every row
inside one snapshot and is unaffected by paging. Other in-process readers
(timeline, live events, administrator status) still use the unpaginated
repository read. For a stored-ledger archive use the database verifier's export
below; the HTTP chain projection is not an archive of the stored chain. The
chart offers an opt-in scheduled checkpoint check. Live pruning is unsupported.
Mixed-rollout audit cursor adoption correctness is tracked in #866.

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

## Verified JSONL export and offline checking

Use the same read-only database identity and an independently retained
checkpoint. Export verifies every stored row, its original payload bytes and
origin, the independent prefix, and the head in one repeatable snapshot. Pending
legacy adoption is refused. It never constructs a repository or applies a
migration. For example:

```bash
platform-delivery verify-authorization-audit \
  --checkpoint-file=/trusted/audit-checkpoint.json \
  --export-file=/archives/audit-2026-11-16.jsonl \
  > candidate-audit-checkpoint.json
```

The destination must be new. The command stages a mode-0600 file beside it,
commits the read-only snapshot, syncs the file, and publishes the complete archive
with an atomic no-replace hard link and a directory sync. Filesystems must
support those operations. A failed check publishes no archive; a publication
or sync failure issues no receipt. A directory-sync failure can leave the
complete destination in place: inspect and verify it, then use a new name for
any retry. Abandoned staging files after a process crash have names beginning
`.conduit-audit-export-`; they contain sensitive audit bytes and are not
completed archives. Ordinary command failures clean them up.

The Conduit-native `conduit.authorization-audit-export/v1` JSONL envelope
contains one `record` per stored event followed by one terminal `checkpoint`.
Record fields are `sequence`, `previousDigest`, `eventId`, `resourceRid`,
`origin`, `payload` and `digest`. `payload` is base64 of the exact stored JSON
bytes, preserving the digest inputs, including `LEGACY_IMPORT` origin. There
is no time/resource filter: a complete chain is required. Each encoded line is
strictly smaller than 16 MiB; a larger event refuses the export. Memory usage
depends on one record, rather than the number of events. The terminal line is
required, and trailing lines are refused.

Verify the copied archive without PostgreSQL or a database credential:

```bash
platform-delivery verify-authorization-audit \
  --archive-file=/archives/audit-2026-11-16.jsonl \
  --checkpoint-file=/trusted/audit-checkpoint.json \
  > verified-archive-checkpoint.json
cmp candidate-audit-checkpoint.json verified-archive-checkpoint.json
```

Only a successful exit and matching receipts establish that the copy reaches
the verified database tip. Protect the candidate receipt independently before
relying on that longer prefix. `--bootstrap-checkpoint` also works for export
and offline checks, but retains its explicitly weaker internal-consistency
meaning. An archive's own terminal checkpoint is not an independent trust
anchor or a signature. A coherently rewritten or shortened suffix after the
last independently trusted checkpoint remains outside that checkpoint's
guarantee. Archive verification rejects malformed, oversized, discontinuous,
plainly truncated or checkpoint-conflicting input without a success receipt.

Archive contents include the original event bodies and their principal/resource
coordinates. Put archives and receipts into deployment-owned access controls,
encryption, backup and immutable-storage custody as appropriate. The command
does not upload them or select a retention policy.

## Scheduled checkpoint check

Merge `deploy/helm/platform-hub/audit-checkpoint-values.example.yaml` with the
deployment's ordinary chart values. `auditCheckpoint.enabled` defaults to
false. The opt-in UTC CronJob uses the exact Hub image, a separate database
Secret whose identity has only SELECT on the three audit/read-status tables,
and a ConfigMap containing a previously trusted nonempty checkpoint. It has no
Kubernetes API token, no bootstrap mode, no archive output and no permission
to update the trusted checkpoint. A Secret name alone cannot enforce database
privileges: provision the read-only role separately.

The Job verifies a stable database snapshot and emits a bounded checkpoint
receipt on success. Collect failures and receipts in the deployment's existing
monitoring and independent immutable custody. Review a newer receipt before
advancing the trusted checkpoint; the Job never promotes its own observation
to trust. Protect the retained original checkpoint outside the cluster as well:
a cluster administrator can change both a ConfigMap and the Job. With
default-deny egress, allow the `app.kubernetes.io/component=audit-checkpoint`
Pods to reach DNS and PostgreSQL. Their distinct component label keeps them
outside the Hub Service selector. This is an executable chart example, not
evidence that a scheduled Job has run on a cluster.

## Archive custody and pruning boundary

Keep the live ledger and its head intact. A verified export is an additional
copy; it is not authority to delete rows, disable the append-only triggers,
renumber events or reset genesis. Restoring the database still requires the
normal consistent backup and independent checkpoint procedure below; an export
is not a full metadata backup or a supported database-import format.

Before expiring any redundant external backup/export copy under the
deployment's retention and legal-hold policy, retain another independently
verified complete copy and the associated independent receipts for every
event being preserved. Verify the retained copy after transfer, record its
custody and restore/readability checks, and expire only the redundant copy.
Conduit supplies no deletion command or automatic expiry policy.

There is no supported live-prefix prune procedure today. Issue #865 records
the required archive custody, expiry receipts, duplicate-ID, restore and
archived-read decisions under ADR 0012. Until that design is accepted and
implemented, capacity planning must assume indefinite live retention. This
remaining part of #841 is not completed by export or scheduled verification.

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
