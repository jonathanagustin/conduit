# Metrics ledger — durable, traceable measurement over time

Measures recorded only as prose in a table cell are not queryable, not a time-series, and not traceable
to the code that produced them — so you can't later ask "is this still earning its cost?" from data.
This ledger fixes that.

## The shape: an append-only, immutable time-series

`metrics-ledger.jsonl` (path from `.zenith-protocol.json`) is an **append-only event log** — one
measurement per line. **Rows are immutable once written.** A correction is a *new* row that `supersedes`
an old one by id — never an in-place edit. That keeps the history of *what you measured and when*
auditable. Each row is traceable **to code** (the git `commit` at write time) and **to evidence**
(`source`, and a `pdr`/`hyp` tag).

### Row schema

```jsonc
{
  "id": "m0001", "date": "…Z", "commit": "abc1234",
  "category": "gate", "metric": "coverage", "value": 41, "unit": "services",
  "tier": "observed",          // observed | benchmarked | expected
  "pdr": "P1", "hyp": "H1",     // optional links to the decision/hypothesis
  "source": "verify.mjs",       // the tool/gate that produced it
  "supersedes": "m0003",        // optional — this row corrects an earlier one; the earlier one stays
  "note": "…"
}
```

## Tools (shipped with the plugin)

- **Record** — `node <plugin>/scripts/record-metric.mjs --category <c> --metric <m> --value <v> [--unit
  --tier --pdr --hyp --source --note --supersedes]`. Auto-fills id/date/commit; append-only by construction.
- **Read/decide** — `node <plugin>/scripts/metrics-summary.mjs [--metric M | --pdr P1 | --hyp H1]
  [--json]`. Per series: latest value, point count, Δ vs first — the decision view.

## Discipline

1. **Only real, produced measurements** — never a number seeded to look populated. Predictions get
   `--tier expected` and say so.
2. **Append, never rewrite.** Corrections are new `supersedes` rows. History is the point.
3. **Record it in the same cycle you measured it**, and **tag it** (`--pdr`/`--hyp`) so it joins the
   decision it informs.
