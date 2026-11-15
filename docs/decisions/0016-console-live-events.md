# ADR 0016: SSE for the first console live stream

Status: accepted (2026-11-14), issue #388. Implementation is tracked in
#389 and #390; this decision does not mount a route or start a fan-out service.

Use same-origin server-sent events for the first general operator-console
stream. The browser needs server-to-client invalidations and progress; existing
HTTP commands retain their approval and authorization boundaries. SSE works
with the current HTTP server, supports event IDs and reconnect, and requires
neither a bidirectional socket protocol nor the Connect migration to ship.

WebSocket would require a second command, authentication, heartbeat and reconnect
protocol for a one-way need. Connect server-streaming is a valid later transport,
but making the first stream depend on #381 would couple independent work.
The versioned event envelope is transport-independent: future Connect messages
must preserve its identities, filtering, ordering and resume semantics, and
must not create a second event vocabulary. Dual serving may wrap the same
committed event records. REST commands remain authoritative during migration.

## Contract

`api/live-events/event.schema.json` is the JSON Schema 2020-12 contract.
`api/live-events/resume-fixtures.json` and its Go tests exercise duplicate,
late, out-of-order, gap and changed-stream deliveries. Run
`go test ./api/live-events`; the existing full Go gates run these tests too.
The fixture consumer is a reference algorithm, not a production subscription.

The proposed route is `GET /v1/events`, `text/event-stream`, `Cache-Control:
no-store`, with proxy buffering disabled. It accepts no state-changing commands.
Each SSE `event` is the envelope's `kind`, `data` is its JSON, and `id` is its
opaque `cursor`. Heartbeats are SSE comments, never resource events. Unsupported
schema versions cause a visible reconnect/resync failure, never best-effort
interpretation.

Every envelope has a version, opaque stream ID and cursor, a monotonically
increasing per-stream sequence, UTC occurrence time, resource identity and
revision, and one of these bounded payloads:

| Kind | Meaning | Payload |
| --- | --- | --- |
| `packaging.step` | A recorded packaging step changed | Phase and outcome codes |
| `plan.changed` | A Plan read model changed | Status code |
| `approval.changed` | A Plan approval changed | Approval status code |
| `spoke.reported` | An environment report was recorded | Liveness classification |
| `assistant.activity` | An assistant run changed activity | Activity code |
| `index.invalidated` | Overview/KPI or an area index needs a read | Allowlisted area names |

Resource IDs and revisions identify the owning read model. A Plan revision
must come from its persisted transition, never a browser clock. Packaging
activity does not imply Plan approval. These messages are hints to reload an
authorized read model, not authority to mutate it or proof of successful rollout.
Credentials, tokens, prompt or response bodies, generated files, raw tool
arguments, tenant configuration, unredacted subprocess logs and error messages
are forbidden. Schemas disallow extra fields and arbitrary strings in payloads.
Identifiers are bounded opaque IDs, not places to embed those bodies. A producer
must construct fields from the stored resource, not accept a caller's arbitrary
event JSON. Occurrence time is informational and never orders deliveries.

## Identity, authorization and localhost

The stream uses the existing same-origin operator session and resource visibility
rules. Anonymous requests receive 401, including on localhost; an authenticated
caller with no stream permission receives 403. Development identity headers
retain their explicit insecure-development opt-in and are never enabled by this
feature. Agents do not obtain an operator stream. No CORS widening, new outbound
connection, public listener, credential in a URL, or custom identity supplied
by browser state is introduced.

Authorize before allocating a subscriber. Filter every resource before enqueue,
including replay. Bind a stream and its opaque, integrity-protected cursors to
the credential owner, delegated identity and authorization revision. Cursors
are positions, never bearer authority. Resume with another identity or a changed
authorization revision requires a new stream and authorized catch-up; it must
not disclose whether hidden resource events exist. Revocation closes the stream.
Do not reuse subscriber queues between principals. Resource filtering precedes
assignment of contiguous delivery sequence numbers so hidden events cause no
visible sequence gaps or count leak.

## Ordering, bounded buffering and resume

The producer persists a transition before publishing it. The first committed
visible event has sequence 1; subsequent deliveries increment by exactly one
within the stream epoch. The same event on replay retains its stream, sequence
and cursor. A resource revision cannot move backwards; a later sequence for an
older resource revision is ignored for that resource, while its delivery cursor
still advances. Revisions are scoped to `(resource.type, resource.id)`.

Clients keep their last applied cursor in memory, scoped to the current identity.
They resume with `Last-Event-ID`; a fetch-based SSE client may set that header,
while native EventSource sends it on automatic reconnect. Do not store cursors
in shared local storage or pass them as query parameters. Equal/earlier sequence
numbers are duplicate/late replay and do not reapply side effects. A forward gap
or a different stream ID stops incremental application and initiates catch-up;
never skip a missing event and call the view current.

For the first implementation, cap each subscriber at 256 queued envelopes and
1 MiB of encoded data (whichever comes first), each envelope at 4 KiB, and replay
retention at 10 minutes or 10,000 events per identity stream (whichever comes
first). Use a 15-second heartbeat and a 30-minute connection lifetime. On a full
queue, terminate the connection rather than dropping an event. Missing, expired,
invalid or unavailable replay state yields a reset requirement (HTTP 409 before
SSE headers); it does not silently start at the newest cursor. HTTP 401/403
stops reconnect until the identity is re-established. These are contract bounds,
not claims that the existing packaging history offers a durable global cursor.

Catch-up opens a new authorized stream and buffers it before fetching the
currently visible read models. Read `GET /v1/packaging-runs/{runID}` plus
`GET /v1/packaging-runs/{runID}/events` for packaging detail, and the existing
Plan, approval, environment catalog, overview and KPI read routes for other
views. The future server must supply a stream-ready watermark so that reads
and buffered invalidations cannot lose a transition. Apply only matching-resource
newer revisions after the snapshot; if a read model lacks comparable revisions,
coalesce the buffered invalidations and refetch it instead of guessing. Another
overflow during catch-up restarts the process with backoff and a visible stale
indicator. The exact read adapters and durable outbox are #389, hooks are #390.

## Interaction with agent protocols

The agent run stream in #640 uses AG-UI lifecycle, text, tool and state events.
It is a separate run-specific public protocol, not permission to put model text
or tool bodies into this general operations stream. `assistant.activity` carries
only an activity code and opaque run identity. A2UI surfaces (#641) stay on the
authorized agent channel. Neither channel can approve a Plan or start compute
before its existing Accept control point.
