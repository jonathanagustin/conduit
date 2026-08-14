# Manual CI

Repository-authored workflows must use `workflow_dispatch` only. Do not add
`push`, `pull_request`, `schedule`, or an automatic workflow chain. Local
verification runs first so paid Actions time is used only for a candidate that
is ready to review. A pre-existing aggregate failure is still a failed candidate;
repair it locally or explicitly remove the invalid gate before dispatching CI.

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
does not satisfy the candidate. After that preflight, the workflow publishes a
pending `Trunk gate` commit status and replaces it with success or failure from
an `always()` finalizer. This status bridge is required because a
`workflow_dispatch` check suite is not attached to the pull-request rollup.

Watch a deliberately started run with `gh run watch <run-id> --exit-status`.
Do not retry a failed or cancelled run until the failure is understood. A
successful run is evidence for review; it does not authorize merge, release,
publication, or deployment.
