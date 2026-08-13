# Provider enrollment safety

Platform's operator roles govern resources inside one Hub trust domain. They
are insufficient for a service provider facing abuse by a tenant administrator
or a compromised tenant Hub. Provider safety authority must live outside
tenant-controlled Hub state and remain enforceable when that Hub is unavailable
or hostile.

Apollo's public Hub setup documentation directly establishes two provider-hosted
topologies: a customer-dedicated `palantircloud.com` Hub and a
`palantirapollo.com` Hub shared by multiple customers. It also assigns domain
setup, Kubernetes egress allowlisting, and SAML integration to a Palantir
representative. This is direct evidence for provider-owned configuration around
customer-controlled Apollo Teams and RBAC; it is not evidence of the private
storage schema, billing service, or suspension API.

Platform's startup hosted-service topology is correspondingly one
provider-owned shared Hub with logically isolated customer enrollments. A
customer may be an administrator of
its teams, products, environments, and Plans without owning the Hub process,
state-store infrastructure, ingress, identity-client registration, backups, or
provider lifecycle authority. Dedicated Hubs remain an optional paid or
regulated isolation tier, not the default cost floor. A self-hosted deployment
assigns provider authority to the organization operating that Hub.

Shared hosting is permitted only once H80's enrollment key, query scoping,
storage/encryption context, quotas, audit attribution, and cross-enrollment
adversarial gates are proven. H79 defines provider authority; it does not by
itself establish tenant isolation.

This is a Platform requirement, not a claim about a private Palantir tenant-ban
API. Public evidence establishes enrollment/organization administration,
federated identity, Apollo management overrides, suppressions, and governed
network-security changes, but not one public universal suspension operation.

## Required lifecycle

- `RESTRICTED`: deny high-risk mutations while preserving authenticated read
  access and evidence collection.
- `SUSPENDED`: deny new identity sessions, Hub mutations, Plan approvals and
  leases, Spoke registration, and tenant ingress/egress except an explicit
  evidence/recovery allowlist. Preserve state, backups, logs, and billing facts.
- `TERMINATION_PENDING`: retain the suspension while legal, contractual,
  recovery, export, and retention obligations are reviewed.
- `TERMINATED`: execute a separately approved decommission Plan with immutable
  deletion and retention receipts.

Suspension must be an idempotent, durable provider record bound to enrollment,
incident, reason code, author, approval policy, effective time, optional expiry,
and evidence-retention policy. It must not delete data or imply termination.
Restoration requires a distinct provider operation and independent approval.

## Enforcement and authority

The provider record is evaluated at every authority-bearing boundary:

1. edge ingress and token exchange;
2. Hub API authorization and Change Request creation/approval;
3. Plan lease issuance and renewal;
4. Spoke registration and trust rotation;
5. provider-owned Cilium quarantine policy; and
6. infrastructure lifecycle and backup deletion.

Tenant administrators cannot create, alter, delete, or override provider
records. Provider identities use a separate trust root, hardware-backed MFA for
humans, narrowly scoped service credentials, immutable external audit export,
and two-person restoration/termination approval. A local tenant role, group,
CEL policy, management override, compromised Hub database, or stale Spoke lease
must not bypass an effective suspension.

## Acceptance gates

The capability remains open until tests prove:

- a tenant administrator with every tenant-local role cannot modify the record;
- a suspended enrollment cannot open, approve, lease, renew, or report success
  for new work, including through a previously authenticated session;
- an already executing attempt reaches a bounded safe stop and cannot receive a
  replacement lease;
- provider recovery/evidence traffic is the only explicit fail-closed exception;
- expiry does not silently restore authority; it requires an approved restore;
- suspension survives Hub restart, restore, and tenant-state rollback;
- restoration and termination have independent approvers and receipts; and
- cross-enrollment tests prove one record never affects an unrelated tenant.

Until these gates pass, production multi-customer service must retain external
provider controls at identity, ingress, cloud account, and Spoke trust layers.
