package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func TestAnonymousCallerIsRefused(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error {
		return StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
	}})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs/sample-app-1", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestUnconfiguredReaderIsNotAnApprovalSurface(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return nil }})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/packaging-runs/sample-app-1", nil)
	mux.ServeHTTP(response, request)
	if response.Code == http.StatusOK {
		t.Fatal("POST was accepted")
	}
	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs/sample-app-1", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.Bytes())
	}
	var body domain.RunStatus
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Configured || body.Authority != "packaging-run" || body.ApprovalAuthority != "conduit-governance" || body.PlanAuthority != "conduit-plan" {
		t.Fatalf("body = %#v", body)
	}
}

type fixedReader struct{ status domain.RunStatus }

func (r fixedReader) Read(context.Context, string) (domain.RunStatus, error) {
	return r.status, nil
}

func (r fixedReader) List(context.Context) ([]domain.RunStatus, error) {
	if r.status.RunID == "" {
		return nil, nil
	}
	return []domain.RunStatus{r.status}, nil
}

func (r fixedReader) Events(context.Context, string) ([]domain.StepEvent, error) {
	return nil, ErrNotFound
}

func TestReaderCannotRelabelTheAuthority(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{
		Authorize: func(*http.Request) error { return nil },
		Reader: fixedReader{status: domain.RunStatus{
			Authority: "temporal-approval", PlanID: "plan_abc", Phase: domain.PhaseReview,
		}},
	})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/packaging-runs/sample-app-1", nil))
	var body domain.RunStatus
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Authority != "packaging-run" || body.ApprovalAuthority != "conduit-governance" || body.PlanID != "plan_abc" {
		t.Fatalf("body = %#v", body)
	}
}
