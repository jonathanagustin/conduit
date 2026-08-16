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
- **Close the issues whose work is already on `main`.** #13 (reviewer-requirement
  satisfiability) and #25 (intermittent Environment silence) both shipped in
  PR #29 and still carry `status:needs-review`. #16 (scan-before-install) shipped
  its one real gap — freshness — in PR #35, its premise having proved false.
  These need no decision; they need closing.
- **Close the decided issues.** #17 (Foundry Space) → **won't-do** (serving a
  synthesized `spaceId` behind an `OBSERVED` coordinate would make the
  provenance disclosure lie; recommend close). #19 → already closed. #21 (Gitless
  propose principal) → recommended **option 3** (per-binding nominated
  principal), kept deferred until a real consumer. #26 (Namespace) → **defer**.
- **Decide what to do about the merge queue (#20).** This was previously written
  here as a setting to flip. It is not one. GitHub offers merge queues on public
  repositories, and on private ones only under Team or Enterprise Cloud, so this
  private repository under a personal account has no "Require merge queue"
  control at all — confirmed 2026-08-16 against `main`'s branch-protection form,
  where the option is absent between "Require linear history" and "Require
  deployments to succeed before merging", with "Require a pull request before
  merging" ticked and its sub-options rendering. Rulesets are gated identically.
  So `merge-queue.yml` (PR #33) stays inert unless the repository moves to an
  organization on GitHub Team, or becomes public — a decision about repository
  ownership rather than about CI. If it ever does move, the required check must
  become `Merge queue` and not `Trunk gate`: `platform-ci.yml` is
  `workflow_dispatch` with a required pull-request input and cannot run on a
  `merge_group`, so leaving it required would strand every merge group waiting
  for a status nothing posts.
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

## Bootstrapping a fresh session

The whole tree is on `main` (PRs #30–#44 merged), so a new session does not
re-apply any work — it re-orients and re-provisions. Live PR subscriptions and
scheduled self-check-ins are session-local and do not carry over; re-`subscribe_pr_activity`
for anything that needs watching.

### First message to paste into the new session

```
Work on jonathanagustin/conduit. Attach it with add_repo if it isn't in scope,
then clone and register it.

Before doing anything, read these to load context -- do not skip:
- handoff.md                                  (this file: state, open items, gotchas)
- docs/operations/manual-ci.md                (CI is manual workflow_dispatch)
- docs/operations/ASSUMPTIONS-LEDGER.md       (A1-A9; A4 overturned by ADR 0006)
- docs/decisions/0006-automated-workflow-triggers.md
- docs/operations/local-verification.md
- docs/operations/controlplane-ceiling.json   (line ceiling; only moves down)
- AGENTS.md and docs/operations/AGENTS.md

Adopt these working rules for this repo:
- New branch per issue/change; open a PR; never push to main. CI is manual -- I
  dispatch the Trunk gate (platform-ci.yml) against the PR head per manual-ci.md.
- Run ./scripts/verify-local.sh before handoff; it runs every gate CI runs.
- Ground every Apollo/Hub-contract claim in the code (grep the Go json tags /
  const values); never reason from plausibility. Console fixtures must be
  corrected from the Go source, never from what makes a test pass.
- MAPPED-or-REFUSED: the Apollo GraphQL interface serves a coordinate from real
  Conduit data or refuses it; never null, never a synthesized constant behind an
  OBSERVED coordinate.
- internal/controlplane has a line ceiling that only moves down; raising it needs
  a justification in the same commit. New behaviour belongs in a bounded context.
- After any web/src change, run `pnpm --dir web build` and commit the regenerated
  internal/consoleui/dist bundle (verify-console-assets.sh checks it byte-for-byte).
- Put design decisions and premise-reversals through an adversarial review (a
  fresh agent, ideally model: fable) before implementing -- it caught #17 and #16.

Then tell me the current open issues and which decisions still need me, and wait
for direction. Do not carry a to-do list in from this block: it previously said
"close #17 won't-do, #19/#20/#21/#26; enable the merge queue in branch
protection", which had gone stale in both directions -- #17 and #16 are closed,
and the merge queue is not a setting to flip (see the body of this handoff).
Read the issue list rather than this paragraph.
```

### Shell bootstrap (rebuild the toolchain)

```bash
# Repo (skip the clone if add_repo already placed it):
cd /workspace/conduit || (git clone <clone-url-from-add_repo> /workspace/conduit && cd /workspace/conduit)
git fetch origin && git checkout main && git pull

# Toolchains verify-local.sh needs on PATH (it fetches pinned helm/hadolint/zarf itself):
#   Go (go.mod toolchain go1.26.6), Node 24.x + pnpm 11.x, ruby, jq -- usually preinstalled.
pnpm --dir web install --frozen-lockfile

# Only for the mutation SWEEP (not the Trunk gate): pinned gremlins.
GOTOOLCHAIN=go1.26.6 go install github.com/go-gremlins/gremlins/cmd/gremlins@v0.6.0

# Prove the tree is where we left it:
node scripts/verify-console-contract.mjs        # 7 vocabularies match the Hub
node scripts/verify-mutation-scores.mjs         # mutation baseline intact
./scripts/verify-local.sh                        # full gate set (downloads pinned tools, runs -race, console, etc.)
```
