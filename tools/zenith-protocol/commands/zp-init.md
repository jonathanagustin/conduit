---
description: Scaffold the zenith-protocol ledgers and config into the current repo (idempotent) — copies the five ledger templates and writes a .zenith-protocol.json if absent.
---

Scaffold the zenith-protocol operating model into this repo. Do this idempotently — never overwrite
an existing ledger that already has content.

1. Determine the target ledger dir (default `docs/ledgers/`). If `.zenith-protocol.json` exists at the
   repo root, use its paths; otherwise create `.zenith-protocol.json` from this default:
   ```json
   {
     "ledgerDir": "docs/ledgers",
     "capabilityMap": "docs/ledgers/CAPABILITY-MAP.md",
     "backlog": "docs/ledgers/HYPOTHESIS-BACKLOG.md",
     "assumptions": "docs/ledgers/ASSUMPTIONS-LEDGER.md",
     "pdrs": "docs/ledgers/PROCESS-IMPROVEMENTS.md",
     "metricsDoc": "docs/ledgers/METRICS.md",
     "metricsLedger": "docs/ledgers/metrics-ledger.jsonl",
     "operatingModelDoc": "AGENTS.md",
     "operatingModelStartMarker": "THE DEFAULT OPERATING MODEL",
     "emittersDir": "scripts",
     "emitterPattern": "^emit-.*\\.mjs$",
     "generatedTrees": []
   }
   ```
   Adjust `operatingModelDoc`/`generatedTrees` to this repo's reality.
2. For each of the five ledgers, if the target file does NOT already exist, copy the matching template
   from `${CLAUDE_PLUGIN_ROOT}/templates/` (`CAPABILITY-MAP.md`, `HYPOTHESIS-BACKLOG.md`,
   `ASSUMPTIONS-LEDGER.md`, `PROCESS-IMPROVEMENTS.md`, `METRICS.md`). Leave existing ledgers untouched.
3. Confirm the tools resolve: run `node ${CLAUDE_PLUGIN_ROOT}/scripts/metrics-summary.mjs` (should print
   "no ledger yet" or the current summary) and `node ${CLAUDE_PLUGIN_ROOT}/scripts/meta-retro.mjs`.
4. Report what was created vs left as-is. Commit or push only when the target repository's policy and
   user authority permit it.

$ARGUMENTS
