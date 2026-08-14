---
status: accepted
date: 2026-08-14
decision-makers:
  - repository owner
consulted: []
informed: []
---

# Run Conduit-owned durable reconciliation in production Hubs

## Context and problem statement

Conduit already compiled desired state into governed Plans and accepted exact
reported state, but Plan creation depended on a request reaching the legacy
facade. A dropped notification or process restart could leave drift without a
durable owner. Flux must not acquire Conduit's tenant intent, approval, or Plan
semantics merely because it reconciles the Conduit runtime.

## Decision drivers

- Convergence must survive process loss and run safely in multiple Hub replicas.
- Desired state that changes during a lease must fence the older attempt.
- Decision, audit, and proposal visibility must be atomic.
- Repeated dispatch after a crash must create one content-addressed ordinary Plan.
- Missing or foreign Plan identity must not be converted into an invented generation.
- Domain and application code must remain independent of PostgreSQL, Flux,
  Kubernetes, HTTP, and the legacy control-plane facade.

## Decision outcome

Production PostgreSQL Hubs run a reconciliation supervisor backed by
context-owned PostgreSQL tables. Each installation key has immutable desired
and observed snapshots, revision-fenced leased work, deterministic decisions,
an audit record, a transactional proposal outbox, and durable Plan receipts.

A temporary control-plane adapter translates only persisted Plan steps and
exact Plan-bound reports. Proposal dispatch uses the ordinary Plan lifecycle;
it does not approve, lease, or execute. Startup and periodic synchronization
repair the adapter's cross-context write window from committed evidence.

File repository mode remains an explicit single-replica development and
migration profile without the durable supervisor.

### Consequences

- Good: any Hub replica may work, but PostgreSQL selects one lease winner.
- Good: an audit failure exposes no decision or proposal.
- Good: a concurrent desired change leaves newer work runnable and emits no
  stale proposal.
- Good: Flux is limited to Conduit runtime and cluster-prerequisite convergence.
- Bad: committed Plans and reconciliation inputs currently use separate
  transactions, requiring periodic evidence-based synchronization.
- Bad: the compatibility planner still calls the legacy Plan lifecycle until
  Delivery's remaining services leave `internal/controlplane`.
- Unknown: database and Kubernetes node failover behavior still needs the
  deployment-availability acceptance gate; no Rubix availability claim follows
  from this decision.

## Confirmation

`scripts/test-postgres-repository.sh` runs race-enabled tests against a
digest-pinned PostgreSQL server for two store instances and the production Hub
composition. `scripts/verify-domain-boundaries.mjs` rejects a planted
control-plane import in reconciliation application code while allowing the
named compatibility adapter.

B6, H10, A9, P11, and metric m0153 record the current scope.

## More information

- Supersedes: Flux or request arrival as tenant convergence owner
- Superseded by: none
- Evidence snapshot/revision: branch commit `f86ab92` plus the documentation
  and boundary-gate commit that follows it
- Pareto/value assessment: the context tables add one production dependency
  already required by the Hub, while avoiding a second Kubernetes controller
  model and preserving all existing governance and execution authority.
