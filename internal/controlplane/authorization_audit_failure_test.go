package controlplane

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

type failingAuditPageRepository struct {
	Repository
	reads int
}

func (repository *failingAuditPageRepository) PageAuthorizationAudit(context.Context, AuthorizationAuditPageQuery) (AuthorizationAuditPage, error) {
	repository.reads++
	return AuthorizationAuditPage{}, errors.New("PRIVATE-AUDIT-READ-DIAGNOSTIC")
}

func TestAuthorizationAuditUnavailableDoesNotExposeBackendDiagnostics(t *testing.T) {
	for _, view := range []string{"events", "chain"} {
		t.Run(view, func(t *testing.T) {
			repository := &failingAuditPageRepository{Repository: NewMemoryRepository()}
			response := governanceRequest(t, auditPageHandler(repository), http.MethodGet, "/v1/authorization-audit?view="+view, "admin", "", nil)
			if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "PRIVATE-AUDIT-READ-DIAGNOSTIC") || !strings.Contains(response.Body.String(), "authorization audit is unavailable") {
				t.Errorf("private backend failure exposed: %d %s", response.Code, response.Body.String())
			}
			if repository.reads != 1 {
				t.Fatal("fixture did not read requested page", repository.reads)
			}
		})
	}
}

func TestAuthorizationAuditRefusalsPrecedePageReads(t *testing.T) {
	for _, test := range []struct {
		name, user, query string
		status            int
	}{
		{"anonymous", "", "", http.StatusUnauthorized},
		{"non-administrator", "viewer", "", http.StatusForbidden},
		{"invalid cursor", "admin", "?cursor=broken", http.StatusUnprocessableEntity},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &failingAuditPageRepository{Repository: NewMemoryRepository()}
			handler := auditPageHandler(repository)
			if test.user == "" {
				handler = NewHandler(repository, HandlerOptions{})
			}
			response := governanceRequest(t, handler, http.MethodGet, "/v1/authorization-audit"+test.query, test.user, "", nil)
			if response.Code != test.status || repository.reads != 0 || strings.Contains(response.Body.String(), "PRIVATE-AUDIT-READ-DIAGNOSTIC") {
				t.Fatalf("refusal reached private page: %d reads=%d %s", response.Code, repository.reads, response.Body.String())
			}
		})
	}
}
