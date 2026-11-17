package composition

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationmemory"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	identityports "github.com/jonathanagustin/conduit/internal/identity/ports"
)

// Everything here drives the assembled Hub -- NewHub, route guard and all --
// with production-shaped Agent identity: no insecure Agent headers, so an Agent
// is whoever a verified client certificate says it is.

const revocationEnvironment = "prod"

// brokenCredentialStore is a deny-list that cannot be read, which is what a
// database outage looks like from here.
type brokenCredentialStore struct {
	identityports.AgentCredentialStore
	broken atomic.Bool
}

func (store *brokenCredentialStore) Revocations(ctx context.Context, environment string) ([]identity.AgentRevocation, error) {
	if store.broken.Load() {
		return nil, errors.New("connection refused")
	}
	return store.AgentCredentialStore.Revocations(ctx, environment)
}

type revocationHub struct {
	handler    http.Handler
	repository controlplane.Repository
	store      *brokenCredentialStore
}

// newRevocationHub has three operators: root, a bootstrap administrator; a
// reader holding only read on the Environment; and an operator holding
// everything on it except manage-permissions.
func newRevocationHub(t *testing.T) revocationHub {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	for team, grant := range map[string]struct {
		member     string
		operations []string
	}{
		"revocation-readers":   {"reader", []string{controlplane.OperationReadResource}},
		"revocation-operators": {"operator", []string{controlplane.OperationReadResource, controlplane.OperationProposeChange, controlplane.OperationOperate}},
	} {
		if _, err := service.PutTeam(t.Context(), controlplane.Team{
			Name: team, DisplayName: team, AdminUsers: []string{"root"}, MemberUsers: []string{grant.member},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{
			ResourceRID: "environment/" + revocationEnvironment, Team: team, Operations: grant.operations,
		}); err != nil {
			t.Fatal(err)
		}
	}
	store := &brokenCredentialStore{AgentCredentialStore: revocationmemory.New()}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"root"},
	}, Options{AgentCredentials: store})
	if err != nil {
		t.Fatal(err)
	}
	return revocationHub{handler: handler, repository: repository, store: store}
}

func agentCertificate(t *testing.T, environment, agent string, serial int64, notBefore time.Time) *x509.Certificate {
	t.Helper()
	uri, err := url.Parse("spiffe://platform.delivery/environment/" + environment + "/agent/" + agent)
	if err != nil {
		t.Fatal(err)
	}
	return &x509.Certificate{
		SerialNumber: big.NewInt(serial), URIs: []*url.URL{uri},
		NotBefore: notBefore, NotAfter: notBefore.Add(90 * 24 * time.Hour),
	}
}

func (hub revocationHub) asAgent(method, target, body string, certificate *x509.Certificate) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}
	response := httptest.NewRecorder()
	hub.handler.ServeHTTP(response, request)
	return response
}

func (hub revocationHub) poll(certificate *x509.Certificate) *httptest.ResponseRecorder {
	identityURI := certificate.URIs[0].Path
	environment := strings.Split(strings.Trim(identityURI, "/"), "/")[1]
	return hub.asAgent(http.MethodPost, "/v1/environments/"+environment+"/leases", `{}`, certificate)
}

func (hub revocationHub) revoke(t *testing.T, body string) identity.AgentRevocation {
	t.Helper()
	response := driveAs(t, hub.handler, http.MethodPost, "/v1/environments/"+revocationEnvironment+"/agent-revocations", "root", body)
	if response.Code != http.StatusOK {
		t.Fatalf("revoking %s: %d %s", body, response.Code, response.Body.String())
	}
	var revocation identity.AgentRevocation
	if err := json.Unmarshal(response.Body.Bytes(), &revocation); err != nil {
		t.Fatal(err)
	}
	return revocation
}

func requireRevoked(t *testing.T, what string, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "Agent credential is revoked") {
		t.Fatalf("%s: %d %s; want 403 revoked", what, response.Code, response.Body.String())
	}
}

// An idle Environment answers a lease poll 204. That, not 200, is "admitted".
func requireAdmitted(t *testing.T, what string, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusNoContent && response.Code != http.StatusOK {
		t.Fatalf("%s: %d %s; want the request admitted", what, response.Code, response.Body.String())
	}
}

