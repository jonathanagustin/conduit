package composition

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	intelapp "github.com/jonathanagustin/conduit/internal/intelligence/application"
)

// A case an operator adds is written to the store the Hub was given, so a
// second Hub on the same store -- a restart, or another replica -- lists it.
// Without the store each Hub keeps its own list and the second one has only
// the built-in cases.
func TestOperatorAddedEvalCaseOutlivesTheHubThatStoredIt(t *testing.T) {
	t.Parallel()
	store := intelapp.NewMemoryCases()
	call := func(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Platform-Subject", "operator")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}
	hub := func(options Options) http.Handler {
		handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true}, options)
		if err != nil {
			t.Fatal(err)
		}
		return handler
	}
	const scenario = "A 3Gi limit on the restart-survival case"
	first := hub(Options{IntelligenceCases: store})
	added := call(first, http.MethodPost, "/v1/intelligence/evals/cases",
		`{"scenario":"`+scenario+`","boundObject":"sample-app-1","value":"3Gi","expectedOutcome":"PASS"}`)
	if added.Code != http.StatusCreated {
		t.Fatalf("adding a case answered %d %s", added.Code, added.Body)
	}
	restarted := hub(Options{IntelligenceCases: store})
	if listed := call(restarted, http.MethodGet, "/v1/intelligence/evals", ""); listed.Code != http.StatusOK || !strings.Contains(listed.Body.String(), scenario) {
		t.Fatalf("a Hub restarted on the same store does not list the case: %d %s", listed.Code, listed.Body)
	}
	// The control: a Hub given no store never saw it.
	if listed := call(hub(Options{}), http.MethodGet, "/v1/intelligence/evals", ""); strings.Contains(listed.Body.String(), scenario) {
		t.Fatal("a Hub with no shared store lists another Hub's case")
	}
}
