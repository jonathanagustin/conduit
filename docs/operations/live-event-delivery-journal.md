# Durable live-event delivery journal

This is the delivery foundation for #389 under accepted
[ADR 0016](../decisions/0016-console-live-events.md). It consumes the existing
`api/live-events/event.schema.json` contract. It adds no runtime route, native
producer, automatic client subscription, approval channel or execution path.
Native producer integration and the SSE HTTP adapter remain #389; hooks remain
#390. The current console continues using its existing reads and polling.

## Storage and ordering

`internal/liveevents` separates domain records, journal/authorization/validation
ports, the delivery service, the committed-schema validator and an atomic file
adapter. A publication succeeds only after its source rows and visible replay
frames are durably stored. The file adapter writes a private temporary file,
fsyncs it, renames it and fsyncs the directory. Failed callbacks leave both the
committed memory view and file unchanged. Callbacks and snapshots cannot retain
mutable aliases into committed state. A directory-sync failure after rename
reports an uncertain durability result while retaining the renamed state in
memory; a retry remains idempotent.

Each source adapter supplies a stable subject-bound producer ID and a persisted
positive source position. The first observed position establishes that source's
baseline; subsequent new positions must be contiguous. Repeated positions are
idempotent, and a changed value at the latest acknowledged position is a conflict.
Producer high watermarks survive replay trimming, so old retries do not become
new events. A separate private journal position orders accepted publications;
it is never exposed as a public cursor or sequence.

Visibility is checked before assigning each stream's contiguous sequence and
cursor. Hidden source rows advance only a private position. Occurrence time is
informational and never sorts deliveries. An index invalidation uses a stable
hash of its private owning subject as its public index ID, with that owning
read model's persisted revision; it does not expose a global index counter.

## Identity and replay

The authorizer supplies hashes of the credential owner, represented identity
and current native authorization revision. Requests cannot choose them. It must
refuse a caller without stream permission before allocation and re-evaluate
native visibility, grants and delegation for every check. The service requires
an explicit producer-readiness port and refuses streams during unavailability.
A failed publication closes active deliveries and prevents new subscriptions
until a successful verified retry; the readiness adapter must also track any
remaining native producer failures/backlog recovery.

Cursors are cryptographically random opaque references to stored positions,
bound to the complete identity tuple. Lookup requires current authorization;
possession conveys no authority. Replay retains the same stream ID, sequence,
cursor and content. Changed owner, delegation or authorization revision,
unknown/expired cursor, missing source history or loss of replay visibility
requires reset. An invalid cursor does not report whether another identity's
stream exists. No cursor is stored in shared browser storage or accepted in a
query parameter by this library.

A newly opened stream returns a ready watermark at sequence zero. Resume returns
the acknowledged sequence, not the journal's private position or a claim that
read models are current. Source records published during a disconnect are
filtered on authorized reconnect before assigning new visible sequences.

## Bounds and failure behavior

The service checks the ADR's 256-envelope/1-MiB queue limits, including the
in-flight frame and SSE line framing, and validates each envelope against the
4-KiB committed schema. A slow connection closes on overflow; its durable
history remains available for bounded incremental replay. Delivery requires an
exact acknowledgement of the in-flight cursor before another frame is exposed.
The transport will acknowledge only after writing the frame. A lost connection
can replay from the client's actual applied cursor even if a socket write was
reported successful.

Replay frames retain at most 10 minutes or 10,000 entries per stream, whichever
comes first. Unavailable prefixes require reset rather than a silent sequence
jump. Connections have a 30-minute application lifetime; the future HTTP adapter
must also enforce its monotonic timeout and the specified 15-second comment
heartbeat. Heartbeats are not resource events. Public sequence and revision
values remain inside the schema's JavaScript-safe integer range.

The initial file adapter is for a single owner of one file in a single Hub
process. It rejects symlink/non-private/incomplete/unknown-version state and
limits serialized journal state to 512 MiB. Its full-state transactions favor
reviewable durability over high-volume throughput; a shared/multi-Hub deployment
needs a database journal adapter. The service defaults to 128 retained streams
and active connections, configurable by composition. These are storage/capacity
limits, not permissions. Capacity/storage failure is unavailable, never an empty
or falsely current stream. Native committed outboxes must retain unacknowledged
records for retry; this library does not make a separate source mutation atomic.
Callers must close subscriptions when the transport ends.

## Producer integration requirements

Runtime composition must construct records from persisted native transitions,
using native resource revisions and stable outbox positions. It must never copy
model text, prompts, configuration, credentials or raw tool/error bodies into
payloads or identifiers. Subject/resource binding, bounded IDs, schema payload
allowlists and duplicate-member rejection provide additional boundary checks.
Duplicate JSON members are rejected before schema validation so an ignored
member cannot carry forbidden free text onto the wire.

The next integration must couple source outbox records atomically with native
Plan/approval/report writes, bridge actual durable packaging history and agent
activity, implement a fresh native authorizer/readiness adapter, and mount the
SSE route with the ADR's headers and HTTP failure rules. Until those exist, this
module is a tested internal foundation and is not advertised as a live feed.

Tests cover two differently authorized subscribers, hidden-event sequence
isolation, restart/offline replay, identity and delegation changes, revocation,
retention expiry and count limits, queue overflow with intact replay, concurrent
publishers, producer deduplication/gaps/retcons, arbitrary/duplicate-member
payloads, callback rollback/aliasing, cancellation, permissions and corrupt state.

## Native source outboxes

