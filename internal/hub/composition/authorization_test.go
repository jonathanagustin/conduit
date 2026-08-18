package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// pathFor turns a register pattern into a concrete request target.
//
// The values are deliberately nonsense. Every decision under test is taken
// before the named record is fetched, or the record does not exist and the
// route answers 404 -- and a 404 is also not a disclosure. What must never
// happen is a 2xx, which is the assertion each test below makes.
var wildcard = regexp.MustCompile(`\{[^}]*\}`)

func pathFor(pattern string) (string, string) {
	method, path, found := strings.Cut(pattern, " ")
	if !found {
		// The notification catch-all is registered without a method.
		method, path = http.MethodGet, pattern
	}
	return method, wildcard.ReplaceAllStringFunc(path, func(name string) string {
		if value, known := realIdentifiers[strings.Trim(name, "{}")]; known {
			return value
		}
		return "probe"
	})
}

// realIdentifiers are the path values that name something the fixture actually
// created.
//
// They exist because "probe" is not good enough for the second row of the
// matrix. A route whose resource check was deleted still answers 404 when the
// Environment in its path does not exist, so a probe built entirely from
// nonsense would report a refusal it did not earn -- and that is not
// hypothetical: deleting requireResourceOperation from the reported-state route
// leaves it answering 404 to a principal holding nothing, and this test passed
// on that mutant until these values were supplied.
//
// Anything not named here stays "probe" and is still asserted, on the weaker
// but still true statement that a principal holding nothing gets no 2xx.
var realIdentifiers = map[string]string{
	"environmentID":  fixtureEnvironment,
	"environmentId":  fixtureEnvironment,
	"environmentRid": fixtureEnvironment,
	"product":        fixtureProduct,
	"productId":      fixtureProduct,
	"version":        fixtureVersion,
	"productVersion": fixtureVersion,
	"teamName":       fixtureTeam,
}

const (
	fixtureEnvironment = "tenant-a-use2"
	fixtureProduct     = "a.example:ledger"
	fixtureVersion     = "0.1.0"
	fixtureTeam        = "tenant-a"
)

// bodyFor is a request body the route will accept far enough to reach its
// authorization decision.
//
// "{}" does for everything except GraphQL, which reports an empty document as a
// bad request before it asks who is calling. That ordering is not a hole -- the
// refusal discloses nothing -- but a probe that accepted the 400 as a refusal
// would also accept it after the authorization was removed, so the GraphQL row
// is driven with a document that parses.
func bodyFor(pattern string) string {
	if strings.HasSuffix(pattern, "/v1/apollo/graphql") {
		return `{"query":"{ __typename }"}`
	}
	return "{}"
}

// TestARouteWithNoRegisteredDecisionIsNotServed is the measurement that made
// this file necessary, kept runnable.
//
// The question PRODUCTION-READINESS.md filed under "Test types" is a routing
// question: when a route is added and nobody registers an authorization
// decision for it, does the Hub deny or allow? The first half of this test
// answers it for the Hub as it was -- a handler composed the way the facade
// composes one, with no guard -- and the answer is allow, to a caller holding
// no credential at all. The second half drives the identical route through
// NewHub and shows it is not served.
//
// The unguarded half is not a straw man: it is exactly what
// mountNotifications returns, which is what NewHub returned before the guard.
// Keeping it here means the defect cannot come back silently, because the day
// the guard is removed this test starts reporting that both halves allow.
func TestARouteWithNoRegisteredDecisionIsNotServed(t *testing.T) {
	t.Parallel()
	const probe = "GET /v1/probe/secrets"
	const disclosure = `{"tenant-b-secret":"the crown jewels"}`
	newSurface := func() *http.ServeMux {
		mux := http.NewServeMux()
		mux.HandleFunc(probe, func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte(disclosure))
		})
		return mux
	}
	// Production HandlerOptions: no insecure identity headers, no OIDC
	// resolver, no client certificate. Every authenticated route refuses.
	facade := controlplane.NewHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{})

	unguarded := newSurface()
	unguarded.Handle("/", facade)
	guarded := newSurface()
	guarded.Handle("/", facade)

	for _, probed := range []struct {
		name    string
		handler http.Handler
		want    int
	}{
		{"unguarded: the Hub before this file", unguarded, http.StatusOK},
		{"guarded: the Hub with the register", guardUnregisteredRoutes(guarded), http.StatusNotFound},
	} {
		t.Run(probed.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			probed.handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/probe/secrets", nil))
			if response.Code != probed.want {
				t.Fatalf("an unauthenticated caller got %d, want %d", response.Code, probed.want)
			}
			if probed.want != http.StatusOK && strings.Contains(response.Body.String(), "crown jewels") {
				t.Fatalf("the refused response carried the handler's body: %s", response.Body.String())
			}
			// The route beside it, which does register a decision, must refuse
			// the same caller. Without this the test would also pass on a Hub
			// that had stopped serving anything at all.
			beside := httptest.NewRecorder()
			probed.handler.ServeHTTP(beside, httptest.NewRequest(http.MethodGet, "/v1/resource-grants", nil))
			if beside.Code != http.StatusUnauthorized {
				t.Fatalf("GET /v1/resource-grants answered %d, want 401", beside.Code)
			}
		})
	}
}

