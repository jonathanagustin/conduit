# Handoff

A snapshot of the work done in the session ending 2026-08-16, what is merged,
what still needs a decision, and the mechanisms a next contributor should know
about before touching this tree. Everything below is on `main` unless marked
otherwise. Issue/PR states were current at handoff; verify against GitHub as
the API was rate-limited when this was written.

## What shipped this session

### Backlog driven to terminal states
- **#28 — comprehensive mutation testing** (PR #30). Real efficacy baselines per
  bounded-context domain (gremlins) and per console file (Stryker), recorded in
  `docs/operations/mutation-scores.json`. A dispatched sweep
  (`scripts/mutation-sweep.sh`, `.github/workflows/mutation-sweep.yml`, now
  weekly) measures and ratchets; a cheap offline gate
  (`scripts/verify-mutation-scores.mjs`, in the Trunk gate) refuses a baseline
  that has rotted. Weakest domain at handoff: `internal/catalog/domain`.
- **A4 overturned** (PR #31, ADR `docs/decisions/0006-automated-workflow-triggers.md`).
  Automated triggers (`schedule`, `merge_group`, …) are now permitted for
  verification/maintenance under cost guardrails; publication/release stay
  manual. See `docs/operations/manual-ci.md`.
- Follow-on automation unblocked by ADR 0006: **weekly mutation sweep** (PR #32),
  **merge-queue workflow** (PR #33, `.github/workflows/merge-queue.yml`),
  **scheduled Apollo-docs drift crawl** (PR #34,
  `.github/workflows/documentation-drift.yml`).
- **#16 — scan-before-install** (PR #35). The issue premise was **false**:
  `ArtifactSecurityPolicy` already blocks install of an unscanned/non-PASS
  Release at plan+apply, and the Hub auto-scans at proposal. The one real gap
  was **freshness**; added `ArtifactSecurityPolicy.MaximumScanAgeSeconds`,
  enforced by rescanning stale evidence at proposal time (not in the clock-free
  `Validate`).

### New console screens (the four placeholder routes)
- **Teams** (#36), **Risk Management** (#37), **Export** (#38), **Import** (#39).
  Each reads a live Hub contract (`/v1/teams`, `/v1/catalog-retirements`,
  `/v1/export-pipelines`, `/v1/bundle-imports`), follows the established page
  conventions, and ships a Playwright POM + spec. The `Placeholder` component is
  now unused.

### Accuracy audit and remediation
An audit diffed **all 53 console readers (~230 fields) against the Go `json`
tags and const values.** Field names were 100% accurate; six status/enum drifts
were found and fixed:
- **Import status vocabulary** (PR #40): the screen guessed
  `IMPORTED`/`BLOCKED`; the Hub emits `ADMITTED`/`PENDING_APPROVAL`/`ERROR`.
- **Export lifecycle** (PR #40): `ACTIVE`/`ARCHIVED`/`DELETED` were unshown, so a
  deleted pipeline read as active.
- **Session applications** (PR #41): the Hub never advertised
  teams/risk/export/import, so those four screens were **unreachable via the
  rail** (URL-only). Now advertised, gated by each endpoint's read capability.
- **Four status→colour maps** (PR #42): `ROLLOUT`/`CONFIG`/`LIFECYCLE`/`PLAN`
  keyed on invented values while omitting real ones, so a `BLOCKED` rollout or
  `RECORDED` plan rendered neutral. Plus a latent `useReviewChangeRequest`
  return-type lie.
- **Contract gate** (PR #43, `scripts/verify-console-contract.mjs`): reads the
  Go source live and fails CI when a console status vocabulary drifts from the
  Hub's constants. Seven vocabularies covered; a planted-violation test proves
  it rejects invented and dropped values.

## Still needs the repository owner

These are decisions or build-or-close calls, not agent work:
- **Close the decided issues.** #17 (Foundry Space) → **won't-do** (serving a
  synthesized `spaceId` behind an `OBSERVED` coordinate would make the
  provenance disclosure lie; recommend close). #19/#20 → decision recorded in
  ADR 0006 and their implementations shipped. #21 (Gitless propose principal) →
  recommended **option 3** (per-binding nominated principal), kept deferred
  until a real consumer. #26 (Namespace) → **defer**. Verify these are closed.
- **Enable the merge queue** in `main`'s branch-protection settings and mark the
  `Merge queue` check required — the workflow (PR #33) is inert until then.
- **Merge order for stacked workflow PRs:** the doc-drift crawl (PR #34) needs
  `verify-documentation-manifest.mjs`, which arrived with PR #29 — already on
  `main`, so nothing pending, but note the dependency if reverting.

## Mechanisms a next contributor must know

- **MAPPED-or-REFUSED / provenance tiers.** The Apollo GraphQL interface either
  serves a coordinate from real Conduit data or *refuses* it (never nulls, never
  fabricates). `OBSERVED`-tier coordinates carry a disclosure that the value came
  from an Apollo Hub, so serving a synthesized constant behind one is a lie. This
  is why #17 is won't-do. See `internal/apollographql/`.
- **The controlplane line ceiling** (`docs/operations/controlplane-ceiling.json`)
  only moves down; raising it requires a justification in the same commit. New
  behaviour belongs in a bounded context, not `internal/controlplane`.
- **Fixtures must be corrected from the Go source, never from what makes a test
  pass.** The console's browser tests mock the Hub, so a fixture using an
  invented status passes while lying. This burned six times; the contract gate
  (PR #43) and the unit tests in `web/src/test/unit/logic.test.ts` now guard the
  status vocabularies, but the discipline still applies to any new reader field.
- **The embedded console bundle** (`internal/consoleui/dist`) is committed and
  checked byte-for-byte by `scripts/verify-console-assets.sh`. After any
  `web/src` change, run `pnpm --dir web build` and commit the regenerated
  bundle, or the console gate fails. Parallel console branches conflict on this
  artifact; resolve by re-merging `main` and rebuilding.
- **Local verification:** `./scripts/verify-local.sh` runs every gate the Trunk
  gate runs (it reads `.github/workflows/platform-ci.yml` so the two cannot
  drift). CI is manual (`workflow_dispatch`); dispatch the Trunk gate against a
  PR head per `docs/operations/manual-ci.md`. A green run is evidence, not
  authorization to merge or release (assumption A5).
- **Mutation testing:** one hand-planted mutant at a time via
  `scripts/verify-mutant-killed.sh` to prove a specific gate rejects; the
  dispatched `mutation-sweep.sh` measures the suite as a whole. gremlins jitters
  killed/timed-out run-to-run, so the sweep carries a tolerance band.

## Follow-ups worth considering (not started)
- Extend the contract gate to import-level statuses if they are given named Go
  consts (currently inline literals; unit-test-pinned). Checked 2026-08-16: the
  Bundle import *task* vocabularies already have named consts and still cannot
  be added, because the gate checks set equality both ways and the console side
  is a deliberate partial map (`statusIntent`) plus a raw-string stage render.
  Adding either needs a console change first, so it is a design decision, not
  the chore it resembles. Full reasoning is in the gate's own header.
- Build-or-close the optional features their decisions unblocked: a Conduit-native
  per-Environment scan-before-install *strictness* beyond freshness (#16 notes),
  or wiring the Gitless propose principal (#21) when a consumer appears.
- Comprehensive mutation testing of `internal/controlplane` itself (deliberately
  excluded from the current sweep as the largest, least-changing target).
