# Identity and authorization contracts

Sources are indexed in `review-ledger.tsv`. The public semantics are adapted to
Platform's named-operation grant model; private Apollo authentication tokens
and role APIs are not compatibility targets.

## Authentication boundary

- Human authentication belongs to the organization's trusted federation
  provider. Platform includes a strict standard OIDC verifier with generic,
  Multipass, and Cognito access-token profiles and never stores passwords or
  implements an identity provider. Multipass support is bounded by public OAuth
  evidence and tenant-supplied issuer/JWKS/claim settings; no private
  administration or role API is assumed.
- Production operator identities may alternatively arrive through a verified,
  operator-specific SPIFFE certificate. Direct identity headers remain an
  explicit local-development escape hatch. External user/group claims resolve
  into durable Teams; service-account delegation is separately authorized and
  audited.

## Role and resource scope

- Grants bind a Team to named roles and/or operations at one resource instance
  or a `global/<resource-type>` scope. Global grants apply to every resource of
  that type.
- Canonical namespaced roles cover permission administrator/viewer;
  Environment and Entity administrator/editor/operator/viewer plus global
  Environment creator; Product administrator/release creator/creator/editor/
  viewer; Release Channel administrator/contributor/restricted contributor/
  viewer; Label administrator/contributor/creator/viewer; and separate
  administrator/approver/viewer families for vulnerability suppressions and
  vulnerability SLA settings.
- Restricted Release Channel contribution succeeds only when one of the
  principal's Teams owns or co-owns the Product; a generic role expansion never
  grants that conditional write authority.
- Environment authorization is inherited additively by its Entities. An Entity
  grant can add authority but cannot remove inherited Environment authority.
  Exact-resource grants and global type grants are evaluated together.
- The permission-administrator role can manage grants after bootstrap, while
  permission viewers remain read-only. Bootstrap principals exist only to
  establish the first durable permission-administrator Team.
- Applying a label requires both `label:contributor` authority on that label and
  write authority on the target resource. Possessing either half alone fails.
- Trusted Module/import auto-approval requires both the exact
  operator-configured username and a verified service-account claim. A human
  principal using the same username does not inherit automation trust.

## Local evidence

- `internal/controlplane/governance.go` resolves external claims, evaluates
  global/exact/inherited scopes, expands canonical roles into least-privilege
  operations, and implements dual label authorization.
- `internal/controlplane/http.go` permits durable permission administrators—not
  just bootstrap identities—to manage resource grants and policies.
- `go test ./internal/controlplane -run 'TestRoleAuthorization|TestPermissionAdministrator|TestServiceAccountDelegation'`
  covers the contract and append-only allow/deny audit behavior.
