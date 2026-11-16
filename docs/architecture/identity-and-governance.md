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

The repository has durable file-backed authorization events. The shared
`ResourceDecision.Allow` gate requires its policy-verdict append to succeed
before it admits a mutation, including a bootstrap administrator or delegated
carrier. A missing required audit sink refuses admission. Unknown operations
receive no audit-outage bypass. Audit failure returns the existing authorization
refusal, so callers cannot mutate state or start sequencing through this gate.

The explicit `resource:read` and `debug-session-request:read` operations retain
their existing best-effort behavior with a configured failing sink. They do not
yet expose a degraded-audit indicator. A recorded ALLOW describes the policy
verdict, not proof that a later mutation completed; a sink can persist and then
report an ambiguous failure, in which case the mutation remains refused.

This closes only the shared resource-decision gate's mutation gap. Direct
administrator predicates, delegation-resolution/request-created notifications
and other best-effort audit calls are separate paths. Existing transactional
Change Request approval audit behavior is unchanged. The remaining target is
fail-closed audit durability for every governed mutation and an explicit degraded
read contract, with idempotent event identities; those broader guarantees are
not established by this bounded gate.

## Review questions

- Which verified credential minted this principal?
- Which exact grant and role conveyed the requested operation?
- Which approval requirement authorized this change?
- Did the authorization event persist before mutation or leaseability?
- Can every answer be produced without exposing a credential or secret?

If any answer is unknown, the governed mutation is blocked.
