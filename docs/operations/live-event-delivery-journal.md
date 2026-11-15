# Durable live-event delivery journal

This is the delivery foundation for #389 under accepted
[ADR 0016](../decisions/0016-console-live-events.md). It consumes the existing
`api/live-events/event.schema.json` contract. The Hub mounts the authenticated
SSE route and can compose native outboxes with a single-owner durable journal.
It adds no automatic client subscription, approval channel or execution path.
Producer coverage/retention work remains #389; hooks remain #390. The current
console continues using its existing reads and polling.

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
synthetic resource revision. Repeated positions, invalid coordinates, oversized
pages, rollback/future checkpoints and failed reads retain their checkpoints and
fail closed. Only an explicit verified source-retention gap retires delivery
epochs transactionally and requires visible authorized catch-up, as described
below; it cannot silently skip a lost prefix.

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
run. Runtime composition registers this source alongside the native controlplane outboxes.

Legacy runs without a journal and status-only changes with no recorded step
emit nothing. In particular, this adapter does not infer successful completion,
cancellation, or a new step from a status snapshot. Legacy coverage remains
unchanged. New v4 workflows retain their latest bounded journal as described
below. The runtime configuration is still required to enable delivery.

## Versioned packaging terminal statuses

Marker version 3 introduced recorded terminal status changes
that lack a completed activity step: cancellation during review, exhausted
review-message budgets, and invalid terminal state without an outcome. They
record the actual blocked phase and failure/cancellation outcome, with the same
native journal sequence and timestamp rules. The source projects those recorded
terminal codes; a status snapshot still cannot manufacture a missing entry.
No Plan is approved, cancelled, or executed by recording the packaging status.

Histories marked version 1 or 2 keep their prior journal behavior. The new
status-journal enablement is private workflow state restored from the version
marker after replay/continue-as-new, not an option in caller JSON. A recorded
synthetic version-2 history is replayed in the test suite, and the version-2
cancellation query is tested separately. The real Temporal recovery test also
cancels current-version runs during review, restarts workers/server, compares
source records and replays the cancelled histories.

Version 4 preserves the 32-entry memory/carry bound but retains the latest
window instead of silently stopping when full. Native event sequences remain
positive, monotonic and unchanged across rotation, proposal replacement and
continue-as-new. Terminal transitions can replace an expired prefix; no missing
past event is invented or backfilled. Older v2/v3 histories preserve their
stop-on-full query/carry behavior. Their recorded command histories replay with
the current worker, and a separate full-journal cancellation test constrains
v3 query compatibility against v4 terminal retention.

The source validates the complete native window before classifying any retained
prefix. It uses native sequence coordinates rather than slice indices: a new
consumer establishes a baseline at the first retained event, a caught-up
consumer reads later positions, and a known checkpoint behind the window signals
an explicit source-retention gap. Counter rollback, missing entries inside a
window, malformed coordinates and partial non-native windows do not qualify for
a retention reset. Native controlplane outboxes apply the same distinction.
The packaging views show a retained-prefix notice while preserving original
sequence labels; an expired prefix is not displayed as newly numbered history.


## SSE and native authorization adapters

`liveevents/adapters/httpapi` implements the ADR 0016 response boundary for
`GET /v1/events`. Hub composition mounts it and registers it in the route
authorization inventory, supplying authenticated request context, current native
operator resolution, configured source refresh, and the durable delivery service.
Without a configured journal, the route authenticates and reports unavailable;
importing the adapter alone does not enable delivery.

The adapter authenticates before source reads or subscriber allocation. It
rejects cross-origin requests, query parameters, request bodies and ambiguous
cursor/origin headers. It exposes the ready watermark headers, writes the
committed envelope as SSE `id`/`event`/`data`, and sends comment heartbeats.
Source failures and permission revocation close the response; private errors
never become event bodies. Replay remains available after a failed write or
flush. The delivery service retains its queue, replay and envelope limits.
Writes have a 15-second deadline, renewed for each frame/heartbeat and retained
through response finalization; net/http clears it before connection reuse.
Connections have a 30-minute lifetime. Streaming middleware must
preserve `ResponseController` deadline/flush support (for example via `Unwrap`).
Same-origin checks use the request's TLS state and Host; deployments terminating
TLS at a proxy must provide trusted original-origin handling in composition,
not accept arbitrary forwarded identity or scheme headers.

`liveevents/adapters/nativeauth` derives opaque owner, represented-principal and
authorization hashes from freshly resolved native principals and grants. Grant
ordering, role/operation ordering and storage timestamps do not create new
stream epochs. Carrier/delegate identities, group/team membership, grant scopes,
permissions/revisions and administrator status do. Invalid UTF-8 or excessive
or cyclic delegation fails closed. No credentials, grant tables or usernames
are written into the delivery identity. Every visibility call resolves again;
a changed stamp requires reset before invoking the native resource predicate.

