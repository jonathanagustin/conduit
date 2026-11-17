# Governed planning runs over AG-UI

The Hub exposes `POST /v1/agent-runs` for the existing owner-scoped planning
session. Create that session with `POST /v1/planning-sessions`; its ID is the
AG-UI thread ID. Send the canonical `RunAgentInput` as JSON and accept
`text/event-stream`. Authentication and owner-store selection precede parsing.
An administrator cannot use this route to open another owner's session.

The wire authority is the primary AG-UI 1.0 JSON Schema at
`ag-ui-protocol/ag-ui@1f694eba3a4f9cbdda1e9feded8b3edba3d3de2c`, retained with
its MIT license and source hashes under `api/agui/upstream`. In this snapshot
resume statuses are `resolved` and `cancelled`; outcomes are discriminated
objects. No SDK implementation is vendored and no dependency version changes.
This is a Conduit planning profile, not a claim of universal SDK compatibility.

## Ownership and authority

The route resolves the same credential owner and the same session service used
by the native planning API. The run port has only Get, Turn and Answer methods.
It cannot Accept a plan or start compute. Client history, context and state are
untrusted grounding, not replacement session state or a grant. Unsupported
media parts are omitted from the text-only model context; the Hub does not
fetch their URLs or resolve opaque provider handles.

Planning turns, answers and Accept serialize per session. Other sessions and
reads remain available. A changed answer invalidates the old draft; an
identical answer preserves it. The operator must obtain a new plan before
Accept after changing answers. Existing Accept audit/binding and compute gates
remain authoritative.

## Exchanges and recovery

Each accepted exchange starts with RUN_STARTED and has one terminal event.
Text and tools have explicit boundaries. Tool arguments must finish before a
result, and successful finish cannot leave a text, step or argument stream
open. The publisher previews ordering and commits its audit record before
publishing the event. State starts with STATE_SNAPSHOT; changed top-level
members use RFC 6902 STATE_DELTA. MESSAGES_SNAPSHOT carries ordered, stable
identities for user messages, tool requests/results and native model replies.

The public HTTP/SSE binding does not resume a broken stream. Reconnect with a
**new run ID** and `forwardedProps: {"conduit":{"mode":"snapshot"}}`. This
Conduit extension waits for an active prior run to settle, sends fresh state and
history, and skips the model and answer mutation. Duplicate run IDs are refused.
A truncated stream is incomplete, never inferred successful from HTTP 200.

A required Needs Input form is one interrupt whose response schema describes
all its questions. The interrupt ID binds the session, source commit, question
definitions and existing answers. A resolved resume must provide all required
values and no unknown fields. Validation completes before Answer runs; a stale
or partial resume changes nothing. A cancelled entry stops that run without
answering, accepting or computing. Uncovered required input remains an
interrupt and cannot advance the model.

## App-defined form tools

The profile supports `fill_field`, `clear_field`, `focus_field` and
`request_confirmation`. The console offers their parameter schemas for each
run. The Hub intersects those schemas with its canonical schemas and
server-owned field metadata. A permissive client schema cannot add a field,
change its type/choices, or make the repository/ref editable. Parsed question
fields use the `question:<id>` namespace. Definitions become eligible only
after the Hub validates the model's question output.

The local model adapter may include `clientToolCalls` alongside its final
questions/plan JSON. Successful admission hands the phase off rather than
continuing the next model phase without results. The terminal success outcome
names pending client calls. Their arguments and the final native state have
already been validated; a client executes draft operations only after the run
successfully closes. Error terminals discard queued operations.

Results return as tool messages, with JSON-string content containing `ok` and,
for successful confirmation requests, `confirmed`. Matching results are
idempotent; conflicting repeated results are refused. These are client-reported
draft outcomes. They never become planning answers, acceptance or execution
permission. A declined confirmation stops continuation. Human input is still a
separate bound resume. After a failed or truncated exchange, recovery eligibility
is explicit; a client must not blindly repeat draft effects or overwrite edits.

## Audit and limits

