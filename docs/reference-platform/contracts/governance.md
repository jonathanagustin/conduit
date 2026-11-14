# Governance contract

Governance owns who may propose, review, approve, and operate a change. This is
a Conduit-native policy boundary. Apollo role names are foreign vocabulary and
do not become grants without an explicit native mapping.

## Invariants

- Authenticated principals, Team membership, and resource grants are evaluated
  before an operation reaches domain behavior.
- Exact-resource, inherited Environment-to-installation, and global scopes are
  additive. A grant cannot subtract authority from another valid grant.
- A Change Request records per-change policy results and reviewer requirements.
- Approval, rejection, cancellation, and error are explicit persisted states.
- Authorization decisions, including successful ones, produce attributable
  audit events.
- External identity and credentials remain outside policy persistence.

[`internal/governance/domain/authorization.go`](../../../internal/governance/domain/authorization.go)
contains the extracted authorization vocabulary and fail-closed Apollo role
aliases. The policy evaluator, Change Request lifecycle, repositories, and HTTP
composition still reside in
[`internal/controlplane`](../../../internal/controlplane) during the DDD
migration.

An Apollo role inventory proves the operations Apollo associates with that
role. Conduit separately decides whether it implements a safe native analogue.
An unmapped role grants nothing.

## Verification

```bash
go test ./internal/governance/...
go test ./internal/controlplane -run 'TestGovernance|TestCEL|TestRoleAuthorization|TestAuthorizationAudit'
```

These gates cover Conduit authorization and review behavior. They do not prove
private Apollo authorization-policy or audit-wire compatibility.
