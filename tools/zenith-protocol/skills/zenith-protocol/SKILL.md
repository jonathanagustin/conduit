---
name: zenith-protocol
description: A portable, self-improving operating model for agentic work — orchestrate → delegate → verify, backed by five durable ledgers, ground-truth gates, and a meta-retrospective loop. Use at the start of any substantial multi-step task (recovery, migration, audit, reconstruction, large refactor) to decompose into verifiable threads, delegate legwork to subagents with tight briefs, independently re-verify every result before committing, and keep the METHOD itself measurably sharpening via PDRs, agent votes, and a metrics ledger. Invoke when a session should run as orchestrator+verifier rather than doing work serially in the main loop.
---

# Operating model — orchestrate → delegate → verify, and improve the method itself

This skill installs a **way of working**, not a task. The main session is the **orchestrator + verifier**;
subagents do the legwork; and the *method itself* is treated as a product that must get measurably
sharper over time. It is repo-agnostic — paths come from `.zenith-protocol.json` (run `/zp-init` to
scaffold). The tools below are shipped with the plugin under `scripts/`.

## 0. Read the ledgers first — don't rediscover

Before any work, read the five durable ledgers (scaffold them with `/zp-init`; defaults under
`docs/ledgers/`). They are the memory that stops you re-deriving solved problems:

