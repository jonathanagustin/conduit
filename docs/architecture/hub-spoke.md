# Hub and Spoke

Conduit separates decisions from cluster execution. The Hub decides and
records what may run. A Spoke runs inside one Environment, asks the Hub for
work, applies that work locally, and reports what it observed.

## Direction of control

The Spoke initiates every control-plane exchange. It leases a Plan through an
outbound connection, starts it with the opaque lease token, renews the lease
while working, and reports completion or failure. The Hub does not open a
management connection into the Environment.

This direction is a security boundary, not a networking convenience. An
Environment may restrict ingress without preventing delivery, and Hub
credentials do not become general cluster credentials.

## Hub responsibilities

The Hub owns:

- desired state and immutable Product and Release coordinates;
- approval and authorization decisions;
- Plan compilation, state transitions, and lease issuance;
- durable records of reported state and execution evidence;
- the public operator surface and Apollo integration adapter.

The Hub must not claim that a change succeeded from intent alone. Reported
state comes back from the Spoke and remains distinct from desired state.

## Spoke responsibilities

The Spoke owns the execution boundary inside its Environment. It:

- authenticates as one Environment and one Agent;
- accepts only work leased for that identity;
- prepares external material through a configured provider;
- renders and applies the declared Helm release;
- observes the resulting release and workload state;
- rolls back completed steps when a later step fails; and
- reports exact outcomes to the Hub.

The worker refuses a Plan with secret mutations when no external material
preparer is configured. It also treats lease renewal failure as cancellation of
the active work. These are current executable behaviors in
[`internal/agent`](../../internal/agent/).

## Lease invariant

A lease binds an Environment, Agent, Plan, opaque token, attempt number, and
expiry. An expired lease is not live, including at the exact expiry instant.
An empty token or Agent identifier never matches a lease. A retry receives a
new attempt number instead of rewriting the prior attempt.

Execution must remain attributable to this lease tuple. A Spoke may not reuse
a receipt from a different Plan or attempt, and the Hub may not accept a state
transition that cannot be tied to the active lease.

The Hub's target continuous loop is specified in
[Conduit reconciliation](reconciliation.md). The present code has deterministic
Plan compilation and leased execution, but not yet the complete durable worker.

## Tenant-specific inputs

Application catalogs, charts, credentials, Environment inventory, and
installation configuration remain in the tenant or product repository. The
Conduit repository contains the reusable control-plane behavior. A tenant
override must be declared data, validated against a contract, and included in
the evidence for the resulting Plan. Neither the Hub nor frontend hydration may
invent missing tenant values.

## Failure behavior

Conduit prefers an explicit blocked state to an inferred success:

- missing approval blocks leasing;
- missing identity blocks the request;
- a lost lease stops execution;
- missing external material support blocks the affected Plan;
- failed observation prevents a successful report;
- rollback failure is recorded as its own failure, not hidden by the original
  error.

Disk-pressure and durable-audit requirements add two preconditions that are not
fully wired today. They are defined in
[Deployment invariants](deployment-invariants.md).
