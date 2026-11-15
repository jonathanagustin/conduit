# Administrator platform status

Audit → Platform status is a read-only administrator view backed by
`GET /v1/admin/status`. The route uses the existing Governance administrator
predicate: bootstrap status follows the credential owner, and platform-wide
permission-management grants retain their existing meaning. Ordinary operators
receive 403; production anonymous requests receive 401. Responses use
`Cache-Control: no-store`.

The OpenAPI source is `api/admin-status.json`. The pinned generator produces
DTOs, a strict server/router and a Go client. Run
`scripts/verify-admin-status-api.sh` to reproduce the boundary; both CI paths
check it, and contract tests call the generated client against the strict server.

## What is measured

- Pending change requests are stored records awaiting approval.
- Active packaging runs are the assessment, proposal, validation, review and
  handoff phases. A missing reader, a failed read or an unknown phase stays
  unavailable. Reader availability is not a worker heartbeat.
- Live, stale and unreachable spokes reuse the environment catalog's existing
  rule, per environment: a report within 45 seconds is live. Missing agents or
  timestamps remain unreachable. These are report observations, not open
  network connections.
- Open recall declarations are non-reverted declarations in the latest recorded
  desired-state snapshot for each environment. They are not unique global
  recall inventory or downstream withdrawal acknowledgments.
- Audit records include the repository's stored records and legacy imports.
  PostgreSQL additionally checks stored audit-chain links in one read-only,
  repeatable snapshot. The returned chain length and check time describe that
  check. No external checkpoint is asserted; file and memory backends do not
  fabricate stored-chain verification. A failed check clears the verification
  timestamp instead of displaying a stale success.
- Hub availability means this Hub served the request. The orchestrator entry
  states the configured driver; it does not claim that remote workers are alive.

Counts and probes are independent observations, not one atomic transaction
across all backend services. A failed panel is unavailable, not a healthy zero.
The view contains no example counts and performs no approval, configuration,
publication or deployment actions. Links open the existing owning views.

## Storage scope

The file repository wires its configured `DATA_DIRECTORY` filesystem into the
read-only capacity port. On Linux, usage is total capacity minus bytes available
to the process. It includes other data on that filesystem and reserved space;
it is not a recursive byte count of the metadata directory.

Warning begins at 80% and critical at 95%, including the exact boundaries.
Invalid measurements stay unavailable. Object-store and registry filesystems
require explicitly configured telemetry providers. For a mounted local backing
filesystem, set `ADMIN_OBJECT_STORE_DATA_DIRECTORY` or
`ADMIN_REGISTRY_DATA_DIRECTORY` to its path (read-only mounts suffice). These
settings do not query remote bucket or registry quotas; the Hub does not guess
remote quotas from its own disk. Other platforms without the implemented
filesystem probe remain unavailable rather than reporting zero.
