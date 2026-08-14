# Deployment invariants

These rules apply to every Conduit deployment path, including Helm, Flux,
local bootstrap, disconnected packages, and future adapters. A tool is an
implementation choice. The invariants remain the same.

## Required invariants

| Boundary | Required behavior |
|---|---|
| Repository | `main` is the only long-lived branch; changes arrive through a short-lived branch and squash merge |
| CI | Repository-authored workflows run only by manual dispatch; the operator chooses when paid CI runs |
| Release | Merge, test, publish, promote, and deploy are separate actions with separate authority |
| Artifact | Workloads and charts resolve to immutable digests with digest-bound security evidence |
| Source | Production clusters pull verified private OCI sources by digest and receive no Git credential |
| Environment | The tenant owns namespaces, credentials, storage classes, cluster-wide RBAC, and installation configuration |
| Connectivity | The Spoke initiates control-plane traffic; the Hub does not require inbound cluster management access |
| Governance | Approval, artifact admission, and fresh preflight evidence are satisfied before leaseability |
| Execution | A Plan executes under one live lease and reports observed state separately from desired state |
| Recovery | Failure records completed work and attempts rollback without hiding rollback errors |
| Audit | Governed mutation does not proceed unless its decision event is durably persisted |

## Transactional Hub state

The production Helm profile runs two independent Hub replicas against
PostgreSQL. The DSN arrives through one read-only deployment-owned Secret file;
it is never a chart value, environment value, log field, Plan, or audit field.
Metadata writes take a database row lock and commit one generation, so approval,
Plan synchronization, application evidence, and their authorization event can
share one visibility boundary. Immutable blob bytes live in a separate table.

The metadata snapshot is a cutover adapter, not the final storage shape. It
serializes writes through one row and therefore prioritizes correctness over
high write throughput. Each bounded context can later move to context-owned
tables behind the same ports. Explicit file mode remains available for local
development and migration only; its chart profile is limited to one replica
with `Recreate` and a retained PVC.

## Manual CI and release

CI is deliberately manual because hosted runner time has a direct cost. The
Trunk gate must be dispatched for the exact pull-request commit before merge.
Other expensive checks and publication workflows are run only when their
release or verification purpose is explicit. No push, pull-request, merge, or
tag event may silently start a repository-authored workflow.

A green Trunk gate authorizes only the branch merge rule it satisfies. It does
not authorize OCI publication, registry promotion, tenant reconciliation, or
deployment.

## Tenant fidelity

Deployment inputs must come from a declared tenant source and remain traceable
to it. Runtime overrides are validated data. An adapter may normalize a value
or translate a public contract, but it may not fill a missing tenant value with
a plausible guess. Unsupported source behavior remains an explicit gap.

The reusable charts must not take ownership of tenant namespaces, credentials,
storage classes, cluster-wide controllers, or broad RBAC. Those resources stay
with the Environment owner and are supplied through bounded references.

## Disk-pressure gate

The required production rule is fail-closed: a Plan that can increase local
storage use is not leaseable unless a fresh, Plan-bound preflight proves enough
bytes and inodes on every eligible node, with DiskPressure false and declared
reserves intact. OCI transfer size, unpacked size, artifact inode demand,
placement, disruption budget, and rollout overlap are part of the projection.

The optimization context currently validates these observations and produces
preflight records. The following links are still missing in the production
path:

- provider collectors that emit immutable, fresh receipts;
- a neutral preflight reference on the governed proposal;
- Hub-side binding and freshness checks at approval and lease time; and
- tested offload and restoration for durable state before local cache eviction.

Until those links exist, Conduit must not claim that delivery is protected from
disk pressure end to end.

## Audit durability gate

The required production rule is also fail-closed for audit. The authorization
decision event must be durably appended before a governed mutation or lease is
made visible. PostgreSQL approval now commits the Change Request, synchronized
Plan, application result, and authorization event together; an injected audit
failure rolls the approval back. Other governed mutation paths and lease audit
events have not yet moved into that transaction boundary. Audit durability is
therefore proved for approval, not yet for every mutation or lease.

## Minimum deployment evidence

A deployment receipt should bind the source revision, Plan and attempt,
Environment and Agent identity, approved change, exact artifact digests,
preflight record, execution observations, rollback outcome, and durable audit
event identifiers. Evidence may be stored locally or in a private OCI-backed
store, but its digest and restoration path must remain verifiable.

Related boundaries are described in [Hub and Spoke](hub-spoke.md),
[Artifacts and releases](artifacts-and-releases.md), and
[Identity and governance](identity-and-governance.md). The selected deployment
source and trust chain are recorded in [Gitless GitOps](gitless-gitops.md) and
[ADR 0001](../decisions/0001-gitless-gitops.md).
