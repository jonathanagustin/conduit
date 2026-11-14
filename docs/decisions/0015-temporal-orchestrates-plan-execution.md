---
status: accepted
date: 2026-11-14
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Temporal orchestrates Plan execution behind the delivery kernel

## Context and problem statement

ADR 0013 chose Temporal for packaging runs and said it was not for Plan
execution. That line kept the delivery kernel as the authority for Plans,
leases, rollout, and observed state. It also left rollout waves, maintenance
windows, approval waits, canary soak, and rollback as logic the kernel and
the Spoke would have to grow inside the request path.

Those are workflow problems: durable timers, a wait for a decision that
already exists somewhere else, and a compensating path when a later wave
fails. Building a second copy of them next to the kernel would duplicate
Temporal. Putting Temporal in charge of approval, leases, or reported state
would duplicate the kernel.

ADR 0003 already runs durable reconciliation for desired state and Plan
proposals. This record does not replace that supervisor. It says what may
sequence an approved Plan after the proposal exists.

## Decision drivers

- PostgreSQL remains the system of record for Plans, approvals, leases,
  desired state, reported state, and the hash-chained authorization audit.
  ADR 0012 is unchanged.
- Every Plan transition goes through a kernel lifecycle method. An activity
  is idempotent and keyed by Plan, wave, and attempt.
- A Temporal Signal or Update only names a governance record. It is not an
  approval. The activity reads that record back from the kernel.
- The Spoke keeps its native reconciler. Temporal does not apply manifests
  or report cluster state.
- With no worker configured, the native lifecycle path runs. That is the
  supported mode for a small or disconnected install.
- ADR 0004 (Gitless GitOps) is unchanged. This record does not put a Git
  credential on a cluster or move runtime convergence off its current adapter.
- A missing Temporal service must not be described as air-gapped or
  production-ready. The operational cost has to be stated.

## Considered options

- **Temporal behind the kernel.** The workflow orders waves, timers, the
  approval wait, canary soak, and a compensating rollback path. Activities
  call the lifecycle methods that already exist. Rejected as the only mode:
  a small install should not have to run a workflow cluster.
- **Keep sequencing inside the kernel and the Spoke only.** One less
  service. Also a second implementation of timers, waits, and sagas as the
  wave model grows. Rejected as the only mode for the same reason ADR 0013
  rejected a private workflow engine for packaging.
- **Temporal as the system of record.** Rejected. Plan status, approvals,
  leases, desired state, reported state, and the audit chain stay in
  PostgreSQL.

## Decision outcome

Temporal may orchestrate Plan execution. The delivery kernel remains the
authority for the transitions. This amends ADR 0013's statement that
Temporal is not for Plan execution. Packaging runs stay on ADR 0013. ADR
0003 still owns reconciliation and Plan proposal. ADR 0015 only sequences
an approved Plan.

The native path is the default. `PLAN_ORCHESTRATOR` empty or `native`
selects it. `temporal` selects the workflow engine. The Hub process does
not read that switch until a later change wires a worker. Until then the
running Hub is the native path, which is today's behavior.

A Signal or Update carries an identifier. The activity loads the governance
record and refuses the step when the record is missing or does not match
the Plan. The signal is not stored as an approval.

Rollback is a compensating path: later failure calls the kernel cancel
(or the lifecycle method that record already uses) for the Plan. It does
not write a second lease table and it does not skip approval state.

### What is whose

```
PostgreSQL (system of record)
    Plans, approvals, leases, desired state, reported state, audit chain
        ^
        |  lifecycle methods, idempotent by plan + wave + attempt
        |
Temporal workflow (optional)
    wave order, maintenance-window timer, approval wait,
    canary soak, compensating rollback
        |
        v
Spoke native reconciler
    cluster apply and exact reported state
```

### Operational cost

A production Temporal deployment is several services, a database, upgrades,
backups, authentication, TLS, namespace retention, and an owner. The dev
server used for packaging tests is one process with a local database. Its
own help text says it is not for production and that it skips HTTP security
checks. This decision does not treat that process as a production topology,
an air-gap deployment, or the default for a small install.

The supported mode without a worker is the native lifecycle path. Spokes
keep reconciling as they do now. Building a private workflow engine inside
the kernel to avoid the dependency is the option this record rejects.

### Later Hub packaging

Not in this record's first code. A later change may ship an optional
worker, off by default, reusing the Hub image, with Helm and Zarf left
disabled until an operator turns them on. Temporal itself stays
operator-supplied. That change has to say what was tested and what was
not. It does not by itself make the deployment air-gapped or production
ready.

## Consequences

- Good: wave timers, the approval wait, canary soak, and rollback
  compensation have one engine, and the kernel keeps the writes.
- Good: a small or disconnected install does not gain a Temporal cluster.
- Bad: two drivers exist. The native driver must stay the one the Spoke
  already uses, or the paths will diverge.
- Bad: a real Temporal cluster is an operational dependency this
  repository has not staffed.
- Unknown: production schema upgrades, backup restore, and multi-worker
  failover were not run.

## Confirmation

The native driver is `NativePlanOrchestrator` in
`internal/controlplane`. `PlanOrchestratorFromEnv` selects it unless
`PLAN_ORCHESTRATOR=temporal`, which returns the unwired skeleton and
does not start a workflow. The workflow's timers and failure path are covered by
`go test ./internal/planorchestration/` using the Temporal Go test suite,
which skips workflow time without sleeping the wall clock. Neither test
is a production drill.

## More information

- Supersedes: the "not for Plan execution" limit in ADR 0013, for Plan
  sequencing only
- Superseded by: none
- Amends: ADR 0013. Refines ADR 0003. Does not change ADR 0004 or ADR 0012.
- Related: `0013-temporal-for-packaging-workflows.md`,
  `0003-durable-conduit-reconciliation.md`,
  `0004-gitless-gitops-ports-and-adapters.md`,
  `0012-authorization-audit-integrity.md`
