package composition

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestNewHubHandlerPreservesCompatibilitySurface(t *testing.T) {
	t.Parallel()
	handler := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{
		AllowInsecureAgentIdentityHeaders: true, AllowInsecureOperatorIdentityHeaders: true,
	})
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/health/live", http.StatusOK},
		{http.MethodGet, "/health/ready", http.StatusOK},
		{http.MethodGet, "/version", http.StatusOK},
		{http.MethodPost, "/health/live", http.StatusMethodNotAllowed},
	} {
		t.Run(test.method+" "+test.path, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
		})
	}
}