Published tool/state events require an audit receipt containing the
credential owner, thread/run IDs and content SHA-256. Native model phases retain
prompt/response digests with the same correlation. Raw prompts, field values,
file contents and model bodies are not copied into the audit log. A failed
audit prevents tool admission/publication. RUN_ERROR reports an audit outage
if a failure receipt itself cannot be persisted; it is never a success receipt.
Network write failure stops tool admission and leaves a truncated exchange.

Separately, every model call -- each planner round and each assistant round,
a successful text-only reply included -- writes one `ai.model_call` audit event
with the SHA-256 of that round's exact prompt and response bytes, chained to
the previous call of the same run. The event holds no text either. Text is
kept only by the opt-in transcript store, redacted, for a set retention, and
only an administrator can export it. A model call whose event cannot be
written fails. See
[model usage, limits and AI transcripts](../operations/ai-usage-and-transcripts.md).

This adapter uses the same planning session service as the native routes. Its
run ledger is process-local and does not resume a broken stream after a Hub
restart. The initial bounds
are 1 MiB input/event, 4 MiB total event data per exchange, 256 input messages,
32 bounded context entries, four offered form tools, 8 KiB tool arguments and
16 KiB offered schemas. Numeric/depth bounds are in the codec. Per owner, the
process retains at most 128 threads and 1024 run-ID tombstones. Reaching a bound
fails explicitly; it never truncates an authority-bearing document into PASS.

The HTTP adapter flushes SSE, disables response caching/proxy buffering, sends
keep-alive comments while running, and bounds a run to thirty minutes. Workspace coding-agent sessions over ACP mirror validated tool and file-edit events onto AG-UI threads (`wsp-acp-*`) as documented in [workspace-acp-connections.md](workspace-acp-connections.md).

## Work Intake client

The console creates a native planning session, then uses AG-UI for model turns
and human form resume. Accept remains a separate explicit native request. The
client checks the Hub explicit-start/end ordering, replaces state/message
snapshots, and applies all six RFC 6902 operations atomically. Invalid paths,
prototype traversal, failed tests and partial patches leave the old state intact.
The client rejects malformed, oversized or truncated exchanges rather than
falling back to a second native model turn. Its Recover action uses a fresh run
ID with `forwardedProps.conduit.mode = "snapshot"`.

The four browser tools offer the same owned schemas as the Hub (a drift test
compares them). Existing Zod validates both the canonical and offered schema.
Only current question drafts are writable; repo/ref and answered forms are
read-only. Human drafts win over model suggestions, including deliberately
cleared fields. Confirmation is human feedback, never an Accept operation.
Tool effects wait for a successful terminal handoff or an explicitly eligible
recovery request. Results are acknowledged in a new run, with a maximum of five
exchanges per action. The call-ID ledger deduplicates effects within this page;
it is not durable across browser restart. Unknown, malformed, unoffered and
conflicting calls produce refused tool results. Backend inspection calls are
progress data only and are never executed by the browser.

The console gate runs the real JavaScript controller against the mounted Hub
over loopback HTTP with a scripted planner: prefill, acknowledgement, interrupt,
snapshot recovery, human override and an unaccepted plan. This fixture uses the
explicit insecure-header test configuration, not production authentication or
a live provider. Unit replay covers dropped/invalid streams and schema policy.
Browser interaction tests separately exercise the Work Intake DOM with owned
wire fixtures. None of these fixtures establishes live-model, tenant, cluster,
publication or workspace-agent qualification.

## Assistant turns and threads

The console assistant is a second producer of the same AG-UI event vocabulary.
It is not a planning run: it has its own routes, no `RunAgentInput`, no form
tools and no resume. What it shares is the event schema under `api/agui` and
the Hub ordering profile `agui.Sequence` checks, which every event passes
before it is written. It does not share the planning publisher, which is
private to `internal/planning/agentstream` and audits per event; the assistant
audits per tool call instead, as it did before.

### Routes

