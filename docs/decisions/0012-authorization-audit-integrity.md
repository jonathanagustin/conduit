# ADR 0012: Authorization audit integrity and retention

Status: accepted (2026-11-13), issue #261.

The PostgreSQL metadata snapshot previously contained the full authorization
audit map. Application writes could not replace an event, but direct database
updates were undetectable and every metadata write serialized the entire log.

Give governance a separate append-only audit table and a serialized chain head.
Each event binds its sequence, predecessor, ID, resource, origin and exact
payload bytes with versioned, length-framed SHA-256. Commit the event and head
in the same SQL transaction as the decision. Nested transaction failures use
SQL savepoints as well as isolated metadata copies. Existing event IDs return
their original records rather than acquiring replacement payloads.

Retain all records indefinitely. There is no automatic expiry, archive deletion
or privileged reset endpoint. SQL guards refuse ordinary UPDATE, DELETE and
TRUNCATE. The deployment owns storage capacity, backups and independently
retained checkpoints. A future destructive retention feature needs a separate
archive and expiry-receipt design; silently pruning this chain is not allowed.

A hash chain is not a signature and does not constrain a database owner who can
recompute it. Verification against an independently retained checkpoint detects
changes to that prefix and rollback behind it. The new suffix is not externally
anchored until its checkpoint is retained. Bootstrap establishes consistency of
the bytes available now; it does not authenticate earlier history. Legacy
imports are labeled LEGACY_IMPORT and make the same bounded claim.

The table is authoritative. Import existing snapshot events transactionally,
then omit them from subsequent PostgreSQL snapshots. New readers also expose
unadopted legacy records left during a rollout; the next new writer imports
them without replacing chained IDs. Old Hub audit readers cannot see the new
table, so finish the Hub rollout before treating audit reads as complete.
Verification refuses unadopted legacy records. Memory/file development stores
retain their existing trusted-store behavior.

The verifier runs inside the Hub binary with mounted database credentials, a
read-only repeatable snapshot, and no repository construction or migration.
The restore drill checks an external checkpoint before opening a repository.
See `../operations/authorization-audit-integrity.md` for the format, procedure,
trust boundary and executable evidence. This is Conduit-owned persistence and
verification; it changes no Apollo audit.3 event transport.
