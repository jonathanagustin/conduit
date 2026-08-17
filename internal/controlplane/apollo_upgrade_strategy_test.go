package controlplane

import (
	"encoding/json"
	"net/http"
	"testing"
)

// publishHelmBundle plumbs the Product manifest's helm chart, artifacts and
// dependencies into the publication and, until this test existed, dropped
// upgrade-strategy on the floor -- so a publisher who declared one was told
// they had published it and Conduit answered as though they had said nothing.
//
// This is the end-to-end assertion for that plumbing, and it exists because the
// unit tests in internal/apollo/application cannot see it: they construct
// NativeHelmPublication directly, so deleting the assignment at this edge left
// every one of them green. That was a real mutation survivor, not a
// hypothetical one.
func TestADeclaredUpgradeStrategySurvivesTheBundlePublicationRoute(t *testing.T) {
	t.Parallel()
	handler := Handler()
	bundle := exactHelmBundle()
	bundle.Manifest.Extensions.UpgradeStrategy = ApolloUpgradeStrategyRolling
	response := requestJSON(t, handler, http.MethodPost, "/v1/product-publications/helm-chart", bundle)
	if response.Code != http.StatusOK {
		t.Fatalf("publish = %d %s", response.Code, response.Body.String())
	}

	response = requestJSON(t, handler, http.MethodGet,
		"/api/v0/products/com.example:application/releases/2.0.0", nil)
	var raw map[string]any
	if response.Code != http.StatusOK {
		t.Fatalf("get release = %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["upgradeStrategy"] != ApolloUpgradeStrategyRolling {
		t.Fatalf("the publisher's declared upgrade strategy did not reach the served Release: %#v", raw)
	}
}

// The other half, and the reason the assertion above is on the raw body: a
// manifest that omits the optional key must produce a Release that omits it
// too. Without this, plumbing the field could be "satisfied" by defaulting it
// at the edge, which is the original defect wearing a different hat.
func TestABundleThatDeclaresNoUpgradeStrategyPublishesNone(t *testing.T) {
	t.Parallel()
	handler := Handler()
	response := requestJSON(t, handler, http.MethodPost, "/v1/product-publications/helm-chart", exactHelmBundle())
	if response.Code != http.StatusOK {
		t.Fatalf("publish = %d %s", response.Code, response.Body.String())
	}

	response = requestJSON(t, handler, http.MethodGet,
		"/api/v0/products/com.example:application/releases/2.0.0", nil)
	var raw map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if value, present := raw["upgradeStrategy"]; present {
		t.Fatalf("a manifest declaring no upgrade strategy served %#v", value)
	}
}