func TestARevokedSerialIsRefusedAndTheAgentsOtherCertificateIsNot(t *testing.T) {
	hub := newRevocationHub(t)
	issued := time.Now().Add(-time.Hour)
	leaked := agentCertificate(t, revocationEnvironment, "primary", 0xA1B, issued)
	replacement := agentCertificate(t, revocationEnvironment, "primary", 0xA1C, issued)
	requireAdmitted(t, "before revocation", hub.poll(leaked))

	// Spelled the way `openssl x509 -noout -serial` prints it.
	hub.revoke(t, `{"scope":"SERIAL","serial":"0A1B","reason":"key leaked"}`)

	requireRevoked(t, "the revoked serial", hub.poll(leaked))
	requireAdmitted(t, "the same Agent's other certificate", hub.poll(replacement))
}

func TestARevokedIdentityIsRefusedWithItsOldCertificateAndAdmittedWithOneIssuedAfter(t *testing.T) {
	hub := newRevocationHub(t)
	instant := time.Now().Add(-30 * time.Minute).UTC().Truncate(time.Second)
	old := agentCertificate(t, revocationEnvironment, "primary", 1, instant.Add(-time.Hour))
	reissued := agentCertificate(t, revocationEnvironment, "primary", 2, instant.Add(time.Minute))
	neighbour := agentCertificate(t, revocationEnvironment, "secondary", 3, instant.Add(-time.Hour))

	hub.revoke(t, `{"scope":"AGENT","agentId":"primary","reason":"key leaked","notBefore":"`+instant.Format(time.RFC3339)+`"}`)

	requireRevoked(t, "the certificate held at revocation", hub.poll(old))
	requireAdmitted(t, "a certificate issued after the instant", hub.poll(reissued))
	requireAdmitted(t, "another Agent of the same Environment", hub.poll(neighbour))
}

func TestAnIdentityRevocationDefaultsToTheMomentItIsMade(t *testing.T) {
	hub := newRevocationHub(t)
	before := time.Now().Add(-time.Second)
	revocation := hub.revoke(t, `{"scope":"AGENT","agentId":"primary","reason":"key leaked"}`)
	if revocation.NotBefore.Before(before) || revocation.NotBefore.After(time.Now().Add(time.Second)) {
		t.Fatalf("default instant %s is not the moment of revocation", revocation.NotBefore)
	}
	if revocation.CreatedBy != "root" {
		t.Fatalf("createdBy = %q, want the authenticated operator", revocation.CreatedBy)
	}
	requireRevoked(t, "a certificate issued before the revocation", hub.poll(agentCertificate(t, revocationEnvironment, "primary", 1, before.Add(-time.Hour))))
}

func TestARevokedEnvironmentRefusesEveryAgentInItAndNoOther(t *testing.T) {
	hub := newRevocationHub(t)
	issued := time.Now().Add(-time.Hour)
	hub.revoke(t, `{"scope":"ENVIRONMENT","reason":"cluster compromised"}`)
	requireRevoked(t, "primary", hub.poll(agentCertificate(t, revocationEnvironment, "primary", 1, issued)))
	requireRevoked(t, "secondary", hub.poll(agentCertificate(t, revocationEnvironment, "secondary", 2, issued)))
	requireAdmitted(t, "an Agent of another Environment", hub.poll(agentCertificate(t, "staging", "primary", 3, issued)))
}

var pathParameter = regexp.MustCompile(`\{[A-Za-z]+\}`)

// agentRoutes is every route the register declares DecisionAgent, as a
// concrete request for the revocation Environment.
func agentRoutes(t *testing.T) []struct{ method, path string } {
	t.Helper()
	var routes []struct{ method, path string }
	for _, route := range HubRouteAuthorization() {
		if route.Decision != DecisionAgent {
			continue
		}
		method, path, _ := strings.Cut(route.Pattern, " ")
		path = strings.NewReplacer("{environmentID}", revocationEnvironment, "{environmentId}", revocationEnvironment).Replace(path)
		routes = append(routes, struct{ method, path string }{method, pathParameter.ReplaceAllString(path, "plan-1")})
	}
	// A floor, so a register that stopped declaring Agent routes cannot turn
	// the two tests below into loops over nothing.
	if len(routes) < 10 {
		t.Fatalf("only %d Agent routes are registered; this test would prove nothing", len(routes))
	}
	return routes
}

