---
description: Run the zenith-protocol conformance checker over this repo's ledgers — verifies the mechanically-checkable protocol invariants (ledgers parse, metrics append-only + valid, PDR/vote/hypothesis discipline, capability-map instruments, drift gate) and reports PASS/FAIL/SKIP.
---

Check that this repo actually FOLLOWS the operating model (not just claims to). This is the
mechanically-checkable half of conformance; the attestation-only half (did the orchestrator truly
re-run the gate, is a vote's justification genuine, was a change holistic) is NOT covered here — those
stay a review concern.

1. Run `node ${CLAUDE_PLUGIN_ROOT}/scripts/conform.mjs` (resolves ledger paths from
   `.zenith-protocol.json`; add `--config <path>` to point at another repo's config, `--json` for
   machine output).
2. Read the grouped result. Exit 0 = every mechanically-checkable invariant passed; non-zero = at least
   one FAIL. A SKIP is not a failure — it means the invariant isn't evaluable here (no emitters
   configured, ledger not tracked, branches not both local) — but note the gap.
3. For each FAIL, the line names the rule + the exact file/line/token. Fix the ledger (or the process
   that produced it), don't silence the check. Common ones:
   - `metrics-append-only` — a committed metric line was edited in place; revert and add a `supersedes`
     row instead.
   - `pdr-single-vote-tally` — a PDR scattered its `good×N` tally across cells; consolidate to one.
   - `pdr-adopted-has-measure` — an ADOPTED PDR has an empty measure cell; add the objective measure or
     move it back to TRIAL.
   - `hypothesis-intake-gatekept` — a PH-# proposal has no ACCEPT/REJECT/NOTE/QUEUED verdict; gatekeep it.
4. To verify the checker itself still catches planted violations (§0a "verify the tool"), run
   `node ${CLAUDE_PLUGIN_ROOT}/scripts/conform.test.mjs`.
5. Promote any ledger fix only according to the target repository's commit and push authority.

$ARGUMENTS
