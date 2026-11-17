# Model usage, limits and AI transcripts

Every model call the Hub makes passes through one governor (`internal/modelgovernor`), whichever capability makes it and whichever transport carries it. The governor bounds concurrency, retries a failure that is safe to retry, meters the call, enforces an optional daily budget, writes one audit event, and keeps an optional transcript. It does not read a model reply and it does not approve a change.

## What is covered

| Capability | [HTTP endpoint](model-http.md) | [Model command](local-model-cli.md) |
| --- | --- | --- |
| `assistant` (every round of a turn) | yes | yes |
| `planning` (every round, on a request and in the Hub's workflow worker) | yes | yes |
| `appgen` | yes | yes |
| `intelligence` (live evals) | yes | yes |
| `packaging` (proposals and the package writer, in the Hub) | yes | yes |

The separate `packaging-workflow` worker process applies the concurrency limits and the retry policy to its planning and packaging calls and counts them in process. It has no audit log, no usage store and no operator, so it enforces no budget, writes no per-call audit event and keeps no transcript. Its calls do not appear in the usage routes.

A call made on an operator's request is attributed to that operator. A call with no operator, such as a planning turn run by the workflow worker, is attributed to `system:unattributed`.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `CONDUIT_MODEL_MAX_CONCURRENCY` | 8 | Model calls in flight in one process. |
| `CONDUIT_MODEL_MAX_CONCURRENCY_PER_OWNER` | 2 | How many of those one owner may hold. |
| `CONDUIT_MODEL_QUEUE_WAIT` | 2s | How long a call waits for a slot before it is refused. |
| `CONDUIT_MODEL_MAX_ATTEMPTS` | 3 | Attempts per call, the first included. |
| `CONDUIT_MODEL_DAILY_TOKEN_BUDGET` | unset: unlimited | Tokens one owner may use per UTC day. |
| `CONDUIT_MODEL_DAILY_CALL_BUDGET` | unset: unlimited | Model calls one owner may make per UTC day. |
| `CONDUIT_AI_TRANSCRIPTS` | `off` | `on` keeps prompt and response text. |
| `CONDUIT_AI_TRANSCRIPT_RETENTION` | none | Required when transcripts are on, for example `720h`. There is no default for how long tenant text is kept. |

A value that does not parse, or transcripts `on` with no retention, stops startup.

## Limits and refusals

A call takes its owner's slot and then a global one. If either stays full for the queue wait the call is refused; a caller that cancels while waiting holds no slot afterwards. Both limits are per process: two replicas allow twice the global number.

A refused call answers **429** with `Retry-After` on every route that would have made it, whatever that capability does with other model failures:

| Code | Meaning | `Retry-After` |
| --- | --- | --- |
| `MODEL_BUSY` | a concurrency limit stayed full | the queue wait |
| `MODEL_BUDGET_EXCEEDED` | the owner's daily budget is spent | seconds until the next UTC midnight |

A response that already started, such as an event stream past its first event, is not rewritten; the refusal is then in the stream's own error.

## Retries

| Failure | Retried |
| --- | --- |
| connection failed before a response | yes |
| 429 from the provider | yes, after its `Retry-After` when it sent one |
| 5xx from the provider | yes |
| model command could not be started | yes |
| 401, 403, another 4xx, a refused redirect | no |
| a reply that is not a completion, or one over the size bound | no |
| timeout | no: the request may have been served, and it already spent the time a retry would spend again |
| model command ran and exited non-zero | no: it may have spent tokens or acted |
| the caller cancelled | no |

A reply the capability cannot parse is not a transport failure and is never retried here. The wait between attempts is an exponential backoff from 500ms with jitter; every wait, a provider's `Retry-After` included, is capped at 30s. A wait that would pass the caller's deadline is not started.

## Usage

Each call records its capability, transport, configured model, provider-reported model, outcome class, attempts, latency, and prompt and response bytes. Token counts are recorded only when the provider's response carries a `usage` object. They are never estimated: a model command reports none, and an endpoint that omits `usage` leaves them null. A token budget therefore does not bind an owner whose provider reports no tokens; set a call budget for that case.

Usage is kept in `conduit_model_usage_daily`, one row per UTC day, owner, capability, transport, configured model, provider model and outcome. With the file repository it is kept in memory: it is lost on restart, and each replica has its own.

- `GET /v1/ai/usage?days=N` returns the caller's own rows, today's totals and the remaining budget. Any authenticated operator may call it; it never answers about another owner.
- `GET /v1/admin/ai/usage?days=N&owner=X` returns every owner's rows, or one owner's. It requires the governance Administrator.
- `conduit ai usage [--days N]` and `conduit ai usage --all [--owner X]` read the same two routes.

`days` is 1 to 92 and defaults to 1.

## Budgets

A budget is checked against persisted usage when a request arrives and again after it acquires its owner and global slots, using the current UTC day. The slots stay held through audit and metering, so a queued request cannot reuse a budget decision made before an earlier call spent it. The budget resets at 00:00 UTC. Calls refused by a limit do not count; every call that reached or tried to reach a model does, failures included.

The check is a read followed, after the call, by a write. It is not a reservation. Calls already in flight when the check is read are not counted, so an owner can pass the budget by at most the calls in flight across all replicas when the limit is crossed: with the defaults, two calls per replica, plus whatever the last calls cost in tokens, which is unknown until they return. With PostgreSQL the budget is shared by every replica. With the file repository it is per process.

If a budget is configured and usage cannot be read, the call is refused. Without a budget a usage outage is logged and counted, and the call proceeds.

## Metrics

The metrics listener publishes counters labelled by `capability`, `transport` and `outcome` only. All three are closed sets fixed in code. An owner or a model name is never a label.

| Metric | Meaning |
| --- | --- |
| `conduit_model_calls_total` | calls; `outcome="throttled"` and `"budget_exceeded"` never reached a model |
| `conduit_model_call_retries_total` | attempts beyond each call's first |
| `conduit_model_call_duration_seconds_sum` | seconds in calls, waits and retries included |
| `conduit_model_prompt_tokens_total`, `conduit_model_completion_tokens_total` | provider-reported tokens |

## The audit event

Each call that reached a model, or failed trying to, writes one `ai.model_call` event to the authorization audit log, on resource `ai-run/<run id>`. It holds the capability, transport, run id, session id (`assistant-thread/<id>` or `planning-session/<id>` when there is one), round index, the SHA-256 of the exact prompt bytes sent and of the response bytes received, the configured and provider-reported model ids, the outcome, the attempt count, and what was done about a transcript. It holds no prompt or response text.

One run is one request, or one planning turn in the workflow worker. Rounds are numbered from 1 and chained: each event carries the previous event's `chainSha256`, and its own is the SHA-256 of that value, the round, and the two digests. A removed, reordered or altered event breaks the chain.

If the event cannot be written the call fails with `AUDIT_UNAVAILABLE` and the reply is discarded. A call refused by a limit reached no model and writes no `ai.model_call` event; it is in the usage rows and the metrics.

## Transcripts

Transcripts are off unless `CONDUIT_AI_TRANSCRIPTS=on`. When off, nothing is stored and the audit event says `transcript: off`.

When on, the prompt and the response of each call are stored after the same redaction that repository content passes through on its way into a prompt (`packagingtools.RedactSecrets`). That filter removes obvious credentials. It is best effort and is not a secret scanner: a transcript can still hold tenant source code, configuration, and anything an operator typed. Turn transcripts on only where policy allows that text to be kept for the retention you set.

Each record keeps the digest of the bytes sent and the digest of the bytes stored. They are equal when redaction changed nothing. When it changed something, the audit event also carries the stored digest, so a verifier can tell a copy that matches what was sent from a redacted copy the audit log vouches for.

A record is at most 2 MiB and a run at most 16 MiB. An exchange over a cap is not stored and its audit event says which cap. A transcript that cannot be written does not fail the call; the audit event says `failed` and an export reports the gap.

### Retention and deletion

A record expires `CONDUIT_AI_TRANSCRIPT_RETENTION` after it was written. An expired record is never returned, swept or not. A sweeper deletes expired records at an interval of one tenth of the retention, between one minute and one hour, so a record stays on disk at most that much past its expiry. The delete is one idempotent statement and is safe with several replicas. Changing the retention applies to records written afterwards. Turning transcripts off stops new records and stops the sweeper: existing records stay unreadable after their expiry but are deleted only when transcripts are next on, or by an operator with `DELETE FROM conduit_ai_transcripts`.

Audit events are not deleted by the sweeper. After a transcript expires, the digests remain and the text does not.

### Export and verification

- `GET /v1/admin/ai/transcripts?owner=X&session=Y` lists runs with a stored transcript, without text.
- `GET /v1/admin/ai/transcripts/{runID}` returns one run's exchanges in order, checked against the audit events.
- `conduit ai transcript list` and `conduit ai transcript export RUN_ID` read the same routes. `export` exits non-zero when verification fails.

All three routes require the governance Administrator, the authority the administrator status page uses. No team administrator and no resource grant reaches them, and no route lets an owner read a transcript, their own included. Every export writes an `ai.transcript_export` audit event before the body is sent, and is refused if that event cannot be written. A refused attempt by a non-administrator is audited too.

The export recomputes the audit chain and checks each stored record against its event. `verified` is false, with a `mismatches` entry per finding, when the chain does not recompute, a record's digests are not the audited ones, or the stored bytes do not hash to the digest the audit log holds for them. `complete` is false, with a `missing` entry per round, when an audited round has no stored record: transcripts were off, the exchange was over a cap, or the record expired. Each present side is marked `exact` or `redacted`.

The check proves the stored text is what the audit log vouches for. It rests on the audit log: see [authorization audit integrity](authorization-audit-integrity.md) for what protects that.

## PostgreSQL

The store's migrations are component `model-governance`: `0001-model-usage-daily` and `0002-ai-transcripts`. They are applied at Hub start and checked by `verify-schema`.
