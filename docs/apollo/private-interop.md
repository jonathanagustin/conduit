# Private Apollo interoperability boundary

Status: blocked pending authorized, sanitized native evidence.

Public OpenAPI and generated OSDK material establish named public client and
data surfaces. They do not establish the Apollo Hub-to-Agent control protocol
for polling, Plan leases, heartbeats, cancellation, execution results, retries,
or complete reported state. Conduit's outbound Spoke protocol is therefore a
Conduit-native contract.

No private-protocol adapter should be added until an authorized evidence set
records all of the following without credentials or tenant identifiers:

1. Exact Hub, Agent, and CLI versions plus binary digests.
2. Transport, authentication, and version-negotiation shapes.
3. Canonical request and response fixtures for lease, start, heartbeat,
   cancellation, completion, failure, retry, and reported-state flows.
4. Status codes, ordering, idempotency, limits, backoff, and unknown-field
   behavior.
5. Negative fixtures for stale leases, wrong Environment identity, cancelled or
   recalled Plans, partial reports, and replay.
6. Source provenance, capture-tool identity, and a sanitization review receipt.

Captured material remains evidence, not implementation authority, until the
observed version and scope are explicit and a conformance test compares the
adapter with those fixtures. A gap stays unsupported rather than receiving a
plausible Apollo behavior.

The current public Agent and Foundry surface is indexed in
[`evidence.md`](evidence.md) and
[`reference/agent-installation.md`](reference/agent-installation.md).
