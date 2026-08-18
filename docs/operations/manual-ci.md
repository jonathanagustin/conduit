# Manual CI

Publication, release, signing, and deployment are manual and stay manual: no
automated trigger may perform them (ADR 0001, A5). Local verification runs first
so paid Actions time is used only for a candidate that is ready to review. A
pre-existing aggregate failure is still a failed candidate; repair it locally or
explicitly remove the invalid gate before dispatching CI.

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
on its own. The Trunk gate below stays dispatch-based
because it authenticates the exact pull-request head; a merge queue, when built,
is a separate `merge_group` workflow, not a change to how the Trunk gate runs.

## Trunk gate

Open the pull request, record its number, and dispatch the gate against the
exact head branch:

```bash
gh workflow run platform-ci.yml \
  --repo jonathanagustin/conduit \
  --ref <short-lived-branch> \
  --field pull_request=<number>
```

The workflow must resolve the open pull request and require its head SHA to
equal `GITHUB_SHA` before it runs repository checks. A run against another SHA
does not satisfy the candidate. That preflight is a job of its own
(`preflight`), and every verification lane `needs` it, so no lane can run
against a tree nobody proposed.

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
read as a failed gate. This status bridge is required because a
`workflow_dispatch` check suite is not attached to the pull-request rollup, and
its shape — including the verdict logic, executed against synthetic lane
results — is guarded by `scripts/verify-pr-fidelity.test.sh`.

Watch a deliberately started run with `gh run watch <run-id> --exit-status`.
Do not retry a failed or cancelled run until the failure is understood. A
successful run is evidence for review; it does not authorize merge, release,
publication, or deployment.
