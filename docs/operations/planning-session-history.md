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
cancel a run. Stop still interrupts browser reading only. Hub-side cancellation
and durable execution fencing remain unfinished in #820.

The tests cover the Temporal SDK boundary with controlled visibility/query
responses, owner and run-id checks, the generated HTTP client/server, composed
Hub ownership and secure-header refusal, browser pagination/deep links, and
stale-owner responses. They do not establish a real Temporal deployment's
visibility latency, retention configuration, scale or restart recovery. No
source control or test result authorizes release, publication or deployment.