// TestTheGuardRefusesAnUnregisteredMethodAndPath states the two refusals that
// are easy to get wrong once "not in the register" stops meaning "404".
//
// A registered path reached by an unregistered method must still be 405 rather
// than 404, because turning one into the other would break every client that
// distinguishes them, and the guard sees a method mismatch as "no pattern
// matched" exactly as it sees an unknown path.
func TestTheGuardRefusesAnUnregisteredMethodAndPath(t *testing.T) {
	t.Parallel()
	handler := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
	})
	for _, probed := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/health/live", http.StatusOK},
		{http.MethodPost, "/health/live", http.StatusMethodNotAllowed},
		{http.MethodGet, "/v1/nothing-here", http.StatusNotFound},
		{http.MethodDelete, "/v1/plans/probe", http.StatusMethodNotAllowed},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(probed.method, probed.path, nil))
		if response.Code != probed.want {
			t.Errorf("%s %s answered %d, want %d", probed.method, probed.path, response.Code, probed.want)
		}
		if response.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s %s lost the security response headers", probed.method, probed.path)
		}
	}
}

// TestEveryRegisteredRouteRefusesAnAnonymousCaller is the route x role matrix,
// executed rather than described.
//
// It drives every row of HubRouteAuthorization with no credential at all,
// against a Hub configured the way production configures one, and requires
// every row that is not DecisionPublic to refuse. This is the assertion the
// document said nothing enumerates; it now ranges over the register, so a route
// added to the register without an authorization check fails here, and a route
// added without a register entry fails the gate and the guard.
func TestEveryRegisteredRouteRefusesAnAnonymousCaller(t *testing.T) {
	t.Parallel()
	store := newNotificationStore()
	handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{},
		Options{NotificationStore: store, NotificationContacts: &contactBook{rows: map[string]domain.Contact{}}})
	if err != nil {
		t.Fatal(err)
	}
	public := 0
	for _, route := range HubRouteAuthorization() {
		method, path := pathFor(route.Pattern)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, strings.NewReader(bodyFor(route.Pattern))))
		if route.Decision == DecisionPublic {
			public++
			continue
		}
		if response.Code >= 200 && response.Code < 300 {
			t.Errorf("%s answers %d to a caller with no credential; its decision is %s (%s)",
				route.Pattern, response.Code, route.Decision, route.Detail)
		}
		if route.Decision == DecisionBundleSignature {
			// The credential is the signature rather than the caller, so the
			// refusal is a rejected envelope and not a 401. Refusing is still
			// the assertion; only its vocabulary differs.
			continue
		}
		// Refused as 401 exactly, not merely refused. Every non-public route
		// currently answers 401 to a caller with no credential, which is worth
		// asserting rather than widening to "some 4xx": a route that reached
		// past its identity check and refused for a downstream reason -- an
		// empty agent id, a body it could not parse -- would still be a 4xx, and
		// deleting requireAgentIdentity from the lease route produces exactly
		// that 403. A caller also learns from 401 that it should authenticate,
		// which no other status tells it.
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s refuses an anonymous caller with %d, want 401 (%s)",
				route.Pattern, response.Code, route.Detail)
		}
	}
	if public == 0 {
		t.Fatal("no public rows: this test would pass on a register that had lost its Decision field")
	}
}

