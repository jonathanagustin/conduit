package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/planning/application"
	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

func TestPlanningRoutesRequireAnOperator(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	Register(mux, Config{Service: application.NewService(nil)})
	for _, route := range []string{
		CreateRoute, GetRoute, TurnRoute, AnswerRoute, AcceptRoute, BindingRoute, EventsRoute, ComputeRoute,
	} {
		method, path, _ := strings.Cut(route, " ")
		path = strings.ReplaceAll(path, "{sessionID}", "ps_missing")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d", route, response.Code)
		}
	}
}

func TestCreateAndGetReturnTheSessionContract(t *testing.T) {
	t.Parallel()
	mux := newMux(t, application.NewService(nil))
	created := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions", map[string]string{
		"repoUrl": "https://github.com/org/app",
		"ref":     "main",
	})
	if created.status != http.StatusCreated {
		t.Fatalf("create status = %d body = %s", created.status, created.raw)
	}
	for _, key := range []string{
		"id", "status", "profile", "intake", "resolvedCommit", "model",
		"inspection", "needsInput", "answers", "plan", "binding", "messages", "events",
		"createdAt", "updatedAt",
	} {
		if _, ok := created.body[key]; !ok {
			t.Fatalf("create response missing %q: %s", key, created.raw)
		}
	}
	if created.body["status"] != "started" {
		t.Fatalf("status = %v", created.body["status"])
	}
	profile, _ := created.body["profile"].(map[string]any)
	if profile["name"] != domain.ProfileOnboard {
		t.Fatalf("profile = %v", created.body["profile"])
	}
	intake, _ := created.body["intake"].(map[string]any)
	if intake["repoUrl"] != "https://github.com/org/app" || intake["ref"] != "main" {
		t.Fatalf("intake = %v", intake)
	}
	id, _ := created.body["id"].(string)
	if !strings.HasPrefix(id, "ps_") {
		t.Fatalf("id = %q", id)
	}
	got := doJSON(t, mux, http.MethodGet, "/v1/planning-sessions/"+id, nil)
	if got.status != http.StatusOK {
		t.Fatalf("get status = %d body = %s", got.status, got.raw)
	}
	if got.body["id"] != id {
		t.Fatalf("get id = %v", got.body["id"])
	}
}

func TestCreateRejectsInvalidIntake(t *testing.T) {
	t.Parallel()
	mux := newMux(t, application.NewService(nil))
	got := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions", map[string]string{
		"repoUrl": "https://user:pass@github.com/org/app",
		"ref":     "main",
	})
	if got.status != http.StatusBadRequest || got.body["code"] != domain.CodeInvalidIntake {
		t.Fatalf("status = %d body = %s", got.status, got.raw)
	}
}

func TestTurnWithoutPlannerReturnsModelNotConfigured(t *testing.T) {
	t.Parallel()
	mux := newMux(t, application.NewService(nil))
	created := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions", map[string]string{
		"repoUrl": "https://github.com/org/app", "ref": "main",
	})
	id, _ := created.body["id"].(string)
	got := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/turns", map[string]string{
		"message": "inspect the repository",
	})
	if got.status != http.StatusServiceUnavailable || got.body["code"] != domain.CodeModelNotConfigured {
		t.Fatalf("status = %d body = %s", got.status, got.raw)
	}
}

