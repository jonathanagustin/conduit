package controlplane

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestValidateApolloProductManifestEndpointWithReconstructedFixture(t *testing.T) {
	body, err := os.ReadFile("../../docs/reference-platform/cli-fixtures/reconstructed/0.814.0/deployment/manifest.yml")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/apollo/product-manifests/validate", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/yaml")
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var result struct {
		Valid    bool                  `json:"valid"`
		Errors   []ValidationError     `json:"errors"`
		Manifest ApolloProductManifest `json:"manifest"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Valid || len(result.Errors) != 0 || result.Manifest.ProductGroup != "com.example.conformance" {
		t.Fatalf("result = %#v", result)
	}
}

func TestValidateApolloProductManifestEndpointRejectsInvalidManifest(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/apollo/product-manifests/validate",
		bytes.NewBufferString("manifest-version: '1.0'\nproduct-version: 1.2\n"),
	)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestValidateApolloProductManifestEndpointRejectsMalformedYAML(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/apollo/product-manifests/validate",
		bytes.NewBufferString("manifest-version: ["),
	)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
