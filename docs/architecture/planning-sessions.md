# Planning sessions

The onboard planning flow starts from a repository URL and ref, inspects the
source, asks Needs Input questions, writes a plan, and records an Accept
binding. Accept is the control point. No package or image compute runs before
it.

The wire contract is [`api/planning-sessions.yaml`](../../api/planning-sessions.yaml).
Generate the DTOs, strict server, router and Go client with
`go tool oapi-codegen --config api/planning-sessions.codegen.yaml api/planning-sessions.yaml`.
`scripts/verify-planning-api.sh` checks reproducibility in local and hosted CI;
contract tests call the generated client against the generated strict boundary.
OpenAPI 3.0 nullable references preserve null inspection, plan and binding fields.
The native service remains behind the transport adapter.

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


## Inspection

A planning turn clones the intake URL at the given ref into an isolated
directory, allows only that git host, and lets the configured model call
`inspect_repo` and `read_file`. Each tool call is recorded as an
`inspection` event. The audit log stores the tool names and the prompt
and response hashes. No package or image compute runs during this step.

## Needs Input and the plan

After inspection, in the same turn, the model is asked for clarifying
questions (`id`, `prompt`, optional `choices`). Questions with choices become
`select` questions; the rest are `string`. They are stored on the session,
streamed as a `needs_input` event, and read from `GET` on the session. Answer
them with `POST /answers`. Accept stays refused while a required question is
unanswered, and a turn leaves the plan unwritten until they are.

With no open question, the next turn (or the same turn, when the model asked
none) has the model write the plan: summary, base image approach, build steps,
ports, network allow rules, SSO need, and risks. The plan is stored as
canonical JSON in a fixed field order, with ports sorted and de-duplicated, so
`PlanHash` is the same for the same plan whatever the model's formatting. It
streams as a `plan` event.

Both prompts require the model to `read_file` the README, package manifests,
and existing build files before it answers. The audit log records
`planning.questions` and `planning.plan` with the prompt and response sha256,
beside `planning.inspect`.

## Accept binding

Accept records, and never rewrites:

- the onboard profile name, version, and hash
- the intake (`repoUrl`, `ref`)
- the resolved source commit SHA
- the model
- the plan (text and sha256)
- the accept time

`POST /compute` is refused with `COMPUTE_BEFORE_ACCEPT` until that binding exists.
After Accept it returns `COMPUTE_NOT_CONFIGURED` until `CONDUIT_IMAGE_BUILDER=docker`
admits the local engine builder, the engine image inspector, and the zarf package
creator. The recorded digest is the local engine image id unless the image is pushed.

## Ownership and storage

Sessions are isolated by the authenticated credential owner's principal. Other
operators receive 404 for its session ID, including turns, answers, events,
Accept, binding reads, and compute. Bootstrap administrator status does not
implicitly share sessions. Session documents are process-local and disappear
on Hub restart; this is not a durable workspace/session implementation.

Accept writes an authorization audit receipt through the configured repository
before binding the session. Audit-write failure leaves the session unaccepted.
The receipt binds profile coordinates, intake URL/ref and opening-message hash,
source commit, model and plan hash without copying model output or opening
message text into the audit. Ordinary model-phase audit records use the
credential owner's identity as well.
