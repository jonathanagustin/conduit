# Route authorization

Every route the Hub mounts, and the decision it makes before it answers.

## The default, measured

`PRODUCTION-READINESS.md` used to file this under "Test types": *"Refusals are
tested where someone thought to; nothing enumerates route × role, so a new route
defaults to untested."* The deficiency underneath that sentence was not
coverage. The question nobody had asked was what the Hub does with a route that
has **no** registered authorization decision, and the document never stated a
fail-closed default because there wasn't one.

It was measured before anything was built. A route was mounted the way new Hub
routes are mounted — in front of the legacy facade, from
`internal/hub/composition` — with no authorization check of any kind, and driven
under production `HandlerOptions`: no insecure identity headers, no OIDC
resolver, no client certificate.

```
--- production shape (no insecure headers) ---
  NEW route, no decision registered     unauthenticated (no headers) -> 200 {"tenant-b-secret":"the crown jewels"}
  NEW route, no decision registered     principal holding nothing    -> 200 {"tenant-b-secret":"the crown jewels"}
  EXISTING route, decision registered   unauthenticated (no headers) -> 401 {"error":"unauthorized","status":"401"}
  EXISTING route, decision registered   principal holding nothing    -> 401 {"error":"unauthorized","status":"401"}
```

**The Hub allowed.** A route added without a decision was reachable by anybody,
and the only thing between an added handler and an anonymous caller was whoever
reviewed the diff. That is a routing defect, not a coverage defect, so the fix
is routing. The measurement is kept runnable as
`TestARouteWithNoRegisteredDecisionIsNotServed`, which drives the identical
route through a guarded and an unguarded handler so that removing the guard
makes the test report that both allow.

## How it fails closed now

`internal/hub/composition/authorization.go` holds `HubRouteAuthorization()`,
which names every mounted pattern and the decision it makes.
`guardUnregisteredRoutes` wraps the whole assembled Hub with a second
`http.ServeMux` carrying exactly the register's patterns. A request whose pattern
the register does not name is answered by that mux — net/http's own 404 for an
unknown path, its own 405 for a known path and an unregistered method, its own
redirect for a path needing cleaning.

Refusal is expressed as *not routed* rather than 403 on purpose. The register is
the definition of the Hub's surface; a pattern it does not name is not a route
this Hub has, and a caller cannot tell the guard apart from a Hub that never had
the route. An unregistered handler therefore cannot be found by probing either.

Three surfaces are covered, and they are mounted three different ways:

| Surface | Mounted by | Routes |
| --- | --- | --- |
| Control-plane REST | `internal/controlplane`, patterns registered on its own mux | 113 |
| Apollo `/api/v0` | `internal/apollo/openapi.gen.go`, generated from the pinned OpenAPI snapshot | 13 |
| Notifications | `internal/notifications/adapters/httpapi`, mounted from `internal/hub/composition` in front of the facade | 8 |
| Packaging runs | `internal/packagingworkflow/httpapi`, mounted from `internal/hub/composition` in front of the facade | 3 |

## The gate

`scripts/verify-route-authorization.mjs` reads the route registrations out of
the Go sources as text and reconciles them against the register in both
directions: a mounted pattern with no entry fails, and an entry naming no
mounted pattern fails. It runs offline, needs no toolchain, and carries a floor
on the number of routes found so that a regex which stopped matching reports a
failure rather than an empty agreement.

`scripts/verify-route-authorization.test.mjs` builds a minimal Hub in a
temporary directory, confirms the gate accepts it, then plants one violation per
rule — including a route mounted with no registered decision — and confirms each
is rejected. Both run in CI and in `scripts/verify-local.sh` under
**Validate route authorization**.

## The decision vocabulary

| Decision | What it requires | Routes |
| --- | --- | --- |
| `public` | nothing; the row must say why that is safe | 11 |
| `operator-any` | an authenticated operator, and nothing else — every operator gets the same answer | 5 |
| `operator-self` | an authenticated operator; the answer is about that operator only | 7 |
| `operator-filtered` | an authenticated operator, then a Governance decision on every row of the answer | 10 |
| `operator-grant` | an authenticated operator plus a Governance decision on a named resource | 95 |
| `agent` | an Agent identity scoped to the Environment or Plan in the path | 8 |
| `bundle-signature` | a Bundle envelope signed by a key this Hub trusts | 1 |

The register itself is the full 137-row table; what follows is every class
except `operator-grant`, which is the bulk and is uniform.

