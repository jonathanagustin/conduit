package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/recorded"
	"github.com/jonathanagustin/conduit/internal/devworkspace/application"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func TestAnonymousAndCrossUserAreRefused(t *testing.T) {
	handler, store := newHandler(t)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/dev-workspaces", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d", response.Code)
	}
	created := createAs(t, handler, "ada", "dev")
	other := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/dev-workspaces/"+created+"/runtime", nil)
	request.Header.Set("X-Test-User", "bea")
	handler.ServeHTTP(other, request)
	if other.Code != http.StatusForbidden {
		t.Fatalf("cross user start = %d %s", other.Code, other.Body.String())
	}
	if _, err := store.Get(context.Background(), created); err != nil {
		t.Fatal(err)
	}
}

func newHandler(t *testing.T) (http.Handler, *recorded.Store) {
	t.Helper()
	store := recorded.New()
	service := application.New(application.Dependencies{
		Records: store, Files: store.Files(), Runtime: store, Pods: store.Pods(),
		Submitter: &allowSubmit{}, Auditor: &nopAudit{}, Now: func() time.Time { return time.Unix(0, 0).UTC() },
		PodsEnabled: true,
	})
	mux := http.NewServeMux()
	Register(mux, Config{
		Service: service,
		Authorize: func(r *http.Request) error {
			if r.Header.Get("X-Test-User") == "" {
				return StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			return nil
		},
		Caller: func(r *http.Request) (domain.Caller, error) {
			return domain.Caller{Username: r.Header.Get("X-Test-User"), Teams: []string{"Developer"}}, nil
		},
		Visible: func(*http.Request, string) bool { return true },
		Permit: func(r *http.Request, _ string) error {
			if r.Header.Get("X-Test-User") == "" {
				return StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
			}
			return nil
		},
	})
	return mux, store
}

func createAs(t *testing.T, handler http.Handler, user, environment string) string {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/dev-workspaces", strings.NewReader(`{"profileId":"profile","environmentId":"`+environment+`","title":"Sample"}`))
	request.Header.Set("X-Test-User", user)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	start := strings.Index(body, `"workspaceId":"`)
	if start < 0 {
		t.Fatal(body)
	}
	rest := body[start+len(`"workspaceId":"`):]
	return rest[:strings.Index(rest, `"`)]
}

type nopAudit struct{}

func (nopAudit) Record(context.Context, string, string, string, string, string) error { return nil }

type allowSubmit struct{}

func (allowSubmit) Submit(context.Context, domain.Caller, domain.Submission) (domain.Receipt, error) {
	return domain.Receipt{ChangeRequestID: "crq_test", Status: "PENDING_APPROVALS"}, nil
}
