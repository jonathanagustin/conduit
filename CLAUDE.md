# CLAUDE.md

Guidance for Claude Code working in this repository.

## Operating doctrine

@ORCHESTRATOR.md

Three companion documents are **not** imported here, deliberately — loading them
into every run is the overhead the doctrine above exists to avoid:

- [`HIGH_ASSURANCE.md`](HIGH_ASSURANCE.md) — read it when, and only when, the
  work is high-impact, sensitive, irreversible, or externally consequential.
  Releasing, publishing, deploying to a tenant, and anything touching
  credentials or production data qualify.
- [`HANDOFF.template.md`](HANDOFF.template.md) — the shape a handoff takes.
  Instantiate for substantial or persistent work only.
- [`HARNESS_REQUIREMENTS.md`](HARNESS_REQUIREMENTS.md) — what this environment
  enforces outside the model, and the one control that currently reads as
  enforced and is not. Read before assuming a guardrail will stop you.

Task-specific goals, success criteria, budgets, deadlines, and authorization
arrive in the task message. They are not recorded here, because a permanent
prompt is the wrong place for a per-task contract.

## Where the state is

- [`handoff.md`](handoff.md) — the live handoff. Its top section carries the
  template's shape; the dated sections below are the historical record.
- `docs/operations/` — how to run the thing.
- `docs/decisions/` — the ADRs, including ADR 0006, which governs when the gate
  may run automatically.

## Verification, before trusting any of the above

`./scripts/verify-local.sh` runs every gate `.github/workflows/platform-ci.yml`
runs, on this machine, and reads that workflow so the two cannot drift. It needs
a Docker/Postgres daemon which is a single shared resource — if another process
holds it, run focused tests instead and say so rather than reporting a suite you
did not run.

A green **Trunk gate** is evidence, not authorization. It does not permit a
merge, a release, a publication, or a deployment.

## Two rules this repository learned the hard way

- **Verify by output, not status.** `Running` pods with broken `logs`, and
  "successful" image imports landing zero images, both happened here. An exit
  code you did not read is not a result.
- **Mutation testing has a specific meaning.** After writing a test, revert only
  the fix, leaving a program that still **compiles**, and confirm the test
  fails. A build failure is not a kill.
