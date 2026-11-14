# Debug-session lifecycle kernel

`internal/debugsessions/domain` is the native lifecycle kernel for #303's
first phase. It does not expose a Hub API, create a durable record, authorize an
operator, run a Pod, issue a credential or attach a shell. The production CLI
and broker remain unchanged. ADR 0015 and the pinned public-source review in
`reference-platform/reference/debug-access.md` remain the behavior authority.

The native event vocabulary is REQUESTED, EXPIRED, REVOKED and DELETED. There
is deliberately no READY/ACTIVE/ATTACHED state or execution permit. One immutable
request binds an opaque random session ID, Environment, namespace, explicit
profile reference, effective owner, authenticated original actor, creation and
fixed expiry. It is marked CONDUIT_NATIVE. The four-hour maximum is established
by the source; these event names, identifier grammar and schemas are native.

Expiry is exclusive: expiry at the exact bound is accepted and revocation at
that instant is not. Revocation and deletion cannot extend or change scope or
owner. A terminal request can be deleted before or after expiry. A three-event
history is sufficient for create, expire/revoke and delete; gaps, duplicates,
reordering and immutable-binding changes fail validation. Terminal states cannot
be reattached, reopened or promoted into execution readiness.

This kernel validates arguments only. Callers must establish current identity
and authorization, generate unpredictable IDs, choose time from an authoritative
clock, enforce owner isolation, persist the whole transition/audit fact before
acknowledging it, handle competing revisions, reconcile expiry and validate
complete histories on read. A standalone Event.Validate call is not proof of a
valid transition. The forthcoming durable application/adapters and authenticated
Hub composition must enforce those obligations. No production authority or
durability follows from these domain tests.

Tests constrain lifetime bounds including one nanosecond, exact-expiry behavior,
revocation/deletion, immutable target/owner/time bindings, broken event histories
and unsupported execution states. They are domain tests; no runtime trial or
broker behavior has been exercised.

## Private journal boundary (adapter implementation outstanding)

`debugsessions/ports.Journal` requires atomic revision comparison and persistence
of the full validated transition/audit event before successful acknowledgement.
Its internal ID enumeration is bounded and grants no access. Failure after a
storage commit can be ambiguous; a caller must inspect the original history,
rather than minting a new request identity and assuming the first write failed.

The private event codec is bounded to 8192 bytes. It rejects duplicate keys,
case aliases, unknown fields, trailing documents, invalid native state and
external session-ID/revision mismatch. Stored times normalize to UTC. Whitespace
and object key ordering have no authority. Decoding one event still does not
validate the history or authorize an operator. This boundary currently has no
storage adapter, application service, authenticated Hub route or production
composition.

The process-local memory journal is a test reference only and is not a durable
production component. It validates the complete current history and expected
revision under one lock, appends the combined transition/audit event, returns
independent copies and bounds sorted ID enumeration to 256 per call. A 40-writer
race test requires exactly one committed transition and 39 revision conflicts;
invalid scope changes, replays and pre-cancelled writes leave history unchanged.
The file/PostgreSQL adapters, application service and Hub wiring are outstanding.
