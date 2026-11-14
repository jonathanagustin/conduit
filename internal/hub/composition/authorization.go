package composition

import (
	"net/http"
)

// The Hub's route x decision matrix, and the routing that fails closed when a
// route is missing from it.
//
// PRODUCTION-READINESS.md filed the missing enumeration under "Test types", as
// though what was absent were coverage. It was not. The unasked question was
// what the Hub does with a route nobody registered a decision for, and the
// answer was measured before this file was written, by mounting one and driving
// it (TestARouteWithNoRegisteredDecisionIsNotServed keeps the measurement
// runnable). Under production HandlerOptions -- no insecure headers, no OIDC
// resolver, no client certificate -- an unauthenticated caller received:
//
//	GET /v1/probe/secrets      -> 200 {"tenant-b-secret":"the crown jewels"}
//	GET /v1/resource-grants    -> 401 {"error":"unauthorized"}
//
// The Hub allowed. A new route defaulted to reachable by anybody, and the only
// thing standing between an added handler and the public internet was whoever
// reviewed it. That is a routing defect, not a coverage defect, so the fix here
// is routing: HubRouteAuthorization is the register, and a request whose
// pattern the register does not name is not served at all.
//
// Why refusal is expressed as "not routed" rather than 403: the register is the
// definition of the Hub's surface. A pattern it does not name is not a route
// this Hub has, and answering 404 says exactly that -- indistinguishable from
// any other absent path, so an unregistered handler cannot be found by probing
// either. The 404 and 405 bodies are the ones net/http itself writes, because
// the guard is a second ServeMux carrying the register's patterns and nothing
// else; a caller cannot tell the guard apart from a Hub that never had the
// route, which is the point.
//
// This lives in internal/hub/composition because that is where Hub wiring
// belongs (internal/AGENTS.md) and because internal/controlplane is on a
// ratcheted line ceiling. It also has to live outside the facade to work: the
// facade's mux is unexported, so nothing inside it can be asked which pattern
// matched, and a guard that has to be remembered at each registration site is
// the convention this file exists to replace.
//
// Keeping the register true to the mounted surface is not this file's job and
// must not be, because a register that derived itself from the routes would
// agree with them by construction and prove nothing.
// scripts/verify-route-authorization.mjs reads the route registrations out of
// the Go sources as text and reconciles them against the table below in both
// directions, and fails the build when either side has an entry the other does
// not. That gate is the one that makes a new route stop the build rather than
// quietly 404.

// Decision names what a route requires before it answers. The vocabulary is
// deliberately small: these are the distinctions a reader has to hold to say
// whether a row is right, and a longer list would be a restatement of the
// handler rather than a claim about it.
type Decision string

const (
	// DecisionPublic is served to anybody, and the row must say why it is safe.
	// Every current member either reads nothing at all or reads something the
	// Hub publishes on purpose.
	DecisionPublic Decision = "public"
	// DecisionOperatorSelf requires an authenticated operator and answers only
	// about that operator -- their standing, their inbox, their addresses. No
	// grant is consulted because none would widen the answer.
	DecisionOperatorSelf Decision = "operator-self"
	// DecisionOperatorAny requires an authenticated operator and nothing else:
	// every operator gets the same answer. It is a class of its own rather than
	// a footnote on operator-self because the two are easy to confuse and only
	// one of them is bounded by who is asking. Enumerating the surface is what
	// separated them -- four routes turned out to be here rather than where a
	// reader would assume, and they are named in
	// docs/operations/route-authorization.md rather than left to be rediscovered.
	DecisionOperatorAny Decision = "operator-any"
	// DecisionOperatorFiltered requires an authenticated operator and then
	// authorizes every row of the answer separately, so a principal holding
	// nothing receives an empty collection rather than a refusal. An index that
	// refused would be unusable; an index that did not filter would be the
	// cross-tenant defect. TestFilteredRoutesShowOneTenantNothingOfAnother
	// drives the distinction with two tenants of real data.
	DecisionOperatorFiltered Decision = "operator-filtered"
	// DecisionOperatorGrant requires an authenticated operator and a Governance
	// decision on a named resource: a resource grant, a platform operation, a
	// Governance administrator, or a team administrator.
	DecisionOperatorGrant Decision = "operator-grant"
	// DecisionAgent requires an Agent identity scoped to the Environment or Plan
	// in the path. An operator credential does not satisfy it and vice versa.
	DecisionAgent Decision = "agent"
	// DecisionBundleSignature requires the request body to carry a Bundle
	// envelope signed by a key this Hub trusts. The credential is the signature
	// rather than the caller.
	DecisionBundleSignature Decision = "bundle-signature"
)