| Route | Decision | Purpose |
| --- | --- | --- |
| `POST /v1/assistant/tools` | operator-any | run one tool a person submitted, including a Confirm |
| `POST /v1/assistant/turns` | operator-any | one message; JSON or, with `Accept: text/event-stream`, SSE |
| `GET /v1/assistant/threads` | operator-self | the caller's threads, newest update first, no messages |
| `POST /v1/assistant/threads` | operator-self | open an empty thread; body `{}` or `{"title": "..."}` |
| `GET /v1/assistant/threads/{threadID}` | operator-self | one thread with its messages |
| `DELETE /v1/assistant/threads/{threadID}` | operator-self | delete it; `204` |

A thread belongs to the authenticated username. A thread that does not exist
and a thread another operator owns are the same `404 THREAD_NOT_FOUND`.

### Supported assistant writes and execution

The Conduit-native `governed_write` allowlist is `add_eval_case`,
`approve_change`, `reject_change`, `cancel_packaging_run` and
`rerun_packaging_run`. Every write has a server action rule. Eval feedback
requires Developer or Operator authority and the existing explicit confirmation;
it calls the real case service. Review calls use the original authenticated
principal, including its delegation constraints, and the existing independent
reviewer rules. Read/query/diff tools remain available.

Compatibility change for #812: `promote`, `recall`, `propose_memory`,
`select_course` and `start_packaging_run` are no longer advertised or accepted.
They have no executable assistant contract and return `400 TOOL_INVALID`.
`propose_governed_action` accepts only cancel/rerun of a PackagingRun. Generic
ontology action names can no longer mint inert assistant proposals. The console
does not offer the removed memory write. Intelligence's separate scoped memory
course proposal remains an intent-only feature, described in
`intelligence-view-controls.md`.

Packaging proposals use target kind `CONDUIT_NATIVE_ASSISTANT_ACTION` and
`conduit.assistant-action/v1` with tier `CONDUIT_NATIVE`. The server resolves
the run, verifies Environment/installation/Product against current desired fleet
state, and records its source digest, proposal revision, target reference,
template and tool versions. Those exact coordinates and the author determine
proposal identity. An unknown or mismatched target is refused; the browser
cannot substitute the first unrelated installation from another screen.

Unattended dispatch re-resolves the author's current server-owned membership and
resource grants. Saved token Groups/Teams are not replayed as credentials.
Group-only or delegated authors cannot use this path; they can use directly
authenticated packaging controls. An unconfigured worker refuses proposals.
These restrictions are Conduit policy, not an Apollo compatibility claim.

Independent approval commits before dispatch. A durable execution claim is
recorded before the shared packaging control service sends a worker command.
The service rechecks the current target and author, writes the exact-bound
decision audit and preserves the ordinary control route's handoff and replay
guards. Neither cancellation nor rerun cancels an existing Plan. Historical
assistant `PLAN` records with only `actionId` remain intent-only: missing binding
evidence is never invented to execute them.

Execution receipts distinguish `CANCEL_REQUEST_ACCEPTED` (the worker accepted a
request; termination has not been observed) from `CANCELLATION_OBSERVED`.
`RUN_STARTED` acknowledges the new run; `RUN_OBSERVED` reports an existing exact
successor. A successful dispatch receipt does not establish application health
or delivery. Failures are stored as failed executions; REST returns the stored
review and execution receipt with its failure response, and assistant review
results distinguish the recorded review from its execution outcome.

A process death or lost receipt can leave execution `RUNNING`; this is visible,
not a success. A still-eligible reviewer who already approved can explicitly
repeat approval with a rationale to recover the same bound operation. Native
reviews bind both credential carrier and effective human; changing an
on-behalf-of identity cannot reuse the earlier review. Recovery
rechecks authority and target, repeats only an idempotent cancel request, or
observes/starts the same deterministic successor workflow ID. It never carries
the old proposal revision into the new run or creates another successor on a
lost acknowledgement. This is retryable command dispatch, not a claim of
exactly-once external effects. Rejected/cancelled or changed targets cannot be
recovered this way. No automatic recovery is scheduled.

Overlapping recovery attempts can submit the same idempotent command. A stored
successful receipt is canonical: a later failure returns that success and
cannot replace its result or advance its revision. An acknowledgement that
arrives after a competing failure can still record success for the same exact
bound operation. Failures cannot overwrite a newer attempt's receipt.

### The turn

