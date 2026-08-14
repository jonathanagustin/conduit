# Identity and governance

Identity answers who is calling. Governance answers what that principal may do.
Conduit keeps those decisions separate so credential validity cannot become an
implicit grant.

## Identity

[`internal/identity`](../../internal/identity/) derives operator and Spoke
identity from verified credentials. Spoke certificates bind one Environment
and one Agent through a SPIFFE URI. Operator certificates bind an operator
subject and normalized groups. Development headers are accepted only when the
composition root explicitly enables the insecure development mode.

OIDC tokens, bearer-token files, and mTLS configuration belong at the transport
adapter. The domain receives normalized claims, not raw credentials. Tokens,
private keys, cookies, and secret values must not enter desired state, Plans,
audit fields, or persisted profiles.

## Authorization

[`internal/governance`](../../internal/governance/) owns Conduit roles,
operations, grants, resource scopes, and effective principals. A service
account acting on behalf of a person is evaluated as that delegated principal.
Canonical roles are valid only on their allowed resource family.

Apollo role identifiers are input vocabulary, not authority. The bounded alias
table maps only the exact Apollo roles whose reduced Conduit meaning is
implemented. An unmapped Apollo role grants no Conduit operation. This policy
is intentionally narrower than any captured Apollo role graph.

## Approval

Authorization to propose a change is distinct from approval to execute it.
Change policy records the required reviewers and the evidence that satisfied
each requirement. A Plan is leaseable only after the applicable policy is
satisfied and other deployment preconditions remain valid.

Revalidation is required when evidence can age or change between approval and
execution. A prior approval must not bless a different artifact digest,
Environment, Plan revision, or substrate observation.

## Audit contract

Every allow and deny decision should produce an attributable audit event with
subject, action, resource, decision, reason, trace identity, bounded request
and result fields, and creation time. Audit data must exclude raw credentials
and secret material.

The repository has durable file-backed authorization events. The current
governance service, however, discards errors returned by
`AppendAuthorizationAudit` on both allow and deny paths. This means successful
authorization can proceed when the audit write failed. That is an explicit
current gap.

The required target is fail-closed for governed mutation: if the decision event
cannot be durably appended, the mutation does not proceed and no lease becomes
available. Read-only operations may use a separately documented degraded mode,
but they must expose that audit durability is unavailable. Retrying an append
must be idempotent by event identity.

## Review questions

- Which verified credential minted this principal?
- Which exact grant and role conveyed the requested operation?
- Which approval requirement authorized this change?
- Did the authorization event persist before mutation or leaseability?
- Can every answer be produced without exposing a credential or secret?

If any answer is unknown, the governed mutation is blocked.
