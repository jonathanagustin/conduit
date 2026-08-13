# plugin-zenith-protocol

A portable, **self-improving operating model** for agentic work, packaged as a Claude Code plugin.
It installs a way of working — **orchestrate → delegate → verify** — plus the machinery that keeps the
*method itself* getting measurably sharper over time.

It was distilled from a large clean-room reconstruction project where a single session ran as
orchestrator+verifier over dozens of delegated subagent threads, and needed a durable way to avoid
re-deriving solved problems, to gate everything against ground truth, and to improve its own process
without churning on unproven ideas.

## What's in the box

- **A skill** (`zenith-protocol`) — the full method: read the ledgers first, decompose into
  single-writer threads, delegate with tight briefs + a fixed return format, gate everything against
  ground truth, independently re-verify before committing, and close every thread by improving the method.
- **An optimization skill** (`optimize-evidence`) — acquire the least-cost sufficient state,
  freeze bitemporal snapshots, preserve unknowns and Pareto trade-offs, model independent evaluators,
  and calibrate predicted value against realized outcomes without granting execution authority. Its
  theory-to-practice note explains the compositional math and complete evidence loop.
- **A writing-review skill** (`review-writing`) — find clustered vocabulary, phrase, cadence, and
  structural tells with line evidence, then revise prose without making authorship claims. Its
  portable Node detector is adapted from ai-ecoverse's Apache-2.0 `ai-writing-detector` taxonomy.
- **The self-improvement loop** (the differentiator):
  - **PDR lifecycle** — every method change is a Process Decision Record with an ADR-style
    PROPOSED→TRIAL→ADOPTED lifecycle. No idea becomes a default until a measured trial clears it.
  - **Agent `process_feedback` voting** — every delegated agent votes on the process it used, with an
    objective justification; the votes are tallied into the PDR they bear on.
  - **A meta-retro** — a process improvement *about* the improvement mechanism, run on a cadence; it
    flags churn / rubber-stamping / bloat / under-measurement and must apply a change or justify none.
  - **A durable metrics ledger** — append-only, git-SHA-stamped measurements so decisions are made from
    data kept over time, not memory.
- **Five ledger templates** — capability map, hypothesis backlog, assumptions ledger, PDR log, metrics.
- **MADR support** — a Markdown Architectural Decision Record template and `/zp-madr` command keep
  architecture trade-offs durable without confusing them with trials, tasks, or assumptions.
- **Runnable tools** (`scripts/`, repo-agnostic via `.zenith-protocol.json`):
  `record-metric.mjs`, `metrics-summary.mjs`, `meta-retro.mjs`, `verify-no-drift.mjs`, `conform.mjs`.
- **A conformance checker** (`conform.mjs`) — makes the mechanically-checkable protocol invariants
  enforceable instead of convention-only (see below).
- **A heterogeneous evaluator evidence gate** (`verify-evaluator-trial.mjs`) — validates immutable
  snapshot/prompt/receipt binding, measured usage and latency, explicit access/cost unknowns,
  independently normalized clusters and dispositions, and append-only later resolutions. `--json`
  derives per-arm and batch calibration, resource, latency, and cost-status metrics from the verified
  evidence. The provider-neutral manifest starts at `templates/EVALUATOR-TRIAL.json`.
- **Slash commands** — `/zp-kickoff`, `/zp-meta-retro`, `/zp-record-metric`, `/zp-init`, `/zp-conform`,
  `/zp-madr`.

## Install (in another repo)

This directory doubles as a single-plugin marketplace. From the target repo:

```
/plugin marketplace add <path-or-git-url-to-this-plugin-dir>
/plugin install plugin-zenith-protocol
```

Then scaffold the ledgers + config into the repo:

```
/zp-init
```

`/zp-init` writes a `.zenith-protocol.json` (defaulting to `docs/ledgers/`) and copies any missing
ledger templates — it never overwrites a ledger that already has content. Adjust `operatingModelDoc` and
`generatedTrees` to your repo, then start a session with `/zp-kickoff`.

