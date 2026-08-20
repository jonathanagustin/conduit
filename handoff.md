# Handoff

**Updated:** 2026-08-19 UTC
**Lead or owner:** orchestrating session (see `docs/operations/ORCHESTRATOR.md`)
**Status:** active — Apollo `core/whats-new` parity program in flight (see the
2026-08-19 section at the end). The 2026-08-18 authorization/credential blocker
below is the prior focus, left as recorded; this session did not touch it.

This file carries the shape of [`HANDOFF.template.md`](HANDOFF.template.md) at
the top — current state, decisions, next actions — and the dated historical
record below it. The history is appended to, not rewritten; a correction to an
earlier session's claim is made where the claim is, and marked.

## Mission

- **Goal:** keep `main` correct and shippable while closing the defects the
  gate cannot catch — authorization and credential-destination faults, which
  pass CI by construction.
- **Deliverables:** merged pull requests with reproducible evidence; the
  operating structure in `docs/operations/ORCHESTRATOR.md` and its companions.
- **Measurable success criteria:** Trunk gate green on the head commit; every
  claimed fix killed by a **compiling** mutant; no regression introduced by a
  fix.
- **Constraints and non-goals:** a green gate is evidence, never authorization
  (A5). Releasing, publishing, and deploying to a tenant each need explicit
  human instruction. Do not run `./scripts/verify-local.sh` while another
  process holds the shared Docker/Postgres daemon.
- **Authorized side effects:** branches, pull requests, issues, and merges in
  `jonathanagustin/conduit` under the standing instruction to handle pull
  requests. Nothing outside this repository's scope.

## Current verified state

- `[VERIFIED]` `main` is at `8d16d35`; merges through #273 have landed —
  checked at 2026-08-18 17:38 UTC via `git fetch && git log`.
- `[VERIFIED]` **A live regression is on `main`.** `registrychallenge.CheckRedirect`
  refuses a credentialed cross-host redirect. Docker Hub answers a credentialed
  blob read with `307 → production.cloudfront.docker.com`, measured directly
  from this sandbox at 17:39 UTC, so `ocipublish.CurrentDigest` — the read the
  Flux converger performs every reconcile — fails against any CDN-backed
  registry. Fix open as PR #274.
- `[VERIFIED]` **#242's regression guard is gone from `main`.** Reverting the
  `intended := *request.URL` capture compiles and the whole `ocipublish`
  package still passes. #248 removed the only thing that made the pre- and
  post-round-trip URLs differ. Restored in PR #274.
- `[VERIFIED]` The two #247 review findings were corrected on `main` by later
  work: `resource_decision.go` now states plainly that "this cannot decide
  without filing" is false for `Administrator`, and the parity test carries the
  `alice → bob` row pinning the delegated-administrator widening.
- `[VERIFIED]` The frontend is green on `main`: 70 unit, 122 stubbed e2e, and
  12 live specs against a real Hub (approve → Agent executes → `SUCCEEDED`),
  plus the embedded-asset check.
- `[VERIFIED]` The console already has a hydration seam: the Hub replaces
  `<!--conduit-console-bootstrap-->` in `web/index.html` with a meta tag, and
  `internal/consoleui/bootstrap.go` defines `conduit.console.bootstrap/v1`. It
  **deliberately** carries no API-origin, authentication, capability, script,
  feature-toggle, or security-policy fields, so tenant data cannot smuggle in a
  capability.
- `[VERIFIED]` A written control does not hold: `.claude/settings.json` denies
  `Bash(gh pr merge:*)`, but merges via `mcp__github__merge_pull_request` do not
  match that rule and have succeeded. See `docs/operations/HARNESS_REQUIREMENTS.md`.
- `[UNKNOWN]` How Apollo Hub supplies runtime configuration to its frontend.
  Matters because the hydration direction was chosen as "however Apollo Hub does
  it", and this repository forbids inferring Apollo behavior. Cheapest probe:
  search the pinned snapshot at
  `docs/apollo/snapshots/apollo-openapi-c663874bd7467a01e719d039509b0f206d05fe2c`
  for a config or session-projection operation; expect the answer to be
  "not established", which is a valid answer.

## Decisions

- **Strip the credential across a redirect rather than refusing to follow** —
  alternatives: keep refusing; refuse only the scheme-downgrade row. Reason:
  the policy sits on the client, so it governs credentialed blob reads, and
  refusing breaks CDN-backed registries; stripping keeps the identical set of
  destinations that may receive the credential. Evidence: PR #274, five
  compiling mutants. Reversible: yes.