Request: `{"mode", "message", "context"?, "threadId"?}`. `threadId` is the
only new field and is optional. Without it the turn reads and writes no
conversation state, and its first prompt is byte-for-byte what it was.

Response, `200`:

```json
{
  "label": "Live model call",
  "text": "One environment: zeta.",
  "provenance": {"Adapter": "...", "PromptSHA256": "...", "ResponseSHA256": "..."},
  "results": [{"tool": "query_ontology_objects", "ok": true, "...": "..."}],
  "toolCalls": [{"id": "call_1_1", "round": 1, "name": "query_ontology_objects", "arguments": {"objectType": "Environment"}}],
  "rounds": 2,
  "threadId": "ath_...",
  "model": {"kind": "openai-http", "model": "..."}
}
```

`label`, `text`, `provenance` and `results` are unchanged in shape. `text` is
now the model's last text, after it has read the tool results. `toolCalls[i]`
is the call that produced `results[i]`, with the arguments the server ran.
`threadId` is present only on a thread turn. `model.kind` is the adapter;
`model.model` is present only when the adapter reads a model id from the
provider. `provenance` describes the last model call.

A turn is a loop. The Hub calls the model, runs the tool calls in the reply,
and sends the results back, until a reply has no tool calls. The bounds are
four model calls per turn, three tool calls per reply and eight per turn; the
ninth is not run and the model is told. The fourth call has to answer: tool
calls in it are not run and the turn fails with `ROUND_CAP`.

Tool results, the model's own earlier replies and earlier turns of a thread
are written into the prompt as JSON lines between
`<<<UNTRUSTED_HUB_DATA>>>` and `<<<END_UNTRUSTED_HUB_DATA>>>`, under a standing
rule that nothing between the markers is an instruction. Marker runs inside
the data are escaped. This is the planner's framing, applied to Hub data.

Governance holds in every round: the mode's tool allowlist, the action rules,
the refusal to approve one's own change request, and one audit entry per tool
call. A turn stops at the first tool call whose audit entry cannot be written.
One rule is new. Once the model has read anything the operator did not type in
this message (a tool result from an earlier round, or earlier turns of a
thread), its tool calls are *held*: a `confirm` flag on a `governed_write` is
discarded, and an `approve_change` or `reject_change` returns `needsConfirm`
with the elicitation card instead of recording the review. A held call can
still open a change request, which needs a separate approver. The person
confirms through `POST /v1/assistant/tools`, as before. The first round of a
turn with no thread history is not held and behaves as it did.

### Threads

A thread turn loads the thread, puts the newest earlier messages in the prompt
(at most 20 messages and 24 KiB of text, an unbroken tail, oldest dropped
first), runs the loop, and appends two messages: the operator's, and the
assistant's with its tool calls, their arguments and their results. A turn
that fails after the model was called is appended too, with `errorCode` on
the assistant message, because its tool calls ran.

```json
{
  "id": "ath_0123456789abcdef0123456789abcdef",
  "title": "which environments exist?",
  "createdAt": "2026-11-17T09:00:00Z",
  "updatedAt": "2026-11-17T09:00:07Z",
  "messageCount": 2,
  "messages": [
    {"role": "user", "text": "...", "mode": "explore", "createdAt": "..."},
    {"role": "assistant", "text": "...", "mode": "explore", "createdAt": "...",
     "toolCalls": [{"id": "call_1_1", "round": 1, "name": "...", "arguments": {}, "result": {}}],
     "errorCode": "ROUND_CAP"}
  ]
}
```

`GET /v1/assistant/threads` returns `{"threads": [...]}` with the same fields
and no `messages`. A thread with no title takes its first message, cut to 120
bytes. Bounds: 50 threads per owner (`409 THREAD_LIMIT`), 200 messages per
thread (`409 THREAD_FULL`, checked before the model is called), 16 KiB of text
and 64 KiB encoded per message. A stored assistant message over the encoded
bound keeps its text and replaces each tool result with `{"truncated": true}`.

