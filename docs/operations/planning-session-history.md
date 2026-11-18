# Planning session history

`GET /v1/planning-sessions` lists native summaries for the authenticated
operator. The page is explicitly marked `CONDUIT_NATIVE`; this is not a recovered
upstream protocol. The caller cannot select another owner. The existing Work Intake view
shows this inventory beside app generations and opens sessions through their
existing deep links. A summary's status is recorded state, not approval or a
successful gate result. Prompts, answers, plans, messages and events are omitted.

The default page has 100 summaries; `limit` accepts 1 through 200. Results are
ordered by session id and return `total`, `pageSize`, `limit`, `hasPrevious` and
available continuation cursors. Authorization precedes counts and pagination.
Cursors seek within the caller's currently available inventory; they are not
credentials or a snapshot. A cursor whose session is no longer available is
refused with 400, so refresh the inventory rather than infer a complete history.

## Storage and limits

The production reader uses the configured Temporal client's visibility API.
It selects the newest listed execution per workflow and pins ownership and
session queries to that exact run id. Only an owner matching the authenticated
operator permits the session-payload query. A successful empty-owner result is
excluded; an unavailable or malformed ownership query fails the entire read.
The returned `workflowRunId` identifies the observed execution. No custom search
attribute, new database or process-local inventory fallback is installed.

Temporal visibility can lag changes, and namespace retention can remove old
executions. Opening a session reads its current state separately. This endpoint
is not a linearizable or permanent-history index. It refuses an inventory beyond
1,000 scanned executions, including other owners and continued executions,
rather than return a shortened successful catalog. Repeated continuation tokens,
invalid execution identity, unavailable state and oversized inventories fail
closed. This bound is not production capacity qualification; that remains #849.

Direct workflow reads and update validation also require a nonempty caller and
verified owner. Missing legacy ownership does not grant shared access. There is
no guessed reassignment or administrator exception.

The development service reads its authenticated owner's process-local store.
Its history disappears with that Hub process. An unavailable production reader
does not silently switch to this development store. Unsupported listing returns
501; unavailable authoritative state returns 502, rather than an empty success.

The browser discards superseded and old-owner list responses. Refreshing or
paging repaints the inventory panel without replacing the composer. An empty
state is shown only after a valid empty page; refusal or invalid metadata is an
explicit unavailable state.

## Cancellation and evidence boundary

Listing, paging and opening a link do not invoke a model, approve a Plan or
cancel a run. During a turn or answer exchange, Stop captures the exact logical
run id, stops browser reading and separately requests
`POST /v1/planning-sessions/{sessionID}/turns/{runID}/cancel`. The generated route
selects the authenticated owner before parsing input; requests cannot choose an
owner or substitute another run in the body. Agent-stream correlation binds
turns and answers to that exact id. A cancellation fence prevents later answers
or model results from changing the session, including when cancellation arrives
before the turn starts. Already committed answers and plans are retained.

The console distinguishes an acknowledgement still being requested, a recorded
`cancellation_requested`, a terminal `cancelled`, and a turn already `completed`
or `failed`. It never presents a failed, unknown, mismatched or missing receipt
as confirmed cancellation. Check cancellation repeats the same exact-run request
for its recorded state. Responses are discarded after the owner, session or
selected operation changes. Snapshot recovery is read-only: stopping it only
stops reading and sends no model-turn cancellation. Stopping during session
creation prevents a late response from starting a run, but does not undo a
session creation already sent to the Hub.

Production records the fence through `planning.CancelTurn` in workflow history,
requests activity cancellation independently of the turn mutex, and rejects late
results before changing session state. `cancellation_requested` is a recorded
fence, not a completed cancellation. The handler later records `cancelled`.
That terminal state describes the durable handler, not proof that an external
provider terminated its work. Updates without logical run ids retain their old
behavior and cannot be cancelled by guessing an id. Accepted compute is outside
this cancellation contract.

The development store applies the same exact-run and late-result guards in its
owner-partitioned, process-local state. Its history and receipts disappear with
that Hub process. It is never a fallback for unavailable production workflows.
A port without cancellation support returns 501; unavailable or invalid
acknowledgements return 502. Both implementations retain at most 512 logical turn
ids per session, including tombstones and failures. Workflow continue-as-new
carries them forward; reuse and overflow are refused rather than evicting a
fence. This finite native safety bound is not capacity qualification.

Tests cover the Temporal SDK workflow boundary and continued workflow inputs,
generated HTTP client/server, actual development-store cancellation with a late
planner result, owner partitioning, browser stream abort, exact-run request and
requested-versus-terminal rendering, and late owner/session/operation responses.
They do not establish real Temporal restart/replay behavior, provider termination,
visibility latency, retention configuration or scale. No source control or test
result authorizes release, publication or deployment.