### `public` — served to anybody

| Route | Why that is safe |
| --- | --- |
| `GET /console/` | the console shell and its assets; every datum it shows is fetched from a route below |
| `GET /console` | a redirect to `/console/`; it reads nothing |
| `GET /health/live` | liveness for a load balancer; it reads no repository state |
| `GET /health/ready` | readiness; it answers ok or degraded and discloses no row |
| `GET /version` | the build stamp this binary already prints to its log |
| `GET /v1/apollo/graphql/schema` | the embedded SDL, identical for every caller |
| `GET /v1/bundles/signing-key` | the Hub's Ed25519 public key, which an air-gapped verifier needs |
| `POST /v1/validate` | validates the caller's own document and reads nothing |
| `POST /v1/apollo/product-manifests/validate` | validates the caller's own document and reads nothing |
| `POST /v1/apollo/environment-settings/validate` | validates the caller's own document and reads nothing |
| `/v1/notifications/{path...}` | the notification method-not-allowed responder; it resolves nobody and reads nothing |

### `operator-any` — any authenticated operator, same answer

These four are worth reading twice, because enumeration is what separated them
from `operator-self` and a reader of the handler would probably assume they were
scoped. They are not: any authenticated operator sees the whole authorization
topology.

| Route | Detail |
| --- | --- |
| `GET /v1/teams` | every team and its membership |
| `GET /v1/teams/{teamName}` | every team |
| `GET /v1/resource-grants` | every resource grant |
| `GET /v1/approval-policies` | every approval policy |

This is pre-existing behaviour and is left as it is here — narrowing it is a
product decision about who may see the governance graph, not a defect fix, and
it would break the console's grant editor. It is written down so the next
audit does not have to rediscover it.

`GET /v1/packaging-runs/{runID}` is also `operator-any`. It is a read of one
packaging run's progress, not of the governance graph. It returns no artifact
bytes and it has no write method, so it cannot accept an approval. Plan status
stays on `GET /v1/plans/{planID}`.

`GET /v1/packaging-runs` and `GET /v1/packaging-runs/{runID}/events` are not
in this class. A list enumerates runs, and the history is the same
information over time, so both are `operator-filtered`: the caller needs
`read` on the environment named by the target-plan reference. A row the
caller cannot read is omitted from the index. The history route answers
404 for that run, the same status it uses when the run does not exist.

### `operator-self` — the caller's own rows only

| Route | Detail |
| --- | --- |
| `GET /v1/session` | `projectSession`, which projects the caller's own standing |
| `GET /v1/notifications/inbox` | `ownedBy(principal, owner)` |
| `GET /v1/notifications/preferences` | `ownedBy(principal, owner)` |
| `PUT /v1/notifications/preferences` | `ownedBy(principal, owner)` |
| `GET /v1/notifications/contacts` | `ownedBy(principal, owner)` |
| `PUT /v1/notifications/contacts` | `ownedBy(principal, owner)` |
| `DELETE /v1/notifications/contacts` | `ownedBy(principal, owner)` |

### `operator-filtered` — every row authorized separately

An index that refused a principal holding nothing would be unusable; an index
that did not filter would be the cross-tenant defect. These answer 200 with an
empty collection.

| Route | Detail |
| --- | --- |
| `GET /v1/environments` | `governance.Authorize(read)` per Environment row |
| `GET /v1/products` | `governance.Authorize(read)` per Product row |
| `GET /v1/artifact-registries` | `governance.Authorize(read)` per Environment row |
| `GET /v1/fleet/installations` | `governance.Authorize(read)` per installation row |
| `GET /v1/plans` | `canReadPlan` per Plan row |
| `GET /v1/change-requests` | `canReadChangeRequest` per row |
| `GET /v1/change-requests/{changeRequestID}` | `canReadChangeRequest`, else 403 |
| `GET /v1/overview` | `BuildOverviewProjection`, which projects only what the principal reads |
| `GET /v1/packaging-runs` | `AuthorizeResourceOperation(read)` on the environment in the target-plan reference; other rows are omitted |
| `GET /v1/packaging-runs/{runID}/events` | the same environment read, else 404 |

### `agent` — an Agent identity, environment- or Plan-scoped