// The deny-list is enforced on every Agent route, whichever of the three
// mechanisms mounted it, and before the handler: none of these requests names
// a Plan or carries a body the handler would accept, and all of them are
// answered "revoked" rather than with whatever the handler would have said.
func TestEveryAgentRouteRefusesARevokedCredentialBeforeItsHandlerRuns(t *testing.T) {
	hub := newRevocationHub(t)
	certificate := agentCertificate(t, revocationEnvironment, "primary", 1, time.Now().Add(-time.Hour))
	hub.revoke(t, `{"scope":"AGENT","agentId":"primary","reason":"key leaked"}`)
	for _, route := range agentRoutes(t) {
		requireRevoked(t, route.method+" "+route.path, hub.asAgent(route.method, route.path, `{}`, certificate))
	}
}

// A deny-list that cannot be read is an unknown deny-list. Agents are refused
// with 503 -- never admitted -- and nothing else is affected, including the
// operator routes somebody would use to find out what is wrong.
func TestAnUnreadableDenyListFailsClosedForAgentsOnly(t *testing.T) {
	hub := newRevocationHub(t)
	certificate := agentCertificate(t, revocationEnvironment, "primary", 1, time.Now().Add(-time.Hour))
	requireAdmitted(t, "with the store healthy", hub.poll(certificate))

	hub.store.broken.Store(true)
	for _, route := range agentRoutes(t) {
		response := hub.asAgent(route.method, route.path, `{}`, certificate)
		if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" {
			t.Fatalf("%s %s with an unreadable deny-list: %d %s; want 503 with Retry-After",
				route.method, route.path, response.Code, response.Body.String())
		}
	}
	for _, target := range []string{"/health/live", "/v1/environments", "/v1/authorization-audit", "/v1/products"} {
		if response := driveAs(t, hub.handler, http.MethodGet, target, "root", ""); response.Code != http.StatusOK {
			t.Fatalf("GET %s with an unreadable deny-list: %d %s; operator routes must not depend on it",
				target, response.Code, response.Body.String())
		}
	}
	// An Agent route reached with no Agent credential is refused as it always
	// was, without the store being consulted.
	if response := driveAs(t, hub.handler, http.MethodPost, "/v1/environments/"+revocationEnvironment+"/leases", "root", `{}`); response.Code != http.StatusUnauthorized {
		t.Fatalf("an operator on an Agent route: %d, want 401", response.Code)
	}

	hub.store.broken.Store(false)
	requireAdmitted(t, "once the store is readable again", hub.poll(certificate))
}

func TestARevocationChangeIsWrittenToTheAuthorizationAuditTrail(t *testing.T) {
	hub := newRevocationHub(t)
	revocation := hub.revoke(t, `{"scope":"SERIAL","serial":"a1b","reason":"key leaked"}`)
	response := driveAs(t, hub.handler, http.MethodDelete,
		"/v1/environments/"+revocationEnvironment+"/agent-revocations/"+revocation.ID, "root", "")
	if response.Code != http.StatusOK {
		t.Fatalf("removal: %d %s", response.Code, response.Body.String())
	}
	events, err := hub.repository.ListAuthorizationAudit(t.Context(), "environment/"+revocationEnvironment)
	if err != nil {
		t.Fatal(err)
	}
	recorded := map[string]controlplane.AuthorizationAuditEvent{}
	for _, event := range events {
		recorded[event.Action] = event
	}
	added, removed := recorded["AGENT_REVOCATION_ADD"], recorded["AGENT_REVOCATION_REMOVE"]
	for action, event := range map[string]controlplane.AuthorizationAuditEvent{"add": added, "remove": removed} {
		if event.Subject != "root" || event.Decision != "ALLOW" || event.RequestFields["revocationId"] != revocation.ID ||
			event.ResultFields["configureInfraRequestId"] != revocation.ID {
			t.Fatalf("%s audit event = %#v", action, event)
		}
		if err := controlplane.ValidateAuditCategoryEvent(event); err != nil {
			t.Fatalf("%s audit event does not satisfy its category: %v", action, err)
		}
	}
	after, _ := added.RequestFields["revocationAfter"].(map[string]any)
	if added.RequestFields["revocationBefore"] != nil || after["serial"] != "a1b" || after["reason"] != "key leaked" || after["scope"] != identity.RevocationSerial {
		t.Fatalf("the add event does not carry what was revoked: %#v", added.RequestFields)
	}
	before, _ := removed.RequestFields["revocationBefore"].(map[string]any)
	if removed.RequestFields["revocationAfter"] != nil || before["serial"] != "a1b" {
		t.Fatalf("the remove event does not carry what was lifted: %#v", removed.RequestFields)
	}
}