func TestAcceptAndComputeFollowTheBindingContract(t *testing.T) {
	t.Parallel()
	service := application.NewService(scriptedPlanner{})
	mux := newMux(t, service)
	created := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions", map[string]string{
		"repoUrl": "https://github.com/org/app", "ref": "v1",
	})
	id, _ := created.body["id"].(string)
	before := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/compute", nil)
	if before.status != http.StatusConflict || before.body["code"] != domain.CodeComputeBeforeAccept {
		t.Fatalf("compute before accept: %d %s", before.status, before.raw)
	}
	acceptBeforePlan := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/accept", nil)
	if acceptBeforePlan.status != http.StatusConflict || acceptBeforePlan.body["code"] != domain.CodePlanRequired {
		t.Fatalf("accept before plan: %d %s", acceptBeforePlan.status, acceptBeforePlan.raw)
	}
	turned := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/turns", map[string]string{
		"message": "inspect the repository",
	})
	if turned.status != http.StatusOK || turned.body["status"] != domain.StatusNeedsInput {
		t.Fatalf("turn: %d %s", turned.status, turned.raw)
	}
	answered := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/answers", map[string]any{
		"answers": []map[string]string{{"id": "sso", "value": "no"}},
	})
	if answered.status != http.StatusOK || answered.body["status"] != domain.StatusPlanning || answered.body["plan"] != nil {
		t.Fatalf("answers retained stale plan: %d %s", answered.status, answered.raw)
	}
	stale := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/accept", nil)
	if stale.status != http.StatusConflict || stale.body["code"] != domain.CodePlanRequired {
		t.Fatalf("stale accept: %d %s", stale.status, stale.raw)
	}
	replanned := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/turns", map[string]string{"message": "rebuild the plan using these answers"})
	if replanned.status != http.StatusOK || replanned.body["status"] != domain.StatusReady {
		t.Fatalf("replan: %d %s", replanned.status, replanned.raw)
	}
	accepted := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/accept", nil)
	if accepted.status != http.StatusOK || accepted.body["status"] != domain.StatusAccepted {
		t.Fatalf("accept: %d %s", accepted.status, accepted.raw)
	}
	binding, _ := accepted.body["binding"].(map[string]any)
	for _, key := range []string{"profile", "intake", "resolvedCommit", "model", "plan", "acceptedAt"} {
		if _, ok := binding[key]; !ok {
			t.Fatalf("binding missing %q: %s", key, accepted.raw)
		}
	}
	profile, _ := binding["profile"].(map[string]any)
	if profile["name"] != domain.ProfileOnboard || profile["version"] != "1" || profile["hash"] != "deadbeef" {
		t.Fatalf("binding profile = %v", profile)
	}
	got := doJSON(t, mux, http.MethodGet, "/v1/planning-sessions/"+id+"/binding", nil)
	if got.status != http.StatusOK {
		t.Fatalf("get binding: %d %s", got.status, got.raw)
	}
	if got.body["resolvedCommit"] != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("binding = %s", got.raw)
	}
	compute := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions/"+id+"/compute", nil)
	if compute.status != http.StatusNotImplemented || compute.body["code"] != domain.CodeComputeNotConfigured {
		t.Fatalf("compute after accept: %d %s", compute.status, compute.raw)
	}
}

func TestTurnStreamAndEventsLog(t *testing.T) {
	t.Parallel()
	service := application.NewService(scriptedPlanner{})
	mux := newMux(t, service)
	created := doJSON(t, mux, http.MethodPost, "/v1/planning-sessions", map[string]string{
		"repoUrl": "https://github.com/org/app", "ref": "v1",
	})
	id, _ := created.body["id"].(string)
	request := httptest.NewRequest(http.MethodPost, "/v1/planning-sessions/"+id+"/turns", strings.NewReader(`{"message":"inspect the repository"}`))
	request.Header.Set("content-type", "application/json")
	request.Header.Set("accept", "text/event-stream")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stream status = %d body = %s", response.Code, response.Body.String())
	}
	if !strings.HasPrefix(response.Header().Get("content-type"), "text/event-stream") {
		t.Fatalf("content-type = %q", response.Header().Get("content-type"))
	}
	body := response.Body.String()
	for _, typ := range []string{"message", "inspection", "needs_input", "plan", "status", "done"} {
		if !strings.Contains(body, "event: "+typ) {
			t.Fatalf("stream missing %s event: %s", typ, body)
		}
	}
	got := doJSON(t, mux, http.MethodGet, "/v1/planning-sessions/"+id+"/events", nil)
	if got.status != http.StatusOK {
		t.Fatalf("events status = %d body = %s", got.status, got.raw)
	}
	items, _ := got.body["items"].([]any)
	types := map[string]bool{}
	for _, item := range items {
		row, _ := item.(map[string]any)
		typ, _ := row["type"].(string)
		types[typ] = true
	}
	for _, typ := range []string{"message", "inspection", "needs_input", "plan", "status", "done"} {
		if !types[typ] {
			t.Fatalf("event log missing %s: %s", typ, got.raw)
		}
	}
}