| ledger | holds |
|---|---|
| `CAPABILITY-MAP.md` | what's **SOLVED / BLOCKED / OPEN**, keyed by hypothesis (H#) |
| `HYPOTHESIS-BACKLOG.md` | surveyed-but-unpursued leads (B#) to pick from |
| `ASSUMPTIONS-LEDGER.md` | load-bearing claims (A#): **ASSUMED / CONFIRMED / REFUTED** |
| `PROCESS-IMPROVEMENTS.md` | method changes as **PDRs** (PROPOSED→TRIAL→ADOPTED) |
| `METRICS.md` + `metrics-ledger.jsonl` | durable, traceable measures over time |

**Write back to them in the same change that taught you something.** A finding that lives only in a
commit message or chat transcript will be rediscovered.

Then read every configured `contextDocs` entry in order. These are the supported project, domain,
and situation overlays: authority boundaries, topology, safety/regulatory constraints, value policy,
and local gates. The portable core yields to system policy, repository instructions, and user
authority. If delegation or commit/push is not authorized, preserve the same decomposition and gates
while executing serially or leaving a verified diff; portability never grants permission.

## 0.5. Control the environment before automating — TRIAL

Apply this provisional phase before decomposition or action when UI, remote-desktop, VM, browser,
terminal, device, or long-running interactive work can drift with sleep, session, focus, windows, or
the control plane:

1. **Observe read-only first.** Identify the real target, control plane, user/session, foreground,
   visible activity, and relevant process/device state. Do not infer them from a stale screenshot or
   a successful transport receipt.
2. **Preserve rollback facts.** Snapshot every reversible setting that may change, including its exact
   identity, prior value, and the command or API that can restore it.
3. **Stabilize only within authority.** Adjust power, display, or lock behavior only when the task
   authorizes that mutation. Never change security or power policy for a read-only or unauthorized
   task; report the constraint instead.
4. **Make work observable.** Keep current activity user-visible, or explicitly disclose that it runs
   in the background and name the authoritative status surface. Normalize window estate so the
   target, foreground, prompts, and user-owned windows remain distinguishable.
5. **Prove liveness once.** Cause one harmless, reversible visual transition through the intended
   input path (for example, focus a known window or move a pointer), then observe the expected change.
   If it fails or is ambiguous, stop; do not compensate with blind retries or coordinate sequences.
6. **Establish recovery before action.** Name an out-of-band recovery path and an authoritative status
   probe that remain usable if the interactive surface freezes or disconnects.

Proceed to automation only when a fresh readiness record names the target/control plane/session,
captures rollback facts for every planned setting change, accounts for foreground and visible-window
deltas, proves exactly one liveness transition, and confirms both recovery and status paths. Record
the count of satisfied checks and required checks; readiness is **PASS only at N/N**, otherwise stop.
Treat this phase as **TRIAL**, measure whether it prevents invalid actions without adding net-negative
latency or disruption, and do not promote it to a default outside applicable drift-prone work yet.

### 0.75 Separate durable state from disposable acceleration — TRIAL

Temporary storage may hold caches, extracted snapshots, or isolated execution workspaces, but never
the only copy of evidence, configuration, source, or bootstrap logic. When a workspace offloads build
state to `/tmp` or another restart-volatile volume, keep an idempotent repository-owned bootstrap that
recreates the cache target before dependency installation. Canonical dependencies must resolve from
an immutable lock coordinate with integrity metadata; keep mutable sibling-source links as an explicit
development override, not a frozen-bootstrap default. Verify semantic loadability—the exact version,
declared export artifact, and a real import—not merely the existence or realpath of a package link.
Test cold reconstruction, the second no-op run, and a manifest whose declared export is missing. A
warm cache passing today is not recovery evidence; run the bootstrap and semantic drift check at the
commit boundary whenever the offload participates in verification.

## 1. Decompose → delegate → single-writer

1. **Decompose** the task into threads that don't share a file-set. Independent threads run
   concurrently; only threads that write the same path are serialized.
2. **Delegate the legwork** to subagents when active execution policy authorizes it; otherwise execute
   the same bounded threads serially. Each brief gives: the ground-truth harness it needs, a fixed
   **return format**, and its **acceptance gate** (the specific check it must pass). Probes are
   READ-ONLY unless a write is explicitly authorized. Investigation agents **return findings, never
   commit**; implementation agents **edit their file-set + run their gate + report the diff**.
3. **Single-writer per file-set** — at most one implementation agent on a given path at a time; any
   number of read-only investigators run in parallel alongside it.
4. **Brief the ground-truth SOURCE + the COMPARISON — never pre-assert the ANSWER.** Give the agent the
   evidence to check against and the check to run; let the gate DISCOVER the result. A brief that names
   the expected answer ("field X is absent") can mislead when reality differs (e.g. an empty optional
   serializes as an explicit `null`, not an absent key) — the agent may pattern-match your assertion
   instead of measuring. Ground-truth pointers and named tool/hazard constraints are the
   highest-leverage brief content; pre-asserted conclusions are the lowest.

## 2. Gate everything — ground truth over vibes

Prefer **extract / generate / compare-like-for-like** over hand-authoring. A hand-written table *about*
someone else's system is a smell — a recoverable artifact was skipped. Two standing gates ship with the
plugin (`pnpm`/`node` from your repo root):

- `node <plugin>/scripts/verify-no-drift.mjs` — every generated module must be **idempotent-emitted +
  drift-gated** (a fresh emit reproduces the committed bytes; anchor each to an `@evidence` sha).
- Prove behavior by **difference**, not just compile: stand your thing up, issue the same operation to
  yours and to the source, require an exact match, and report every structural difference.
- Prefer a semantic contract probe over source-spelling inspection. A test that searches implementation
  text is opaque: a behavior-preserving refactor can make it fail, while a behavior-breaking edit can
  preserve the searched phrase. Call the exported boundary and assert its observable result. Declare
  native tests against the runner that owns their prerequisite, retain portable pure-contract cases on
  every runner, and report **pass, skip, and unavailable separately**—a skip is not compatibility evidence.

## 3. Independently re-verify — never trust a self-report

The orchestrator **re-runs the implementation gate/build itself** before promotion and sanity-checks
the diff. It commits and pushes only when repository policy and user authority allow it. This has
caught real defects a self-report claimed were clean.

### 3.5 Heterogeneous evaluator trial — TRIAL

For consequential reviews where one evaluator's blind spots matter, run a measured heterogeneous
trial rather than asking several models informally. This is provisional and must earn its recurring
cost through verified unique-defect yield.

1. Freeze one immutable repository commit and scoped tree. Hash a deterministic archive, run a pinned
   credential-pattern scan with zero matches, and materialize the archive into an isolated read-only
   execution workspace. A temporary workspace is acceptable; the receipt is not. Never depend on
   `/tmp` for durable evidence.
2. Give every arm byte-identical prompt bytes and the same readable materialized scope. Do not require
   `.git` access. Record provider, model family/model/version, invocation/session IDs, raw receipt
   SHA-256, input/output/cache tokens, provider and wall latency, and monetary cost with pinned price
   provenance. If billing cannot be observed, record `UNAVAILABLE` plus the exact reason; never encode
   unknown cost as free.
3. Require each arm to report `COMPLETE` or `INCOMPLETE` input access and retain every residual unknown.
   A zero-finding incomplete run has unavailable recall, not measured zero recall.
4. Treat every model finding as a hypothesis. An independent verifier assigns the cross-model
   `canonicalClusterKey`, reproduces the claim, and records `VERIFIED_DEFECT`, `FALSE_POSITIVE`, or
   `UNRESOLVED`. Correlated wording and model-supplied cluster keys are not independent confirmation.
   Verified defects require an executable gate.
5. Never rewrite the original batch. Its content identity excludes the append-only `resolutions`
   list, while each resolution binds its own content identity to the trial ID. Later evidence may close
   only an unresolved finding. Recompute metrics over the original batch plus ordered verified resolutions.
6. Compare arms and batches by precision, false discovery rate, recall against bounded batch truth,
   run-level unique verified clusters, tokens, provider/wall latency, money/status, and evidence bytes.
   Do not optimize a single scalar. Keep this step only when the next trial improves calibrated yield
   or supplies complementary coverage worth its whole-system cost.

Start from `templates/EVALUATOR-TRIAL.json` and run
`node <plugin>/scripts/verify-evaluator-trial.mjs <trial.json>`. The checker validates evidence shape,
file digests, explicit unknowns, canonical clustering, gates, and append-only resolution semantics;
it cannot prove that a claimed gate was genuinely executed, which remains the orchestrator's
independent-verification duty. Add `--json` to derive comparable per-arm and whole-trial precision,
false-discovery, recall/status, unique-cluster yield, usage, latency, and cost/status measurements from
the verified evidence. Persist that output or append its objective values to the repository metrics
ledger; never hand-copy a model's self-reported calibration.

## 4. Promote according to repository authority

When commits are authorized, use one logical change per commit and the repository's branch/promotion
rules. Otherwise retain a verified diff and report it. Never manufacture commit, merge, deployment,
or production authority from this protocol.

## 5. Self-improve — the REQUIRED closing step (this is the engine)

Every thread ends by making the METHOD sharper, under a hard discipline. This is what separates the
operating model from a mere checklist.

- **Write back what you learned** — assumptions → `ASSUMPTIONS-LEDGER.md`, system facts →
  `CAPABILITY-MAP.md`, method lessons → `PROCESS-IMPROVEMENTS.md`.
- **Measure it durably.** Justify any method change with an OBJECTIVE number (wall-clock, tokens,
  defects-caught, a count) and record it with `record-metric.mjs` — append-only, git-SHA-stamped, so a
  later cycle can ask "is this still earning its cost?" from data, not memory. Prose is a snapshot; the
  ledger is the time-series.
- **Holistic value check.** Weigh a change across the WHOLE system: stakeholder function, quality,
  safety, latency, tokens, money, operational load, and lifecycle complexity. Preserve conflicting
  criteria as a Pareto set until an explicit policy chooses among them; do not hide trade-offs in one
  ungrounded score. Prefer the smallest non-dominated change and REMOVE a step that stopped earning
  its total cost. For substantial optimization, invoke the `optimize-evidence` skill.
- **A change is not the status quo the moment you think of it — it earns adoption through a trial.**
  Every method change is a **PDR** with an ADR-style lifecycle: **PROPOSED → TRIAL** (used but
  provisional; measured against a stated exit criterion over N cycles) **→ ADOPTED** (only then written
  into the operating model as a default) **or REJECTED/SUPERSEDED**. Do NOT hard-code a PROPOSED/TRIAL
  idea as settled; let the measure decide.
- **Agents vote on the process they used.** Every agent's return ends with a `process_feedback` verdict
  on the operating model *as they experienced it this thread* — one of `good / bad / insignificant /
  out-of-scope / missing-the-big-picture` — with a one-line **objective** justification. Subjective
  reactions are allowed but must be prefixed `SUBJECTIVE:` so they're never mistaken for evidence. Tally
  votes into the relevant PDR. A `bad` / `missing-the-big-picture` vote with a real reason is a trial
  signal, not noise. Keep ONE running `good×N` tally in a single cell so the automated count stays honest.
  Treat votes as evaluator observations, not adoption evidence by themselves: record the evaluator
  identity and later disposition, and calibrate repeated evaluators against defects verified by gates.
- **Use MADR for durable architecture choices.** Create a numbered Markdown Architectural Decision
  Record from `templates/MADR.md` (or `/zp-madr`) when multiple viable designs exist and the selected
  trade-off will matter to future maintainers. Keep method experiments in PDRs, tasks in the backlog,
  and uncertain facts in the assumptions ledger. Link evidence snapshots, Pareto/value assessments,
  experiments, approval, executable confirmation, and superseded records instead of duplicating them.
  A MADR explains a decision; it never grants execution authority or replaces a gate.
- **Meta-retro every N≈5 threads.** Run `node <plugin>/scripts/meta-retro.mjs --record`. It measures PDR
  state, vote spread, method size (bloat proxy), and ledger usage, and flags **CHURN / RUBBER-STAMP /
  BLOAT / UNDER-MEASURED**. It obeys the rule it audits: it must apply a change (adopt / downgrade /
  tighten / REMOVE) or record the measure that says none is needed. A meta-retro that only ever prints
  "healthy" is itself a removal candidate.

## Conformance — check the repo actually FOLLOWS this, don't just trust it

The invariants above are **testable**. `node <plugin>/scripts/conform.mjs` (or `/zp-conform`) enforces
the **mechanically-checkable** ones: the 5 ledgers exist + parse, `metrics-ledger.jsonl` is valid JSONL
with the required fields AND **append-only** across git history (corrections `supersede`, never edit in
place), each PDR has a valid status + (if ADOPTED) a non-empty measure + **one** `good×N` vote tally,
every proposed hypothesis is gatekept, BLOCKED/OPEN capability rows name an instrument, and the drift
gate is green. Exit non-zero on any violation, with the exact file/line. Run it in the orchestrator's
re-verify step and/or a `Stop` hook — the cost is one sub-second script run (see the README's
enforcement-posture analysis; a live supervisor agent costs ~2× and duplicates the orchestrator, so
reserve it for high-stakes long threads only).

**The attestation gap — do NOT fake a check for these.** Three requirements are not mechanically
verifiable: that the orchestrator *truly* re-ran the agent's gate, that a `process_feedback` vote's
objective line is *genuine* (not a rubber-stamp), and that a change was *holistic*. These are covered by
**attestation + review**: the vote line IS the attestation record, and the orchestrator reads the return
**body** for real critique before trusting the vote. A green `conform.mjs` means the ledgers are
well-formed, not that the method was practiced in good faith.

## This protocol is a living artifact — keep it current

The skill/tools/templates here are the **canonical portable copy** of the method, not a frozen snapshot.
When a method lesson proves out in a project using this protocol — a PDR graduates to ADOPTED, the
meta-retro repeatedly catches a class of defect, a brief pattern demonstrably saves rework — **propagate
it back into this plugin in the same spirit it was earned**: under the trial discipline (don't hard-code
an unproven idea), with the smallest change that carries the lesson, and only if it's net-positive for
the whole method (a heavier protocol is a regression). Equally: **remove** a step here that stopped
earning its cost. The measure of this plugin is whether it gets measurably *sharper*, not bigger.

## When blocked

Formulate a new numbered hypothesis, record it (capability map / backlog), then get past it — or mark it
**BLOCKED** with the exact instrument that failed (the tool + the error), so it's never rediscovered.
Keep everything repeatable with committed code.

## Hypotheses have an intake + a gatekeeper — from the orchestrator AND from agents

New hypotheses may be formulated by the orchestrator proactively (don't wait for a survey or a block —
propose your own) OR by a delegated agent (via `proposed_hypotheses` in its return). Every proposed
hypothesis is **accounted for** in the backlog intake with its **origin**, then the orchestrator
**gatekeeps** it: ACCEPT → assign an id and track it, or REJECT → log a one-line reason so it's never
re-proposed. Nothing an agent proposes is silently dropped; nothing is silently adopted — the same
discipline as PDRs, one level down for hypotheses.

## The return-format contract (paste into every delegated brief)

```
1. <task-specific results>
...
N-1. proposed_hypotheses:            # optional — leads/blocks/generalizations you spotted
   - <hypothesis>  (why it's worth tracking)
N. process_feedback:
   verdict: good | bad | insignificant | out-of-scope | missing-the-big-picture
   objective: "<one line, measured/counted — what the process cost or saved you THIS thread>"
   subjective: "SUBJECTIVE: <optional feeling; must keep this prefix>"
```