func TestRevocationRoutesTakeTheEnvironmentsPermissionAuthority(t *testing.T) {
	hub := newRevocationHub(t)
	collection := "/v1/environments/" + revocationEnvironment + "/agent-revocations"
	body := `{"scope":"ENVIRONMENT","reason":"incident"}`
	held := hub.revoke(t, `{"scope":"SERIAL","serial":"ff","reason":"held"}`)

	// Operate on the Environment is not authority over who may act in it.
	for _, subject := range []string{"reader", "operator", "stranger"} {
		if response := driveAs(t, hub.handler, http.MethodPost, collection, subject, body); response.Code != http.StatusForbidden {
			t.Fatalf("%s adding a revocation: %d %s; want 403", subject, response.Code, response.Body.String())
		}
		if response := driveAs(t, hub.handler, http.MethodDelete, collection+"/"+held.ID, subject, ""); response.Code != http.StatusForbidden {
			t.Fatalf("%s removing a revocation: %d %s; want 403", subject, response.Code, response.Body.String())
		}
	}
	listed, _ := hub.store.Revocations(t.Context(), revocationEnvironment)
	if len(listed) != 1 || listed[0].ID != held.ID {
		t.Fatalf("a refused request changed the deny-list: %#v", listed)
	}
	// Reading takes the Environment's read authority.
	for subject, status := range map[string]int{"root": 200, "reader": 200, "operator": 200, "stranger": 403} {
		for _, target := range []string{collection, "/v1/environments/" + revocationEnvironment + "/agent-credentials"} {
			if response := driveAs(t, hub.handler, http.MethodGet, target, subject, ""); response.Code != status {
				t.Fatalf("%s reading %s: %d, want %d", subject, target, response.Code, status)
			}
		}
	}
	// A grant on one Environment is not a grant on another.
	if response := driveAs(t, hub.handler, http.MethodGet, "/v1/environments/staging/agent-revocations", "reader", ""); response.Code != http.StatusForbidden {
		t.Fatalf("reader reading another Environment's deny-list: %d", response.Code)
	}
	// An Agent certificate is not an operator credential here.
	certificate := agentCertificate(t, revocationEnvironment, "primary", 1, time.Now().Add(-time.Hour))
	production, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []struct{ method, target, body string }{
		{http.MethodGet, collection, ""}, {http.MethodPost, collection, body}, {http.MethodDelete, collection + "/" + held.ID, ""},
	} {
		response := revocationHub{handler: production}.asAgent(request.method, request.target, request.body, certificate)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("an Agent certificate on %s %s: %d %s; want 401", request.method, request.target, response.Code, response.Body.String())
		}
	}
}