- **Adversarial review before merging security-relevant work** — reason: two
  live credential leaks (#228, #242) were found by review of already-merged,
  already-green code, and review of #247/#248 found two more real defects the
  gate could not. Evidence: the review reports. Reversible: yes, but the base
  rate argues against.
- **Superseded:** "the Trunk gate job's conclusion may differ from the status it
  publishes" — closed by #249; both surfaces now agree.

## Active workstreams

- PR #274 — strip-not-refuse plus the restored #242 guard — gate running as of
  17:49 UTC — no blocker — high return, it clears a live regression.
- Multipass IAM port and Apollo hydration research — both agents killed by a
  session limit before producing anything — blocked on re-dispatch, not on
  information.
- **Integration owner:** this session.

## Actual cost and value

- Four subagents lost to a session limit (reset 13:40 UTC) mid-task; ~7 hours of
  wall time passed with other sessions merging through #273.
- Information gained that changed decisions: the Docker Hub 307 measurement, and
  the surviving-mutant proof that #242's guard was gone. Both overturned what a
  status report alone would have said.

## Blocker

- **Exact blocker:** GitHub Actions is not allocating runners for this
  repository. Every job fails within about two to three seconds with
  `runner_id: 0`, no runner name, no `steps` array at all, and a 404 on its
  log. Verified across two independent branches and four run attempts.
- `[VERIFIED]` The whole run finishes in 8 seconds
  (`run_duration_ms: 8000`) against 399 seconds for the last green run
  (`32154882696`). That is the reliable quantitative discriminator.
- `[VERIFIED]` It is not caused by any change on either branch: PR #275 is
  documentation-only and its first run failed identically, and the same
  workflow succeeded on eight runs earlier today (most recent green:
  `32154882696`, 15:30 UTC).
- `[INFERRED]` Possibly an exhausted Actions minutes quota or a reached
  spending limit — this repository is private. Confidence: **low**. There is no
  billing evidence behind this; it rests only on the shape of the failure.
- **A measurement error to not repeat.** This blocker was first recorded citing
  `get_workflow_run_usage` reporting `total_ms: 0` and `duration_ms: 0` for
  every job, as proof that nothing executed. That evidence is worthless: the
  same endpoint reports `total_ms: 0` and `duration_ms: 0` for run
  `32154882696`, which was **fully green** and whose Go suite ran for 4m41s
  with complete logs. This endpoint reports zero for this repository whether or
  not a job ran, so it cannot distinguish the two. Use `run_duration_ms`, the
  absence of a `steps` array, and `runner_id: 0` instead.
- **Minimum external input or authorization required:** someone with billing
  access checks Actions minutes and the spending limit for the account. If that
  is not it, the run pages for `32167586350` and `32168615490` will carry the
  reason in the web UI, which the API does not expose.
- **Useful work preserved at:** branches `claude/fix-redirect-strip-not-refuse`
  (PR #274) and `claude/orchestrator-deployment` (PR #275), both pushed. Both
  trees were verified locally; #274's five lanes all passed on GitHub at 17:55
  UTC before the aggregator stopped being schedulable.
- **Do not** keep re-running. Four attempts produced the identical signature; a
  fifth without new evidence is the repeated unchanged action the doctrine
  forbids. Elapsed time counts as new evidence at most once an hour.

## Prioritized next actions

1. Unblock Actions (see above) — nothing else in CI can proceed until then —
   requires billing access, which this session does not have.
2. Merge PR #274 once the gate can run — clears a live regression on the
   reconcile path — high confidence — low cost — reversible. Its five
   verification lanes already passed on the exact head `6c13975`; only the
   status-publishing job never ran, so the commit status is stuck `pending`
   rather than red.
3. Decide the `mcp__github__merge_pull_request` gate (close it, or correct the
   policy to match reality) — this is the repository owner's call, not the
   agent's — see `docs/operations/HARNESS_REQUIREMENTS.md`.
4. Re-dispatch the Apollo hydration research and the Multipass IAM port — both
   are scoped and briefed; neither is blocked on information or on CI.

## Resume recipe

- **Read first:** this section, then `docs/operations/ORCHESTRATOR.md`.
- **Current branch:** `main` at `8d16d35`; open work on
  `claude/fix-redirect-strip-not-refuse` (PR #274).
- **Fast health check:** `go build ./... && go test -count=1 ./internal/gitless/...`
- **Tests or evaluators:** the Trunk gate; `./scripts/verify-local.sh` when the
  shared Docker/Postgres daemon is free.
- **Mutable facts to revalidate:** `main`'s head, open pull requests, and
  whether PR #274 merged — all of them move between sessions.
- **First recommended action:** check PR #274's Trunk status.

---

## Historical record

A snapshot of the work done in the session ending 2026-08-18, what is merged,
what is still open, and the mechanisms a next contributor should know before
touching this tree. Everything is on `main` unless marked otherwise.

Earlier handoffs covered the sessions ending 2026-08-16 (PRs #30–#44) and
2026-08-17. Their "Mechanisms" and "Bootstrapping" sections are carried forward
below, corrected where a later session proved them wrong.

## External audit, 2026-08-18

Two reviewers from different labs audited `docs/operations/PRODUCTION-READINESS.md`.
Neither had repository access and both said so. Archived verbatim under
`docs/operations/EXTERNAL-AUDIT-2026-08-18.md`; raw text at
`/workspace/audit-archive/` in the session that produced it.

**Both independently ranked the same fix first, and it is real.**
`cmd/platform-delivery/main.go:88-100` lets a Hub start and **sign with an
ephemeral key** when `BUNDLE_SIGNING_KEY_FILE` is unset -- a warning, then it
carries on. Verified against the code before acting. Their reasoning is the part
worth keeping: a restore that "comes up healthy" with a new signing identity is a
split-brain supply chain, and it presents as health. The repository already knows
how to fail closed -- `ALLOW_INSECURE_*_IDENTITY_HEADERS` cannot combine with a
secure mechanism and the chart aborts -- so identity fails closed while signing
fails open.

**The structural finding.** The Air-gap READY row and Tier 2 gap 5 (recalls never
reach disconnected targets) contradict each other, and the tiering hides it by
keeping them on different pages. You cannot claim readiness for disconnected
installation while filing "a recall never arrives there" as a parity nice-to-have.

**The charge worth re-reading before writing the next readiness row:** the
document grades *components*, and production failures are *scenarios*. Walk the
system's own reason for existing -- a CVE at 2am against 40 clusters, 12
disconnected -- and every row reads "partially closed, honestly bounded" while
the mission fails end to end. No row asks the assembled question.

Also found: the residual list fails its own admission criterion ("nothing in this
repository can complete them") for at least Renovate activation; nothing asserts
cross-tenant isolation for core resources, only for the notification inbox; and
the authorization test gap is filed under coverage when the unasked question is
whether an unregistered route denies or allows.

**Treat both audits as hypotheses.** They reasoned from one document. Where a
finding is refuted by the code, the refutation belongs in the record.

## Orchestration run, 2026-08-18 (in flight)

Durable state for a fresh agent. Everything below is either merged, open with a
named next action, or delegated to a running agent.

### Merged this run

| PR | What |
| --- | --- |
| #219 | Notification ports, application and Postgres adapter; one live emit site (a reconciliation proposal that spent its retry budget). Tier 1 gap 1 partly closed. |
| #206 | Review actions the Hub categorically refuses are no longer offered |
| #211 | One release instruction instead of four |
| #224 | A ledger test no longer drops a table other packages are reading |

**#224 is worth knowing about before running the suite.**
`TestDifferentComponentsRacingAFreshDatabaseDoNotCollide` used to reach the
fresh-database case by dropping `conduit_schema_migrations` in the shared test
database. Its own comment said that was why it "cannot run beside the others" and
confined it to one package. Confinement to a package is not confinement: `go test`
runs packages in parallel, so `cmd/platform-delivery` reading the ledger inside
that window failed with `relation "conduit_schema_migrations" does not exist`, in
a package that had done nothing wrong. CI never saw it because
`scripts/test-postgres-repository.sh` invokes each package separately and the main
`Test` gate runs without a DSN. The test now takes a throwaway database via
`internal/pgtest` and lives in `internal/pgmigrate`. **Nothing drops a shared
object any more, and nothing should.**

### Open, with the next action named

| PR | State | Next action |
| --- | --- | --- |
| #216 | Trunk gate FAILED: `pull request has no completed tenant-fidelity claim row` — its table's Class cell said `n/a`, which is not one of the accepted values. Body fix delegated. | Re-dispatch the Trunk gate once the body is fixed, then merge. |
| #220 | Codex-authored: private OCI registry auth for the Gitless Hub. Under adversarial review + gate dispatch by an agent. | Read the agent's verdict; do not merge before it lands. |

### Delegated and running

Four agents, each in its own git worktree so they cannot collide:

| Worktree | Branch | Task |
| --- | --- | --- |
| `/workspace/wt-delivery` | `claude/notification-delivery-channel` | Tier 1 gap 1: a transport port, a real adapter, and a dispatcher draining undelivered inbox entries with leases and a terminal-failure state |
| `/workspace/wt-http` | `claude/notification-http-surface` | Tier 1 gap 1: authenticated HTTP surface for inbox, resource events and preferences, mounted through `internal/hub/composition` |
| `/workspace/wt-authz` | `claude/fix-209-authorization-scopes` | #209: `AuthorizationScopes` expands `product/X` but not `platform/root`, so a platform-scoped principal is refused by REST and served by GraphQL. Agent must falsify or confirm with a failing test **before** fixing. |
| `/workspace/conduit-review` | (read-only) | Adversarial review of #220 |

### The binding constraint, and why work is shaped around it

`internal/controlplane` sits **exactly on its ratcheted ceiling**
(`docs/operations/controlplane-ceiling.json`; `node scripts/report-migration-progress.mjs --gate`).
That is what blocks the remaining notification emit sites — a grant changing, a
Plan failing, a Release becoming available are all raised inside the monolith, so
each needs an extraction first. A recall opening was the first of the four to be
freed this way (#253): moving the automatic-security-recall rule into
`internal/catalog/domain` paid for the wiring and left the monolith smaller. Every workstream above is
therefore routed through `internal/hub/composition` or a bounded context, and no
agent is permitted to grow controlplane. **Extracting a context from controlplane
is the highest-leverage unblocking work available**: it lowers the ceiling and
frees emit sites at once.

### Environment facts that cost time to rediscover

- **Docker is preinstalled and is NOT running by default. Start it.**
  `sudo dockerd >/tmp/dockerd.log 2>&1 &` brings up server 29.3.1 in about ten
  seconds. Until this was tried, every session in this environment recorded "no
  Docker daemon" as a fact, excused the `Validate PostgreSQL repository` gate in
  its PR bodies, and deferred database claims to CI. The daemon was there the
  whole time and nobody had run the command. With it up, `verify-local.sh` passes
  **completely** — including `test-postgres-repository.sh`, the notifications and
  pgmigrate packages, and the restore drill. Excusing a gate you could have run
  is weaker than running it. Credit where due: this came from the `containers`
  skill in the `ccw-sandbox` plugin at
  `/workspace/claude-code-on-the-web/plugins/ccw-sandbox`, which states it
  plainly and is worth reading before concluding this sandbox cannot do
  something.
- **PostgreSQL 16 is installed locally** and is the way to test database code
  anyway: `export CONDUIT_TEST_POSTGRES_DSN="postgres://conduit@127.0.0.1:55432/conduit_test?sslmode=disable"`.
  Started with `/usr/lib/postgresql/16/bin/pg_ctl -D /tmp/pgdata -o "-p 55432 -k /tmp" start`.
  Prefer this over deferring database claims to CI.
- **The `gh` CLI is not available**; use the GitHub MCP tools.
- **Commit before planting mutations.** `git checkout` does not restore untracked
  files, and that has silently corrupted a mutation run in this repository more
  than once — a mutant left in place while the harness reports a kill.

### A correction carried forward from the previous session

Commit `db32afa` claimed run 47 of the Trunk gate "hung ... reached the job limit
and died", and weakened a schema re-apply test to avoid an invented
`ACCESS EXCLUSIVE` contention. **All of it was wrong.** Run 47 ran 9m41s and its
log ends `TRUNK_STATE: success`: every gate passed. The only failure was a 503
from GitHub's commit-status API on the final status POST, and the stale `pending`
status was misread as a hang. The test was restored in #144 and the gate was fixed
in #145 so that a status post can no longer decide whether the code is good.

## The security round (2026-08-18, later)

Two live credential leaks were found and closed. **Both were found by reviewing
code that had already merged and gone green.** Neither the Trunk gate nor the
unit tests caught either. That is the durable lesson: this gate is good at
regressions and blind to authorization and destination defects, so merged
security-adjacent code needs an adversarial pass regardless of its verdict.

| Defect | Mechanism | Closed by |
| --- | --- | --- |
| Token realm | The realm came from an UNAUTHENTICATED `WWW-Authenticate` 401, was interpolated into a URL with no scheme or host check, and the real host's credential was attached to it | #228 |
| `Location` header | The same class through a different header: an absolute blob-upload `Location` was followed verbatim, so `Location: http://attacker.example/hijacked` + a 401 handed over the push credential in cleartext, bypassing the realm constraint entirely | #242 |

`TokenEndpoint` (`internal/gitless/adapters/registrychallenge`) now requires an
absolute https URL, rejects userinfo, and requires the realm host to be the
registry's own or one declared in `GITLESS_REGISTRY_TOKEN_REALMS`. The credential
destination is additionally checked in `ocipublish.do` at the point of
attachment, so paths that do not exist yet are covered too.

**Two traps worth not re-discovering:**

- A same-host rule alone breaks Docker Hub, whose realm (`auth.docker.io`)
  is deliberately a different host from its registry (`registry-1.docker.io`).
  That is why the allowlist exists rather than a flat rule.
- The destination must be captured BEFORE the first round trip. A `Transport`
  may rewrite `request.URL` in place — this package's own `routed` test
  transport does, and a proxy or mirror in production would. Guarding the
  post-round-trip URL passes the attack test and breaks two legitimate realm
  tests, which is how the first attempt failed.

**Behaviour change needing a release note:** anonymous PUBLIC Docker Hub and
GitLab pulls now fail until an operator sets
`GITLESS_REGISTRY_TOKEN_REALMS=registry-1.docker.io=auth.docker.io`. Measured
live. The refusal names the variable and the exact value, and it fires even when
the credential source is `Anonymous` (deliberate, `resolve.go:217-220`). There is
no CHANGELOG, so this is the only place an upgrading operator would find it.

**Still open from the same review:** #244 (a redirect from a permitted realm
still carries the credential to a subdomain, or downgrades it to http — note the
cross-host case is safe only because of Go's stdlib, not our code) and #241 (the
notification routes treat a delegated principal as an administrator where REST
does not, and write no audit record).

## What the environment can actually do

Three capabilities were believed unavailable and are not. Re-test an inherited
constraint before designing around it.

- **Docker runs.** `sudo dockerd &`, or `scripts/start-docker.sh`. The
  PostgreSQL gate and `test-postgres-repository.sh` pass here.
- **A k3s cluster runs.** `kubectl` is NOT a separate binary — it is
  `sudo k3s kubectl`. Checking for `kubectl` and concluding "no cluster" is a
  false negative that cost time.
- **The live console suite runs.** `scripts/simulate-console.sh` builds the Hub,
  seeds through its own API, and drives a browser at the embedded bundle with no
  stub in the loop: 12/12 passing, including a reviewer approving a Change
  Request and the Agent executing the Plan to SUCCEEDED. The stubbed suite (70
  unit, 122 e2e) cannot catch a renamed field, an unread query parameter, or a
  Go nil slice encoded as `null`; this one can.

The gitless clean-cluster acceptance still fails here, but NOT in the product:
`registryauth`'s publication and verification acceptance tests pass against a
real zot registry with real cosign. The failure is Flux's controllers never
becoming ready in k3d, which is an environment gap.

## The gate now runs itself

`platform-ci.yml` triggers on `pull_request` as well as `workflow_dispatch`, with
concurrency keyed per pull request so a second push cancels the first rather than
stacking. Head authentication moved to
`scripts/authenticate-pull-request-head.sh` and validates both paths; on a
`pull_request` event `GITHUB_SHA` is a synthetic MERGE commit, not the head, so
the naive port silently checks the wrong tree.

**The `Trunk gate` commit status must keep being posted** even though a
`pull_request` check suite is attached to the rollup, because
`scripts/verify-release-preconditions.sh` reads
`repos/{repo}/commits/{sha}/status` — the combined-status endpoint reports
commit statuses, NOT check runs. Deleting the bridge would refuse every release
for want of a gate that had run.

## Resource limit reached, and what it cost (2026-08-18 ~06:55 UTC)

The orchestration run hit the agent API session limit (resets 08:40 UTC). Five
delegated workstreams died mid-flight with `You've hit your session limit`. Two
had already pushed their work and it survived; the rest left nothing and must be
re-dispatched from scratch:

| Workstream | State when it died |
| --- | --- |
| #209 GraphQL per-coordinate authorization | **Survived** — branch `claude/fix-209-authorization-scopes` with an open PR |
| #197 notification ports | **Survived** — merged as #219; `internal/notifications/` now has domain, ports, application, adapters |
| #205 SUCCEEDED-but-unconverged Plan | **Lost** — no branch |
| #208 version comparator intransitivity | **Lost** — no branch |
| CLI review residuals (empty coordinate echo, removed-namespace drop) | **Lost** — no branch |
| #195 automatic gate trigger | **Lost** — died on its first tool call |
| dev-cluster via ccw-sandbox | **Lost** — no branch |

**The orchestration lesson, stated plainly:** fan-out was too aggressive. Around
thirteen concurrent agents each dispatching gates exhausted both the agent budget
and the runner pool — with the gate now split into five jobs per run, six
concurrent pull requests is thirty queued jobs, so the runner pool became the
binding constraint and lane-split runs took longer than the 16-minute
single-job baseline they replaced. Fan out to the constraint, not past it.

## A review methodology error that produced confident, wrong findings

An external cursor-agent review was run with the correct commit SHAs named in
its prompt, while the checkout sat on an unrelated feature branch. **It read
working-tree files, not the commits.** It reported two confident "still broken"
defects against code already fixed on `main` — `release-channel export --latest`
(which does refuse with a reason at `internal/conduitcli/release_channel_commands.go:195`)
and the two console contract docs (both already corrected) — and four CLEAN
verdicts that are equally worthless, because they describe a tree nobody has.

Before any external review of merged work: `git checkout main && git pull
--ff-only`, then state the resolved HEAD sha in the prompt. **An external
reviewer reads the files it can see, whatever SHAs you name.** Batch two
(#173, #175, #177, #180, #182, #194) therefore still has no valid independent
review and needs one.

## Session ending 2026-08-18

### What shipped

Thirteen issues fixed and merged, found by assessments of the CLI, the console,
and the Apollo What's New page, then implemented in parallel:

| Issue | Fix |
| --- | --- |
| #151 | Apollo GraphQL required an authorization decision before binding sources |
| #152 | Reconciliation stopped reporting a terminal Plan as a successful dispatch |
| #172 | `asset publish` stopped reporting coordinate/image/label bindings it never made |
| #173 | `module lock --verify` asserts the lock instead of echoing `true` |
| #174 | `entity aggregate` orders versions numerically |
| #175 | Review-context flags are absent rather than demanded and discarded |
| #176 | `version compare -r` keeps universal Entities |
| #177 | Six inert CLI flags implemented, removed, or refused with a reason |
| #178 | Authored Environment descriptor carries the enforced API group |
| #180 | Overview distinguishes request failure from panel unavailability |
| #181 | Plan detail polls until the Hub is done with a Plan |
| #182 | Console drift: dead fleet filter, stale fixtures, two wrong contract docs |
| #194 | The publication workflow's manual-only trigger is now machine-enforced |

### The adversarial review round, and what it overturned

After merging, three reviewers of different model families (gpt-5.3-codex,
grok-4.6, Fable) independently reviewed the seven core commits. **Five of seven
carried real defects.** Every finding below was verified against source before
being filed; several reviewer claims were rejected as false positives.

Open, with evidence, and worth reading before touching the relevant code:

- **#209 — the GraphQL guard leaks.** `AuthorizationScopes`
  (`internal/governance/domain/authorization.go:378-394`) expands `product/X` to
  `{product/X, global/product}` but expands `platform/root` to **itself only**.
  So a platform-scoped principal gets 403 on REST products and **200 on GraphQL
  with the full product projection**. #151 closed the "holds nothing" hole and
  left a narrower one in the same class. A single platform/root bar cannot fix
  this; per-coordinate authorization matching REST is required.
- **#208 — `CompareVersions` is not a total order.** It falls back to
  `strings.Compare` when *either* side is non-orderable
  (`internal/catalog/domain/version.go:125-130`), giving the cycle
  `1.10.0 > 1.9.0 > 1.5.0-hotfix > 1.10.0`. Under `sort.Slice` the original
  1.9.0-over-1.10.0 bug reproduces whenever one non-orderable version shares a
  channel. A tiebreak alone does not fix intransitivity.
- **#205 — a SUCCEEDED-but-drifted Plan still reports success forever.** #152
  guarded only FAILED and CANCELLED. The hard part is real: naively refusing
  SUCCEEDED breaks the ordinary case where a report merely lags a completed
  rollout, so this needs a distinguishing signal, not another status in the list.

**A correction that cost real time, recorded so it is not repeated.** #207 was
filed asserting that an exhausted proposal wedges a desired state *permanently*,
on the strength of two verified facts: `LeaseProposal` has
`WHERE exhausted = false`, and the outbox insert is `ON CONFLICT DO NOTHING`.
The conclusion was wrong. Proposal IDs are **not** derived from desired state
alone — `Input.Digest()` marshals the whole `Input`, which embeds
`Observation.ObservedAt`, so every fresh report mints a new proposal ID with a
new retry budget. The real defect is the opposite: exhaustion never accumulates,
so it is not a durable escalation signal. **Verifying two of three premises is
not verifying the claim.**

### Measured: where the 16-minute gate actually goes

From a green run (32101056711, total 16m20s). Four independent steps are
**79%**: CLI release targets 5m51s (36%), Test 3m17s, operator console 2m15s,
Lint 1m34s. The remaining 34 steps total about 10 seconds. Filed as #213 with
the full table; the lever is splitting the single 45-step job into parallel
lanes with one aggregating job owning the `Trunk gate` status.

**Rejected after measuring, so nobody re-derives it:** parallelising the six
cross-compiles inside `verify-cli-release-targets.sh` looks like a 2x win on a
warm Go build cache (10–12s → 5s) and is **not** one cold. Two cold runs gave
237s → 226s and 256s → 205s — 5% and 20%, disagreeing by 4x under sandbox load.
A cold `go build` already saturates the cores. The step's real cost is a cold
cross-compile cache, which `actions/setup-go`'s `cache: true` does not cover
because each `GOOS/GOARCH` pair has its own entries.

### Mechanisms learned the hard way this session

- **The Trunk gate starts itself on `pull_request` (#195); a dispatch is only
  for re-running an unchanged tree.** When you do dispatch, set `ref` to the PR
  branch and pass `pull_request=<number>`: the preflight asserts the ref is the
  pull request's branch and sits on its head, so dispatching from `main` fails
  in about 11 seconds.
- **A PR body that does not follow `.github/pull_request_template.md` fails gate
  step 3.** `scripts/verify-pr-fidelity.sh` requires a non-empty `## Outcome`, a
  non-empty `### Deliberate divergences`, and at least one complete
  tenant-fidelity row whose class is one of the exact permitted strings.
- **`internal/controlplane` sits exactly on its line ceiling.** Two separate
  workstreams tried the better fix — a shared constant, a review-context field —
  measured the ratchet, and correctly chose a weaker-looking design over raising
  a ceiling that may only move down. Expect to do the same.
- **`internal/consoleui/dist` serialises all console work** (#200). It is a
  committed build artifact with content-hashed filenames, so any two console PRs
  conflict by construction even when their sources do not. Resolution is never a
  merge: delete the tree, `pnpm --dir web build`, re-verify. Observed twice.
- **`.claude/worktrees/` is now gitignored** — agent worktrees are full second
  copies of the tree and were making `git status` dirty.

### Direction set this session

- **Plugins over copies.** `.claude/settings.json` registers the
  `jonathanagustin-private` marketplace and enables `ccw-sandbox` and
  `crossreview` (`docs/operations/claude-plugins.md`). Note that project
  settings register the marketplace but do **not** auto-install an external
  plugin — one `claude plugin install` per machine is still required.
  `scripts/bootstrap-k3s.sh` is deliberately **not** deleted yet: its header
  claims a fifth sandbox fix the upstream plugin misses, and if that holds the
  fix belongs upstream rather than in the bin.
- **Release and upgrade path**, filed with evidence: #193 (Conduit does not
  publish itself as a Conduit Product, so its own upgrade bypasses every
  mechanism it provides), #197 (nothing delivers notifications), #195 (every PR
  needs a hand-dispatched gate though ADR 0006 already permits automating
  verification), #196 (a release is four hand-coordinated steps, and dispatching
  publication from a non-tag ref publishes a fraction and reports success).

## What shipped this session

### #145 — a GitHub outage no longer decides whether the code is good

The Trunk gate posted its commit status with a bare `gh api` call under
`bash -e`. Two consequences, both real rather than theoretical, and both hit
during this session's multi-hour 503 on GitHub's commit-status API:

- The **opening** status post ran before the 30 verifications. A transient 503
  there aborted the whole job, so an outage in a status API decided whether the
  code was tested.
- The **closing** post ran after them. A 503 there turned an all-green run red.

Both calls now go through `scripts/retry-github-api.sh` (four attempts,
exponential backoff) and **neither can fail the job**. Proven by
`scripts/test-retry-github-api.sh`, which is itself a gate.

This is why runs in this period show `TRUNK_STATE: success` with a failing final
step and a green job — that is the fix working, not a broken gate. It also means
**a PR can be fully verified and show no status badge**; read the run, not the
badge.

### #144 — the signal that separates a wedged Hub from a busy one

`conduit_reconciliation_pending`, `_leased`, `_exhausted` and
`_oldest_pending_seconds` per queue, plus `conduit_build_info`. Depth alone
cannot distinguish a stuck Hub from a busy one — both sit at some number — so
the lag gauge is the one that matters.

**A correction worth keeping, because it was mine.** I read a stale `pending`
commit status as a hung test, invented `ACCESS EXCLUSIVE` lock contention to
explain it, and weakened a test to avoid a hazard that had no evidence behind
it. Run 47's log ends `TRUNK_STATE: success` — every gate passed, including
PostgreSQL — and the only failure was a 503 on the final status POST. The commit
was reverted and the record corrected in the branch and the PR body. **A missing
status badge is not a failing test**; that mistake is now cheap to repeat and
worth guarding against.

### #146 — schema migrations applied once, and recorded

`internal/pgmigrate` applies an ordered slice of named migrations once per
database and records them in `conduit_schema_migrations`. Each migration commits
**in one transaction with the row that claims it ran**, so there is no state in
which the ledger is wrong. Both schemas (`reconciliation`, `controlplane`) are
on it; the idiom is written down in `docs/operations/schema-migrations.md`.

**An adversarial review found the design's central claim false, and it was.**
The first version took one advisory lock keyed on the component and claimed that
solved the concurrent `CREATE TABLE IF NOT EXISTS` hazard. Two different
components hash to two different keys and exclude each other not at all: four
components racing a fresh database failed **36 times in 20 rounds** with
`SQLSTATE 23505` on `pg_type_typname_nsp_index`. Reachable in production — a Hub
opens the control-plane repository *and* the reconciliation store. Fixed by
creating the ledger under a fixed lock every caller shares, taken and released
before the per-component one.

Five further findings, all fixed: a best-effort unlock that could strand a lock
on a pooled connection for `ConnMaxLifetime`; a concurrency test flaky in the
*passing* direction; a retype guard that knew `ALTER COLUMN c TYPE` but not
`ALTER COLUMN c SET DATA TYPE`; an adoption test running migration 0002's
backfill against an empty outbox; and no adoption test at all for the control
plane.

Also removed: the `-run '^TestPostgres'` filter in
`scripts/test-postgres-repository.sh` for two of its three packages. That filter
encoded a naming convention nobody wrote down and silently excluded new
database-backed tests, which then ran **nowhere** — skipped locally for want of a
DSN and filtered out in CI. `cmd/platform-delivery` keeps its filter deliberately.

### Backup, restore and disaster recovery — Tier 1 gap 4, closed as far as this repository can close it

Gap 4 was the only Tier 1 row with nothing behind it. It now has a procedure
(`docs/operations/backup-and-restore.md`) and a drill that holds the procedure
down rather than the other way round.

**`pgmigrate.Pending`** reports what `Apply` would run, in order, applying
nothing — and **creating** nothing, which is the requirement that shapes it. The
database an operator most wants to ask about before a first start is the one that
has never been migrated, and that is exactly the database with no ledger table;
an implementation that created the ledger in order to read it would make asking
the question change the schema. `to_regclass` answers the absence for what it
means. This also closes the first "what remains" bullet of gap 3.

**`TestPostgresBackupAndRestoreDrill`** (`cmd/platform-delivery`) seeds both
schemas through their real constructors, takes `pg_dump --format=custom` **while
decisions are being committed**, restores into a fresh database, and asserts:

- nothing pending for either component — read on a plain connection **before**
  either repository is constructed, because constructing one applies whatever the
  restore lost and then reports a healthy database. Order is the test;
- no torn decision/audit/outbox triple, with row counts asserted first so the
  predicates cannot pass over empty tables (the failure #146's adoption test had);
- state read back through `ListPlans`, `OpenBlob` with the bytes compared, and
  `CurrentDesired`.

It **fails rather than skips** when `pg_dump` is missing or older than the
server. A skip on a gate that promises a restore drill is a green run for a
backup nobody tested.

**The client/server coupling was not hypothetical and it bit immediately.** The
previous handoff said to watch for divergence; they had already diverged. The
pinned image is PostgreSQL **18** and the CI runner's client is **16**, and
`pg_dump` refuses a server newer than itself — so the first version of this
drill, built on the runner's client, would have turned the Trunk gate red on its
first dispatch. The fix is that the drill uses **the pinned image's own client**,
handed to it by `scripts/test-postgres-repository.sh` as
`CONDUIT_TEST_PG_TOOL_PREFIX="docker exec -i <container>"` plus the server's
in-container address. That client matches its server by construction and is
already digest-pinned; installing a matching one instead would add an unpinned
apt source to a repository that pins everything else, and downgrading the server
to match the runner would test backups against a PostgreSQL nobody runs. The
archive moves over stdin and stdout because the client does not share the test's
filesystem. Verified on both paths: `pg_dump 18 … (via docker exec)` under the
real gate, and a local client with no prefix.

Four mutants killed by hand: creating the ledger inside `Pending` (the first
attempt did not compile, which is not a kill — the second one did); deleting a
ledger row from the restored database; deleting an audit row to tear a committed
triple. Also: `./internal/pgmigrate` is now in
`scripts/test-postgres-repository.sh`. Without that line its new
database-backed tests would skip locally for want of a DSN and be reached by
nothing in CI — the "runs nowhere" trap #146 removed from that same script.

The controlplane ceiling moved up 13 (25035 → 25048) with the reason recorded in
the same commit: a dry run covering only the reconciliation store answers half
the question, since a Hub start applies both schemas and this is the one holding
the artifact bytes. `internal/pgtest` is new and is where the throwaway-database
helper lives, so neither caller had to grow one.

**What is deliberately not closed, and cannot be here**: the backup schedule,
retention, WAL archiving/PITR and secrets copies. The chart consumes a DSN from a
Secret it does not create, so those are the deployment's — carried to
PRODUCTION-READINESS residual item 5 rather than left implied.

### Sandbox skills (`.claude/skills/cluster`, `.claude/skills/containers`)

The environment knowledge needed to run k3s, Docker, podman and a real
PostgreSQL in a web session, as two skills with their scripts. Merged as #147.

**One substantive change from the upstream copy: fix 5 was ported in.** This
repository's own `scripts/bootstrap-k3s.sh` resolves the runc to wrap by reading
the data-bin directory from the running server's `PATH`
(`/proc/<pid>/environ`); the skill's copy used `data/current`, which goes stale
once more than one k3s version has run — the wrapper is then installed where
nothing reads it while every pod keeps failing with the fix apparently applied.
`bootstrap-k3s.test.sh` covers five cases; reverting the resolution kills three
of them, including the one that reproduces the field failure.

Still referenced and still absent: `containers/docs/USE-CASES.md` and the
`ccw-sandbox:diagnose` skill.

## Environment facts this session established (read before re-deriving them)

- **Docker is installed and its daemon is not running.** `scripts/start-docker.sh`
  starts it and works as shipped. An earlier note in this session that "Docker is
  unavailable" was wrong.
- **Docker Hub's blob CDN is no longer blocked, and the PostgreSQL gate runs
  here.** The previous session recorded a `403 Forbidden` from
  `production.cloudfront.docker.com` on the pinned image and concluded
  `scripts/test-postgres-repository.sh` "cannot run in this sandbox at all".
  That is now false: `docker pull` of the same digest succeeds, and
  `./scripts/test-postgres-repository.sh` exits 0 with all four packages green.
  **Run the real gate first and only fall back if it fails** — a sandbox fact
  with a shelf life is exactly the kind this repository has been burned by.
  Start the daemon with `sudo bash scripts/start-docker.sh`.
- **The pinned image is PostgreSQL 18** (alpine 3.24, built 2026-08-13),
  resolved from the registry rather than guessed. It is pinned by digest with no
  tag, and Renovate does not manage it (its dockerfile manager matches only
  `Dockerfile`/`Containerfile` paths, and this is a shell script), so the version
  is now written down in a comment beside the digest — that comment is the only
  place it exists.
- **The local PostgreSQL fallback is 16.13**, which is what Ubuntu 24.04 ships
  and not a choice this repository made. It is two majors behind the gate, so a
  green fallback run is weaker evidence than a green gate; say which one you
  used. This is the exact path used to verify #146:

  **Use absolute paths inside `su`.** `su postgres -c` starts a fresh shell that
  discards the exported `PATH`, so the `initdb` line below fails with "command
  not found" while the surrounding `export` looks like it worked — and with
  `initdb` skipped, every later line fails with "connection refused", which
  reads as a broken server rather than a lost `PATH`.

  ```bash
  bin=/usr/lib/postgresql/16/bin
  export PATH="${bin}:$PATH"          # for psql/pg_dump in this shell
  mkdir -p /tmp/pgdrill && chown postgres:postgres /tmp/pgdrill
  su postgres -c "${bin}/initdb -D /tmp/pgdrill/data -U conduit --auth=trust"
  su postgres -c "${bin}/pg_ctl -D /tmp/pgdrill/data -o '-p 5434 -k /tmp/pgdrill' -l /tmp/pgdrill/server.log start"
  psql -h 127.0.0.1 -p 5434 -U conduit -d postgres -c 'CREATE DATABASE conduit_test'
  export CONDUIT_TEST_POSTGRES_DSN="postgres://conduit@127.0.0.1:5434/conduit_test?sslmode=disable"
  go test -race ./internal/controlplane \
                 ./internal/delivery/reconciliation/adapters/postgres \
                 ./internal/pgmigrate
  go test -race ./cmd/platform-delivery -run '^TestPostgres'   # includes the restore drill
  ```

  The drill shells out to `pg_dump` and `pg_restore`; `/usr/bin` carries both at
  16.13, so the `PATH` export above is enough. 16.13 is what Ubuntu 24.04 ships
  and is not a choice this repository made — CI pins `postgres@sha256:a1d02e4b…`,
  a bare digest whose major version is recorded nowhere, and `pg_dump` refuses a
  server newer than itself. The drill reads `server_version_num` and fails loudly
  if the client is older, which is the only thing that will catch that
  divergence.

- **Never judge a gate by a piped exit code.** `./scripts/test-postgres-repository.sh | tail -20`
  reports `tail`'s status, which is always 0. A failing gate read as passing
  here until the log was actually read.
- **CI was unreliable for reasons outside this repository**: a multi-hour 503 on
  the commit-status API, and a 429 on the `actions/setup-go` download that killed
  run 52 during `Set up job` before any step ran. Re-dispatching is the correct
  response to the latter and to nothing else. Run 54 is the green one for #146.

## Open branches and PRs

- **`claude/adversarial-review-run-52-kxnmvk`** — merged as #147; the branch is
  stale and can be deleted.
- **`claude/schema-migration-ledger`** — merged as #146; the branch is stale and
  can be deleted.
- **`claude/handoff-markdown-8lkz6v`** — the backup/restore work above.
- **#114 and #115 (Renovate, Rust crates).** `base64` `0.22.1 → 0.23.1` and
  `sha2` `0.10.9 → 0.11.0`. **Do not merge these without building the crate.**
  Both are breaking-version bumps: `base64` 0.23 raises the MSRV to 1.71 and adds
  a default-on `simd-unsafe` feature, and `sha2` 0.11 is a major RustCrypto
  release. Their base is also stale (`89d5534`). They need a `cargo build`/test
  against `solver/`, which was not run this session.

## Next work, already scoped

The production-readiness record (`docs/operations/PRODUCTION-READINESS.md`)
Tier 1 rows, in the order they are worth taking. **Gap 4 (backup, restore and
disaster recovery) came off this list this session** — see above; what is left
of it is an operator action, carried to that document's residual item 5. The
scouted research behind it has been spent and does not need re-running.

1. **Gap 3 remainder** — schema verification against the ledger now exists
   (`pgmigrate.Verify`, `platform-delivery verify-schema`) but is invoked rather
   than scheduled; no upgrade rehearsal against a production-shaped dataset.
2. **Observability (gap 2)** — no tracing, metrics only under
   `REPOSITORY_KIND=postgres`, endpoint off unless `METRICS_ADDRESS` is set, and
   no alerting rules ship.
3. **Notifications (gap 1)** — a whole subsystem: no ports, no store, no HTTP
   surface, no emit sites, no console inbox.

A research subagent whose findings did not survive the previous session was
going to classify every table as authoritative or derivable and derive SQL
predicates for every cross-table invariant a torn backup could violate. The
drill now asserts three of them — the `CommitDecision` triple in both directions,
and an outbox entry without its decision, joined on the input digest inside the
serialised proposal because that is the only link between those two tables. The
rest of that sweep is still unwritten: `UpsertDesired`, `UpsertObservation`,
`LeaseWork`, `FailWork`, `RepairDroppedWork`, `LeaseProposal`,
`CompleteProposal` and `FailProposal` each establish invariants nothing checks
after a restore. Adding one is now a two-line change to
`assertNoTornDecisionTriple`'s map, so this is cheap and worth finishing.

## Mechanisms a next contributor must know

- **Registered invention / provenance tiers.** The Apollo GraphQL interface
  serves a coordinate from real Conduit data, or from a value Conduit invented
  that says so on the wire. It never nulls — a null is a claim of absence. Under
  [ADR 0007](docs/decisions/0007-registered-invention-over-refusal.md) an invented
  value carries `@provenance(tier: OBSERVED, valueTier: CONDUIT_NATIVE)`. ADR 0007
  supersedes 0005 and reopens #17. See `internal/apollographql/`.
- **Schema changes go through `internal/pgmigrate`.** Position is the order, the
  name is identity and never changes once merged, and each migration commits with
  the row recording it. Forward-only by decision, not omission. Read
  `docs/operations/schema-migrations.md` before adding one; it has the
  add-nullable / backfill / constrain idiom and the rules
  (`CREATE INDEX CONCURRENTLY` cannot appear in a migration — every one runs in a
  transaction).
- **The controlplane line ceiling** (`docs/operations/controlplane-ceiling.json`)
  only moves down; raising it requires a justification in the same commit.
- **Fixtures must be corrected from the Go source, never from what makes a test
  pass.** The console's browser tests mock the Hub, so a fixture using an
  invented status passes while lying. The contract gate guards the status
  vocabularies; the discipline still applies to any new reader field.
- **The embedded console bundle** (`internal/consoleui/dist`) is committed and
  checked byte-for-byte. After any `web/src` change run `pnpm --dir web build`
  and commit the regenerated bundle.
- **Local verification:** `./scripts/verify-local.sh` runs every gate the Trunk
  gate runs (it reads `.github/workflows/platform-ci.yml` so the two cannot
  drift). CI is manual (`workflow_dispatch`); dispatch per
  `docs/operations/manual-ci.md`. A green run is evidence, not authorization to
  merge or release (assumption A5). The PostgreSQL gate runs in this sandbox
  once the Docker daemon is started; the environment section above records the
  correction and the fallback.
- **Mutation testing:** one hand-planted mutant at a time via
  `scripts/verify-mutant-killed.sh` to prove a specific gate rejects. Two traps
  seen this session: a mutation applied with shell escaping that mangled the
  anchors was **not applied at all**, so its "survival" was void; and a mutation
  can be killed *for the wrong reason* (a driver type error rather than the
  property under test), which is not a kill.
- **The merge queue (#20) is not a setting to flip.** GitHub offers merge queues
  on public repositories, and on private ones only under Team or Enterprise
  Cloud, so this private repository under a personal account has no control at
  all. `merge-queue.yml` stays inert unless the repository moves to an
  organization or becomes public. If it ever does, the required check must become
  `Merge queue`, not `Trunk gate`: `platform-ci.yml` runs on `pull_request` and
  `workflow_dispatch`, neither of which fires for a `merge_group`.

## Bootstrapping a fresh session

Live PR subscriptions and scheduled self-check-ins are session-local and do not
carry over; re-`subscribe_pr_activity` for anything that needs watching.

### First message to paste into the new session

```
Work on jonathanagustin/conduit. Attach it with add_repo if it isn't in scope,
then clone and register it.

Before doing anything, read these to load context -- do not skip:
- handoff.md                                  (this file: state, open items, gotchas)
- docs/operations/PRODUCTION-READINESS.md     (the Tier 1 gaps and what remains)
- docs/operations/manual-ci.md                (CI is manual workflow_dispatch)
- docs/operations/schema-migrations.md        (how a schema change is made here)
- docs/operations/ASSUMPTIONS-LEDGER.md       (A1-A9; A4 overturned by ADR 0006)
- docs/operations/local-verification.md
- AGENTS.md and docs/operations/AGENTS.md

Adopt these working rules for this repo:
- New branch per change; open a PR; never push to main. Opening or pushing the
  PR runs the Trunk gate (platform-ci.yml) against its head; dispatch it only to
  re-run a tree that has not changed. Publication stays manual.
- Run ./scripts/verify-local.sh before handoff. The PostgreSQL gate DOES run in
  this sandbox -- start the daemon with `sudo bash scripts/start-docker.sh`
  first. An earlier handoff said the image pull was blocked; it is not any more.
  Fall back to the local PostgreSQL 16 recipe in handoff.md only if the real
  gate fails, and say which path you used.
- Verify by reading output, never by a piped exit code, and never by a status
  badge -- an absent badge here often means GitHub's status API failed, not that
  a gate did.
- Ground every Apollo/Hub-contract claim in the code; never reason from
  plausibility. Console fixtures come from the Go source, not from what makes a
  test pass.
- Registered invention (ADR 0007): serve every coordinate, from real data or
  from an invention that discloses itself. Never null.
- internal/controlplane has a line ceiling that only moves down.
- After any web/src change, run `pnpm --dir web build` and commit the
  regenerated internal/consoleui/dist bundle.
- Put design decisions and premise-reversals through an adversarial review
  before implementing. It has now overturned a central design claim twice --
  most recently the migration ledger's locking, which was false as written.

Then tell me the open issues and PRs and which decisions still need me, and wait
for direction. Read the live issue list rather than any to-do list quoted here.
```

### Shell bootstrap (rebuild the toolchain)

```bash
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
./scripts/verify-local.sh                        # full gate set
```

## Apollo `core/whats-new` parity program, 2026-08-19

**Goal (standing):** full Apollo feature parity, next-best-step, one act-gated PR
at a time. Trigger was reading `core/whats-new` and filing issues for everything
Conduit lacks; the user then directed "undecline and implement" the deliberate
boundaries too.

**Method that held.** All 192 `core/whats-new` items were audited against the Go
source (eight parallel Explore surveys), then reconciled against the repo's own
prior disposition — `docs/apollo/ROSETTA-RELEASE-NOTES.md` (the maintainers had
already mined this exact page on 2026-08-15), `docs/apollo/IMAGE-DERIVED-GAPS.md`,
`docs/apollo/conformance.md`. Finding: the large majority are already
implemented, already tracked (#15/#17/#54/#65/#126/#150/#169/#170/#173/#175/#241),
or documented deliberate boundaries. Only the genuine, unfiled, non-duplicate
residual was filed.

**Filed (deduped against the 32 open issues):**
- Bucket A — control-plane/CLI gaps: #290 (Risk app is recalls-only, no
  cross-release vuln inventory), #291, #292, #293, #294 (promotion lifecycle
  states), #295, #296 (module recall → cancel CRs), #297 (registry type +
  delete-prevent).
- Bucket B — console surfacing of backend-present data: #298 (export console),
  #299 (env console), #300 (teams tabs), #301 (entity source pills + RID search),
  #302 (notification deep-links).
- Bucket C — undecline & implement boundaries: #303 / #304 (debug terminals /
  JIT — reverse the H30 boundary in `docs/apollo/reference/debug-access.md`,
  needs a superseding ADR before code), #305 (`--ignore-recalls` /
  `--ignore-secret-requirements` as a *reviewed* change-request field, not a CLI
  bypass). Plus undecline comments on #17 (adopt Space tenancy) and #119 (native
  SMTP + first-class Slack delivery).
- Skipped as existing: sensitive recall-metadata redaction is #170; canary-by-
  filter is #169.

**Merged this session (each gated green on all four act Trunk lanes):**
- #291 → PR #307: PLANNED_FIX suppressions require `PlannedFixReference` evidence.
- #292 → PR #306: `cve check -o csv` vulnerability report export (added a
  `CSVMarshaler` seam to `writeOutput`).
- #293 → PR #308: `release-channel diff --releases` compares Release sets to find
  the promotion worklist.
- #295 → PR #309: module install inherits the Environment `DefaultReleaseChannel`
  (via `moduleInstallChannel` reading `latestDesiredStates`).

**Remaining work, with tractability notes (next contributor read this first):**
- Ceiling is the gating constraint: `internal/controlplane` sits at 25087 / 25089
  (2 lines under). Every control-plane parity change now needs an offset or a
  domain extraction; the ratchet only moves down. CLI-only (#292/#293) and
  domain-only changes avoid it.
- #297 (registry type) is broader than it looks: the type belongs on the registry
  catalog *record* (assign only copies existing records — `environment_commands.go`),
  so it touches `artifact_registry_catalog.go` too. The delete-prevent half is
  largely already enforced: a Release referencing an unassigned registry host
  already fails `validateArtifactRegistryAssignment`; the observed-state ("still
  reported") nuance is the remaining boundary.
- #296 (recall → cancel CRs) is semantically ambiguous in Conduit: module
  *installs* create installations directly, not change requests, so "which open
  CR references the recalled version" needs a definition before code. Scope it
  first.
- #294 (promotion lifecycle states) and #290 (risk vuln inventory) are the two
  largest — new state model / new aggregate API + console, respectively.
- Bucket B console issues avoid the ceiling but each needs `pnpm --dir web build`
  (CI=true) + a regenerated `internal/consoleui/dist` commit + the console gate.
  Verify per issue whether the console API already returns the data (pure
  frontend) or needs an API addition (back to the ceiling).
- Bucket C reversals (#303/#304, and #17/#119) are architecture-scale and want an
  ADR / product decision before implementation.

**Resume:** `main` is at the #309 merge. Task list item #12 tracks the program.
The act gate harness is unchanged (`/tmp/act-gate` helper, `conduit-act:24.04`
image, warm Go caches, lanes run one-at-a-time foreground with `timeout 580`;
strip `needs: preflight`). Next best step: pick the most tractable remaining
issue (a Bucket B pure-frontend one, or #297 with a ceiling offset), implement →
act-gate four lanes → PR → merge.
