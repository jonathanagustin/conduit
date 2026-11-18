# Engineer conversation focus

The assistant's Focus button and the Developer workspaces entry open a full
workspace view of the existing conversation. Return, Escape, Studio navigation,
or closing the dock restore the ordinary layout. Dock width preferences are
retained. Background workspace/navigation elements become inert while covered;
their previous inert state is restored on exit and teardown. Root overflow is
clipped during focus so disclosure focus cannot horizontally scroll the hidden
background and move the dock outside a narrow viewport.

This is Conduit-native presentation for the engineer-workspace portion of #558.
It reuses the existing assistant-ui runtime and portal mount. Entering or leaving
focus submits no model turn or write request, creates no thread, and never copies
messages or drafts. Opening the existing dock may load its ordinary read data.
The turn outline subscribes to the active runtime's user messages and navigates
to assistant-ui's pinned message identifiers. Counts describe conversation turns,
not completed work or executions. Text remains React-escaped and navigation
respects reduced-motion preferences.

Source, Diff and Preview links return to the existing Studio. File/command
authority and confirmation/change-request paths are unchanged. Focus does not
establish an isolated coding workspace or a live ACP editor connection. Those
remain the runtime/editor issues. Summarize and Fork remain follow-on work.
Removal already uses the existing saved-conversation bar: select Delete, then
confirm Delete conversation. This sends the owner-scoped Hub deletion request;
it is not a second focus-only deletion path. A running turn disables deletion,
and a Hub refusal remains visible rather than removing the conversation locally.
This slice does not close #558.

The read-only Capabilities/Skills/Docs tray uses native disclosure controls in
focus view. Capabilities describes only tools already shown by the selected
console mode, using the same mode filter as the context actions. It does not
assert caller permission, model configuration or a complete Hub tool catalog.
The Hub still authorizes every request. Skills has an explicit unavailable
inventory state rather than fabricated installed entries. Docs links to this
guide, Studio scope and identity/governance; links open separately with no opener.
Opening or closing disclosures sends no request and owns no conversation state.

## What the Hub serves behind the conversation

The console posts one message per turn through the existing streamed assistant
route. Its saved-conversation bar creates, lists, reopens and deletes the
operator's Hub-backed threads. The browser remembers the selected thread id;
it reads the messages back from the Hub after reload. PostgreSQL provides
durable thread storage when configured; the memory adapter remains process-local
and loses threads on restart. The route, ownership and storage contract is in
[`agent-runs.md`](agent-runs.md#assistant-turns-and-threads):

- A turn is a bounded tool loop. Tool results go back to the model until it
  answers in text, for at most four model calls and eight tool calls.
- `POST /v1/assistant/turns` takes an optional `threadId`. With one, the Hub
  reads the earlier turns into the prompt and appends this one. Threads are
  private to the operator who opened them.
- The same route streams AG-UI events when asked with
  `Accept: text/event-stream`, and closing the connection cancels the model
  call.
- A failure carries a stable `code` beside its message.

Retrieved-source evidence is also supported. The native documentation tool
searches five embedded references, and permission-filtered ontology reads can
retain excerpts. The console renders audited source coordinates, quoted text,
digests and retrieval metadata in disclosures; unavailable, empty and truncated
evidence remain distinct. These records establish what was retrieved, not that
every answer statement is supported. They do not search arbitrary repository
files or make an answer correct. See the
[retrieved-excerpt contract](agent-runs.md#retrieved-source-excerpts).

Turn metadata names the provider-reported model when available and labels
provider-reported input/output tokens. Missing token counts remain unknown;
cost is not estimated. Preferences separately exposes the operator's own daily
Hub usage and allowance as an explicit snapshot, with the limitations in
[AI usage and transcripts](../operations/ai-usage-and-transcripts.md).

The existing conversation bar offers **Fork conversation** for a saved thread.
`POST /v1/assistant/threads/{threadID}/fork` copies only the caller's saved
messages into a fresh owned conversation, with the existing per-owner thread
and per-thread message limits. The source is unchanged, and a copy remains
readable after its source is removed. Tool records, evidence and reported usage
retain their historical meaning: copying invokes no model, executes no tool and
adds no new model usage. It is not a Git branch or durable lineage graph.
The memory store remains process-local; PostgreSQL copies the complete snapshot
in one transaction when configured. Late responses cannot replace a different
conversation selected while the copy is running.

**Summarize recent history** sends an explicit Explore-mode request through the
existing streamed turn path for the current saved conversation. It requires a
configured model and uses normal usage/error reporting. The Hub supplies its
bounded saved-history window (currently at most 20 messages and 24 KiB), so this
is not a complete-conversation or answer-correctness guarantee. The request asks
for decisions, unresolved questions, next actions and context gaps. Explore mode
excludes proposal and write tools; it does not create a separate approval path.
An unsent composer draft stays in the composer. Empty, loading, running and
unsaved conversations cannot submit the summary action.

Confirmed removal
uses `DELETE /v1/assistant/threads/{threadID}`; another owner's thread and a
missing thread produce the same not-found response. Removal is a local Hub
conversation operation, not identity-provider logout or transcript-store
retention/deletion authority.

Verification includes the same mount/composer identity, preservation of unsent
drafts and in-flight requests, real turn counts and outline focus, Escape/return,
Studio navigation, narrow layout, and absence of model/tool writes from layout
changes. The complete exact-head local console and repository gates remain
required before merge.
