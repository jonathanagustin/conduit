---
description: Start a session under the zenith-protocol operating model — read the ledgers, work as orchestrator+verifier, gate everything, and close with the self-improvement loop.
---

Conform to the **zenith-protocol** operating model for this session. Invoke the `zenith-protocol`
skill and follow it. In brief:

1. **Read the ledgers first** (don't rediscover; write back in the same change): the capability map,
   hypothesis backlog, assumptions ledger, PDR log, and metrics ledger (paths from
   `.zenith-protocol.json`; run `/zp-init` if they don't exist yet).
2. **Work as orchestrator + verifier.** Decompose into independent (non-shared-file) threads. Delegate
   legwork to subagents with a tight brief: ground-truth harness, fixed return format, acceptance gate.
   Single-writer per file-set; investigators run read-only in parallel.
3. **Gate everything** (ground truth over vibes). Prefer extract/generate/compare-like-for-like over
   hand-authoring. Generated modules must be idempotent-emitted + drift-gated (`verify-no-drift.mjs`).
4. **Independently re-verify** every agent's result yourself before committing. Never trust a self-report.
5. **Promote only within authority:** follow the target repository's commit, branch, review, and push
   rules. If promotion is not authorized, leave a verified diff and report it.
6. **Self-improve (required close):** assumptions→ledger, facts→capability map, method lessons→PDR with an
   OBJECTIVE measure (`record-metric.mjs`) + a holistic check. Each delegated agent ends with a
   `process_feedback` vote. Every ~5 threads run `meta-retro.mjs --record` and apply/record a change.

When blocked: formulate a numbered hypothesis, record it, get past it or mark it BLOCKED with the exact
failing instrument — so it's never rediscovered. Keep it repeatable with committed code.

$ARGUMENTS
