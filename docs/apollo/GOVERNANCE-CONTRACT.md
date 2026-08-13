# Governance and Change Request contract

This note defines Platform's Apollo-aligned governance kernel. It reconstructs
public behavior without claiming compatibility with private Apollo Hub or Agent
protocols.

## Responsibility boundary

Platform owns authenticated operator identity, Teams, resource grants, policy
evaluation, Change Request state, reviewer decisions, and immutable
authorization audit events. Flux distributes the reviewed Platform runtime;
it does not evaluate human approval policy or bypass a Plan. The outbound Spoke
Agent can lease only a Plan whose Change Request has reached `APPROVED`.

External identity providers remain authoritative for users and groups. The
server resolves those claims into persisted Team membership. Production callers
use an injected identity resolver or a verified client certificate with URI SAN
`spiffe://platform.delivery/operator/<subject>`; the distinct URI prevents a
Spoke Agent certificate from becoming an operator identity. Direct identity
headers are an explicit development-only option.

## Teams and resource grants

A Team has at least one administrator and one member user or external group.
Bootstrap administrators may create Teams. Thereafter Team administrators may
edit their Team. Resource grants attach `OWNER` or `CO_OWNER`, named roles, and
named operations to a Team and resource RID. CEL inputs expose the author's
roles plus the resource's owner/co-owner Teams.

Grants may target one resource or a `global/<resource-type>` scope. Canonical
role names expand to least-privilege operations; exact and global grants are
additive. Environment grants are inherited by their Entities, and Entity grants
can add but never subtract authority. Permission administrators may manage
grants after bootstrap. Label application is a dual check requiring Label
Contributor authority on the label and write authority on the target resource.

## Per-change CEL policy evaluation

Every Plan step becomes a stable Entity change with a change type, request,
field diff, and referenced Environment, Product, and existing installation.
Every enabled policy is evaluated independently for every change. Policies are
combined with logical AND:

- `approve(reason)` records an approving policy opinion;
- `reject(reason)` rejects the Change Request;
- `no_op()` contributes no opinion, and all policies returning `no_op` for one
  change places the request in `ERROR`;
- `require_team`, `require_role`, and `require_operation` create reviewer
  requirements;
- `require_any` creates one requirement with alternative eligible reviewers.

Policy inputs include `type`, `author`, `onBehalfOf`, `editors`, `metadata`,
`request`, `diff`, `referencedResources`, `createdAt`, `now`, and typed policy
variables. The evaluator uses the Apache-2.0
[`cel-go`](https://github.com/google/cel-go) implementation.

## Change Request lifecycle

Persisted statuses are `PENDING_APPROVALS`, `APPROVED`, `REJECTED`,
`CANCELLED`, and `ERROR`. One reviewer may satisfy several requirements, while
every requirement for every Entity change must have at least one reviewer.
Review rationale and resolved Teams are durable. Unauthorized attempts are
also recorded in the append-only authorization audit log.

The exact `Do not merge` label prevents transition to `APPROVED` even after all
requirements are satisfied. A reviewer may reject with a required reason. The
author may cancel or reopen a rejected/cancelled request; reopening clears stale
reviews and re-evaluates current policy. Plan approval/cancellation is kept in
lockstep with its Change Request.

The HTTP surface is rooted at:

- `/v1/teams` and `/v1/resource-grants`;
- `/v1/approval-policies`;
- `/v1/change-requests` with approve, reject, cancel, reopen, and label actions;
- `/v1/authorization-audit`;
- `/v1/plans`, which always creates and links a governed Change Request.

## External boundary

Bundle imports create one atomic Change Request per target Environment; bulk
operations expand into resumable, individually authorized installation
changes; proposal/read/operation routes enforce resource grants; and delegated
service accounts require explicit `onBehalfOf` authority. A UI and external
identity-provider administration remain consumers of this control-plane
contract rather than duplicated platform responsibilities.

Public semantic references reviewed 2026-08-01:

- [CEL policies overview](https://www.palantir.com/docs/apollo/cel-policies/overview/)
- [CEL functions](https://www.palantir.com/docs/apollo/cel-policies/cel-functions/)
- [Change Requests](https://www.palantir.com/docs/apollo/managing-changes/change-requests/)
- [Teams](https://www.palantir.com/docs/apollo/core/teams/)
- [Change Requests on Environment import](https://www.palantir.com/docs/apollo/export-import/change-requests-on-environment-import/)
