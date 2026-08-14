# Process Decision Records (PDRs) — the self-improvement mechanism

A method change does **not** auto-become the status quo. It is an ADR-style decision with a lifecycle,
so the process can't churn on unproven ideas:

```
PROPOSED ──▶ TRIAL (used, but provisional; measured over a defined window) ──▶ ADOPTED (then, and only
                                                                        │        then, a default)
                                                                        └──▶ REJECTED / SUPERSEDED
```

Rules:
- Enter **PROPOSED**, run a **TRIAL** against a stated exit criterion (a measure over N cycles), and only
  **ADOPT** once the trial clears it. Until then it's used but marked provisional.
- Every record carries an **objective measure** (`[benchmarked]` before/after, `[observed]` this session,
  or `[expected]` predicted) and a **holistic check** (net-positive for the whole system, not a local win
  that costs more tokens/latency/complexity). A change that only makes the method *bigger* is REJECTED.
- ADOPTED steps are re-examined: if a measure regresses, move back to TRIAL or REJECT. Be willing to
  **remove** a step that stops earning its cost.
- Keep ONE running `good×N` vote tally per PDR (a single cell) so the meta-retro's automated count stays
  honest — never scatter `good×N` tokens across cells.

## Records

> The rows below are the **distilled core method**, carried as strong priors from the project that grew
> this plugin. They are marked ADOPTED because they earned it there — but the trial discipline says
> **re-measure them in YOUR context**; if a measure regresses here, move it back to TRIAL. Delete rows
> that don't apply to your work.

| PDR | change | status | trial exit criterion | measure | holistic check (cost) | votes |
|---|---|---|---|---|---|---|
| P2 | Single-writer per file-set; orchestrator owns commits | **ADOPTED (prior)** | 0 file-set races over a session | re-measure here | net-positive, ~0 cost; readers stay parallel | — |
| P3 | Orchestrator independently re-runs the agent's gate before committing | **ADOPTED (prior)** | catches ≥1 wrong self-report, or is cheap insurance | re-measure here | cheap vs a broken pushed artifact; skip for trivial doc edits | — |
| P4 | Test load-bearing claims before trusting; log them | **ADOPTED (prior)** | reclaims real work vs trivial cost | re-measure here | strongly net-positive; load-bearing claims only | — |
| P7 | Push working + fast-forward shared branches on every commit | **ADOPTED (prior)** | trackers always current | re-measure here | negligible cost; FF-only | — |
| P8 | Agents vote + comment on the process (`process_feedback`) | **TRIAL** | over ~5 threads, does ≥1 vote surface a real method problem the orchestrator would have missed? | re-measure here | risk = rubber-stamping; mitigated by the objective-line requirement + reading the return body | — |
| P9 | Meta-retro every ~5 threads (improvement about improvement) | **TRIAL** | does it catch a churn/bloat/rubber-stamp/under-measure problem per-thread retro missed? if it only prints "healthy", widen N or remove | re-measure here | risk = meta-ceremony; one measured tool run on a cadence | — |
| P1 | Fan out one subagent per independent thread | **TRIAL** | parallel wall-clock beats serial on ≥2 independent threads without a token blowup | re-measure here | gated on "independent + non-trivial", not "parallelize everything" | — |

## Add your own

Append a row when you change the method. New idea → **PROPOSED**; use it provisionally; let the measure
graduate it. Run `/zp-meta-retro` every ~5 threads to keep this log honest.
