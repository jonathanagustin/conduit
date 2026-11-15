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
