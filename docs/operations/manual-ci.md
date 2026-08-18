# Manual CI

Publication, release, signing, and deployment are manual and stay manual: no
automated trigger may perform them (ADR 0001, A5). Verification is not: since
#195 the Trunk gate runs itself on every pull request, under the guardrails
below. Local verification still runs first, so paid Actions time is spent on a
candidate that is ready to review rather than on discovering a formatting error.
A pre-existing aggregate failure is still a failed candidate; repair it locally
or explicitly remove the invalid gate before proposing it.

Automated triggers (`schedule`, `merge_group`, `push`, `pull_request`, and
`workflow_run` chains) are permitted for verification and maintenance workflows
under the guardrails in ADR 0006. Until 2026-08-16 they were prohibited outright
(assumption A4); the prohibition came from a real incident — automatic triggers
queued one verification per merge, each superseding the last — and the guardrails
are what make that incident structurally impossible rather than merely discouraged.
An automated workflow must:

- **Cancel superseded runs.** `concurrency` with `cancel-in-progress: true`,
  keyed so a newer run of the same scope cancels the older. This is the direct
  remedy for the stacking incident that motivated A4.
- **Cap its runtime** with `timeout-minutes`, so a hung run fails fast rather
  than billing to the six-hour default.
- **Run no more often than it must** — a `schedule` at the lowest useful
  frequency, stated in a comment; an event rather than a poll where one exists.
- **Stay least-privilege** with a minimal `permissions:` block.
- **Never publish, release, sign, or deploy.**

The publication, release, and acceptance workflows stay `workflow_dispatch`
only, enforced by `scripts/verify-trusted-supply-chain.rb`
(`platform-images.yml`),
`scripts/verify-optimizer-candidate-release-workflow.rb`
(`platform-optimizer-candidate-release.yml`),
`scripts/verify-flux-runtime.rb` (`platform-gitless-e2e.yml`), and -- for the
release path itself, meaning `platform-release.yml` and the
`platform-images.yml` publication it dispatches --
`scripts/verify-release-workflow.rb`. Cutting a release is one dispatched
instruction, described in
[`release-and-publication.md`](release-and-publication.md); it is one because
the workflow chains the tag and the publication, not because any trigger fires
on its own. The Trunk gate below runs automatically on `pull_request` under
those guardrails (#195); a merge queue, when built, is still a separate
`merge_group` workflow.

## Trunk gate

The gate runs itself. Opening a pull request against `main`, or pushing to one,
starts it on the `pull_request` event — nobody dispatches anything, and a green
run still authorizes nothing (A5).

Dispatch remains, for the one case the event cannot cover: a re-run against a
tree that has not changed. No event fires when nothing was pushed, so a flake,
or a fix to the gate itself that landed on `main`, is re-verified like this:

```bash
gh workflow run platform-ci.yml \
  --repo jonathanagustin/conduit \
  --ref <short-lived-branch> \
  --field pull_request=<number>
```

Both paths verify the same commit: the pull-request head. On the event path
that is `github.event.pull_request.head.sha`, **not** `GITHUB_SHA`, which on a
`pull_request` event is an ephemeral merge commit GitHub built from the head and
`main`. Every job checks that sha out explicitly and every Trunk status is
published against it, so the tree that is verified, the tree that is reported
on, and the tree that will be merged are one commit.

`scripts/authenticate-pull-request-head.sh` proves that, and it is what stops
the gate verifying a tree nobody proposed. It runs in a job of its own
(`preflight`) that every verification lane `needs`, and it refuses a run whose
pull request is closed, whose head is a fork, whose head sha is not the commit
the run checked out, or whose ref belongs to some other pull request. The two
paths differ only in where the identities come from:

| | `pull_request` | `workflow_dispatch` |
| --- | --- | --- |
| Pull request number | the event | the `pull_request` input, required on this path by the authenticator rather than by the trigger |
| Head commit | `head.sha` from the event, proven against the API | `GITHUB_SHA`, which must equal the pull request's head |
| Ref | must be `refs/pull/<number>/merge` for that same number | must be `refs/heads/<head branch>` |

`scripts/authenticate-pull-request-head.test.sh` exercises every refusal above
on both paths against a stubbed API, and runs in the gate itself and in
`verify-local.sh`.

Superseded runs cancel. `concurrency` is keyed on the pull request number rather
than on `github.ref`, because the two paths name the same pull request with
different refs: a second push cancels the first run, and a dispatched re-run
supersedes an automatic one instead of billing beside it.

Measured on #239, the pull request that made the change: opening it started run
32119952760 on the `pull_request` event with nobody dispatching anything, and
that run's preflight resolved pull request 239 from the event, pinned every
checkout to the head commit `fcd125c`, and published the pending `Trunk gate`
status against that same commit rather than against the merge commit
`GITHUB_SHA` named.

The gates then run as four parallel lanes — `go`, `cli-matrix`, `console`, and
`checks` — because as one sequential job the gate took 16m20s, of which four
independent steps were 79% (#213). The wall clock is now roughly the longest
lane. Nothing about *which* gates run changed; `scripts/verify-local.sh` still
reconciles its coverage against every step name in the file, so a gate that
moves between lanes is still a gate that has to run somewhere.

The preflight publishes a pending `Trunk gate` commit status, and one
aggregating job (`trunk-status`) replaces it with success or failure. That job
`needs` every lane, runs under `always()`, and computes its verdict from the
lanes' results rather than from `job.status` — which, in a job that only
publishes a status, is green no matter what the lanes did. It reports success
only when every lane it needs ended in `success`, so a lane that failed, was
cancelled by its timeout, or was skipped because head authentication failed all
read as a failed gate. Its shape — including the verdict logic, executed
against synthetic lane results — is guarded by
`scripts/verify-pr-fidelity.test.sh`.

That status bridge was first written because a `workflow_dispatch` check suite
is not attached to the pull-request rollup. The `pull_request` trigger covers
that half now: its check suite *is* attached. The bridge stays anyway, because
the release path reads the *commit status*, not the check suite —
`scripts/verify-release-preconditions.sh` refuses to cut a release unless
`Trunk gate` is `success` in `GET /commits/{sha}/status` for the head, and that
endpoint does not report check runs. Deleting the bridge would leave every
release refused for want of a gate that had in fact run.

Watch a run with `gh run watch <run-id> --exit-status`. A run that reports
`cancelled` is usually not a failure at all: pushing again supersedes the
in-flight run by design, and the newer run is the one to read. Do not retry a
genuinely failed run until the failure is understood. A
successful run is evidence for review; it does not authorize merge, release,
publication, or deployment.