The store is a port with two adapters that run one conformance suite.
`threadpostgres` (ledger component `assistant-threads`, migration
`0001-assistant-threads`) is the durable one. `threadmemory` is what the Hub
uses when `composition.Options.AssistantThreads` is nil: threads then live in
one process, are lost on restart and are not shared between replicas.
`cmd/platform-delivery` does not yet open the PostgreSQL adapter, so that is
the current state of a deployed Hub.

### Streaming

`POST /v1/assistant/turns` with `Accept: text/event-stream` and the same body
answers with `data: <event JSON>` lines. The events are the ones that have
really happened; the model ports are request and response, so there is no
token stream.

| Event | When | Fields beyond `type` |
| --- | --- | --- |
| `RUN_STARTED` | before the first model call | `threadId`, `runId`, `protocolVersion` |
| `STEP_STARTED` / `STEP_FINISHED` | around each round | `stepName`: `round-1` ... `round-4` |
| `TOOL_CALL_START` | a tool is about to run | `toolCallId`, `toolCallName` |
| `TOOL_CALL_ARGS` | once per call | `toolCallId`, `delta`: the whole argument JSON |
| `TOOL_CALL_END` | arguments complete | `toolCallId` |
| `TOOL_CALL_RESULT` | the tool returned | `toolCallId`, `messageId`, `content`: the result object as a JSON string |
| `TEXT_MESSAGE_START` / `_CONTENT` / `_END` | the answer, once, whole | `messageId`, `role`, `delta` |
| `RUN_FINISHED` | success | `threadId`, `runId`, `result`: the JSON turn document |
| `RUN_ERROR` | failure after `RUN_STARTED` | `code`, `message` |

`runId` is `arun_` plus 16 hex characters. `threadId` is the thread's id, or
the `runId` again on a turn with no thread. A failure before `RUN_STARTED`
(authentication, a bad body, no model, an unknown or full thread) is an
ordinary JSON error with its HTTP status, not a stream. After `RUN_ERROR` the
tool results already sent stand; on a thread turn the partial turn is in the
thread. The stream sends a `: keep-alive` comment every fifteen seconds and
is bounded to ten minutes.

Both forms run on the request context. Closing the connection cancels the
model call in flight; a thread turn cancelled this way is still appended,
with `errorCode` `CANCELLED`.

### Errors

Every failure body is `{"code", "error"}`. `MODEL_NOT_CONFIGURED` also carries
`"label": "No model call"`, as the `422` always did. A failed turn that got as
far as a model call also carries `"turn"`, the partial turn document.

| Code | Status | Meaning |
| --- | --- | --- |
| `MODEL_NOT_CONFIGURED` | 422 | no model adapter is configured |
| `MODEL_FAILED` | 502 | the model call failed |
| `MODEL_TIMEOUT` | 502 | the model call timed out |
| `MODEL_REPLY_INVALID` | 502 | no valid reply object in the model's output |
| `ROUND_CAP` | 502 | the model was still calling tools in its last round |
| `MODEL_BUSY` | 429 | a model concurrency limit stayed full; `Retry-After` is set |
| `MODEL_BUDGET_EXCEEDED` | 429 | the caller's daily model budget is spent; `Retry-After` is the time to 00:00 UTC |
| `AUDIT_UNAVAILABLE` | 503 | a tool call, model call or reply could not be audited; the turn stopped |
| `THREAD_NOT_FOUND` | 404 | no such thread for this operator |
| `THREAD_LIMIT` / `THREAD_FULL` | 409 | a store bound was reached |
| `THREADS_NOT_CONFIGURED` | 503 | no conversation store |
| `INVALID_REQUEST` | 400 | malformed body, empty message, or message over 16 KiB on a thread turn |
| `TOOL_INVALID` | 400 | `POST /v1/assistant/tools` with an unknown tool or bad arguments |
| `UNAUTHORIZED` | 401 | no operator credential |
| `CANCELLED` | — | the request went away; seen as `errorCode` in a thread |
| `TURN_FAILED` | 502 | anything else |

The reply parser accepts the reply object alone, inside a Markdown fence, or
after prose or log lines, preferring the last object, which is what the
planner's parser does. The object itself is still decoded strictly: an unknown
field, an unknown tool or bad arguments fail the turn.
