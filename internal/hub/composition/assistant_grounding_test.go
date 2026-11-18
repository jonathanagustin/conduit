package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/assistant/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestAssistantDocumentationIsShippedAuditedAndRequiresAnOperator(t *testing.T) {
	handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{OperatorIdentityResolver: bearerOperators(nil), BootstrapAdministrators: []string{"ada"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{"", "ada"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/assistant/tools", strings.NewReader(`{"mode":"explore","tool":"search_product_documentation","arguments":{"query":"CONDUIT_MODEL_BASE_URL"}}`))
		request.Header.Set("Content-Type", "application/json")
		// A forged development header cannot authenticate the anonymous request.
		request.Header.Set("X-Platform-Subject", "ada")
		if subject != "" {
			request.Header.Set("Authorization", "Bearer "+subject)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if subject == "" {
			if response.Code != http.StatusUnauthorized && response.Code != http.StatusForbidden {
				t.Fatalf("anonymous doc read=%d %s", response.Code, response.Body.String())
			}
			continue
		}
		var result application.Result
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil || !result.OK || result.ReadOps != 1 || result.WriteOps != 0 || result.AuditEventID == "" || len(result.Sources) == 0 {
			t.Fatalf("native documentation not available: %d %s", response.Code, response.Body.String())
		}
		for _, source := range result.Sources {
			if !source.Valid() || source.AuditEventID != result.AuditEventID || source.RetrievedAt.IsZero() || !strings.Contains(source.Quote, "CONDUIT_MODEL_BASE_URL") {
				t.Fatalf("unbound documentation source: %+v", source)
			}
		}
	}
}
