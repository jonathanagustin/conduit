package composition

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	environmentsconnect "github.com/jonathanagustin/conduit/gen/go/conduit/environments/v1/environmentsv1connect"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestUnknownEnvironmentSummaryMethodIsNotServed(t *testing.T) {
	procedure := environmentsconnect.EnvironmentReadServiceListEnvironmentSummariesProcedure + "Unknown"
	for _, route := range HubRouteAuthorization() {
		if strings.Contains(route.Pattern, procedure) {
			t.Fatalf("contract-only method entered authorization register: %s", route.Pattern)
		}
	}
	handler := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"contract-reviewer"}})
	for _, subject := range []string{"", "reader-without-grants", "contract-reviewer"} {
		request := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader("{}"))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Connect-Protocol-Version", "1")
		if subject != "" {
			request.Header.Set("X-Platform-Subject", subject)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("unserved method as %q answered %d: %s", subject, response.Code, response.Body.String())
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/environments?view=summary", nil)
	request.Header.Set("X-Platform-Subject", "contract-reviewer")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"environments":[]`) {
		t.Fatalf("native REST fallback unavailable: %d %s", response.Code, response.Body.String())
	}
}
