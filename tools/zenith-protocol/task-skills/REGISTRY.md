# Task-skill registry — reusable agent-assignment specs (TRIAL — PDR P13)

> **Status: PROPOSED/TRIAL, not adopted.** This is a provisional dimension being measured before it
> becomes a default. Use it, but do not treat it as settled method until PDR P13 graduates.

A **task-skill** is a reusable `SKILL.md`-style spec for a *recurring kind of delegated thread*, so the
orchestrator cites a skill + a few parameters instead of re-writing a ~60-line bespoke brief every time.
It captures the repeatable ~75% of a brief (harness, comparison rules, return format, budget guard,
`process_feedback` contract); the delegation supplies only the task-specific ~25% (which service, which
inputs, which gate values).

Each task-skill is **extracted from ≥2 real threads that already ran**, never invented — same discipline
as any other artifact (§0a). A skill that has only ever been imagined is a hypothesis, not a task-skill.

## How to use (during the trial)

When delegating a thread that matches a registered task-skill, the brief becomes:
> "Follow task-skill `<name>` (`task-skills/<name>.md`). Parameters: <the
> task-specific bindings>. Return in the skill's format."
Then orchestrate → **independently re-verify** → commit exactly as always. The task-skill changes *how the
brief is written*, nothing about the verify/commit/ledger discipline.

## Registry

| task-skill | use when delegating… | extracted from | acceptance gate |
|---|---|---|---|
| _(none extracted yet)_ | — | — | — |

Candidates to extract once each has run ≥2× and is diffed: an **office-toolset build** (Excel + Outlook
already ran from `office-com-toolset/SKILL.md`) and a **reverse-engineering research thread** (the Teams
surface fan-out ×3 + the protocol-extraction ×2 share one shape: read source → implementation-ready spec
in a fixed format → cite every source).

## Adding a task-skill (only after a pattern recurs ≥2×)

1. Diff the ≥2 briefs that share the pattern; the common part is the task-skill body, the differing part
   is its parameter list.
2. Write `<name>.md` with: goal, the shared harness/rules, the parameter list, the acceptance gate, and
   the `process_feedback` return contract.
3. Add a registry row here (with `extracted from`).
4. Measure the next use (brief length before vs after; same gate pass rate) and log it in PDR P13.