The native Memory, file, and PostgreSQL repositories record `index.invalidated`
hints for successful Plan, change-request, and environment-report writes.
Each private record binds its native subject, persisted resource revision,
producer ordinal, and commit-recording timestamp. The timestamp describes when
the adapter recorded the mutation; it is not an observed Agent timestamp or an
ordering guarantee. Payloads are static area codes, with no model text or native
record serialization. Plan invalidations include approvals so consumers refetch
the persisted approval projection rather than infer a decision from a hint.

Memory transactions and PostgreSQL's repository-state transaction include these
outboxes in their rollback boundary. The single-process file adapter embeds the
outbox in the same atomic JSON replacement as the owning object. Existing files
remain readable and begin recording on their next write. New zero-revision Plans
and change requests receive native revision 1 at creation. Native public reads
and exports do not include the private outbox metadata.

The combined file change-request/Plan operation still has two separate file
commits. If the first succeeds and the second fails, only the first file and its
own subject-bound hint are committed. A change-request hint never claims that
its associated Plan or approval projection also committed.

`CommittedSource.ReadCommittedEvents` is a private adapter port, not an operator
endpoint. It returns at most 256 copied records in stable producer/ordinal order.
The consumer supplies durable per-producer checkpoints from the delivery
journal, publishes a batch, persists that journal, then advances its checkpoints.
Each native subject retains its latest 10,000 records and its durable ordinal.
A known checkpoint behind retained history, or ahead of its source, requires a
reset. A new consumer may establish its baseline at the first retained record.
Legacy writes before the outbox was introduced cannot be reconstructed as events.

This source adapter does not mount `/v1/events`, implement current native
subscriber authorization, or connect source draining to delivery readiness.
Packaging and assistant producers remain separate work. The delivery feature
must stay unavailable until its required producers, authorization, and transport
composition are installed and verified.

## Draining configured committed sources

`application.Config.Sources` fixes the named native source adapters when the
service is constructed; the service copies the registry. `DrainCommitted`
accepts only a configured name and a bounded budget of 1–64 pages, each of at
most 256 records. It reads source checkpoints from the delivery journal before
every page and uses the publisher's same durable transaction for records and
new checkpoints. It does not maintain a second in-memory acknowledgement.
After a failed journal write or restart, the source therefore supplies the
unacknowledged page again. Concurrent drains are serialized. The source read
runs outside the delivery mutex and outside native mutation callbacks, allowing
visibility checks to read native state without a repository-lock cycle.

The service combines its configured readiness port with an internal readiness
check for every configured source. All start unready. Each source must complete
a successful read and delivery commit at least once every 30 seconds; this is a
Conduit-native freshness bound, not an upstream transport guarantee. Failed,
stale, or backwards-clock checks refuse new subscriptions. A drain failure also
wakes and terminates existing subscriptions. The caller owns polling and must
schedule every required source even when it has no events. A successful bounded
page can mark a source healthy while backlog remains; readiness proves that the
adapter works, not that native read models or queues are caught up. `More` tells
the caller that the page budget was exhausted and another drain is due.

An idle recovery performs a journal write validation before marking the source
healthy or clearing publication failure. It creates no event, sequence, or
synthetic resource revision. A retention gap, repeated source position, invalid
producer coordinate, oversized page, or failed native read remains a failure;
the drainer never discards checkpoints or invents an automatic reset to hide it.

The native repository/delivery integration tests exercise Memory, file, and
PostgreSQL sources, filtered visible sequences, another database replica,
rollback, durable retry, and replay after reopening the delivery journal. They
use a bounded test authorizer; they do not establish production operator
identity policy. Runtime polling, current native subscriber authorization,
remaining producer families, and the SSE HTTP adapter are still not composed.

## Native packaging step source

`packagingworkflow/adapters/liveevents.Source` reads the same native step journal
as `GET /v1/packaging-runs/{runID}/events`. The runtime reader reconstructs that
journal from Temporal workflow history, including entries carried through
continue-as-new. The real pinned-CLI restart test compares projected records
before and after restarting both worker and Temporal server. Source producer
positions and resource revisions use the journal's persisted `Sequence`; event
times use its `At`. These are step-history coordinates, not a new revision field
on `RunStatus`. A client lacking a comparable read-model revision must use the
coalesced refetch/catch-up behavior from ADR 0016.

The source projects only known phase/outcome codes into `packaging.step`.
`WAITING` remains a wait, and `HANDOFF_RECORDED` remains a recorded Plan handoff.
Native approval dispositions and unknown codes use a subject-bound
`index.invalidated` hint instead. Reasons, approvals, proposal metadata, target
references, file contents, tool output, and model text are never copied into the
hint. The native read route remains the place to fetch authorized detail.

Continued-workflow execution rows are deduplicated by logical run ID before
reading the current journal. Each source read validates all loaded journals and
returns at most 256 records in stable producer/sequence order. Bounds match the
existing native index (1,000 execution rows) and native journal (32 entries per
run); run IDs must fit the committed live contract's 200-character single-token
bound. Invalid coordinates, unreadable runs, history gaps, or unavailable workers
fail the source. They do not cause a fabricated completion or a silently skipped
run. Registry wiring belongs to runtime composition and remains outstanding.

Legacy runs without a journal and status-only changes with no recorded step
emit nothing. In particular, this adapter does not infer successful completion,
cancellation, or a new step from a status snapshot. The existing journal cap and
legacy coverage are unchanged. Completing #389 still requires closing the native
status-transition coverage gaps, production authorization, and SSE composition;
the adapter alone does not make the feature available.
