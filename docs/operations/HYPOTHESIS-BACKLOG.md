# Hypothesis backlog

These are leads, not product claims. Acceptance means the work is worth a
bounded investigation; it does not pre-approve implementation.

| ID | Hypothesis | Gate before closure | State |
| --- | --- | --- | --- |
| B1 | The pinned Apollo OpenAPI can generate all transport DTOs and a strict server interface without leaking generated types into Conduit domain packages. | Fresh generation is byte-stable; compile-time adapter tests cover every operation; the domain-boundary gate passes. | DONE |
| B2 | Apollo translation can leave `internal/controlplane` after Catalog and Environment types move behind neutral ports. | Import graph has no cycle; focused behavior tests pass before and after the move. | QUEUED |
| B3 | One tenant-neutral frontend bundle can consume safe tenant runtime overrides. | Schema rejects unknown keys, executable content, and security-policy weakening; two tenant fixtures produce only the allowed differences. | QUEUED |
| B4 | A transactional repository can support multiple Hub replicas without changing domain contracts. | Concurrent lease, approval, idempotency, and crash-recovery tests pass against the selected adapter. | QUEUED |
| B5 | Remaining large UI collections can adopt the shared opaque-cursor contract. | Each endpoint authorizes before filtering and paging; browser tests retain URL-owned state across next, previous, reload, and retry. | QUEUED |
| B6 | A Conduit-owned durable reconciliation worker can replace Flux as the owner of tenant delivery convergence without duplicating Kubernetes controller semantics. | One installation converges end to end through native ports; duplicate and dropped events, crashes, stale reports, audit failure, and desired-generation supersession pass deterministic tests. | QUEUED |
| B7 | A clean cluster can consume the signed private Conduit runtime through Flux without Git access. | Install Flux without Git; pull with read-only registry authority; reject the wrong signer; reconcile the exact digest; inventory zero Git credentials and `GitRepository` objects. | IN-PROGRESS |
| B8 | The unadmitted HiGHS candidate can move to Go 1.26.6 without weakening its reproducibility or security evidence. | Rebuild the amd64 and arm64 candidates from the new pinned builder; regenerate SBOM and vulnerability evidence; require byte-identical repeated images, zero high-severity matches, and all candidate mutation gates. | QUEUED |

## Proposed-hypothesis intake

Use `PH-<number>` rows for agent proposals. Every row needs an `ACCEPT`,
`REJECT`, `NOTE`, or `QUEUED` verdict before the change closes.

| Proposal | Origin | Proposed hypothesis | Gatekeeper verdict |
| --- | --- | --- | --- |
