# Conduit reconciliation

Conduit owns continuous convergence of reviewed tenant intent. Flux may install
and upgrade the Conduit runtime, and Helm may apply an approved Plan, but
neither owns Conduit's Product, Plan, approval, lease, rollback, or convergence
semantics.

## Current implementation

Production PostgreSQL Hubs run a Conduit-owned reconciliation supervisor in
every replica. The implementation is split by responsibility:

- `internal/delivery/reconciliation/domain` defines the stable
  `Environment + installation` key, immutable desired and observed snapshots,
  and deterministic `CONVERGED`, `PROPOSE_PLAN`, `BLOCKED`, and `WAIT`
  decisions;
- `application` owns leased processing, bounded retry, proposal dispatch,
  startup priming, periodic resynchronization, and dropped-work repair;
- `ports` defines storage, planning, clock, and synchronization capabilities;
- `adapters/postgres` owns context tables for inputs, work, decisions, audit,
  proposal outbox entries, and Plan receipts; and
- `adapters/controlplane` is a temporary anti-corruption adapter that observes
  committed Plans and reports and submits proposals through the ordinary Plan
  lifecycle.

The worker does not approve or execute a Plan. Governance still authorizes it,
Execution still leases it, and the Spoke still applies and reports it.

Explicit file-repository mode remains a one-replica development and migration
profile. It does not claim durable continuous reconciliation.

## Durable loop

1. A committed Plan or exact Plan-bound report is translated into immutable
   per-installation input.
2. The PostgreSQL adapter records the input and increments a work revision in
   the same transaction.
3. One replica leases the key with `FOR UPDATE SKIP LOCKED`, an opaque token,
   attempt number, expiry, and input revision.
4. The domain computes a deterministic decision from the latest desired and
   observed snapshots.
5. The store atomically commits the decision, its audit event, and any proposal
   outbox entry. An audit failure leaves all three invisible.
6. A dispatcher submits the proposal through `Lifecycle.CreatePlan`; this
   creates an ordinary Plan with the existing approval and execution rules.
7. The Plan receipt and outbox acknowledgement commit together. A crash before
   acknowledgement replays the content-addressed proposal safely.
8. A later exact report enqueues the key again and produces `CONVERGED` only
   when generation, target digest, freshness, and completion evidence match.
9. Startup and periodic scans reconstruct inputs from committed Plans and
   reports, then repair a missing work notification by comparing the current
   input digest with the latest decision.

A work revision is a fencing value, not a desired-state generation. If input
changes while a worker holds a lease, the old decision may remain as audit
evidence, but it cannot publish an outbox proposal or acknowledge the newer
work. The planner independently rechecks the current desired snapshot before
and after ordinary Plan creation. A superseded proposal is terminally retired
instead of retried as stale authority.

## Fidelity boundary

The adapter derives a desired target digest only from the persisted Plan step.
An observed digest uses the same closed projection: Product, version, Release
digest, namespace, configuration digest, and expected lifecycle. Operational
completion also requires the exact health, rollout, configuration, and
lifecycle result accepted by the existing Plan result contract.

A report without an exact local Plan ID is not assigned a plausible generation.
Imported or relayed evidence that names an unknown Plan remains compatibility
evidence; it cannot assert local convergence. The planner loads the latest
committed full-environment desired state at dispatch time, so an unchanged
installation cannot replay an old payload after another installation changes.

## Package boundary

Dependencies point inward:

```text
control-plane compatibility adapter  PostgreSQL adapter
                 \                    /
                  ports -> application -> domain
```

The reconciliation domain, application, and ports do not import HTTP, Flux,
Helm, Kubernetes, or `internal/controlplane`. Only the named compatibility
adapter may import the temporary facade. `scripts/verify-domain-boundaries.mjs`
and its planted-control regression test enforce that rule.

## Safety rules

- Reconciliation proposes; Governance authorizes; Execution leases; the Spoke
  applies.
- Missing, stale, conflicting, or incomplete reported state is never
  convergence.
- An audit append failure prevents the decision and proposal from becoming
  visible.
- Credentials and secret values never enter reconciliation inputs, decisions,
  outbox entries, or logs.
- Retry is bounded and attributable. Superseded desired state receives a new
  work revision rather than borrowing an old attempt's authority.
- Disk and artifact preflight remain separate last-safe-point requirements
  before an approved Plan becomes leaseable.

## Executable evidence

`scripts/test-postgres-repository.sh` runs the race detector against a
digest-pinned disposable PostgreSQL server. It proves multi-replica single
winners, exact input round trips, stale-lease rejection, proposal crash
recovery, decision/audit/outbox atomicity, supersession fencing, dropped-work
repair, and production Hub composition through the ordinary Plan lifecycle.

The deterministic application tests additionally cover exact no-op convergence,
duplicate events, stale observations, retry exhaustion, latest-generation
selection, planner idempotency, and terminal retirement of stale proposals.
Flux is therefore only the Gitless runtime bootstrap/upgrade reconciler; it is
not the owner of tenant delivery convergence.
