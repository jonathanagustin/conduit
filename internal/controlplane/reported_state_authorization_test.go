package controlplane

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// GET /v1/environments/{id}/reported-state shipped with no principal check and
// no resource check, and nothing tested the route, which is how it survived. An
// adversarial review of the authorization boundary found it as the only such
// route among roughly sixty.
//
// The reasoning is the one already written on the GraphQL interface: a read
// surface that skipped authentication because it was new would be a way around
// Governance rather than a view of it. This Report is not a thin one -- it names
// every installation with its release and config digests, the namespaces, and
// each secret's provider reference, Kubernetes secret name and key names -- so
// serving it openly was fleet topology and secret-provider reconnaissance for
// any environment id a caller cared to guess.
//
// These tests exist so the next person to touch the route finds out.

func reportedStateRequest(t *testing.T, handler http.Handler, subject, environment string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/v1/environments/"+environment+"/reported-state", nil)
	if subject != "" {
		request.Header.Set("X-Platform-Subject", subject)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestReportedStateRefusesAnUnauthenticatedCaller(t *testing.T) {
	t.Parallel()
	// No insecure-header option, so no identity can be presented at all -- the
	// same construction TestApolloGraphQLRequiresAnOperator uses.
	handler := NewHandler(NewMemoryRepository())

	response := reportedStateRequest(t, handler, "", "dev-use2")

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated = %d %s", response.Code, response.Body.String())
	}
	// A refusal that leaked the report would defeat the point of refusing.
	for _, disclosed := range []string{"installations", "namespaces", "secrets"} {
		if strings.Contains(response.Body.String(), disclosed) {
			t.Errorf("the refusal disclosed %q to an unauthenticated caller: %s", disclosed, response.Body.String())
		}
	}
}

// Authenticating is not the same as being allowed. A principal with no grant on
// this environment must not read its report, or the check would only be
// establishing that somebody is on the other end.
func TestReportedStateRefusesAnOperatorWithoutAGrant(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})

	response := reportedStateRequest(t, handler, "someone", "dev-use2")

	if response.Code != http.StatusForbidden {
		t.Fatalf("ungranted operator = %d %s", response.Code, response.Body.String())
	}
}

// And the route still works for the callers that already authenticated -- the
// operator CLI and the spoke end-to-end script both present an identity today,
// so adding the check took nothing away from them.
func TestReportedStateServesAGrantedOperator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	// The handler builds its own governance service over the same repository,
	// so seeding through this one is what the route will read.
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, Team{Name: "fleet-readers", DisplayName: "Fleet readers", AdminUsers: []string{"owner"}, MemberUsers: []string{"someone"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("dev-use2"),
		Team:        "fleet-readers",
		Operations:  []string{OperationReadResource},
	}); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := reportedStateRequest(t, handler, "someone", "dev-use2")

	// No report has been submitted for this environment, so not-found is the
	// correct answer -- what matters is that the caller got past the door.
	if response.Code == http.StatusUnauthorized || response.Code == http.StatusForbidden {
		t.Fatalf("granted operator was refused: %d %s", response.Code, response.Body.String())
	}
}
