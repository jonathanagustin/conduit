---
description: Append a durable, traceable measurement to the metrics ledger (append-only, git-SHA-stamped). Use to record any objective measure behind a method change or recovery fact.
---

Record a measurement to the metrics ledger. Parse the arguments and run:

`node ${CLAUDE_PLUGIN_ROOT}/scripts/record-metric.mjs --category <c> --metric <name> --value <v> [--unit u] [--tier observed|benchmarked|expected] [--pdr P#] [--hyp H#] [--source "tool"] [--note "..."] [--supersedes m####]`

Rules:
- Only a **real, produced** measurement — never a number seeded to look populated. A prediction gets
  `--tier expected` and says so in the note.
- Tag it (`--pdr`/`--hyp`) so it joins the decision it informs.
- A correction is a NEW row with `--supersedes <old-id>`, never an edit of the old one.

Then promote the updated `metrics-ledger.jsonl` only according to the target repository's commit and
push authority.

$ARGUMENTS