## Configuration (`.zenith-protocol.json`, repo root)

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
  "generatedTrees": [],
  "contextDocs": ["AGENTS.md", "docs/domain-operating-profile.md"]
}
```

All paths are resolved against the repo root (found by walking up to `.zenith-protocol.json` or
`.git`). Tools work with no config too — they fall back to these defaults.

`contextDocs` is the supported project/domain/situation overlay. Read these files in order after the
ledgers and before decomposition. They carry local authority, topology, domain constraints, value
policy, and acceptance gates without forking the portable method. Conformance fails when a configured
context document is missing. The protocol never overrides higher-level policy or user authority.

## Conformance — is the repo actually FOLLOWING the protocol?

The protocol is a set of **testable invariants**. Without a check, "conformance" is convention only —
so `conform.mjs` enforces the **mechanically-checkable** half. Run it with `/zp-conform` or
`node <plugin>/scripts/conform.mjs` (add `--config <path>` to target another repo, `--json` for
machine output). Exit 0 = all pass; non-zero = a FAIL; SKIP = not evaluable here.

| check | invariant | modeled on |
|---|---|---|
| `ledgers-exist` | the 6 ledgers exist at their configured paths | — |
| `context-docs-exist` | every configured project/domain overlay exists | — |
| `metrics-jsonl-valid` | every metrics line is JSON with the required fields | JSON Schema / ajv row validation |
| `metrics-append-only` | no committed metric line was ever edited in place (corrections `supersede`) | git append-only guard (pre-commit/CI) |
| `pdr-status-valid` | every PDR row parses; status ∈ PROPOSED/TRIAL/ADOPTED/REJECTED/SUPERSEDED | ADR-tools status lint |
| `pdr-adopted-has-measure` | no ADOPTED PDR with an empty measure cell | ADR lifecycle discipline |
| `pdr-single-vote-tally` | ≤1 `good×N` (and ≤1 `bad×N`) token per PDR row — the meta-retro count stays honest | — (bespoke: the vote-tally is protocol-specific) |
| `hypothesis-intake-gatekept` | every PH-# proposal has an ACCEPT/REJECT/NOTE/QUEUED verdict | policy-as-code deny rule |
| `capmap-blocked-names-instrument` | BLOCKED/OPEN rows name a tool/error/path (heuristic) | markdown/AST lint (heuristic) |
| `drift-gate` | generated modules match a fresh emit (reuses `verify-no-drift.mjs`) | conformance test suite |
| `push-merge-one-sha` | configured shared branches sit at one SHA (optional) | git invariant |

**What it CANNOT check — the attestation gap.** Three protocol requirements are *not* mechanically
verifiable and must not be faked with a check: (1) that the orchestrator **truly re-ran** an agent's
gate before committing; (2) that a vote's **objective justification is genuine**, not rubber-stamped;
(3) that a method change was **holistic** (net-positive system-wide). These stay a review/attestation
concern — the orchestrator reads the agent's return *body* (per the meta-retro's rubber-stamp
refinement) and a human spot-checks. Model them as self-attestation (the `process_feedback` line is the
attestation record), not as a passing script.

**Enforcement posture — the cheap one, by default.** Applying the protocol's own holistic-cost rule to
conformance itself:

- **(a) Static checker at the natural gate — RECOMMENDED default.** Wire `conform.mjs` into a Claude
  Code **hook** (`hooks/hooks.json`: a `Stop`/`SubagentStop` hook — exit 2 blocks the stop and feeds
  stderr back; or `PreToolUse` on the commit command) and/or have the worker **self-run** it before
  returning and the orchestrator run it in its existing re-verify step. Marginal cost ≈ **one script
  run (sub-second)**; catches every mechanical violation at the moment it would matter. Posture:
  **warn** in-session (surface the FAIL), **block** only at the commit boundary where a bad ledger
  would be pushed.
- **(b) Live supervisor agent — reserve for high-stakes only.** A second agent watching a worker can't
  cheaply observe its tool calls live, so it **polls artifacts** — which is exactly what the static
  checker already does, at **~2× per-thread agent cost**, and it duplicates the orchestrator's
  re-verify role. Use it **only** for very-long parallel threads where a late-caught violation is
  expensive to unwind, and even then prefer "checker FAILs → one `SendMessage` course-correction round"
  (already the orchestrate→delegate→verify loop) over continuous supervision. A heavier mechanism is a
  regression unless it earns its cost.

## Right-size the ceremony to the thread

The full machinery (ledgers, ground-truth differentials, drift gates) pays off on recovery /
reconstruction / migration / audit work. For a trivial doc edit or a pure refactor, the heavy parts are
overhead — keep the orchestrate→delegate→verify split and single-writer rule, and skip the rest. A
heavier method is a regression, not an improvement.