The controlplane bridge exposes current stream eligibility and Plan,
Change Request and reported-environment visibility using existing native read
predicates. Eligibility requires administrator authority or a relevant explicit
native resource-read grant; it does not invent a new role. An author/reviewer
without either remains ineligible for this general stream even though their
existing single-resource reads continue to work. Per-resource checks preserve
Change Request author/reviewer visibility and carrier-only bootstrap authority;
a service account cannot inherit bootstrap authority by representing an
administrator. Missing resources are hidden and unavailable stores fail closed.
Packaging visibility is composed through its owning status reader and native
environment-read decision. Agent-run visibility remains separate; this bridge
grants neither packaging nor agent authority by itself. The resolver must revalidate the
original credential and delegation on every call and must reject absent
operator identity even where development routes have a localhost fallback.

Tests exercise two operators, filtering before visible sequence assignment,
stable cursor replay, permission revocation, source failure, slow clients,
malformed requests, current grant snapshots and delegated native read rules.
This backend runtime does not replace the console's polling or complete #390.
The agent-specific AG-UI stream remains separate; this channel does not emit
model/tool bodies or invent assistant activity from another protocol.


## Runtime configuration and ownership

Set `LIVE_EVENT_JOURNAL_FILE` to an absolute path on a private durable local
filesystem, unique to this Hub replica. The CLI opens a private `.lock` sibling
and takes a nonblocking exclusive process lock before loading the journal.
A second owner, symlink/public lock, corrupt journal or unusable path fails
startup. Shutdown releases the lock, and an OS process exit releases it after a
crash; never delete the lock file while any process may still use it. Journal
files are private (0600). There is no implicit memory fallback or silent
checkpoint discard. Existing state persists across restart on the same volume.

This file adapter is single-owner, not shared multi-replica delivery storage.
Use separate journal paths/volumes for replicas; resuming against another
replica returns 409 and requires authorized catch-up. A shared PostgreSQL native
outbox does not turn the local delivery file into a multi-writer database.

Packaging Temporal must also be configured and readable. Missing workers,
malformed history or source-read failures make the stream unavailable. The Hub
retains its ordinary Plan/read routes when the optional stream is unavailable.
Sources are drained only on an authorized stream request and every five seconds
while connected. Refresh is coalesced across subscribers, has a ten-second
process-context timeout, and transfers at most four 256-record batches per source
per refresh. An operator disconnect cannot cancel another operator's source read.
No periodic background worker is started; native outboxes retain idle writes.
The journal's source-health freshness, bounded live queues, fail-closed gaps and
stable cursor rules still apply. Configuring the stream grants no approval,
execution, publication or outbound agent connection.

The runtime revalidates the original operator credential and current native
membership/delegation/grants instead of caching a request's initial principal.
It preserves the carrier-only bootstrap rule. No raw credential or principal is
persisted in the event journal. It does not trust forwarded scheme/identity
headers or widen CORS. Direct Hub TLS or a same-origin development listener is
the supported origin boundary; TLS-terminating proxies require an explicit
trusted-origin adapter before browser SSE can be enabled through them.


## Explicit reset after a retained source prefix is lost

A verified `ErrSourceRetentionGap` from a trusted configured source triggers one
journal transaction that retires **all** stored identity streams/cursors,
source rows and producer checkpoints together. Active subscribers are closed
with a reset requirement, and all configured sources become unready. The HTTP
request discovering the gap receives sanitized 409 before ready headers. The
next refresh establishes fresh native source baselines, but old cursors still
receive 409, including after a Hub restart. A caller must open a new authorized
stream at sequence 0 and perform the ADR 0016 read-model catch-up. No subscriber
continues over the lost prefix; no lost completion is fabricated. The reset is
resource-agnostic and exposes no source identity, private ordinal or row count.

The reset is global because the delivery journal orders multiple native sources
in one private position space; it is not a permission or per-tenant data change.
It runs outside native write locks and commits before replacing any baseline.
A failed reset write preserves the prior durable evidence and keeps delivery
unavailable. Generic failures, malformed histories, conflicting records and
checkpoints ahead of their source never trigger this recovery or erase evidence;
they require source/storage repair. Existing queue overflow and expired public
replay still require their ordinary stream-specific catch-up.

The full-journal real Temporal recovery test retains a cancellation at native
position 34, restarts worker and server, compares the source projection and
replays the history. Hub integration tests discover a missed prefix, verify 409
and rejection of the retired cursor, then receive the next native terminal
position as sequence 1 of a fresh stream. Storage tests also cover reset rollback,
all-source readiness and restart between retiring an epoch and rebuilding its
source checkpoints.
