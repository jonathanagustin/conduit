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

The separate `packaging-workflow` worker process applies the concurrency limits and the retry policy to its planning and packaging calls and counts them in process. It has no audit log, no usage store and no operator, so it enforces no budget, writes no per-call audit event and keeps no transcript. Its calls do not appear in the usage routes. It refuses a configured daily call/token budget or `CONDUIT_AI_TRANSCRIPTS=on` before model execution; it cannot silently discard those requested policies. Keep those policies on Hub-governed execution until the standalone worker has durable usage and transcript custody. With transcripts off, a retention duration alone does not enable recording.

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

Usage rows keep `calls` for all requests with that outcome, `budgetedCalls` for the exact number of policy charges, and `attempts` for the total reported attempts including retries. For new requests, one policy charge requires at least one transport invocation: a request retried three times spends one call, while a queue cancellation, missing-auditor refusal or unsuccessful pending-audit retry before invocation spends none. A request that invoked the provider and then cancelled or failed audit still spends a call. Request/outcome metrics retain all requests, including refusals.

Historical PostgreSQL rows written before `0004-model-attempted-calls` can mix zero-attempt and attempted requests in one outcome row. Summed attempts cannot reconstruct the exact count of attempted requests. The migration therefore preserves each row's former conservative budget charge rather than guessing or refunding it. `budgetedCalls` combines those retained legacy charges with exact policy charges from new invocation evidence; it never claims to establish an exact historical invocation count. New requests add their charges independently, even when they share that historical row's key. A new UTC day starts without those prior-day charges. The internal `attempted_calls` column keeps its original migration name and stores this policy charge, which is exposed as `budgetedCalls`.

Usage is kept in `conduit_model_usage_daily`, one row per UTC day, owner, capability, transport, configured model, provider model and outcome. With the file repository it is kept in memory: it is lost on restart, and each replica has its own.

- `GET /v1/ai/usage?days=N` returns the caller's own rows, today's totals and the remaining budget. Any authenticated operator may call it; it never answers about another owner.
- `GET /v1/admin/ai/usage?days=N&owner=X` returns every owner's rows, or one owner's. It requires the governance Administrator.
- `conduit ai usage [--days N]` and `conduit ai usage --all [--owner X]` read the same two routes.

`days` is 1 to 92 and defaults to 1.

### Console snapshot

Preferences includes **Your AI usage**. **Load AI usage** reads the caller's
current UTC day from the self-usage route; **Refresh** and **Retry** repeat that
read. It displays the authenticated actor's account, including during delegated
resource access. Identity changes discard the previous account's snapshot and
in-flight results. The panel does not change budget configuration or invoke a
model, and it does not use the administrator usage route.

Zero remaining allowance is exhausted; a null configured allowance is
**Unlimited**. Token counts the provider omitted are **Not reported**, and
partly reported totals are marked incomplete. Token allowance uses reported
counts only. Call charges retain the historical accounting caveat above. These
are Hub-metered usage snapshots; standalone worker calls are not included.
Read failures clear the previous display and offer an explicit retry. A delayed
read updates only the usage panel, preserving other preference drafts and focus.

## Budgets

A budget is checked against persisted usage when a request arrives and again after it acquires its owner and global slots, using the current UTC day. The slots stay held through audit and metering, so a queued request cannot reuse a budget decision made before an earlier call spent it. The budget resets at 00:00 UTC. Calls refused by a limit do not count; every call that reached or tried to reach a model does, failures included.

When either budget is configured, the governor reserves a usage receipt before invoking the provider. The receipt and the current owner/day totals are read atomically, and those totals are checked again before invocation. A spent budget cancels the unused receipt without counting a model call. Only one receipt for an owner and UTC day may remain unsettled: budgeted calls for that owner are conservatively serialized, across replicas when they share PostgreSQL. Another active or orphaned receipt returns `MODEL_BUSY` with `Retry-After`; it is never cleared to admit a contender. Other owners and other UTC days remain independent. The concurrency settings continue to bound calls without a budget and calls across owners.

The receipt does not predict provider token consumption. A final admitted call can cross a token limit by its actual reported cost; a provider that reports no tokens is still not bound by a token budget. All replicas must use the same receipt and attempted-call accounting protocol implementation as well as consistent budget settings. Applying the database migrations alone does not upgrade an older writer: older governors can ignore receipts or fail to maintain attempted-call counts even with matching settings. Quiesce model-call admission while replacing those writers, and resume only after every participating replica uses the current protocol. A replica running without a budget uses the unbudgeted path and does not participate in receipt admission. With the file repository, totals and receipts are process-local and restarting loses both; it is not a durable budget deployment.

If a budget is configured and usage cannot be read or reserved, no provider is invoked. Completion updates the daily aggregate and the receipt's exact final payload together. The first call's reply may already be returned when metering fails, but the active governor retains the exact usage record and refuses further calls for that owner/day until an idempotent completion retry succeeds. A committed write whose acknowledgement was lost is recognized by receipt ID and exact payload and is not added twice. Refusals caused by unavailable usage are metered as `usage_unavailable` and do not spend the call budget. Without a budget, usage remains best effort: no receipt is created, an outage is logged and counted, and the call proceeds.

### Interrupted accounting and recovery

PostgreSQL receipts survive a pool, Hub or process restart. An incomplete receipt keeps its original owner/day closed even if the process lost the final token counts. The new UTC day has its own budget and can admit calls; rollover does not delete or mark the old receipt complete. There is no automatic expiry or reset of an unsettled receipt, and no public reconciliation endpoint.

Within the original running governor, restoring the usage store allows the next call for that owner/day to retry the retained final record and then check the resulting budget. After process loss, do not guess the final payload, blindly replay additive usage updates, delete the receipt, or cancel a receipt when a provider may have run. Leave the affected day closed if exact final evidence is unavailable. Any exceptional reconciliation before rollover requires a separately reviewed operation that recovers the exact final `UsageRecord` and completes its original receipt idempotently; this document grants no reset authority. Backups and restores must retain receipt and aggregate tables together.

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

New model-call audit event IDs are derived from the run and round, independently of timestamp resolution. Distinct rounds remain distinct even at the same instant, while retrying one round retains its original event identity. Previously stored audit records keep their existing IDs and bytes.

If the event cannot be acknowledged the call fails with `AUDIT_UNAVAILABLE` and the caller receives no reply. The run retains the exact pending event and reserves its transcript round. Before another model call in that run, the governor retries the event unchanged; a continuing audit outage refuses the new invocation. A recovered event joins the chain once, including when the original write committed but its acknowledgement was lost. Audit adapters must deduplicate identical retries. Only acknowledged events advance the run's reported round count.

Pending events are held in the active run's memory. If the process or run ends before recovery, a stored transcript can remain without an audit event until its normal expiry. Export explicitly reports that orphan as a mismatch; it cannot be verified or silently reused for another exchange. A call refused by a limit reached no model and writes no `ai.model_call` event; it is in the usage rows and the metrics.

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

The store's migrations are component `model-governance`: `0001-model-usage-daily`, `0002-ai-transcripts`, `0003-model-usage-receipts` and `0004-model-attempted-calls`. They are applied at Hub start and checked by `verify-schema`. Receipt payloads contain usage metadata, not prompt or response text; the attempted-call migration preserves their exact bytes and idempotent completion semantics.
