package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/appgen/application"
	"github.com/jonathanagustin/conduit/internal/appgen/domain"
)

func TestCreateRequiresConfirmAndListsTheResult(t *testing.T) {
	store := &application.Memory{}
	service := application.Service{
		Generator: okGen{},
		Sandbox:   passSandbox{},
		Store:     store,
		Audit:     noopAudit{},
	}
	mux := http.NewServeMux()
	Register(mux, Config{Service: service, Authorize: func(*http.Request) error { return nil }})
	held := post(t, mux, `{"name":"hello-http","description":"a small page"}`)
	if held.Code != http.StatusOK || !strings.Contains(held.Body.String(), `"confirmRequired":true`) {
		t.Fatalf("held = %d %s", held.Code, held.Body.String())
	}
	created := post(t, mux, `{"name":"hello-http","description":"a small page","confirm":true}`)
	if created.Code != http.StatusOK || !strings.Contains(created.Body.String(), `"label":"No model call"`) {
		t.Fatalf("created = %d %s", created.Code, created.Body.String())
	}
	var result application.Result
	if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	got := get(t, mux, "/v1/app-generations/"+result.ID)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "Dockerfile") {
		t.Fatalf("get = %d %s", got.Code, got.Body.String())
	}
	listed := get(t, mux, "/v1/app-generations")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), result.ID) {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}
}

func TestUnauthorizedIsRejected(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return ErrUnauthorized }})
	response := post(t, mux, `{"name":"hello-http","description":"a small page","confirm":true}`)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func post(t *testing.T, mux *http.ServeMux, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/v1/app-generations", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func get(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

type okGen struct{}

func (okGen) Generate(context.Context, domain.Request) (domain.Spec, domain.Provenance, error) {
	return domain.Spec{
		Name: "hello-http", Port: 8080,
		Routes:    []domain.Route{{Method: "GET", Path: "/health", Status: 200, Body: "ok"}},
		Page:      domain.Page{Title: "Hello", Body: "Hello"},
		Resources: domain.Resources{Memory: "64Mi", CPU: "50m"},
		Files:     []domain.SourceFile{{Path: "main.go", Language: "go", Content: "package main\nfunc main() {}\n"}},
	}, domain.Provenance{Adapter: "recorded", PromptSHA256: strings.Repeat("a", 64), ResponseSHA256: strings.Repeat("b", 64)}, nil
}

type passSandbox struct{}

func (passSandbox) Build(context.Context, application.BuildRequest) (application.BuildResult, error) {
	return application.BuildResult{Passed: true, Reason: "built", ImageID: "local"}, nil
}

func (passSandbox) Check(context.Context, application.BuildResult, application.BuildRequest) (application.HealthResult, error) {
	return application.HealthResult{Passed: true, Reason: "ok"}, nil
}

type noopAudit struct{}

func (noopAudit) Record(context.Context, application.Audit) error { return nil }
