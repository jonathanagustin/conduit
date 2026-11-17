package revocationhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationmemory"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

type fixture struct {
	mux      *http.ServeMux
	store    *revocationmemory.Store
	auditErr error
	audited  []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	fixture := &fixture{mux: http.NewServeMux(), store: revocationmemory.New()}
	err := Register(fixture.mux, Config{
		Store: fixture.store,
		Now:   func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) },
		Authorize: func(request *http.Request, _ string, _ bool) (string, error) {
			if subject := request.Header.Get("X-Test-Subject"); subject != "" {
				return subject, nil
			}
			return "", StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
		},
		Audit: func(_ context.Context, actor, action string, _, _ *domain.AgentRevocation) error {
			if fixture.auditErr != nil {
				return fixture.auditErr
			}
			fixture.audited = append(fixture.audited, actor+" "+action)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *fixture) do(method, target, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	request.Header.Set("X-Test-Subject", "alice")
	response := httptest.NewRecorder()
	fixture.mux.ServeHTTP(response, request)
	return response
}

func TestTheRoutesRefuseToMountWithoutAnAuthorizerOrAnAuditSink(t *testing.T) {
	complete := Config{
		Store: revocationmemory.New(), Now: time.Now,
		Authorize: func(*http.Request, string, bool) (string, error) { return "alice", nil },
		Audit: func(context.Context, string, string, *domain.AgentRevocation, *domain.AgentRevocation) error {
			return nil
		},
	}
	if err := Register(http.NewServeMux(), complete); err != nil {
		t.Fatal(err)
	}
	for name, strip := range map[string]func(*Config){
		"store":      func(config *Config) { config.Store = nil },
		"clock":      func(config *Config) { config.Now = nil },
		"authorizer": func(config *Config) { config.Authorize = nil },
		"audit":      func(config *Config) { config.Audit = nil },
	} {
		config := complete
		strip(&config)
		if err := Register(http.NewServeMux(), config); err == nil {
			t.Errorf("mounted with no %s", name)
		}
	}
}

// Lifting a revocation readmits a credential. If that cannot be recorded it
// must not stand.
func TestARemovalThatCannotBeAuditedIsUndone(t *testing.T) {
	fixture := newFixture(t)
	created := fixture.do(http.MethodPost, "/v1/environments/prod/agent-revocations", `{"scope":"AGENT","agentId":"primary","reason":"key leaked"}`)
	if created.Code != http.StatusOK {
		t.Fatalf("%d %s", created.Code, created.Body.String())
	}
	held, _ := fixture.store.Revocations(t.Context(), "prod")

	fixture.auditErr = errors.New("audit store is down")
	response := fixture.do(http.MethodDelete, "/v1/environments/prod/agent-revocations/"+held[0].ID, "")
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "restored") {
		t.Fatalf("unaudited removal: %d %s", response.Code, response.Body.String())
	}
	after, _ := fixture.store.Revocations(t.Context(), "prod")
	if len(after) != 1 || after[0] != held[0] {
		t.Fatalf("an unaudited removal left the deny-list as %#v, want the revocation still in force", after)
	}

	fixture.auditErr = nil
	if response := fixture.do(http.MethodDelete, "/v1/environments/prod/agent-revocations/"+held[0].ID, ""); response.Code != http.StatusOK {
		t.Fatalf("audited removal: %d %s", response.Code, response.Body.String())
	}
	if after, _ := fixture.store.Revocations(t.Context(), "prod"); len(after) != 0 {
		t.Fatalf("revocation survived an audited removal: %#v", after)
	}
	if got := strings.Join(fixture.audited, ", "); got != "alice AGENT_REVOCATION_ADD, alice AGENT_REVOCATION_REMOVE" {
		t.Fatalf("audited = %q", got)
	}
}

// A revocation that landed and could not be recorded stays in force, and the
// caller is told it failed so they repeat it.
func TestAnAdditionThatCannotBeAuditedStaysInForceAndIsReportedAsAFailure(t *testing.T) {
	fixture := newFixture(t)
	fixture.auditErr = errors.New("audit store is down")
	response := fixture.do(http.MethodPost, "/v1/environments/prod/agent-revocations", `{"scope":"ENVIRONMENT","reason":"incident"}`)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "in force") {
		t.Fatalf("unaudited addition: %d %s", response.Code, response.Body.String())
	}
	if held, _ := fixture.store.Revocations(t.Context(), "prod"); len(held) != 1 {
		t.Fatalf("an unaudited addition was rolled back: %#v", held)
	}
}

func TestMalformedRequestsAreRefusedWithoutTouchingTheDenyList(t *testing.T) {
	fixture := newFixture(t)
	for name, test := range map[string]struct {
		target, body string
		status       int
	}{
		"not JSON":                    {"/v1/environments/prod/agent-revocations", `{`, http.StatusBadRequest},
		"unknown field":               {"/v1/environments/prod/agent-revocations", `{"scope":"ENVIRONMENT","reason":"r","permanent":true}`, http.StatusBadRequest},
		"environment disagrees":       {"/v1/environments/prod/agent-revocations", `{"scope":"ENVIRONMENT","environment":"staging","reason":"r"}`, http.StatusUnprocessableEntity},
		"no reason":                   {"/v1/environments/prod/agent-revocations", `{"scope":"ENVIRONMENT"}`, http.StatusUnprocessableEntity},
		"environment is not a label":  {"/v1/environments/Prod/agent-revocations", `{"scope":"ENVIRONMENT","reason":"r"}`, http.StatusBadRequest},
		"caller chooses the identity": {"/v1/environments/prod/agent-revocations", `{"scope":"ENVIRONMENT","reason":"r","createdBy":"mallory","id":"arv_x"}`, http.StatusOK},
	} {
		response := fixture.do(http.MethodPost, test.target, test.body)
		if response.Code != test.status {
			t.Errorf("%s: %d %s, want %d", name, response.Code, response.Body.String(), test.status)
		}
	}
	// The one accepted request above tried to name its own author and id.
	held, _ := fixture.store.Revocations(t.Context(), "prod")
	if len(held) != 1 || held[0].CreatedBy != "alice" || held[0].ID == "arv_x" {
		t.Fatalf("deny-list = %#v; want one record attributed to the authenticated operator", held)
	}
	if response := fixture.do(http.MethodDelete, "/v1/environments/prod/agent-revocations/arv_missing", ""); response.Code != http.StatusNotFound {
		t.Fatalf("removing nothing: %d", response.Code)
	}
	unauthenticated := httptest.NewRecorder()
	fixture.mux.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/v1/environments/prod/agent-revocations", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("no subject: %d", unauthenticated.Code)
	}
}