func TestGetUnknownSessionIsNotFound(t *testing.T) {
	t.Parallel()
	mux := newMux(t, application.NewService(nil))
	got := doJSON(t, mux, http.MethodGet, "/v1/planning-sessions/ps_missing", nil)
	if got.status != http.StatusNotFound || got.body["code"] != domain.CodeSessionNotFound {
		t.Fatalf("status = %d body = %s", got.status, got.raw)
	}
}

type scriptedPlanner struct{}

func (scriptedPlanner) Turn(_ context.Context, _ *domain.Session, _ string) (*application.TurnResult, error) {
	return &application.TurnResult{
		Messages:       []domain.Message{{Role: domain.RoleModel, Text: "Plan follows."}},
		Inspection:     &domain.Inspection{Language: "TypeScript", Kind: "static", ToolCalls: []string{"inspect_repo"}},
		NeedsInput:     []domain.Question{{ID: "sso", Name: "sso", Label: "Does this need SSO?", Type: domain.QuestionBoolean, Required: true}},
		Plan:           &domain.Plan{Text: "Package the static site.", SHA256: "abc"},
		ResolvedCommit: "0123456789abcdef0123456789abcdef01234567",
		Model:          "recorded-model",
		Profile:        domain.ProfileRef{Name: domain.ProfileOnboard, Version: "1", Hash: "deadbeef"},
	}, nil
}

func newMux(t *testing.T, service *application.Service) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, Config{Service: service, Authorize: func(*http.Request) error { return nil }})
	return mux
}

type jsonResponse struct {
	status int
	body   map[string]any
	raw    string
}

func doJSON(t *testing.T, mux http.Handler, method, path string, body any) jsonResponse {
	t.Helper()
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("content-type", "application/json")
	}
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	out := jsonResponse{status: response.Code, raw: strings.TrimSpace(response.Body.String()), body: map[string]any{}}
	if out.raw != "" {
		if err := json.Unmarshal(response.Body.Bytes(), &out.body); err != nil {
			t.Fatalf("decode %s: %v", out.raw, err)
		}
	}
	return out
}

type slowPlanner struct{ delay time.Duration }

func (p slowPlanner) Turn(ctx context.Context, session *domain.Session, message string) (*application.TurnResult, error) {
	time.Sleep(p.delay)
	return scriptedPlanner{}.Turn(ctx, session, message)
}

func TestTurnOutlivesTheServerWriteTimeout(t *testing.T) {
	t.Parallel()
	mux := newMux(t, application.NewService(slowPlanner{delay: 400 * time.Millisecond}))
	server := httptest.NewUnstartedServer(mux)
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	defer server.Close()
	created, err := http.Post(server.URL+"/v1/planning-sessions", "application/json", strings.NewReader(`{"repoUrl":"https://github.com/org/app","ref":"v1"}`))
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(created.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	_ = created.Body.Close()
	response, err := http.Post(server.URL+"/v1/planning-sessions/"+session.ID+"/turns", "application/json", strings.NewReader(`{"message":"plan it"}`))
	if err != nil {
		t.Fatalf("turn did not reach the client: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`"status"`)) {
		t.Fatalf("turn = %d %s (%v)", response.StatusCode, body, err)
	}
}
