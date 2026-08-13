# Plan orchestration contracts

Sources are indexed in `review-ledger.tsv`. This note records semantic
alignment on Platform's native Plan wire; it does not claim compatibility with
Apollo's private orchestration protocol.

## Derived contract

- Installation, version/configuration reconciliation, ordinary uninstall, and
  secret lifecycle are durable Plans with explicit status, Plan-level
  timestamps, ordered reconcile steps, aggregate failure evidence, and approval
  state.
- Commands have first priority. Recalled-Release roll-offs have priority over
  routine Plans. A newer desired generation cancels older non-executing work;
  leased/running work is never silently replaced.
- Removing an installed Entity from desired state is work, not catalog
  garbage collection. Dependents uninstall before dependencies, and external
  secret deletion follows successful workload removal. Failure reverses those
  operations in transaction order.
- Suppression and maintenance constraints are re-evaluated when a Plan is
  leased. Commands bypass ordinary constraints. A failed attempt is rolled
  back before its automatic suppression is created; human suppression is
  never ignored by a later ordinary Plan.
- Product version and resolved configuration remain one atomic reconcile step.

## Structured execution diagnostics

Each completed Agent attempt now persists an attributable execution record with
the server-derived Agent ID and lease-attempt number, ordered task sequence,
phase/status, start/end timestamps, installation and namespace, rollback
outcomes, and typed Kubernetes/Helm resource diagnostics. The Agent reports
provider output only through an explicit diagnostic error; generic command
output is not promoted automatically.

Failure summaries, resource messages, and optional Agent output are redacted
again at the Hub boundary. Output is bounded to 4 KiB per task and 32 KiB per
attempt, all diagnostic data is capped at 256 KiB, and resource and task counts
are bounded. Plan admission computes the worker's complete late-failure
envelope before approval: validation, every secret mutation, apply and observe
for every step, a possible lease failure, and rollback of every operation that
can produce a receipt. A Plan whose worst-case timeline would exceed the 512
durable-task limit is rejected rather than becoming executable work whose
failure evidence the Hub cannot persist. Timestamps cannot overlap, every installation/namespace must
belong to the leased Plan, and rollback statuses are confined to rollback tasks.
A successful Plan cannot contain failed tasks and a failed Plan must contain one.
The ordinary authenticated Plan GET endpoint projects the same durable record;
no separate unaudited log channel exists. During a rolling upgrade, an older
Agent that omits the new timeline is accepted and stored as one explicitly
`LEGACY` task; new Agents always send the full structured record.

## Platform and Flux boundary

Flux continuously reconciles the reviewed Hub/Spoke runtime source. The
delivery controller evaluates each submitted immutable Environment generation,
persists Plans, and rechecks dynamic lease constraints. The repository does
not claim Apollo's private four-hour recomputation timer or Hub invalidation
wire. A source adapter must resubmit a new generation when channel/catalog,
Entity settings, or dependency topology changes; reported-state-aware
selection is then recomputed and pending older Plans are superseded.

Executable evidence:

```text
go test ./internal/controlplane ./internal/agent -run 'TestDesiredEntityRemoval|TestRecallRollOffLeases|TestWorker|TestPlanExecution|TestBuildPlanRejectsAnUnreportable|TestManualSuppression|TestLeaseWaits'
```
