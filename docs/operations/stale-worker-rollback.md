# Stale workers, successor executions and rollback

Issue #851. This note describes Conduit's own lease and Helm execution rules;
it makes no claim about a private Apollo protocol.

## Execution and ownership

`Worker.RunOnce` in [`internal/agent/worker.go`](../../internal/agent/worker.go)
leases and starts a Plan. It puts the Plan generation and lease attempt in the
executor context with `withExecutionClaim`. Its work heartbeat calls the Hub's
renewal operation every third of the lease duration.

`HelmExecutor.Apply` in [`internal/agent/helm.go`](../../internal/agent/helm.go)
writes `ownedReleaseDescription` on a Helm install, upgrade or retained-history
uninstall. The description
contains the release/configuration/chart identity and an execution fence with
generation, attempt, namespace and the recorded Helm release name. Its returned
`Receipt` carries that fence and the previous revision. The Entity name and the
Helm release name can differ; rollback uses `PlanStep.HelmRelease()`.

An uninstall also writes its own fence with Helm's `--description` option,
supported by [Helm 3](https://helm.sh/docs/v3/helm/helm_uninstall/) and
[Helm 4](https://helm.sh/docs/helm/helm_uninstall/). Its retained history therefore
identifies the uninstalling attempt when that attempt needs to restore the
previous revision. Without this, the old install's fence would incorrectly
refuse the uninstalling attempt's own rollback.

## Failure and rollback

When application or observation fails, `Worker.RunOnce` stops its work heartbeat
and examines any renewal failure. A lost lease suppresses rollback. A cancelled
Plan permits cleanup because the Hub does not give that cancelled Plan to a new
execution.

Otherwise, `Worker.rollback` first calls `renewForRollback` using the caller's
context. Every renewal error except Plan cancellation stops cleanup. A separate
heartbeat renews through the unwind and cancels its context if renewal fails.
Cleanup reverses post-apply secret deletions, step receipts and pre-apply secret
mutations; cancellation is checked between operations.

A renewal reply can be delayed until after its lease expires and a successor
starts. `HelmExecutor.Rollback` therefore independently calls
`confirmReleaseOwnership` before an uninstall or revision rollback. For a fenced
receipt, a different live fence refuses the operation. A missing or malformed
live fence also refuses it: unknown ownership cannot authorize the old receipt.
Before this change, that last case returned success and let a stale worker
uninstall the successor's release. An operator must investigate and reconcile
an unreadable fence rather than asking an old attempt to infer ownership.

The Hub's [`Lifecycle.FinishPlan`](../../internal/controlplane/lifecycle.go)
uses one repository transaction. `finishPlanOnce` checks the current lease token
through `transitionWithLease` and requires the reported execution attempt to
equal the lease attempt before persisting terminal status and evidence. A stale
worker's final failure report is refused; it cannot replace the successor's
running Plan record or Environment report. Final reporting has a separate,
bounded context, so caller cancellation does not make reporting unbounded.

## Regression and evidence

`TestStaleWorkerRollbackPreservesARunningSuccessorAndItsReports` in
[`internal/agent/successor_rollback_test.go`](../../internal/agent/successor_rollback_test.go)
runs two actual `Worker.RunOnce` loops, the actual Hub `Lifecycle` with its memory
repository, and the actual `HelmExecutor`. The command runner simulates Helm;
the test does not run Helm or a Kubernetes cluster.

The predecessor applies, then fails observation. Its rollback renewal succeeds,
but a barrier delays that reply. The fixture moves the recorded expiry into the
past to model time passing; the successor then obtains a real second lease,
starts, applies the same recorded release and waits at observation. Returning
the old renewal reply makes the predecessor enter its actual rollback path while
the successor owns both the lease and release.

The regression checks readable, missing and malformed successor fence responses.
It requires no uninstall or rollback command, an unchanged successor release,
an unchanged running Plan and prior Environment report after the refused stale
finish, then successful completion and attempt-two evidence from the successor.
Before the fix, the missing/malformed cases issue `helm uninstall custom-release`
and the successor cannot complete its observation.

## Scope of the guarantee

Lease validation and the Helm status check are separate operations. The Helm
command interface provides no atomic compare-and-swap over the description and
the subsequent mutation. These checks refuse observed stale or unknown
ownership; this test does not establish atomic fencing against a release change
after the last status read, an external administrator, or an executor that does
not record the fence. Secret-preparer rollback has its own adapter behavior and
is not exercised by this Helm regression.
