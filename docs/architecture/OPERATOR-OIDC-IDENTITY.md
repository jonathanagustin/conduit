# Operator OIDC identity

Platform uses an external OpenID Connect issuer to authenticate human and
service-user calls made through Conduit or the Hub API. The issuer proves who
the caller is. Platform Teams, role grants, CEL policy, Change Requests, and
Plan approvals remain the only authorization authority.

The preferred connected-tenant profile is `multipass`. The pinned public
Apollo OpenAPI snapshot establishes bearer JWT authentication, OAuth 2.0
authorization-code and client-credentials flows, Multipass authorization and
token endpoints, and the `apollo-api:use-api` scope. It does not establish a
tenant's issuer URL, discovery document, JWKS URL, username/group claim shape,
or private role API. Those values must be supplied by the tenant and no private
Multipass administration contract is claimed.

Apollo's public Hub setup guide separately documents SAML federation for the
browser Hub: a Palantir representative configures entity ID, ACS URL, single
logout URL, and signing certificate, after which SSO groups may mirror into
Apollo Teams. Platform's current OIDC work covers Conduit and Hub API bearer
authentication; it does not claim SAML protocol parity or silently treat OIDC
as proof of Apollo's documented browser-session flow.

## Provisioning boundary

Each downstream tenant registers a distinct Hub/Conduit OAuth client with its
existing issuer and supplies public trust configuration through the Hub chart:

```yaml
operatorOIDC:
  enabled: true
  profile: multipass
  issuer: https://identity.example.mil
  clientID: tenant-platform-conduit
  requiredScopes: [apollo-api:use-api]
```

The chart stores no client secret or bearer token. It mounts the already-owned
external CA bundle and passes only issuer, client ID, claim vocabulary, scope,
and signing-algorithm policy. Standard discovery is preferred; an explicit
HTTPS JWKS URL supports constrained environments.

The Hub requires exact issuer and audience, an allowed asymmetric signature,
expiry/not-before validity, a bounded recent `iat`, required scope, non-empty
subject and username, and bounded typed groups. Multiple Authorization headers,
oversized tokens, malformed claims, and unknown profiles fail closed. A
verified operator SPIFFE certificate remains a separate fallback when no
bearer token is present.

Profiles are configuration conveniences, not new trust protocols:

- `multipass` uses `sub` as the stable username and requires
  `apollo-api:use-api` unless overridden by captured tenant evidence;
- `cognito-access` validates Cognito's `client_id`, `username`,
  `cognito:groups`, and `token_use=access` access-token shape;
- `generic` accepts any conforming issuer with explicit claim configuration,
  including tenant-local Keycloak for disconnected environments.

Provider groups are identity attributes only. They do not become Platform
roles implicitly. An administrator must create explicit, audited Team or role
grants. Spoke workload identity continues to use SPIFFE and cloud workload
identity; OIDC never substitutes for Kubernetes RBAC, IAM/IRSA, or a Plan
lease.

One Hub may serve many environments and teams inside one trust domain. It is
not yet a hard shared-SaaS tenant boundary: mutually untrusted tenants or
security domains receive distinct Hubs, state stores, OAuth clients, signing
keys, trust roots, and lifecycle receipts until mandatory tenant keys on every
resource/query and cross-tenant adversarial tests prove stronger isolation.

## Remaining live proof

Full Multipass support remains open until an authorized tenant proves discovery
or explicit JWKS, token claim shape, browser login, client-credentials login,
key rotation, and revocation/expiry behavior without exposing a token.
