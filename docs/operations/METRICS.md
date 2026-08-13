# Metrics

`metrics-ledger.jsonl` is append-only. Every non-empty line is a JSON object
with `id`, `date`, `commit`, `category`, `metric`, `value`, and `tier`.
Corrections add a new row with `supersedes`; committed rows are never edited.

Useful measures for this repository include capability rows closed, defects
caught by independent verification, conformance duration, and live Flux
reconciliation time. Do not invent token or time savings that were not
measured.
