package composition

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

type readyPlanningRunner struct{}

func (readyPlanningRunner) Turn(ctx context.Context, session *planningdomain.Session, _ string) (*planningapp.TurnResult, error) {
	if planningActor(ctx) != "alice" {
		return nil, errors.New("missing authenticated audit actor")
	}
	return &planningapp.TurnResult{Plan: &planningdomain.Plan{Text: "Package the app"}, ResolvedCommit: strings.Repeat("a", 40), Model: "fixture", Profile: planningdomain.ProfileRef{Name: "onboard", Version: "1", Hash: strings.Repeat("b", 64)}}, nil
}

type planningAuditFailure struct {
	controlplane.Repository
	fail bool
}

func (repository *planningAuditFailure) AppendAuthorizationAudit(ctx context.Context, event controlplane.AuthorizationAuditEvent) (controlplane.AuthorizationAuditEvent, error) {
	if repository.fail {
		return controlplane.AuthorizationAuditEvent{}, errors.New("audit unavailable")
	}
	return repository.Repository.AppendAuthorizationAudit(ctx, event)
}

func planningRequest(t *testing.T, handler http.Handler, actor, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if actor != "" {
		request.Header.Set("X-Platform-Subject", actor)
	}
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestPlanningOwnershipAndAcceptanceAudit(t *testing.T) {
	repository := &planningAuditFailure{Repository: controlplane.NewMemoryRepository()}
	handler := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"bob"}}, readyPlanningRunner{})
	secure := planningHandler(http.NotFoundHandler(), repository, controlplane.HandlerOptions{}, readyPlanningRunner{})
	response := planningRequest(t, secure, "", "POST", "/v1/planning-sessions", `{"repoUrl":"https://github.com/org/app","ref":"main"}`)
	if response.Code != 401 {
		t.Fatalf("anonymous: %d", response.Code)
	}
	response = planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions", `{"repoUrl":"https://github.com/org/app","ref":"main"}`)
	var session planningdomain.Session
	if response.Code != 201 || json.Unmarshal(response.Body.Bytes(), &session) != nil || session.ID == "" {
		t.Fatalf("create: %d %s", response.Code, response.Body.String())
	}
	path := "/v1/planning-sessions/" + session.ID
	for _, route := range []struct{ method, suffix, body string }{{"GET", "", ""}, {"GET", "/binding", ""}, {"GET", "/events", ""}, {"POST", "/turns", `{}`}, {"POST", "/answers", `{"answers":[]}`}, {"POST", "/accept", `{}`}, {"POST", "/compute", `{}`}} {
		response = planningRequest(t, handler, "bob", route.method, path+route.suffix, route.body)
		if response.Code != 404 {
			t.Fatalf("other operator %s %s: %d %s", route.method, route.suffix, response.Code, response.Body.String())
		}
	}
	response = planningRequest(t, handler, "alice", "POST", path+"/compute", `{}`)
	if response.Code != 409 || !strings.Contains(response.Body.String(), "COMPUTE_BEFORE_ACCEPT") {
		t.Fatalf("pre-accept compute: %s", response.Body.String())
	}
	response = planningRequest(t, handler, "alice", "POST", path+"/turns", `{"message":"onboard this app"}`)
	if response.Code != 200 {
		t.Fatalf("turn: %d %s", response.Code, response.Body.String())
	}
	repository.fail = true
	response = planningRequest(t, handler, "alice", "POST", path+"/accept", `{}`)
	if response.Code != 502 {
		t.Fatalf("audit failure: %d %s", response.Code, response.Body.String())
	}
	response = planningRequest(t, handler, "alice", "GET", path+"/binding", "")
	if response.Code != 409 {
		t.Fatalf("failed audit bound session: %d", response.Code)
	}
	repository.fail = false
	response = planningRequest(t, handler, "alice", "POST", path+"/accept", `{}`)
	if response.Code != 200 {
		t.Fatalf("accept: %d %s", response.Code, response.Body.String())
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "planning-session/"+session.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("accept receipt: %v %+v", err, events)
	}
	event := events[0]
	if event.Subject != "alice" || event.Action != planningapp.ActionAccept || event.Context["planHash"] == "" || event.Context["profileHash"] != strings.Repeat("b", 64) || event.Context["resolvedCommit"] != strings.Repeat("a", 40) || event.Context["intakeMessageSha256"] == "" {
		t.Fatalf("receipt lost binding: %+v", event)
	}
	response = planningRequest(t, handler, "alice", "POST", path+"/compute", `{}`)
	if response.Code != http.StatusNotImplemented || !strings.Contains(response.Body.String(), "COMPUTE_NOT_CONFIGURED") {
		t.Fatalf("unconfigured compute: %d %s", response.Code, response.Body.String())
	}
}

func TestPlanningModelAuditUsesCredentialOwner(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	ctx := context.WithValue(t.Context(), planningActorKey{}, "alice")
	err := (planningAudit{repository: repository}).Record(ctx, planner.Record{SessionID: "session", Action: planner.ActionInspect, Outcome: "pass", PromptSHA256: strings.Repeat("a", 64), ResponseSHA256: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	events, err := repository.ListAuthorizationAudit(t.Context(), "planning-session/session")
	if err != nil || len(events) != 1 || events[0].Subject != "alice" || events[0].Context["promptSha256"] != strings.Repeat("a", 64) {
		t.Fatalf("model audit: %+v %v", events, err)
	}
}
