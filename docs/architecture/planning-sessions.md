# Planning sessions

The onboard planning flow starts from a repository URL and ref, inspects the
source, asks Needs Input questions, writes a plan, and records an Accept
binding. Accept is the control point. No package or image compute runs before
it.

The wire contract is [`api/planning-sessions.yaml`](../../api/planning-sessions.yaml).
Go types and mounted routes live under `internal/planning`. Other console lanes
build against this contract:

- the chat composer (repository URL and ref)
- `inspect_repo` and `read_file` inspection in the chat
- Needs Input cards
- the model-written plan and the plan review screen
- Accept and the binding panel

## Routes

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/v1/planning-sessions` | Start a session from `repoUrl` and `ref`. Records intake only. |
| `GET` | `/v1/planning-sessions/{sessionID}` | Read the session, including Needs Input, the plan, events, and the binding. |
| `POST` | `/v1/planning-sessions/{sessionID}/turns` | One planning turn. JSON returns the session. `Accept: text/event-stream` streams events. |
| `GET` | `/v1/planning-sessions/{sessionID}/events` | Recorded events in order, including inspection events. |
| `POST` | `/v1/planning-sessions/{sessionID}/answers` | Answer outstanding Needs Input questions. |
| `POST` | `/v1/planning-sessions/{sessionID}/accept` | Record the binding. Refused until a plan is ready and required questions are answered. |
| `GET` | `/v1/planning-sessions/{sessionID}/binding` | Read the Accept binding. |
| `POST` | `/v1/planning-sessions/{sessionID}/compute` | Start package compute. Returns `COMPUTE_BEFORE_ACCEPT` until Accept. |

## Turn stream

A turn may return Server-Sent Events. Each event has `id`, `event` set to the
type, and `data` as the Event JSON. Types are:

- `message` — operator or model transcript line
- `inspection` — source inspection (language, tree, tool calls)
- `needs_input` — outstanding questions
- `plan` — the model-written plan
- `status` — session status after the step
- `error` — a typed failure, with `code` when present
- `done` — the turn stream is complete

`GET /events` returns the same items so a console can poll or replay without a stream.

## Accept binding

Accept records, and never rewrites:

- the onboard profile name, version, and hash
- the intake (`repoUrl`, `ref`)
- the resolved source commit SHA
- the model
- the plan (text and sha256)
- the accept time

`POST /compute` is refused with `COMPUTE_BEFORE_ACCEPT` until that binding exists.
