package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/intelligence/application"
)

type brokenCases struct{}

func (brokenCases) Add(context.Context, application.Case, int) (application.Case, error) {
	return application.Case{}, errors.New("dial tcp 10.0.0.7:5432: connection refused")
}
func (brokenCases) List(context.Context) ([]application.Case, error) {
	return nil, errors.New("dial tcp 10.0.0.7:5432: connection refused")
}

func serve(t *testing.T, service *application.Service, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	Register(mux, Config{API: service, Authorize: func(*http.Request) error { return nil }})
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(recorder, request)
	return recorder
}

func TestAddedCaseIsListedThroughTheStore(t *testing.T) {
	service := application.New("", "")
	added := serve(t, service, http.MethodPost, "/v1/intelligence/evals/cases", `{"scenario":"operator rejected 2Gi","boundObject":"a","value":"2Gi","expectedOutcome":"FAIL_REQUIRED"}`)
	if added.Code != http.StatusCreated {
		t.Fatalf("add = %d %s", added.Code, added.Body.String())
	}
	listed := serve(t, service, http.MethodGet, "/v1/intelligence/evals", "")
	if listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), "operator rejected 2Gi") {
		t.Fatalf("list = %d %s", listed.Code, listed.Body.String())
	}
	invalid := serve(t, service, http.MethodPost, "/v1/intelligence/evals/cases", `{"scenario":"","expectedOutcome":"PASS"}`)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("an invalid case = %d", invalid.Code)
	}
}

// A store fault is the server's failure, not the caller's bad request, and
// its detail does not leave the server.
func TestStoreFaultIsAServerErrorWithNoDetail(t *testing.T) {
	service := application.New("", "")
	service.Stored = brokenCases{}
	for _, response := range []*httptest.ResponseRecorder{
		serve(t, service, http.MethodGet, "/v1/intelligence/evals", ""),
		serve(t, service, http.MethodPost, "/v1/intelligence/evals/cases", `{"scenario":"s","expectedOutcome":"PASS"}`),
	} {
		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500: %s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "10.0.0.7") {
			t.Fatalf("the store's error detail was returned: %s", response.Body.String())
		}
	}
}
