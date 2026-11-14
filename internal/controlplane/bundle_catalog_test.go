package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBundleCatalogListsStoredBundlesAndRefusesAnonymous(t *testing.T) {
	repository := NewMemoryRepository()
	if _, err := repository.CreateBundle(context.Background(), BundleEnvelope{
		Manifest:       BundleManifest{ID: "bndl-sample", PipelineID: "sample-app"},
		ManifestDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}); err != nil {
		t.Fatal(err)
	}
	open := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := httptest.NewRecorder()
	open.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/bundles", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}
	var page struct {
		Bundles []bundleCatalogItem `json:"bundles"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Bundles) != 1 || page.Bundles[0].ID != "bndl-sample" || page.Bundles[0].PipelineID != "sample-app" || page.Bundles[0].ManifestDigest == "" {
		t.Fatalf("bundles = %#v", page.Bundles)
	}

	closed := NewHandler(repository, HandlerOptions{})
	anonymous := httptest.NewRecorder()
	closed.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/v1/bundles", nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d %s", anonymous.Code, anonymous.Body.String())
	}
}