// RouteAuthorization is one cell of the matrix: a mounted pattern and the
// decision it makes.
//
// Detail names the check in the source, so that a reader can go and look rather
// than take the Decision on trust. It is prose on purpose -- it is read by
// people and by the gate's completeness check, never by the guard.
type RouteAuthorization struct {
	Pattern  string
	Decision Decision
	Detail   string
}

// HubRouteAuthorization is every route this Hub mounts, with the decision each
// one makes.
//
// Three surfaces are enumerated here and they are mounted by three different
// mechanisms, which is exactly why one table has to cover all of them: the
// internal/controlplane REST facade registers patterns directly, the Apollo
// /api/v0 routes are registered by generated code from the pinned OpenAPI
// snapshot, and the notification routes are mounted from this package in front
// of the facade. A gate that only knew about the first would have said nothing
// about the newest and least-covered surface.
//
// Sorted by pattern so that a diff adding a route is a one-line diff.
func HubRouteAuthorization() []RouteAuthorization {
	return []RouteAuthorization{
		{Pattern: "/v1/notifications/{path...}",
			Decision: DecisionPublic, Detail: "the notification method-not-allowed responder; it resolves nobody and reads nothing"},
		{Pattern: "DELETE /v1/debug-session-requests/{requestID}",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + current native Environment/namespace read or manage permission + owner check"},
		{Pattern: "DELETE /v1/export-pipelines/{pipelineID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "DELETE /v1/labels/{labelID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "DELETE /v1/notifications/contacts",
			Decision: DecisionOperatorSelf, Detail: "operatorPrincipal + ownedBy(principal, owner)"},
		{Pattern: "DELETE /v1/suppressions/{suppressionID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /api/v0/environments",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, platform/root)"},
		{Pattern: "GET /api/v0/environments/{environmentRid}",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, platform/root)"},
		{Pattern: "GET /api/v0/environments/{environmentRid}/installations",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, platform/root)"},
		{Pattern: "GET /api/v0/environments/{environmentRid}/installations/{installationRid}",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, platform/root)"},
		{Pattern: "GET /api/v0/namespaces",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, platform/root)"},
		{Pattern: "GET /api/v0/products",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, global/product)"},
		{Pattern: "GET /api/v0/products/{productId}",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, product/<id>)"},
		{Pattern: "GET /api/v0/products/{productId}/releases",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, product/<id>)"},
		{Pattern: "GET /api/v0/products/{productId}/releases/{productVersion}",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizeOperator(read, product/<id>)"},
		{Pattern: "GET /api/v2/ontologies",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; each environment row requires resource:read"},
		{Pattern: "GET /api/v2/ontologies/{ontology}",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; the catalog lists no environment the caller cannot read"},
		{Pattern: "GET /api/v2/ontologies/{ontology}/actionTypes",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; action types carry no environment row"},
		{Pattern: "GET /api/v2/ontologies/{ontology}/actionTypes/{actionType}",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; one action type carries no environment row"},
		{Pattern: "GET /api/v2/ontologies/{ontology}/fullMetadata",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; object and link types carry no environment row"},
		{Pattern: "GET /api/v2/ontologies/{ontology}/interfaceTypes",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; interface types are empty"},
		{Pattern: "GET /api/v2/ontologies/{ontology}/interfaceTypes/{interfaceType}",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; an unknown interface type is not found"},
		{Pattern: "GET /api/v2/ontologies/{ontology}/objectTypes",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; object types carry no environment row"},
		{Pattern: "GET /api/v2/ontologies/{ontology}/objectTypes/{objectType}",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; one object type carries no environment row"},
		{Pattern: "GET /api/v2/ontologies/{ontology}/objectTypes/{objectType}/fullMetadata",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; one object type and its links carry no environment row"},
		{Pattern: "GET /console",
			Decision: DecisionPublic, Detail: "a redirect to /console/; it reads nothing"},
		{Pattern: "GET /console/",
			Decision: DecisionPublic, Detail: "the console shell and its assets; every datum it shows is fetched from a route below"},
		{Pattern: "GET /health/live",
			Decision: DecisionPublic, Detail: "liveness for a load balancer; it reads no repository state"},
		{Pattern: "GET /health/ready",
			Decision: DecisionPublic, Detail: "readiness; it answers ok or degraded and discloses no row"},
		{Pattern: "GET /v1/apollo/graphql/schema",
			Decision: DecisionPublic, Detail: "the embedded SDL, identical for every caller"},
		{Pattern: "GET /v1/approval-policies",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; every approval policy is shown to every operator"},
		{Pattern: "GET /v1/artifact-registries",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + governance.Authorize(read) per Environment row"},
		{Pattern: "GET /v1/artifacts/blobs/{digest}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/authorization-audit",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireGovernanceAdministrator"},
		{Pattern: "GET /v1/bundle-imports",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/bundles",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation; bundle id, pipeline, and manifest digest only"},
		{Pattern: "GET /v1/bundles/signing-key",
			Decision: DecisionPublic, Detail: "the Hub's Ed25519 public key, which an air-gapped verifier needs"},
		{Pattern: "GET /v1/bundles/{bundleID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/channel-subscriptions",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; each environment row requires resource read"},
		{Pattern: "GET /v1/catalog-retirements",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation + requirePlatformOperation"},
		{Pattern: "GET /v1/change-requests",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + canReadChangeRequest per row"},
		{Pattern: "GET /v1/change-requests/{changeRequestID}",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + canReadChangeRequest, else 403"},
		{Pattern: "GET /v1/change-requests/{requestID}/comments",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + governance.GetReadableChangeRequest on the original carrier"},
		{Pattern: "GET /v1/debug-session-requests",
			Decision: DecisionOperatorFiltered, Detail: "operatorFrom + current native Environment/namespace permission + owner-filtered metadata page"},
		{Pattern: "GET /v1/debug-session-requests/{requestID}",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + current native Environment/namespace permission + owner or explicit manager"},
		{Pattern: "GET /v1/debug-session-requests/{requestID}/history",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + current native Environment/namespace permission + owner or explicit manager"},
		{Pattern: "GET /v1/environments",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + governance.Authorize(read) per Environment row"},
		{Pattern: "GET /v1/environments/{environmentID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/environments/{environmentID}/commands",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/environments/{environmentID}/entities",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/environments/{environmentID}/health-targets",
			Decision: DecisionAgent, Detail: "requireAgentIdentity"},
		{Pattern: "GET /v1/environments/{environmentID}/module-installations",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/environments/{environmentID}/profile",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/environments/{environmentID}/profile/revisions",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/environments/{environmentID}/reported-state",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/environments/{environmentID}/suppressions",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/environments/{environmentID}/target-hub-versions",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation + requirePlatformOperation"},
		{Pattern: "GET /v1/export-pipelines",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/export-pipelines/{pipelineID}/schedule-runs",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/fleet/installations",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + governance.Authorize(read) per installation row"},
		{Pattern: "GET /v1/labels",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/labels/{labelID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/labels/{labelID}/revisions",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/module-installations/{installationID}/update",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireModuleInstallationOperation"},
		{Pattern: "GET /v1/modules",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/modules/{moduleName}/releases",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/modules/{module}/auto-update-policy",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/notifications/contacts",
			Decision: DecisionOperatorSelf, Detail: "operatorPrincipal + ownedBy(principal, owner)"},
		{Pattern: "GET /v1/notifications/events",
			Decision: DecisionOperatorGrant, Detail: "operatorPrincipal + resourceOperation(resourceRid, read)"},
		{Pattern: "GET /v1/notifications/inbox",
			Decision: DecisionOperatorSelf, Detail: "operatorPrincipal + ownedBy(principal, owner)"},
		{Pattern: "GET /v1/notifications/preferences",
			Decision: DecisionOperatorSelf, Detail: "operatorPrincipal + ownedBy(principal, owner)"},
		{Pattern: "GET /v1/operator-identity",
			Decision: DecisionPublic, Detail: "whether operator sign-in is configured, and the issuer address when it is; no secret"},
		{Pattern: "GET /v1/overview",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + BuildOverviewProjection, which projects only what the principal reads"},
		{Pattern: "GET /v1/packaging-runs",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + AuthorizeResourceOperation(read) on the environment in the target-plan reference; omitted when the caller cannot read that environment"},
		{Pattern: "GET /v1/packaging-runs/{runID}",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; read-only packaging-run progress for every authenticated operator, with no artifact bytes and no approval"},
		{Pattern: "GET /v1/packaging-runs/{runID}/events",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + AuthorizeResourceOperation(read) on the run target environment, else 404; read-only step history with no approval"},
		{Pattern: "GET /v1/packaging-runs/{runID}/feedback",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; review notes on a packaging run, with no approval"},
		{Pattern: "GET /v1/plans",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + canReadPlan per Plan row"},
		{Pattern: "GET /v1/plans/{planID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/products",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal + governance.Authorize(read) per Product row"},
		{Pattern: "GET /v1/products/{productID}/download-releases",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + assetAuthority.RequireRead on the original carrier"},
		{Pattern: "GET /v1/products/{productID}/releases/{version}/download-assets",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + assetAuthority.RequireRead on the original carrier"},
		{Pattern: "GET /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/products/{product}/publications",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/products/{product}/releases",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/products/{product}/releases/{version}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "GET /v1/promotions",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/promotions/{promotionID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "GET /v1/resource-grants",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; every resource grant is shown to every operator"},
		{Pattern: "GET /v1/session",
			Decision: DecisionOperatorSelf, Detail: "requireOperatorPrincipal + projectSession, which projects the caller's own standing"},
		{Pattern: "GET /v1/spaces",
			Decision: DecisionOperatorFiltered, Detail: "Space authority: administrator or a readable anchor; no visibility gained from selection"},
		{Pattern: "GET /v1/spaces/{spaceID}",
			Decision: DecisionOperatorGrant, Detail: "Space authority: administrator or a readable anchor"},
		{Pattern: "GET /v1/spaces/{spaceID}/bindings",
			Decision: DecisionOperatorFiltered, Detail: "Space authority and per-anchor canonical resource:read"},
		{Pattern: "GET /v1/teams",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; every team and its membership is shown to every operator"},
		{Pattern: "GET /v1/teams/{teamName}",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; every team is shown to every operator"},
		{Pattern: "GET /version",
			Decision: DecisionPublic, Detail: "the build stamp this binary already prints to its log"},
		{Pattern: "HEAD /v1/artifacts/blobs/{digest}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /api/v0/products/helmChart/{mavenCoordinate}/publish",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizePublication (artifact, product, release)"},
		{Pattern: "POST /api/v0/products/service/{mavenCoordinate}/publish",
			Decision: DecisionOperatorGrant, Detail: "requireApolloIdentity + authorizePublication (artifact, product, release)"},
		{Pattern: "POST /api/v0/status/environments/{environmentId}/entities/health",
			Decision: DecisionAgent, Detail: "requireApolloIdentity -> requireAgent, environment-scoped"},
		{Pattern: "POST /api/v0/status/environments/{environmentId}/entities/info",
			Decision: DecisionAgent, Detail: "requireApolloIdentity -> requireAgent, environment-scoped"},
		{Pattern: "POST /api/v2/ontologies/{ontology}/actions/{action}/apply",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; propose permission on the named environment, then the existing policy evaluation, which does not approve the request"},
		{Pattern: "POST /api/v2/ontologies/{ontology}/objectSets/loadObjects",
			Decision: DecisionOperatorFiltered, Detail: "requireOperatorPrincipal; each object requires resource:read on its environment"},
		{Pattern: "POST /conduit.packaging.v1.PackagingRunService/GetPackagingRun",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; Connect read of packaging-run progress for every authenticated operator, same decision as GET /v1/packaging-runs/{runID}"},
		{Pattern: "POST /v1/adapted-plans/{adapter}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/apollo/environment-settings/validate",
			Decision: DecisionPublic, Detail: "validates the caller's own document and reads nothing"},
		{Pattern: "POST /v1/apollo/graphql",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation per bound source"},
		{Pattern: "POST /v1/apollo/product-manifests/validate",
			Decision: DecisionPublic, Detail: "validates the caller's own document and reads nothing"},
		{Pattern: "POST /v1/artifact-scans",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/artifacts/blobs",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/bundle-transfer-confirmations/{confirmationID}/rescind",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/bundles/import",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/bundles/preview",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation; Preview still verifies the envelope before reporting the signature as verified"},
		{Pattern: "POST /v1/bundles/verification",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation; reports verified, unsigned, or failed from the envelope check and omits contents unless the signature verified"},
		{Pattern: "POST /v1/bundles/{bundleID}/transfer-confirmations",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/change-requests/{changeRequestID}/approve",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + governance.Approve, which admits only an eligible approver"},
		{Pattern: "POST /v1/change-requests/{changeRequestID}/cancel",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + governance.Cancel, which admits only the author or an approver"},
		{Pattern: "POST /v1/change-requests/{changeRequestID}/reject",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + governance.Reject, which admits only an eligible approver"},
		{Pattern: "POST /v1/change-requests/{changeRequestID}/reopen",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + governance.Reopen, which admits only the author or an approver"},
		{Pattern: "POST /v1/change-requests/{requestID}/comments",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + governance.RequireChangeRequestAnnotation; discussion never approves"},
		{Pattern: "POST /v1/change-requests/{requestID}/label-mutations",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + governance.MutateChangeRequestLabel with author/reviewer access and serialized Plan projection"},
		{Pattern: "POST /v1/temporary-maintenance-overrides",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + Environment propose grant for every Entity, normal governance policy"},
		{Pattern: "POST /v1/debug-session-requests",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + explicit native create permission at Environment and namespace scopes + declared namespace"},
		{Pattern: "POST /v1/debug-session-requests/{requestID}/revoke",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + current native Environment/namespace permission + owner or explicit manager"},
		{Pattern: "POST /v1/environments/{environmentID}/maintenance-admission/leases",
			Decision: DecisionAgent, Detail: "AgentIdentityFromRequest + Environment scope and serialized approved temporary maintenance projection"},
		{Pattern: "POST /v1/plans/{planID}/maintenance-admission/start",
			Decision: DecisionAgent, Detail: "AgentIdentityFromRequest + Environment and Agent lease ownership, opaque token and fresh maintenance check"},
		{Pattern: "POST /v1/module-override-proposals",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + ModuleOverrideService environment operation check; explicit independent review"},
		{Pattern: "POST /v1/module-override-proposals/{requestID}/apply",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + current environment operation, readable review and serialized exact-scope permit"},
		{Pattern: "POST /v1/commands",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/environments/{environmentID}/health-observations",
			Decision: DecisionAgent, Detail: "requireAgentIdentity"},
		{Pattern: "POST /v1/environments/{environmentID}/leases",
			Decision: DecisionAgent, Detail: "requireAgentIdentity"},
		{Pattern: "POST /v1/environments/{environmentID}/module-installations",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/environments/{environmentID}/suppressions",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/export-pipelines/{pipelineID}/archive",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/export-pipelines/{pipelineID}/build",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/export-pipelines/{pipelineID}/preview",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/export-pipelines/{pipelineID}/unarchive",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/sites/check-in",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; records a site check-in or queues a change"},
		{Pattern: "GET /v1/blackouts",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; returns the blackout reason when a change is blocked"},
		{Pattern: "POST /v1/blackouts",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; declare confirms, clear opens a change request"},
		{Pattern: "GET /v1/rollouts",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; reads staged rollouts"},
		{Pattern: "POST /v1/rollouts",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; pause confirms, start resume and roll back open a change request"},
		{Pattern: "POST /v1/fleet/propose-fix",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; opens a change request for one drifted installation and does not apply it"},
		{Pattern: "POST /v1/fleet/actions",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireBulkTargetOperation"},
		{Pattern: "POST /v1/module-installations/{installationID}/pause",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireModuleInstallationOperation"},
		{Pattern: "POST /v1/module-installations/{installationID}/resume",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireModuleInstallationOperation"},
		{Pattern: "POST /v1/module-installations/{installationID}/unlink",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireModuleInstallationOperation"},
		{Pattern: "POST /v1/module-installations/{installationID}/update",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireModuleInstallationOperation"},
		{Pattern: "POST /v1/module-installations/{installationID}/update/automatic",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireModuleInstallationOperation"},
		{Pattern: "POST /v1/modules/lock",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/modules/releases",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/modules/{moduleName}/releases/{version}/promote",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/modules/{moduleName}/releases/{version}/recall",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/packaging-runs/{runID}/cancel",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + AuthorizeResourceOperation(operate) on the environment in the target-plan reference; audited; does not cancel a Plan"},
		{Pattern: "POST /v1/packaging-runs/{runID}/feedback",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; records review input on one file, manifest field, or step and does not approve"},
		{Pattern: "POST /v1/packaging-runs/{runID}/model-proposal",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; asks the configured local model CLI for one proposal, then the packaging gate, and does not approve"},
		{Pattern: "POST /v1/assistant/tools",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; runs one ontology read, governed-action proposal, or change-request diff and does not apply the action"},
		{Pattern: "POST /v1/assistant/turns",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; asks the configured local model CLI for tool calls, validates them, and does not apply an action"},
		{Pattern: "GET /v1/intelligence/logic",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; reads the proposal pipeline configuration and does not call a model"},
		{Pattern: "GET /v1/intelligence/evals",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; lists eval cases"},
		{Pattern: "POST /v1/intelligence/evals/run",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; scores eval cases with the packaging gate and does not apply a change"},
		{Pattern: "POST /v1/intelligence/evals/cases",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; records an operator feedback case"},
		{Pattern: "POST /v1/intelligence/courses",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; scores recorded memory options with the packaging gate"},
		{Pattern: "POST /v1/intelligence/courses/select",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; opens a change request that still needs a person and does not apply it"},
		{Pattern: "GET /v1/intelligence/events",
			Decision: DecisionOperatorAny, Detail: "requireOperatorPrincipal; lists observed packaging runs beside automation rules that are not executed"},
		{Pattern: "POST /v1/packaging-runs/{runID}/rerun",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + AuthorizeResourceOperation(operate) on the environment in the target-plan reference; audited; starts a new run id and does not approve"},
		{Pattern: "POST /v1/plans",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/plans/{planID}/approvals",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/plans/{planID}/cancel",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + the Change Request's author, else requireResourceOperation; and Operate on the Environment whenever the cancellation asks for a suppression, which is the bar POST /v1/environments/{environmentID}/suppressions sets for the same effect (#150)"},
		{Pattern: "POST /v1/plans/{planID}/finish",
			Decision: DecisionAgent, Detail: "authorizeAgentForPlan"},
		{Pattern: "POST /v1/plans/{planID}/heartbeat",
			Decision: DecisionAgent, Detail: "authorizeAgentForPlan"},
		{Pattern: "POST /v1/plans/{planID}/start",
			Decision: DecisionAgent, Detail: "authorizeAgentForPlan"},
		{Pattern: "POST /v1/plans/{planID}/undo-unmanage",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/product-publications/download-assets",
			Decision: DecisionOperatorGrant, Detail: "operatorFrom + assetAuthority.RequirePublication + explicit Space selection authority"},
		{Pattern: "POST /v1/product-publications/helm-chart",
			Decision: DecisionOperatorGrant, Detail: "resolveOperator + authorizePublication"},
		{Pattern: "POST /v1/product-publications/terraform",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}/pause",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/products/{product}/promotion-stages/{fromChannel}/{toChannel}/resume",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/products/{product}/releases/{version}/retire",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/products/{product}/retire",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/promotions/evaluations",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "POST /v1/resource-grants",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "POST /v1/spaces",
			Decision: DecisionOperatorGrant, Detail: "Space authority: canonical platform/root permissions:manage, original carrier retained"},
		{Pattern: "POST /v1/validate",
			Decision: DecisionPublic, Detail: "validates the caller's own document and reads nothing"},
		{Pattern: "PUT /v1/approval-policies/{policyID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireGovernanceAdministrator"},
		{Pattern: "PUT /v1/artifacts/blobs/{digest}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "PUT /v1/change-requests/{changeRequestID}/labels",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + governance.SetLabels, which admits only an eligible approver"},
		{Pattern: "PUT /v1/environments/{environmentID}/profile",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "PUT /v1/export-pipelines/{pipelineID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "PUT /v1/export-pipelines/{pipelineID}/target-hub-versions/{targetHub}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requirePlatformOperation"},
		{Pattern: "PUT /v1/labels/{labelID}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "PUT /v1/modules/{module}/auto-update-policy",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireResourceOperation"},
		{Pattern: "PUT /v1/notifications/contacts",
			Decision: DecisionOperatorSelf, Detail: "operatorPrincipal + ownedBy(principal, owner)"},
		{Pattern: "PUT /v1/notifications/preferences",
			Decision: DecisionOperatorSelf, Detail: "operatorPrincipal + ownedBy(principal, owner)"},
		{Pattern: "PUT /v1/spaces/{spaceID}/bindings",
			Decision: DecisionOperatorGrant, Detail: "Space authority: platform/root permissions:manage and resource:read on the exact anchor"},
		{Pattern: "PUT /v1/teams/{teamName}",
			Decision: DecisionOperatorGrant, Detail: "requireOperatorPrincipal + requireTeamAdministrator"},
	}
}

// guardUnregisteredRoutes is the fail-closed half.
//
// It builds a second ServeMux carrying the register's patterns and asks it,
// per request, whether this one is a route the register names. A match is
// handed to the Hub, which then applies the route's own decision. A miss is
// answered by that same mux, so the caller receives net/http's own 404, its own
// 405 for a known path and an unregistered method, and its own redirect for a
// path needing cleaning -- the responses a Hub without the route would give.
//
// The security response headers are restated on the refusal path because the
// facade sets them inside itself, and a request refused here never reaches it.
// A 404 that lost nosniff would be a small regression introduced by a security
// fix, which is the kind of thing that goes unnoticed for a year.
func guardUnregisteredRoutes(hub http.Handler) http.Handler {
	registered := http.NewServeMux()
	for _, route := range HubRouteAuthorization() {
		registered.Handle(route.Pattern, hub)
	}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		refusal, pattern := registered.Handler(request)
		if pattern != "" {
			hub.ServeHTTP(writer, request)
			return
		}
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'; sandbox")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		if request.TLS != nil {
			writer.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		refusal.ServeHTTP(writer, request)
	})
}
