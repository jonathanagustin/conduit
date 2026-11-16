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
