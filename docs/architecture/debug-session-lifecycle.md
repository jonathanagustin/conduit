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
valid transition. Application authority, durable adapters and authenticated
Hub composition must enforce their respective obligations. No production authority or
durability follows from these domain tests.

Tests constrain lifetime bounds including one nanosecond, exact-expiry behavior,
revocation/deletion, immutable target/owner/time bindings, broken event histories
and unsupported execution states. They are domain tests; no runtime trial or
broker behavior has been exercised.

## Private journal boundary (production integration outstanding)

`debugsessions/ports.Journal` requires atomic revision comparison and persistence
of the full validated transition/audit event before successful acknowledgement.
Its internal ID enumeration is bounded and grants no access. Failure after a
storage commit can be ambiguous; a caller must inspect the original history,
rather than minting a new request identity and assuming the first write failed.

The private event codec is bounded to 8192 bytes. It rejects duplicate keys,
case aliases, unknown fields, trailing documents, invalid native state and
external session-ID/revision mismatch. Stored times normalize to UTC. Whitespace
and object key ordering have no authority. Decoding one event still does not
validate the history or authorize an operator. This boundary has no application service, authenticated Hub route or production
composition.

The process-local memory journal is a test reference only and is not a durable
production component. It validates the complete current history and expected
revision under one lock, appends the combined transition/audit event, returns
independent copies and bounds sorted ID enumeration to 256 per call. A 40-writer
race test requires exactly one committed transition and 39 revision conflicts;
invalid scope changes, replays and pre-cancelled writes leave history unchanged.
The application service and Hub wiring are outstanding.


The private file adapter persists each canonical event and audit fact together
in a 0600 immutable file under a required private directory. It validates the
observed full history before writing, fsyncs the new file, atomically hard-links
the exact next revision and fsyncs the directory before acknowledging success.
The unique revision filename rejects stale competing writers across independent
Store instances. Interrupted temporary files are ignored. A failure after the
hard link can represent an ambiguous commit; callers must read history instead
of assuming rollback. This is not a transaction with the Hub's other stores or
an execution authority.

The file tests exercise reopen, a writer child process exiting before a reader
starts, 24 independent concurrent writers with exactly one committed revision,
invalid immutable bindings, pre-cancelled writes, missing predecessors, malformed
filenames, symlinks, oversized/corrupt records, private-directory permissions and
bounded ordered ID pages. They do not simulate a power loss, malicious storage
administrator or a live broker. Persistent records can still only represent
non-executing requests. Current authorization and production Hub composition
remain outstanding.


The PostgreSQL adapter uses the `debug-session-journal` migration component and
one immutable row per session/revision, with bounded document size and C-collated
native IDs. A transaction-scoped per-session advisory lock surrounds current
history validation and append; acknowledgement follows commit with local
`synchronous_commit=on`. PostgreSQL/storage configuration remains the database
operator's responsibility. A revision collision or invalid history cannot be
acknowledged as a new event.

The disposable PostgreSQL gate now runs the journal tests without a name filter.
Two independent pools and 32 competing writers produced one new transition/audit
row and 31 conflicts. Reopen, invalid/cancelled writes and missing predecessors
are tested. The broader migration and existing restore gate also passed, but the
journal is not yet composed into the Hub restore fixture; journal-specific
backup/restore integration remains outstanding alongside application authority.


## Application authority (production bridge outstanding)

The application service requires current trusted authority for create, read and
owner cleanup, and repeats it immediately before a journal append. Original
actor and effective owner come only from that port. Opaque UTF-8 subjects up to
256 bytes are preserved exactly, including plus-addressed email and issuer IDs
containing a pipe; controls and surrounding whitespace are rejected. They never
become filenames or resource paths. Resource-coordinate grammar stays separate.

Creation requires a caller-retained native request ID. Reusing a key with the
same owner, target and lifetime returns the existing request without extending
expiry or changing original actor; a conflicting target/lifetime fails. A key
belonging to another owner grants no access. No actor, owner or management flag
can be supplied in a Draft. A trusted scope decision can explicitly authorize
management of another owner's metadata. Production authorization is not wired.

Current reads reconcile past-deadline REQUESTED records into a persisted EXPIRED
transition before reporting that state. The EXPIRED state denotes automatic
clock expiry; its system actor label is not a human identity assertion. Deleted
requests remain available in authorized immutable history and cannot be revived
by replay. Revoke/delete retries are idempotent. Failed journal acknowledgement
never produces a successful lifecycle response; callers retain the original ID
to resolve an ambiguous commit.

List results are filtered by exact Environment/namespace, owner (or explicitly
trusted management permission), and deletion state. A continuation cursor comes
only from a returned visible ID. A 10000-record scan ceiling fails unavailable
rather than reporting a false empty/complete list. The current scope authority
is checked again before a page is returned. This is bounded native paging, not
a recovered Apollo wire contract or a cross-record snapshot transaction.

Identity/grant decisions and the journal are separate boundaries. Checking
current authority before an append is not a transaction with concurrent grant
administration. A stored request is never lasting authority; every subsequent
operation, and especially future broker admission/attachment, must reauthorize.
The service currently has no production authority bridge, HTTP route, CLI
request command or broker integration. Profile is requested metadata, not an
assertion that an image, registry or execution preflight has been verified.
