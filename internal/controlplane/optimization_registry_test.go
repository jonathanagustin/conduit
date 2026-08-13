package controlplane

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestOptimizationAcquisitionRegistryNamesRegisteredReadOnlyRoutes(t *testing.T) {
	handler := NewHandler(NewMemoryRepository())
	_, calls := optimization.PlatformStateAcquisitionRegistry()
	for _, call := range calls {
		if call.Method != http.MethodGet {
			t.Fatalf("acquisition call can mutate: %#v", call)
		}
		path := strings.ReplaceAll(strings.ReplaceAll(call.Endpoint, "{environmentID}", "test-environment"), "{planID}", "pln_test")
		request := httptest.NewRequest(http.MethodPost, path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s is not a registered GET-only route: status %d", call.Endpoint, response.Code)
		}
	}
}
