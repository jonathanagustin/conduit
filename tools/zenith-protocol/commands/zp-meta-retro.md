---
description: Run the meta-retrospective — a process improvement about the improvement mechanism. Measures PDR state, vote spread, method size, and ledger usage; flags churn/rubber-stamp/bloat/under-measure.
---

Run the meta-retro over the PDR log + metrics ledger and act on it:

1. Execute `node ${CLAUDE_PLUGIN_ROOT}/scripts/meta-retro.mjs --record` (resolves ledger paths from
   `.zenith-protocol.json`).
2. Read its FLAGS. It obeys the rule it audits: you must **apply a change** (adopt / downgrade / tighten /
   REMOVE a step in the PDR log) **or record the measure that says none is needed** — a meta-retro that
   only ever prints "healthy" is itself a removal candidate.
3. For a RUBBER-STAMP flag, remember the refinement: vote-uniformity ≠ rubber-stamp if the agent return
   *bodies* carry genuine critique. Check the recent bodies before downgrading the vote PDR.
4. Promote any PDR/measure change only according to the target repository's commit and push authority.

$ARGUMENTS