// What happens to a lease held by an Agent that is then revoked. Nothing new:
// the revoked Agent can no longer start, renew or finish, so the lease runs out
// and LeaseNextPlan hands the Plan to the next Agent that asks, exactly as it
// does for an Agent that crashed.
func TestARevokedAgentCannotReportItsInFlightLeaseAndThePlanIsLeasedAgain(t *testing.T) {
	hub := newRevocationHub(t)
	lifecycle := controlplane.NewLifecycle(hub.repository)
	created, err := lifecycle.CreatePlan(t.Context(), desiredState(revocationEnvironment, "r.example:thing"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ApprovePlan(t.Context(), created.ID, "root", "approved for the revocation test"); err != nil {
		t.Fatal(err)
	}
	issued := time.Now().Add(-time.Hour)
	leaked := agentCertificate(t, revocationEnvironment, "primary", 1, issued)
	standby := agentCertificate(t, revocationEnvironment, "standby", 2, issued)

	lease := hub.asAgent(http.MethodPost, "/v1/environments/"+revocationEnvironment+"/leases", `{"durationSeconds":1}`, leaked)
	if lease.Code != http.StatusOK {
		t.Fatalf("leasing the approved Plan: %d %s", lease.Code, lease.Body.String())
	}
	var leased controlplane.PlanRecord
	if err := json.Unmarshal(lease.Body.Bytes(), &leased); err != nil || leased.Lease == nil || leased.Lease.Token == "" {
		t.Fatalf("lease response %s: %v", lease.Body.String(), err)
	}
	token := leased.Lease.Token

	hub.revoke(t, `{"scope":"AGENT","agentId":"primary","reason":"key leaked mid-run"}`)

	report := func(action, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/plans/"+leased.ID+"/"+action, strings.NewReader(body))
		request.Header.Set("X-Platform-Lease-Token", token)
		request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaked}, VerifiedChains: [][]*x509.Certificate{{leaked}}}
		response := httptest.NewRecorder()
		hub.handler.ServeHTTP(response, request)
		return response
	}
	requireRevoked(t, "start", report("start", ""))
	requireRevoked(t, "heartbeat", report("heartbeat", `{"durationSeconds":60}`))
	requireRevoked(t, "finish", report("finish", `{"status":"SUCCEEDED"}`))

	stored, err := hub.repository.GetPlan(t.Context(), leased.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != controlplane.PlanLeased || stored.Lease == nil || stored.Lease.AgentID != "primary" {
		t.Fatalf("the Plan moved under a revoked Agent: status %s lease %#v", stored.Status, stored.Lease)
	}

	// While the lease is live nobody else is handed the Plan.
	if response := hub.poll(standby); response.Code != http.StatusNoContent {
		t.Fatalf("standby poll while the revoked Agent's lease is live: %d %s", response.Code, response.Body.String())
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		response := hub.poll(standby)
		if response.Code == http.StatusOK {
			var retaken controlplane.PlanRecord
			if err := json.Unmarshal(response.Body.Bytes(), &retaken); err != nil {
				t.Fatal(err)
			}
			if retaken.ID != leased.ID || retaken.Lease == nil || retaken.Lease.AgentID != "standby" || retaken.Lease.Attempt != 2 {
				t.Fatalf("the expired lease was not handed on as a second attempt: %#v", retaken.Lease)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the revoked Agent's lease never expired to another Agent: %d %s", response.Code, response.Body.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	// And the revoked Agent is not handed it back.
	requireRevoked(t, "the revoked Agent polling again", hub.poll(leaked))
}

// The certificate an admitted Agent presents is what the operator API reports,
// so an expiry can be seen coming and a serial can be found to revoke.
func TestAnAdmittedAgentsCertificateExpiryIsVisibleToOperators(t *testing.T) {
	hub := newRevocationHub(t)
	issued := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	certificate := agentCertificate(t, revocationEnvironment, "primary", 0xA1B, issued)
	requireAdmitted(t, "poll", hub.poll(certificate))

	read := func() []map[string]any {
		t.Helper()
		response := driveAs(t, hub.handler, http.MethodGet, "/v1/environments/"+revocationEnvironment+"/agent-credentials", "reader", "")
		var body struct {
			AgentCredentials []map[string]any `json:"agentCredentials"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("%d %s: %v", response.Code, response.Body.String(), err)
		}
		return body.AgentCredentials
	}
	credentials := read()
	if len(credentials) != 1 {
		t.Fatalf("credentials = %#v", credentials)
	}
	seconds, _ := credentials[0]["secondsUntilExpiry"].(float64)
	want := certificate.NotAfter.Sub(time.Now()).Seconds()
	if credentials[0]["agentId"] != "primary" || credentials[0]["serial"] != "a1b" ||
		credentials[0]["notAfter"] != certificate.NotAfter.Format(time.RFC3339) || seconds < want-5 || seconds > want+5 {
		t.Fatalf("credential view = %#v; want serial a1b expiring in about %.0fs", credentials[0], want)
	}
	if _, revoked := credentials[0]["revokedBy"]; revoked {
		t.Fatalf("an unrevoked certificate is reported revoked: %#v", credentials[0])
	}
	revocation := hub.revoke(t, `{"scope":"SERIAL","serial":"A1B","reason":"found in the credentials view"}`)
	if credentials = read(); credentials[0]["revokedBy"] != revocation.ID {
		t.Fatalf("the view does not say the certificate is now revoked: %#v", credentials[0])
	}
	// A refused request is not a sighting: the revoked certificate is not
	// re-recorded as though the Agent were healthy.
	sightings, _ := hub.store.Sightings(t.Context(), revocationEnvironment)
	firstSeen := sightings[0].SeenAt
	requireRevoked(t, "poll after revocation", hub.poll(certificate))
	sightings, _ = hub.store.Sightings(t.Context(), revocationEnvironment)
	if !sightings[0].SeenAt.Equal(firstSeen) {
		t.Fatal("a refused request was recorded as a sighting")
	}
}
