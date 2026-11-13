# Keep the personal repository private and disable the merge queue

Status: Accepted (2026-11-13)

## Context

GitHub merge queues are unavailable for this private, personally owned
repository. ADR 0006 permits automatic verification, but cannot make the
repository eligible for GitHub's queue feature. Issues #20 and #186 remained
open after the workflow shipped because no merge group could exercise it.

## Decision

The owner chose to keep the repository private and personally owned, and to
disable the queue. Remove `.github/workflows/merge-queue.yml` so a dormant
workflow does not imply a configured or verified capability. Pull requests
continue to run the automatic Trunk gate and are squash-merged after that gate
passes. Keep the required `Trunk gate` check; do not require `Merge queue`.

## Consequences

This resolves the eligibility decision by declining the queue under the current
ownership model. It does not claim merge-group verification was exercised.
Publication remains manual. No transfer, visibility change, or subscription
change is part of this decision.

If ownership or GitHub eligibility changes, propose a new decision and restore
and validate a queue workflow against the current Trunk gate set before making
its check required. `scripts/test-postgres-repository.sh` already provisions its
own pinned disposable database; the historical absence of a workflow service
was not a missing database dependency.
