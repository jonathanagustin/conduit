# Conduit reconciliation

Conduit will own the reconciliation of approved desired state into observed
deployment state. Flux may bootstrap and upgrade the Conduit runtime, and Helm
may apply a release, but neither tool owns Conduit's Product, Plan, approval,
lease, rollback, or convergence semantics.

## Current implementation

The repository already has the main reconciliation primitives:

- `BuildPlanWithObserved` resolves desired state against an Environment report
  and emits ordered `reconcile` or `uninstall` steps;
- Plan generations and persisted desired-state bytes are immutable;
- governance controls when a proposed Plan becomes executable;
- one Spoke leases a Plan, applies it through bounded adapters, observes the
  result, and reports completion or failure; and
- desired and reported state remain separate in the fleet projection.

These pieces support a reconciliation operation, but they are not yet a durable
continuous reconciler. Plan creation is still driven through the legacy
`internal/controlplane` service. There is no context-owned work queue, periodic
resynchronization loop, or transactional decision-and-audit outbox.

## Target loop

A native Hub reconciliation worker should process a stable key such as
`Environment + installation`:

1. Enqueue the key when desired state, reported state, catalog admission,
   recall, policy, or a relevant timer changes.
2. Lease the queue item with an attempt number and bounded expiry.
3. Read one immutable input snapshot: desired generation, reported-state
   digest and observation time, catalog coordinates, constraints, and current
   policy references.
4. Compute a deterministic result: `CONVERGED`, `PROPOSE_PLAN`, `BLOCKED`, or
   `WAIT`.
5. For `PROPOSE_PLAN`, derive an idempotency key from the input snapshot and
   proposed intent, then atomically persist the proposal, decision event, and
   future wake-up.
6. Let Governance approve or reject the proposal. The reconciler cannot
   approve its own work.
7. Let Execution lease an approved Plan to the matching Spoke. The reconciler
   cannot execute it.
8. Enqueue the key again when execution or reported state changes. Continue
   until exact convergence or an attributable blocked state is recorded.
9. Periodically rescan bounded keys so a lost notification cannot leave state
   permanently unreconciled.

Retries use bounded exponential backoff with jitter, but the durable key and
input digest make repeated attempts idempotent. A new desired generation does
not rewrite an old attempt; it creates a new reconciliation input.

## Ownership and package boundary

The target package is a Delivery application service, for example:

```text
internal/delivery/reconciliation/
  domain/       decision, key, convergence and blocked reasons
  application/  deterministic reconciler and worker
  ports/        desired/report/catalog/policy readers, queue, outbox and clock
  adapters/     repository-backed queue and composition bindings
```

The domain layer must not import HTTP, Helm, Kubernetes, Flux, a generated API,
or `internal/controlplane`. Cross-context inputs arrive as immutable snapshots.
The Hub composition root supplies adapters.

## Safety rules

- Reconciliation proposes; Governance authorizes; Execution leases; the Spoke
  applies. No layer silently gains the next layer's authority.
- Missing, stale, conflicting, or incomplete reported state cannot be treated
  as convergence.
- An audit append failure prevents the proposal or state transition from
  becoming visible.
- Disk and artifact preflight evidence must be fresh and Plan-bound before an
  approved Plan becomes leaseable.
- Credentials and secret values never enter the queue, decision, Plan, or
  reconciliation log.
- A blocked result names the failed invariant and the evidence needed to wake
  it; it is not an infinite retry loop.

## Acceptance gate

The first implementation slice should reconcile one installation end to end
without importing `internal/controlplane` into the new domain or application
packages. Tests must cover stable no-op convergence, idempotent duplicate
events, desired-state supersession, stale observations, crash recovery between
decision and persistence, audit failure, retry exhaustion, and periodic repair
of a deliberately dropped event.

Only after that slice is production-consumed should Flux be described solely
as Conduit runtime bootstrap and upgrade machinery rather than the continuous
owner of tenant software delivery.
