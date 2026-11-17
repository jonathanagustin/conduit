# Plan completion and evidence

Native Conduit Plan completion has one write authority:
`Lifecycle.FinishPlan`. It validates the lease, execution attempt and exact
reported coordinates, then commits terminal status, execution, observed state,
automatic failure suppression and any environment escalation in one repository
transaction. An empty failed observation records execution and suppression
without inventing deployed state. Entity commands use this same path; their
report merge preserves observations outside the command's targets.

`PlanRecord.completionRecorded` is a Conduit-native receipt in that transaction.
A same-status retry with the original lease token returns the stored record
without replaying observations, suppression or live invalidations, including
after lease expiry. A different terminal outcome or foreign token is refused.
The receipt is server-owned; the finish request cannot submit it.

Older terminal records without the receipt receive one transactional backfill.
Missing execution is filled without changing existing execution, terminal
status, failure, or completion time. A report from this Plan is retained, and an
observation at the same instant as or later than completion is preserved. The backfill
records its receipt with all remaining effects, so another retry does no work.
This is a completion receipt, not an archive of every Environment observation.
An existing failure-suppression record bound to this exact Plan and installation
is evidence that the suppression decision was already recorded. Backfill keeps
that record, including an operator removal or expiry, without creating a fresh
suppression or replaying environment escalation. A report alone cannot prove
that a historical multi-write completion reached its suppression write; a
genuinely absent suppression still receives the existing transactional backfill.
The timestamp fence applies only to historical backfill. A Plan's first finish
publishes its exact observed state even when a previous report's timestamp is
equal; the active lease and validated execution establish that completion.

## Caller and persistence audit

The following audit covers production Go callers and the upgrade-data probe.
`ChangeExecution` on approved governance changes is a separate aggregate from
`PlanExecution`; none of its writers changes a Plan's terminal state.

| Path | Commit boundary and authority |
| --- | --- |
| `internal/agent/worker.go` → `HTTPControlPlane.Finish` in `internal/agent/client.go` | Both success and failure send execution to the native finish route. Success includes exact observed entities and secret metadata. Failure may send no observation after rollback. Neither client changes Hub persistence. Cancellation returns through its separate cancellation lifecycle. |
| `POST /v1/plans/{planID}/finish` in `internal/controlplane/http.go` | Authenticates the scoped Agent and calls `Lifecycle.FinishPlan`; acknowledges only after the transaction succeeds. This is the only production caller of `FinishPlan`. |
| `finishPlanOnce`, `completeRecordedPlan`, `persistFinishEvidence` in `internal/controlplane/lifecycle.go` | Terminal Plan writes, execution/backfill, report merging and automatic suppression all use the callback's repository view. `createPlanFailureSuppression` and `reconcileEnvironmentEscalation` in `suppression.go` inherit that view, including escalation creation/removal. No other production code assigns `PlanSucceeded` or `PlanFailed`. |
| `MemoryRepository.Transaction` in `postgres_repository.go` | Changes an isolated state copy under the repository lock; callback failure exposes none of it. Its native live outbox is part of the copied state. |
| `FileRepository.Transaction` in `repository.go` | Changes a sibling staging tree under the instance lock, then replaces the repository tree. Callback failure discards staging. A failed staging-to-root rename restores the original tree. File mode remains a single-process adapter; the two rename operations are not a power-loss atomic filesystem transaction. |
| `PostgresRepository.Transaction` / `withStateTransaction` in `postgres_repository.go` | Locks the metadata row and writes the resulting state in the same SQL transaction. State-write failure or a rejected SQL commit rolls back status, evidence, suppressions and native live outbox together. |
| Reconciliation repository decorator in `internal/delivery/reconciliation/adapters/controlplane/bridge.go` | Forwards the transaction callback to the underlying transactional view. It does not publish an input notification before commit. `Sync` repairs notifications by reading committed Plans and reports. Its observed-target `Complete` flag is derived from exact committed report coordinates; it does not finish a native Plan. |
| `Announcements.planFinished` in `internal/controlplane/announcements.go` | Runs after successful finish commit. Notification failure cannot turn the recorded completion into a refusal. Repeated announcements use the same Plan identity and preserved completion time; they do not mutate completion/evidence. |
| `applyHealthObservations` in `internal/controlplane/health.go` | Selects targets from a committed succeeded Plan and updates one existing committed Environment report. It never writes terminal status or execution; health alone cannot finish a Plan. |
| `apolloReportingPort.UpdateAgentReports` / `updateApolloAgentReports` in `internal/controlplane/apollo_agent_reports.go` | Commits raw compatibility inputs and their normalized observation together: memory/SQL transaction or one file envelope replacement. Projection reads committed Plan/report state. It does not write native terminal status, execution, or the completion receipt. |
| `apolloReportingPort.PutReport` in `internal/controlplane/apollo_ports.go` | Observation-only port retained by the Environment interface; no production caller invokes it. Each repository stores one report per call. It grants no native completion authority. |
| `PutEnvironmentReport` in memory/file/generated PostgreSQL repository adapters | Persistence primitives used by the paths above; FinishPlan invokes them only through its transaction view. Direct calls cannot change Plan status, execution or receipt. |
| Bundle import in `internal/controlplane/bundles.go`, read projection in `repository.go` | Imported/relayed reports are stored with their catalog and remain observations from another Hub. They do not finish a local Plan. Reconciliation refuses to relabel an unknown Plan identity as a local generation. |
| `tools/upgrade-data/testdata/probe/main.go` | Writes synthetic reports inside its explicit repository transaction for the upgrade-data test. It is not a runtime completion path. |

Apollo compatibility reporting remains bounded by the public transport contract
and its registered projection. The native completion receipt changes no Apollo
wire DTO or private protocol claim.

## Executable checks

`internal/controlplane/plan_finish_boundaries_test.go` injects errors immediately
before and after every Plan update, report write, entity failure suppression,
environment escalation creation and escalation removal, plus after the last
operation before commit. It exercises success, failure, empty failed
observations, and legacy terminal records with and without execution on memory,
file and PostgreSQL. Rollback comparisons include native live invalidations;
file and SQL assertions reopen the store. The successful retry must then commit
all expected effects, and later same-status, opposite-status and foreign-token
retries must preserve the committed state.

Separate regressions retain a successor observation and an operator-removed
suppression when an old finish is retried at a later time, and preserve newer
observations while backfilling a historical terminal row. The older
`plan_finish_atomic_test.go` remains an independent suppression-failure test.
Upgrade cases omit the receipt and retain either the original or a successor
report, with an active or operator-removed failure suppression, on every store.
Same-instant cases preserve a tied observation during historical backfill and
replace a predecessor report during a Plan's first completion.
`plan_finish_commit_test.go` removes a completed file staging tree to fail the
actual final rename, rejects the SQL state write with a trigger, and rejects
SQL COMMIT with a deferred trigger. It also verifies that the HTTP route neither
acknowledges nor announces a rolled-back completion and retains notification
identity across a successful retry.

Targeted mutation controls remove the completion receipt guard, remove the
newer-observation guard, ignore an evidence-write error, and discard the
existing-suppression guard; each must fail its regression. Equivalent boolean
or equality operand forms remain green, bounding the mutation harness against
known equivalent controls.

Run the focused tests with:

```sh
go test -race ./internal/controlplane -run '^TestFinishPlan' -count=1
```

Set `CONDUIT_TEST_POSTGRES_DSN` to a disposable test database to include SQL;
these tests reset that database's repository state and must run serially.
`scripts/test-postgres-repository.sh` provisions the standard disposable database
and runs the whole control-plane package with the race detector.
