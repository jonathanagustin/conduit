package composition

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
)

func TestTimelineRequiresNativeEnvironmentRead(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	for _, tc := range []struct {
		actor, environment string
		status             int
	}{{"admin", "a-one", 200}, {"reader", "a-one", 200}, {"reader", "b-two", 403}, {"outsider", "a-one", 403}} {
		response := spaceRequest(f.handler, tc.actor, http.MethodGet, "/v1/environments/"+tc.environment+"/timeline", "")
		if response.Code != tc.status {
			t.Errorf("%s Environment %s: status=%d body=%s; want %d", tc.actor, tc.environment, response.Code, response.Body, tc.status)
		}
	}
}

func TestTimelinePreservesOriginalDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "private-plan", Plan: controlplane.Plan{Environment: "private-environment"}}); err != nil {
		t.Fatal(err)
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented string
		status             int
	}{{"alice", "", 200}, {"alice", "bob", 200}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403}} {
		response := as(t, handler, "/v1/environments/private-environment/timeline", tc.actor, tc.represented)
		if response.Code != tc.status || (tc.status == 200 && !strings.Contains(response.Body.String(), "private-plan")) || (tc.status != 200 && strings.Contains(response.Body.String(), "private-plan")) {
			t.Errorf("%s as %s: status=%d body=%s", tc.actor, tc.represented, response.Code, response.Body)
		}
	}
	secure := NewHubHandler(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"alice"}})
	for _, subject := range []string{"", "alice"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/environments/private-environment/timeline", nil)
		request.Header.Set("X-Platform-Subject", subject)
		response := httptest.NewRecorder()
		secure.ServeHTTP(response, request)
		if response.Code != 401 || strings.Contains(response.Body.String(), "private-plan") {
			t.Fatalf("untrusted identity %q: %d %s", subject, response.Code, response.Body)
		}
	}
}
