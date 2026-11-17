package controlplane

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
)

// The production resolver returns Identity's ErrUnauthorized. A Hub verifying
// real tokens must answer a refused credential with 401 -- it answered 500
// while this package declared an unrelated error with the same text.
func TestACredentialTheResolverRefusesIsAnswered401(t *testing.T) {
	t.Parallel()
	if !errors.Is(ErrUnauthorized, identity.ErrUnauthorized) {
		t.Fatal("controlplane.ErrUnauthorized is not Identity's ErrUnauthorized")
	}
	for name, refusal := range map[string]error{
		"bare": identity.ErrUnauthorized, "wrapped": fmt.Errorf("%w: with a reason", identity.ErrUnauthorized),
	} {
		handler := NewHandler(NewMemoryRepository(), HandlerOptions{
			OperatorIdentityResolver: func(*http.Request) (IdentityClaims, error) { return IdentityClaims{}, refusal },
		})
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/session", nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d %s, want 401", name, response.Code, response.Body.String())
		}
	}
}

func TestOperatorIdentityStaysDisabledUntilAProviderIsConfigured(t *testing.T) {
	t.Parallel()
	if got := operatorIdentityDocument(OperatorSignIn{}); got["enabled"] != false {
		t.Fatalf("document = %#v", got)
	}
	handler := NewHandler(NewMemoryRepository())
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/operator-identity", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d %s", response.Code, response.Body.String())
	}
	var body struct {
		Enabled   bool  `json:"enabled"`
		Providers []any `json:"providers"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Enabled || len(body.Providers) != 0 {
		t.Fatalf("body = %s err=%v", response.Body.String(), err)
	}
}

func TestOperatorIdentityPublishesTheProviderWithoutASecret(t *testing.T) {
	t.Parallel()
	document := operatorIdentityDocument(OperatorSignIn{
		Issuer: "https://identity.example.test/realms/operators", ClientID: "console",
	})
	if document["enabled"] != true {
		t.Fatal(document)
	}
	providers, _ := document["providers"].([]any)
	provider, _ := providers[0].(map[string]any)
	if provider["issuer"] == "" || provider["clientId"] != "console" {
		t.Fatal(document)
	}
	if _, present := provider["authorizationEndpoint"]; present {
		t.Fatal("an unpublished browser address must be omitted")
	}
	withAddress := operatorIdentityDocument(OperatorSignIn{
		Issuer: "https://identity.example.test/realms/operators", ClientID: "console",
		AuthorizationEndpoint: "https://identity.example.test/realms/operators/protocol/openid-connect/auth",
	})
	providers, _ = withAddress["providers"].([]any)
	provider, _ = providers[0].(map[string]any)
	if provider["authorizationEndpoint"] == "" {
		t.Fatal(withAddress)
	}
}
