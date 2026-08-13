# Four-family evaluator trial

This trial evaluated immutable Platform snapshot
`snap_767ee880ecbbc73b150ee0a6b98f94e1d490dd79605b2f17c2e8bfa4624e5a48`
with Grok, Gemini, Kimi, and Opus through Cursor Agent. Every arm received the
same prompt bytes and a readable materialization of the same zero-secret-match
archive. Raw provider receipts, timing, runner-version observation, prompt,
snapshot receipt, normalization policy, normalized request, and content-addressed
result are retained here; no durable evidence depends on `/tmp`.

The 24 model hypotheses were independently dispositioned against commit
`7a26525f785c5a3d3328ca10b3aa017822b5c611`: 22 findings reproduced as defects,
one exceeded the documented snapshot-authenticity claim and was a false positive,
and one bounded acquisition-budget hypothesis remains unresolved. Independent
canonical clustering reduced the verified findings to 18 defect clusters. The
durable batch is
`evb_f69ab348c5d7f081a2e960b993e8acd914751efce15ef23028a7a3c03644b94e`.

Measured per-run calibration is preserved in `batch-result-v2.json`. Kimi's 7
verified findings span 7 clusters, 5 unique to its run; Grok's 4 findings span 3
clusters, 2 unique; Gemini's 2 verified findings span 2 clusters alongside 1 false
positive, with 1 unique cluster; Opus's 9 findings span 9 clusters, 7 unique. All
arms reported complete input access. Together they consumed 647842 input, 129365 output,
7660346 cache-read, and 191005 cache-write tokens, with 1551850 ms summed
provider latency. Monetary cost is explicitly `UNAVAILABLE`, never zero: the
personal integration key received HTTP 401 from the Cursor team billing API.

Rebuild and independently verify the durable batch from `platform/`:

```sh
node scripts/normalize-evaluator-receipts.mjs \
  docs/operations/evidence/evaluators/2026-08-08-02 \
  7a26525f785c5a3d3328ca10b3aa017822b5c611 \
  docs/operations/evidence/evaluators/2026-08-08-02/normalization.json
go run ./cmd/platform-optimizer record-evaluators \
  < docs/operations/evidence/evaluators/2026-08-08-02/batch-request-v2.json
```

The second command must return the batch ID above. The executable gates named by
each verified disposition must also pass independently; content addressing alone
does not prove that a claimed reproduction occurred.

`zenith-trial.json` is the provider-neutral export of the same evidence. Verify
and derive portable metrics with AgentDriver Zenith Protocol:

```sh
node /path/to/agentdriver/plugins/zenith-protocol/scripts/verify-evaluator-trial.mjs \
  --json docs/operations/evidence/evaluators/2026-08-08-02/zenith-trial.json
```
