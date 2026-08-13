# Private Hub/Agent interoperability intake

Status: **BLOCKED on authorized native evidence**.

Public `palantir/osdk-ts` now proves an Apollo Agent-to-Foundry Install Manager
surface. The durable crosswalk is
[`../reference/agent-installation.md`](../reference/agent-installation.md).
It does not publish the Hub control transport used for polling, Plan leases,
heartbeats, cancellation, execution results, or reported Environment state.

Future authorized capture should stay under `/tmp` until sanitized. A complete
intake must record:

1. native Agent and Hub/CLI versions plus binary SHA-256 (never the binaries);
2. transport/authentication and version-negotiation metadata with identities,
   hostnames, certificates, and tokens replaced by typed placeholders;
3. canonical request/response schemas for poll/lease, start, heartbeat,
   cancellation, finish, failure, retry, and full reported-state flows;
4. exact status codes, retry/backoff rules, size limits, ordering, idempotency,
   and unknown-field behavior;
5. negative fixtures for stale leases, wrong Environment identity, recalled or
   cancelled Plans, partial reports, and replay;
6. source date, upstream build identity, capture-tool hash, and a sanitization
   review record.

Only after those artifacts exist should code under `internal/agent` or
`internal/controlplane` gain a named private-protocol adapter. Until then the
platform-native authenticated outbound Agent protocol remains the supported
implementation, and Kubernetes convergence stays behind the Flux/Helm adapter
boundary.
