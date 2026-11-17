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
remain the runtime/editor issues. Summarize, Remove and Fork remain follow-on UI
work; no unsupported actions are presented as working controls. This slice does
not close #558.

The read-only Capabilities/Skills/Docs tray uses native disclosure controls in
focus view. Capabilities describes only tools already shown by the selected
console mode, using the same mode filter as the context actions. It does not
assert caller permission, model configuration or a complete Hub tool catalog.
The Hub still authorizes every request. Skills has an explicit unavailable
inventory state rather than fabricated installed entries. Docs links to this
guide, Studio scope and identity/governance; links open separately with no opener.
Opening or closing disclosures sends no request and owns no conversation state.

## What the Hub serves behind the conversation

The console still posts one message per turn and keeps its thread in the
browser. The Hub side of #811 is in place for it to move onto; the contract is
in [`agent-runs.md`](agent-runs.md#assistant-turns-and-threads). In short:

- A turn is a bounded tool loop. Tool results go back to the model until it
  answers in text, for at most four model calls and eight tool calls.
- `POST /v1/assistant/turns` takes an optional `threadId`. With one, the Hub
  reads the earlier turns into the prompt and appends this one. Threads are
  private to the operator who opened them.
- The same route streams AG-UI events when asked with
  `Accept: text/event-stream`, and closing the connection cancels the model
  call.
- A failure carries a stable `code` beside its message.

Not done on the Hub: grounding over documentation with citations, and token
or cost figures. The turn names the adapter that served it, and the model id
when the adapter reports one. Summarize, Remove and Fork still have no Hub
operation behind them.

Verification includes the same mount/composer identity, preservation of unsent
drafts and in-flight requests, real turn counts and outline focus, Escape/return,
Studio navigation, narrow layout, and absence of model/tool writes from layout
changes. The complete exact-head local console and repository gates remain
required before merge.
