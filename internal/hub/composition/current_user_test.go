package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

func currentUserRequest(handler http.Handler, subject, onBehalfOf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/apollo/graphql", strings.NewReader(`{"query":"query GetCurrentUser { me { id fullName } }"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform-Subject", subject)
	if onBehalfOf != "" {
		req.Header.Set("X-Platform-Service-Account", "true")
		req.Header.Set("X-Platform-On-Behalf-Of", onBehalfOf)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}

func assertCurrentUser(t *testing.T, response *httptest.ResponseRecorder, id string) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d %s", response.Code, response.Body.String())
	}
	var body struct {
		Data       struct{ Me map[string]any }
		Errors     []any
		Extensions struct {
			Provenance struct{ ConduitMintedValues []string }
		}
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Errors) > 0 || body.Data.Me["id"] != id {
		t.Fatalf("identity mismatch: %s", response.Body.String())
	}
	name, exists := body.Data.Me["fullName"]
	if !exists || name != nil {
		t.Fatal("missing display-name claim must be explicit null, never a fabricated name")
	}
	for _, coordinate := range []string{"Query.me", "ConduitCurrentUser.id", "ConduitCurrentUser.fullName"} {
		found := false
		for _, value := range body.Extensions.Provenance.ConduitMintedValues {
			found = found || coordinate == value
		}
		if !found {
			t.Fatalf("missing native value disclosure for %s: %s", coordinate, response.Body.String())
		}
	}
}

func TestCurrentUserUsesVerifiedIdentityAndRejectsAnonymous(t *testing.T) {
	t.Setenv("PLAN_ORCHESTRATOR", "native")
	anonymous := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{})
	response := currentUserRequest(anonymous, "forged-owner", "forged-delegate")
	if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), `"me"`) {
		t.Fatalf("anonymous=%d %s", response.Code, response.Body.String())
	}
	secure := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{
		BootstrapAdministrators: []string{"verified-owner"},
		OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
			return controlplane.IdentityClaims{Username: "verified-owner"}, nil
		},
	})
	assertCurrentUser(t, currentUserRequest(secure, "forged-owner", "forged-delegate"), "verified-owner")
}

func TestCurrentUserKeepsCredentialOwnerDuringDelegation(t *testing.T) {
	t.Setenv("PLAN_ORCHESTRATOR", "native")
	handler, repository, _ := delegationHub(t)
	service := controlplane.NewGovernanceService(repository)
	grants, err := repository.ListResourceGrants(t.Context(), "platform/root")
	if err != nil || len(grants) != 1 {
		t.Fatalf("delegation fixture grants: %v %v", grants, err)
	}
	grant := grants[0]
	grant.Operations = append(grant.Operations, governance.OperationReadResource)
	if _, err := service.PutGrant(t.Context(), grant); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ owner, delegate string }{{"alice", ""}, {"alice", "bob"}, {"robot", "alice"}} {
		t.Run(tc.owner+"/"+tc.delegate, func(t *testing.T) { assertCurrentUser(t, currentUserRequest(handler, tc.owner, tc.delegate), tc.owner) })
	}
	denied := currentUserRequest(handler, "bob", "alice")
	if denied.Code == http.StatusOK || strings.Contains(denied.Body.String(), `"me"`) {
		t.Fatalf("unapproved delegation disclosed identity: %d %s", denied.Code, denied.Body.String())
	}
}