| Route | Detail |
| --- | --- |
| `POST /v1/environments/{environmentID}/leases` | `requireAgentIdentity` |
| `GET /v1/environments/{environmentID}/health-targets` | `requireAgentIdentity` |
| `POST /v1/environments/{environmentID}/health-observations` | `requireAgentIdentity` |
| `POST /v1/plans/{planID}/start` | `authorizeAgentForPlan` |
| `POST /v1/plans/{planID}/heartbeat` | `authorizeAgentForPlan` |
| `POST /v1/plans/{planID}/finish` | `authorizeAgentForPlan` |
| `POST /api/v0/status/environments/{environmentId}/entities/info` | `requireApolloIdentity` → `requireAgent`, environment-scoped |
| `POST /api/v0/status/environments/{environmentId}/entities/health` | `requireApolloIdentity` → `requireAgent`, environment-scoped |

### `bundle-signature` — the credential is the signature

| Route | Detail |
| --- | --- |
| `POST /v1/bundles/preview` | `artifacts.VerifyEnvelope` against the trusted Bundle keys |

This one is unusual and is called out because the enumeration is what made it
visible. The route reads Hub state — admitted bundle imports — and reports which
resource keys in the caller's envelope are added, unchanged or removed against
it. That is a state-disclosure oracle, and the only thing gating it is that the
envelope must carry a signature by a key this Hub trusts. That is a real
credential, so the route is not unauthenticated; it is also not an operator
decision, and a deployment that trusts a widely-held key should know it.

## The matrix, executed

The register would be a description if nothing drove it, so four tests in
`internal/hub/composition/authorization_test.go` range over it:

| Test | What it asserts |
| --- | --- |
| `TestARouteWithNoRegisteredDecisionIsNotServed` | the measurement above, guarded and unguarded |
| `TestEveryRegisteredRouteRefusesAnAnonymousCaller` | every non-public row answers **exactly 401** to a caller with no credential, and every public row is reachable |
| `TestEveryGrantedRouteRefusesAPrincipalHoldingNothing` | every `operator-grant` and `agent` row refuses an authenticated principal holding no team, grant or administrator standing, and the refusal is **earned** — 401 or 403, not an incidental 404 |
| `TestTheGuardRefusesAnUnregisteredMethodAndPath` | 405 stays 405 and 404 stays 404, with the security response headers intact |

Three of those assertions are narrower than they first look, and each was
narrowed by a mutation that survived the looser version:

- **Exactly 401, not "some 4xx".** Deleting `requireAgentIdentity` from
  `POST /v1/environments/{environmentID}/leases` leaves Lifecycle refusing a
  request with no agent id — a 403, which a "refused somehow" assertion accepts.
- **Earned, not incidental.** Deleting `requireResourceOperation` from
  `GET /v1/environments/{environmentID}/reported-state` leaves it answering 404
  because the record is absent, which is also not a 2xx. The fixture therefore
  creates the Environment, Product, Release and Plan the probe names, and the 23
  routes it still cannot reach are listed with the exact status they answer, so
  a route sliding from an earned 403 into an incidental 404 fails.
- **Public rows are driven, not skipped.** Reclassifying a granted route as
  `public` used to silence the test for it. A public row must now actually be
  reachable without a credential.

## Cross-tenant isolation

A decision class cannot settle whether an authorized caller gets somebody else's
rows: `operator-filtered` names a per-row check, and a per-row check written
wrong still satisfies the matrix while leaking the whole index.

`TestFilteredRoutesShowOneTenantNothingOfAnother` seeds two tenants through the
Hub's own routes, grants each principal read and operate on exactly one, and
drives every filtered index and every by-name read as both, plus the write half
(a Plan approval across tenants). It passed on the first run — cross-tenant
isolation was already correct, it had simply never been asserted for
Environments, Products, Releases, Plans, change requests or the fleet.

What existed before, measured rather than assumed:

- `plan_approval_authorization_test.go` and `plan_cancel_authorization_test.go`
  already assert cross-Environment refusal for Plans, so the notification inbox
  was not the only cross-principal refusal test.
- `plans_index_test.go` asserts a principal with *no* grants sees an empty Plan
  index — weaker, since a filter returning everything to anyone holding any
  grant would pass it.
- Nothing anywhere drove an index route as one tenant while another tenant's
  rows existed.

## Adding a route

1. Mount it wherever it belongs. `internal/hub/composition` for new surfaces —
   `internal/controlplane` is on a ratcheted line ceiling.
2. Add a row to `HubRouteAuthorization()` naming the decision it makes. Until
   you do, the Hub will not route it and the build will fail with its name.
3. If the decision is `public`, the `Detail` has to say why that is safe. The
   gate rejects a public row whose detail is too short to be a reason.