// TestEveryGrantedRouteRefusesAPrincipalHoldingNothing is the second row of the
// matrix: authenticated, and holding no team, grant or administrator standing.
//
// DecisionOperatorSelf rows are excluded by construction rather than by
// exception -- they answer about the caller, so a caller holding nothing gets
// an answer about nothing, and demanding a refusal from them would be demanding
// the wrong behaviour. What they must not do is answer about somebody else,
// which is TestOperatorSelfRoutesDiscloseNoOtherPrincipal.
func TestEveryGrantedRouteRefusesAPrincipalHoldingNothing(t *testing.T) {
	t.Parallel()
	repository := controlplane.NewMemoryRepository()
	store := newNotificationStore()
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		// Named, so the "local-developer" bootstrap shortcut is off and every
		// decision below has to come from a grant this principal does not hold.
		BootstrapAdministrators: []string{"root"},
	}, Options{NotificationStore: store, NotificationContacts: &contactBook{rows: map[string]domain.Contact{}}})
	if err != nil {
		t.Fatal(err)
	}
	// A real Environment, Product, Release and Plan, and a team the probing
	// principal is not in. Without them the Environment- and Product-scoped
	// routes answer 404 before they ever consult a grant, and the assertion
	// below would be about the fixture rather than about authorization.
	seedFixture(t, handler, repository)
	checked, witnessed := 0, 0
	for _, route := range HubRouteAuthorization() {
		if route.Decision != DecisionOperatorGrant && route.Decision != DecisionAgent {
			continue
		}
		checked++
		method, path := pathFor(route.Pattern)
		request := httptest.NewRequest(method, path, strings.NewReader(bodyFor(route.Pattern)))
		request.Header.Set("X-Platform-Subject", "nobody")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code >= 200 && response.Code < 300 {
			t.Errorf("%s answers %d to a principal holding nothing; its decision is %s (%s)",
				route.Pattern, response.Code, route.Decision, route.Detail)
		}
		// Refusing is not enough. "Refused" and "the record in the path does
		// not exist" are both non-2xx and only one of them is an authorization
		// decision, so a route that slid from an earned 403 to an incidental
		// 404 -- which is exactly what deleting its requireResourceOperation
		// does -- would pass as a refusal it never made. Every route this
		// fixture reaches must therefore answer 401 or 403, and the ones it
		// does not reach are named below with the status they do answer.
		if want, listed := refusedBeforeTheDecision[route.Pattern]; listed {
			if response.Code != want {
				t.Errorf("%s is listed as answering %d before its decision but answered %d; "+
					"if it now reaches the decision, delete its line", route.Pattern, want, response.Code)
			}
			continue
		}
		if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
			t.Errorf("%s answers %d to a principal holding nothing, not an authorization refusal (%s)",
				route.Pattern, response.Code, route.Detail)
		}
		witnessed++
	}
	if checked < 100 {
		t.Fatalf("only %d granted routes were driven; the register has stopped enumerating the surface", checked)
	}
	if witnessed < 75 {
		t.Fatalf("only %d of %d granted routes were witnessed refusing; the exception list is growing",
			witnessed, checked)
	}
}

// TestTheRegisterNamesEveryDecisionExactlyOnce guards the table's own shape.
// A duplicate pattern would make the guard's ServeMux panic at assembly, and an
// empty Detail would turn a matrix cell into a claim with nothing behind it.
func TestTheRegisterNamesEveryDecisionExactlyOnce(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, route := range HubRouteAuthorization() {
		if seen[route.Pattern] {
			t.Errorf("%s is registered twice", route.Pattern)
		}
		seen[route.Pattern] = true
		if strings.TrimSpace(route.Detail) == "" {
			t.Errorf("%s names a decision with no detail", route.Pattern)
		}
		switch route.Decision {
		case DecisionPublic, DecisionOperatorSelf, DecisionOperatorAny, DecisionOperatorFiltered,
			DecisionOperatorGrant, DecisionAgent, DecisionBundleSignature:
		default:
			t.Errorf("%s carries the unknown decision %q", route.Pattern, route.Decision)
		}
	}
	if len(seen) < 130 {
		t.Fatalf("the register names %d routes; the Hub mounts more than that", len(seen))
	}
}

