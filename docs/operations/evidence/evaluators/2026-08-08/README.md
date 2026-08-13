# Pinned multi-model evaluator trial

This trial evaluated snapshot
`snap_cf8ecd3a9ae3fc7cd5e65e9bac262f823a7d1bdb07db91d32b3094a401adacba`
with Grok, Gemini, and Kimi through Cursor Agent. The same prompt and immutable,
zero-secret-match snapshot receipt are embedded in every content-addressed run.
Raw provider receipts are retained beside the normalized request and result.

Ten of twelve hypotheses were independently reproduced and constrained by named
Go regression tests in commit `70829a72fea63403faf084232069f85d373edc21`.
Gemini's scheduler tolerance hypothesis and Kimi's unknown-cost tie-policy
hypothesis were initially `UNRESOLVED`. Both were later independently reproduced,
mutation-checked, and closed by the content-addressed resolution
`evr_fe329e1c54aaa4a8115359a19184234212b55251f4fcee494dc0fbc9038255d4`
against commit `3079e18055316611f81465a741e186ea5c2d6cd3`; the original batch remains
unchanged. Grok could not access the pinned tree, so its zero findings are
inconclusive, as its retained residual-unknowns record states.

Token usage and provider latency are measured exactly from the Cursor receipts.
Monetary cost is explicitly `UNAVAILABLE`, not zero: the personal integration
key cannot access Cursor's team billing API (HTTP 401 on 2026-08-08). This is a
known observability boundary rather than an estimated charge.

Rebuild and verify the durable batch from `platform/`:

```sh
node scripts/normalize-evaluator-receipts.mjs \
  docs/operations/evidence/evaluators/2026-08-08 \
  70829a72fea63403faf084232069f85d373edc21
go run ./cmd/platform-optimizer record-evaluators \
  < docs/operations/evidence/evaluators/2026-08-08/batch-request.json
go run ./cmd/platform-optimizer resolve-evaluators \
  < docs/operations/evidence/evaluators/2026-08-08/resolution-request-3079e1805.json
```
