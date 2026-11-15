# Console live updates

The operator console consumes the Conduit-native SSE contract in
[ADR 0016](../decisions/0016-console-live-events.md). This is a Conduit extension,
not a claim about private Apollo transports. The wire authority is
`api/live-events/event.schema.json`; `tools/liveevents/generate-validator.mjs`
produces the committed browser validator. `pnpm --dir web check:events` and both
console build paths reject stale output. Validation runs without dynamic code
generation under the Hub Content Security Policy.

## Read model integration

The active operator console uses `createLiveHub`, not TanStack Query. Its existing
native read model remains authoritative. `LiveSubscription` in `web/src/api`
opens authenticated same-origin `GET /v1/events`, buffers before initial catch-up,
then awaits authorized model reads before committing a cursor. Catch-up includes
packaging-run event history. Buffered changes are coalesced into native refreshes;
invalidations never copy event payloads into business state or invoke approval.
Existing bundle preview and verification POSTs remain read-only queries.

`useLiveEvents(identity, { catchUp, invalidate })` exposes the same lifecycle to
React views through `useSyncExternalStore`. Callbacks receive an AbortSignal and
must discard cancelled results. `liveAreas(event)` maps events to affected views,
including overview, KPI, packaging, plans, approvals, fleet and assistant.
Imperative consumers can use the Hub bridge's `subscribeLive`, `liveStatus`,
`onLiveEvents` and `retryLive`; event notifications follow successful native reads.
The KPI strip and operations feed can consume these APIs; their presentation work
remains tracked in #373. Reserved assistant activity has no new producer here.

## Recovery and authorization

Cursors, sequence and resource revision watermarks stay in memory per operator
identity and grants; changing identity or stopping clears them and cancels reads.
Normal reconnect resumes only the last successfully applied cursor. Duplicates
and older resource revisions are ignored; gaps, changed epochs, HTTP 409 and
queue overflow require a fresh connection and authorized catch-up. Nothing is
stored in localStorage or sent as an identity query parameter.

The client bounds buffered plus in-flight events to 256 frames and 1 MiB, limits
revision metadata to 10,000 resources, validates 4 KiB envelopes, times out
handshake and callbacks after 20 seconds, and detects idle streams after 45
seconds. Retry uses exponential backoff with jitter (up to 37.5 seconds).
HTTP 401/403 and unsupported contracts stop automatic reconnect until explicit
retry or an identity change. Error bodies never become status text.

The header reports syncing, live, polling or authorization failure. A healthy
stream pauses the 30-second fallback refresh; unavailable streams retain native
polling and expose Retry live. The indicator turns live only after catch-up and
buffered invalidations succeed. It does not claim event coverage for unrelated
catalog surfaces or grant write authority. Native reads remain subject to current
server authorization, including a session check before and after refresh.

## Verification

Unit tests cover chunked framing, schema fixtures, reconnect, applied cursors,
duplicates, ordering, gaps, bounded queues, idle detection, identity changes and
authorization failures. Browser tests exercise native Plan refresh under the Hub
CSP, visible denial and fallback polling, and assert that viewing events sends no
approval writes. These browser stream fixtures complement server endpoint and
Temporal recovery tests; they do not claim a live tenant integration.