// report prints the matrix, so that a reader who wants the table rather than
// the assertions can get it from `go test -run TestRouteAuthorizationMatrix -v`.
func TestRouteAuthorizationMatrix(t *testing.T) {
	t.Parallel()
	counts := map[Decision]int{}
	for _, route := range HubRouteAuthorization() {
		counts[route.Decision]++
	}
	for _, decision := range []Decision{
		DecisionPublic, DecisionOperatorSelf, DecisionOperatorAny, DecisionOperatorFiltered,
		DecisionOperatorGrant, DecisionAgent, DecisionBundleSignature,
	} {
		t.Logf("%-18s %d routes", decision, counts[decision])
	}
}

// --- cross-tenant isolation -------------------------------------------------

// TestFilteredRoutesShowOneTenantNothingOfAnother is the second finding, and it
// is a different question from the one above.
//
// The route x decision matrix answers "does an unauthorized caller get in".
// This answers "does an authorized caller get somebody else's rows", which no
// decision class can settle on its own: DecisionOperatorFiltered names a
// per-row check, and a per-row check that was written wrong -- an || that
// should be an &&, an administrator shortcut that swallowed the row test --
// still satisfies the matrix and leaks the entire index.
//
// What existed before this test, measured rather than assumed:
//
//   - internal/controlplane/plan_approval_authorization_test.go and
//     plan_cancel_authorization_test.go do assert cross-Environment refusal for
//     Plans: a grant on one Environment does not approve or cancel a Plan in
//     another. So the claim that the notification inbox held the only
//     cross-principal refusal test is not correct, and is recorded here as a
//     negative result rather than repeated by the next reader.
//   - plans_index_test.go asserts that a principal with NO grants sees an empty
//     Plan index. That is a weaker statement than this one: a filter that
//     returned everything to anyone holding any grant would pass it.
//   - Nothing anywhere drove an index route as one tenant while another
//     tenant's rows existed. Environments, Products, Releases, Plans, change
//     requests and the fleet were all in that gap.
//
// So this is deliberately about the second class. Two tenants exist, each
// principal holds a read grant on exactly one, and every filtered index and
// every by-name read is driven as both.
func TestFilteredRoutesShowOneTenantNothingOfAnother(t *testing.T) {
	t.Parallel()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	ctx := t.Context()

	tenants := []struct{ environment, product, team, principal string }{
		{"tenant-a-use2", "a.example:ledger", "tenant-a", "alice"},
		{"tenant-b-use2", "b.example:invoices", "tenant-b", "bob"},
	}
	for _, tenant := range tenants {
		if _, err := service.PutTeam(ctx, controlplane.Team{
			Name: tenant.team, DisplayName: tenant.team,
			AdminUsers: []string{tenant.principal}, MemberUsers: []string{tenant.principal},
		}); err != nil {
			t.Fatal(err)
		}
		for _, rid := range []string{
			"environment/" + tenant.environment, "product/" + tenant.product,
		} {
			if _, err := service.PutGrant(ctx, controlplane.ResourceGrant{
				ResourceRID: rid, Team: tenant.team,
				Operations: []string{controlplane.OperationReadResource, controlplane.OperationOperate},
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"root"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Seeded through the Hub's own routes as an administrator, so the fixture
	// is data this Hub accepted rather than rows written past its validation.
	for _, tenant := range tenants {
		state := desiredState(tenant.environment, tenant.product)
		body, marshalErr := json.Marshal(state)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		response := driveAs(t, handler, http.MethodPost, "/v1/plans", "root", string(body))
		if response.Code != http.StatusCreated {
			t.Fatalf("seeding %s: %d %s", tenant.environment, response.Code, response.Body.String())
		}
	}

	// Every filtered index, driven as each tenant. The assertion is symmetric
	// on purpose: "alice sees no tenant-b" alone would pass on a Hub that
	// showed nobody anything, so each tenant must also see its own.
	indexes := []string{
		"/v1/environments", "/v1/products", "/v1/plans", "/v1/change-requests",
		"/v1/fleet/installations", "/v1/artifact-registries", "/v1/overview",
	}
	// The control that makes the substring search meaningful. An administrator
	// sees both tenants on the indexes that carry an Environment, so a search
	// that had stopped matching anything at all would fail here rather than
	// pass everywhere below.
	for _, index := range []string{"/v1/environments", "/v1/plans", "/v1/fleet/installations"} {
		body := driveAs(t, handler, http.MethodGet, index, "root", "").Body.String()
		for _, tenant := range tenants {
			if !strings.Contains(body, tenant.environment) {
				t.Fatalf("%s does not name %s even for an administrator: %s", index, tenant.environment, body)
			}
		}
	}
	for _, index := range indexes {
		for _, tenant := range tenants {
			response := driveAs(t, handler, http.MethodGet, index, tenant.principal, "")
			if response.Code != http.StatusOK {
				t.Errorf("%s as %s = %d %s", index, tenant.principal, response.Code, response.Body.String())
				continue
			}
			body := response.Body.String()
			for _, other := range tenants {
				if other.principal == tenant.principal {
					continue
				}
				if strings.Contains(body, other.environment) || strings.Contains(body, other.product) {
					t.Errorf("%s showed %s a row belonging to %s: %s",
						index, tenant.principal, other.team, body)
				}
			}
		}
	}
	// The Plan index specifically must be non-empty for its own tenant, so the
	// loop above cannot pass by returning nothing to everybody.
	own := driveAs(t, handler, http.MethodGet, "/v1/plans", "alice", "")
	if !strings.Contains(own.Body.String(), "tenant-a-use2") {
		t.Fatalf("alice cannot see her own tenant's Plans: %s", own.Body.String())
	}

	// By-name reads, where a refusal rather than an empty list is the answer.
	for _, read := range []string{
		"/v1/environments/tenant-b-use2",
		"/v1/environments/tenant-b-use2/entities",
		"/v1/environments/tenant-b-use2/reported-state",
		"/v1/environments/tenant-b-use2/suppressions",
		"/v1/products/b.example:invoices/releases",
		"/v1/products/b.example:invoices/releases/0.1.0",
		"/v1/products/b.example:invoices/publications",
	} {
		response := driveAs(t, handler, http.MethodGet, read, "alice", "")
		if response.Code != http.StatusForbidden {
			t.Errorf("GET %s as alice = %d %s, want 403", read, response.Code, response.Body.String())
		}
		if response := driveAs(t, handler, http.MethodGet, read, "bob", ""); response.Code == http.StatusForbidden {
			t.Errorf("GET %s refused bob, who holds tenant-b: %s", read, response.Body.String())
		}
	}

	// And an approval, which is the write half. The Plan belongs to tenant-b;
	// alice holds operate on tenant-a and nothing else.
	var index struct {
		Plans []struct {
			ID   string `json:"id"`
			Plan struct {
				Environment string `json:"environment"`
			} `json:"plan"`
		} `json:"plans"`
	}
	all := driveAs(t, handler, http.MethodGet, "/v1/plans", "root", "")
	if err := json.Unmarshal(all.Body.Bytes(), &index); err != nil {
		t.Fatal(err)
	}
	approved := 0
	for _, plan := range index.Plans {
		if plan.Plan.Environment != "tenant-b-use2" {
			continue
		}
		approved++
		response := driveAs(t, handler, http.MethodPost,
			"/v1/plans/"+plan.ID+"/approvals", "alice", `{"rationale":"borrowed"}`)
		if response.Code != http.StatusForbidden {
			t.Errorf("alice approved a tenant-b Plan: %d %s", response.Code, response.Body.String())
		}
	}
	if approved == 0 {
		t.Fatal("no tenant-b Plan was found to approve; the fixture stopped exercising the write half")
	}
}

// driveAs drives one request as a named operator.
func driveAs(t *testing.T, handler http.Handler, method, target, subject, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("X-Platform-Subject", subject)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// desiredState is the smallest DesiredState this Hub accepts, in a named
// Environment and for a named Product, so two of them are two tenants.
func desiredState(environment, product string) controlplane.DesiredState {
	const digest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	return controlplane.DesiredState{
		APIVersion:  "platform.delivery/v1alpha1",
		Kind:        "EnvironmentDeployment",
		Environment: controlplane.Environment{ID: environment, Generation: "0123456789abcdef"},
		Releases: []controlplane.Release{{
			Product: product, Version: "0.1.0",
			Chart:  controlplane.Artifact{Repository: "registry.example/thing", Tag: "0.1.0", Digest: digest},
			Images: []controlplane.Artifact{{Repository: "registry.example/thing", Tag: "0.1.0", Digest: digest}},
		}},
		Entities: []controlplane.Entity{
			{Name: "thing", Product: product, Version: "0.1.0", Namespace: "example"},
		},
	}
}

// seed creates the records realIdentifiers names, as an administrator, through
// the Hub's own routes.
func seedFixture(t *testing.T, handler http.Handler, repository controlplane.Repository) {
	t.Helper()
	service := controlplane.NewGovernanceService(repository)
	if _, err := service.PutTeam(t.Context(), controlplane.Team{
		Name: fixtureTeam, DisplayName: fixtureTeam,
		AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"},
	}); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(desiredState(fixtureEnvironment, fixtureProduct))
	if err != nil {
		t.Fatal(err)
	}
	if response := driveAs(t, handler, http.MethodPost, "/v1/plans", "root", string(body)); response.Code != http.StatusCreated {
		t.Fatalf("seeding the fixture: %d %s", response.Code, response.Body.String())
	}
}

// refusedBeforeTheDecision are the granted routes this fixture cannot witness
// refusing, with the status each answers instead.
//
// Two reasons, both honest and both worth being able to see at a glance. Most
// name a record the fixture does not create -- an installation, a change
// request, a Plan other than the seeded one -- and answer 404 before reaching a
// decision. The rest validate the request body or a required query parameter
// first and answer 400, 409 or 422 to the placeholder body this test sends;
// that ordering discloses nothing, but it does mean the decision is downstream
// of a check this probe cannot satisfy.
//
// It is a list with exact statuses rather than a rule, because a rule would be
// "whatever it answered this time", which is the thing being guarded against.
// Shortening it is an improvement: seed the record or send a body the route
// accepts, then delete the line. Lengthening it needs a reason, because every
// entry is a route whose refusal this test does not witness -- and the floor
// above fails if the list grows past a quarter of the surface.
var refusedBeforeTheDecision = map[string]int{
	// The record in the path does not exist.
	"GET /v1/module-installations/{installationID}/update":            http.StatusNotFound,
	"GET /v1/plans/{planID}":                                          http.StatusNotFound,
	"POST /v1/adapted-plans/{adapter}":                                http.StatusNotFound,
	"POST /v1/change-requests/{changeRequestID}/cancel":               http.StatusNotFound,
	"POST /v1/module-installations/{installationID}/pause":            http.StatusNotFound,
	"POST /v1/module-installations/{installationID}/resume":           http.StatusNotFound,
	"POST /v1/module-installations/{installationID}/unlink":           http.StatusNotFound,
	"POST /v1/module-installations/{installationID}/update":           http.StatusNotFound,
	"POST /v1/module-installations/{installationID}/update/automatic": http.StatusNotFound,
	"POST /v1/plans/{planID}/approvals":                               http.StatusNotFound,
	"POST /v1/plans/{planID}/cancel":                                  http.StatusNotFound,
	"POST /v1/plans/{planID}/undo-unmanage":                           http.StatusNotFound,
	"PUT /v1/change-requests/{changeRequestID}/labels":                http.StatusNotFound,
	// The request is judged before the caller is.
	"DELETE /v1/suppressions/{suppressionID}":                   http.StatusBadRequest,
	"GET /v1/notifications/events":                              http.StatusBadRequest,
	"POST /v1/change-requests/{changeRequestID}/reopen":         http.StatusBadRequest,
	"POST /v1/change-requests/{changeRequestID}/approve":        http.StatusConflict,
	"POST /v1/change-requests/{changeRequestID}/reject":         http.StatusConflict,
	"POST /api/v0/products/helmChart/{mavenCoordinate}/publish": http.StatusUnprocessableEntity,
	"POST /api/v0/products/service/{mavenCoordinate}/publish":   http.StatusUnprocessableEntity,
	"POST /v1/bundles/import":                                   http.StatusUnprocessableEntity,
	"POST /v1/fleet/actions":                                    http.StatusUnprocessableEntity,
	"POST /v1/product-publications/helm-chart":                  http.StatusUnprocessableEntity,
}
