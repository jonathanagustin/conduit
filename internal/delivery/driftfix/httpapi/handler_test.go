package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/delivery/driftfix/application"
)

func TestAnonymousProposeFixIsRefused(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{Authorize: func(*http.Request) error { return http.ErrNoCookie }})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/fleet/propose-fix", strings.NewReader(`{}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", response.Code)
	}
}

func TestProposeFixReturnsTheChangeRequest(t *testing.T) {
	mux := http.NewServeMux()
	Register(mux, Config{
		Authorize: func(*http.Request) error { return nil },
		Propose: func(*http.Request, string, string) (application.Result, error) {
			return application.Result{Allowed: true, Reason: "desired version 1.2.0, observed version 1.1.0", ChangeRequestID: "crq_1", Status: "PENDING_APPROVALS", AuditID: "aud_1"}, nil
		},
	})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/fleet/propose-fix", strings.NewReader(`{"environment":"east","installationId":"ledger"}`))
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || !strings.Contains(response.Body.String(), "crq_1") {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
}
