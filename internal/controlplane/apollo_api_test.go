package controlplane

// Pinned OpenAPI operation coverage:
// publishHelmChart, publishService, listProducts, getProduct, listReleases,
// getRelease, listEnvironments, getEnvironment, listInstallations,
// getInstallation, putStatusEntitiesInfo, putStatusEntitiesHealth,
// listNamespaces.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo"
	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/apollo/workloadmetadata"
)

func TestApolloPublicAPIPublishAndReadCatalog(t *testing.T) {
	handler := Handler()
	publish := map[string]any{
		"extensions": map[string]any{
			"helmChart": map[string]any{
				"helmChartName": "fixture", "helmChartVersion": "1.2.3",
				"helmRepositoryUrl": "oci://registry.example/charts/fixture",
			},
			"artifacts": []map[string]any{{"artifactUrl": "registry.example/fixture:1.2.3"}},
			"productDependencies": []map[string]any{{
				"productGroup": "com.example", "productName": "database",
				"minimumVersion": "2.0.0", "maximumVersion": "2.x.x",
			}},
		},
	}
	response := requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/helmChart/com.example:fixture:1.2.3/publish", publish)
	if response.Code != http.StatusOK || response.Body.String() != "{}\n" {
		t.Fatalf("publish = %d %s", response.Code, response.Body.String())
	}

	response = requestJSON(t, handler, http.MethodGet, "/api/v0/products", nil)
	var products struct {
		Products []apolloProduct `json:"products"`
	}
	decodeResponse(t, response, http.StatusOK, &products)
	if len(products.Products) != 1 || products.Products[0].ID != "com.example:fixture" {
		t.Fatalf("products = %#v", products)
	}

	response = requestJSON(t, handler, http.MethodGet, "/api/v0/products/com.example:fixture", nil)
	var product apolloProduct
	decodeResponse(t, response, http.StatusOK, &product)
	if product.Group != "com.example" {
		t.Fatalf("product = %#v", product)
	}

	response = requestJSON(t, handler, http.MethodGet, "/api/v0/products/com.example:fixture/releases", nil)
	var releases struct {
		Releases []apolloRelease `json:"releases"`
	}
	decodeResponse(t, response, http.StatusOK, &releases)
	if len(releases.Releases) != 1 || releases.Releases[0].MavenCoordinate != "com.example:fixture:1.2.3" ||
		len(releases.Releases[0].ProductDependencies) != 1 || releases.Releases[0].ProductDependencies[0].Optional {
		t.Fatalf("releases = %#v", releases)
	}

	response = requestJSON(t, handler, http.MethodGet, "/api/v0/products/com.example:fixture/releases/1.2.3", nil)
	var release apolloRelease
	decodeResponse(t, response, http.StatusOK, &release)
	if release.RecallStatus.Recalled {
		t.Fatalf("release = %#v", release)
	}
	// Asserted on the raw body rather than on apolloRelease, whose
	// UpgradeStrategy is a plain string and so reads an absent key and an empty
	// one identically -- which is exactly the distinction that matters here. No
	// publish request in the pinned contract carries an upgrade strategy, so
	// nobody stated one and the honest answer is to say nothing. Serving "" for
	// it would be worse than the constant this replaced: "" is not a member of
	// the pinned enum at all.
	var raw map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if value, present := raw["upgradeStrategy"]; present {
		t.Fatalf("a Release nobody declared a strategy for served upgradeStrategy=%#v", value)
	}
}

func TestApolloPublicAPIPublishHelmChartDoesNotAdmitPlatformNativeBundleFields(t *testing.T) {
	handler := Handler()
	digest := "sha256:" + strings.Repeat("a", 64)
	payload := map[string]any{"extensions": map[string]any{
		"helmChart": map[string]any{
			"helmChartName": "fixture", "helmChartVersion": "1.2.3",
			"helmRepositoryUrl": "oci://registry.example/charts/fixture:1.2.3@" + digest,
		},
		"artifacts":      []map[string]any{{"artifactUrl": "registry.example/fixture:1.2.3@" + digest}},
		"platformBundle": map[string]any{"defaultConfig": []byte("replicaCount: 3\n")},
	}}
	response := requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/helmChart/com.example:fixture:1.2.3/publish", payload)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "unknown field") {
		t.Fatalf("public adapter admitted Platform-native field: %d %s", response.Code, response.Body.String())
	}

	// ProductDependencyLocator in the pinned publication request does not carry
	// the optional flag used by the Release response and Conduit's native
	// dependency model. Similar vocabulary is not authority to add the field.
	delete(payload["extensions"].(map[string]any), "platformBundle")
	payload["extensions"].(map[string]any)["productDependencies"] = []map[string]any{{
		"productGroup": "com.example", "productName": "database",
		"minimumVersion": "2.0.0", "maximumVersion": "2.x.x", "optional": true,
	}}
	response = requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/helmChart/com.example:fixture:1.2.3/publish", payload)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `unknown field \"optional\"`) {
		t.Fatalf("public adapter admitted field absent from pinned OpenAPI: %d %s", response.Code, response.Body.String())
	}
}

func TestApolloPublicAPIExactPublicationPersistsAndProjectsIntoCatalog(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	digest := "sha256:" + strings.Repeat("a", 64)
	payload := map[string]any{"extensions": map[string]any{
		"helmChart": map[string]any{
			"helmChartName": "fixture", "helmChartVersion": "1.2.3",
			"helmRepositoryUrl": "oci://registry.example/charts/fixture:1.2.3@" + digest,
		},
		"artifacts": []map[string]any{{"artifactUrl": "registry.example/fixture:1.2.3@" + digest}},
	}}
	response := requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/helmChart/com.example:fixture:1.2.3/publish", payload)
	if response.Code != http.StatusOK {
		t.Fatalf("publish = %d %s", response.Code, response.Body.String())
	}

	catalog, err := ListProductCatalog(context.Background(), repository)
	if err != nil || len(catalog) != 1 || len(catalog[0].Releases) != 1 || catalog[0].Releases[0].Chart.Digest != digest {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewHandler(reopened, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response = requestJSON(t, restarted, http.MethodGet, "/api/v0/products/com.example:fixture/releases", nil)
	var releases struct {
		Releases []apolloRelease `json:"releases"`
	}
	decodeResponse(t, response, http.StatusOK, &releases)
	if len(releases.Releases) != 1 || releases.Releases[0].MavenCoordinate != "com.example:fixture:1.2.3" {
		t.Fatalf("restarted releases=%#v", releases.Releases)
	}
}

func TestPlatformNativeAPIContentAddressesHelmConfigurationBundle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	bundle := exactHelmBundle()
	originalDefaultConfig := append([]byte(nil), bundle.DefaultConfig...)
	path := "/v1/product-publications/helm-chart"
	response := requestJSON(t, handler, http.MethodPost, path, bundle)
	if response.Code != http.StatusOK {
		t.Fatalf("publish=%d %s", response.Code, response.Body.String())
	}
	publications, err := repository.ListProductPublications(context.Background(), "com.example:application")
	if err != nil || len(publications) != 1 {
		t.Fatalf("publications=%#v err=%v", publications, err)
	}
	publication := publications[0]
	for name, expected := range map[string]struct {
		asset    *ProductPublicationAsset
		contents []byte
	}{
		"default config": {publication.DefaultConfig, bundle.DefaultConfig},
		"config schema":  {publication.ConfigSchema, bundle.ConfigSchema},
	} {
		if expected.asset == nil || expected.asset.Digest == "" || expected.asset.Size != int64(len(expected.contents)) {
			t.Fatalf("%s asset=%#v", name, expected.asset)
		}
		_, reader, err := repository.OpenBlob(context.Background(), expected.asset.Digest)
		if err != nil {
			t.Fatal(err)
		}
		got := new(bytes.Buffer)
		_, _ = got.ReadFrom(reader)
		_ = reader.Close()
		if !bytes.Equal(got.Bytes(), expected.contents) {
			t.Fatalf("%s bytes=%q", name, got.Bytes())
		}
	}

	// Repeating exact bytes is idempotent, but changing configuration at the
	// same immutable Product coordinate must conflict.
	if response = requestJSON(t, handler, http.MethodPost, path, bundle); response.Code != http.StatusOK {
		t.Fatalf("idempotent publish=%d %s", response.Code, response.Body.String())
	}
	bundle.DefaultConfig = []byte("replicaCount: 4\n")
	if response = requestJSON(t, handler, http.MethodPost, path, bundle); response.Code != http.StatusConflict {
		t.Fatalf("changed bundle publish=%d %s", response.Code, response.Body.String())
	}

	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.ListProductPublications(context.Background(), "com.example:application")
	if err != nil || len(persisted) != 1 || persisted[0].DefaultConfig == nil ||
		persisted[0].DefaultConfig.Digest != publication.DefaultConfig.Digest {
		t.Fatalf("persisted=%#v err=%v", persisted, err)
	}
	_, reader, err := reopened.OpenBlob(context.Background(), persisted[0].DefaultConfig.Digest)
	if err != nil {
		t.Fatal(err)
	}
	got := new(bytes.Buffer)
	_, _ = got.ReadFrom(reader)
	_ = reader.Close()
	if !bytes.Equal(got.Bytes(), originalDefaultConfig) {
		t.Fatalf("restarted default config=%q", got.Bytes())
	}
}

func TestApolloHealthStatusValidatesAggregatesAndPersistsCanonicalDetails(t *testing.T) {
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureAgentIdentityHeaders: true, AllowInsecureOperatorIdentityHeaders: true,
	})
	response := requestJSON(t, handler, http.MethodPost, "/v1/plans", foundation())
	if response.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", response.Code, response.Body.String())
	}
	now := time.Now().UTC().Add(-time.Minute)
	response = requestApolloStatusJSON(t, handler, "/api/v0/status/environments/dev-use2/entities/info", map[string]any{
		"entitiesInfo": []map[string]any{{
			"entityType": "HELM_CHART", "id": "example-api", "productGroup": "app.example", "productName": "example-api",
			"k8sNamespace": "example", "quorum": "ALL",
			"nodesInfo": []map[string]any{
				{"creationTimestamp": now, "hostId": "host-a", "nodeId": "node-a", "productVersion": "0.1.0"},
				{"creationTimestamp": now, "hostId": "host-b", "nodeId": "node-b", "productVersion": "0.1.0"},
			},
		}},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("info = %d %s", response.Code, response.Body.String())
	}
	response = requestApolloStatusJSON(t, handler, "/api/v0/status/environments/dev-use2/entities/health", map[string]any{
		"entitiesHealth": []map[string]any{
			{"productGroup": "app.example", "productName": "example-api", "productVersion": "0.1.0", "nodeId": "node-a", "containerStatuses": []map[string]any{{"containerId": "api", "liveness": true, "readiness": true}}},
			{"productGroup": "app.example", "productName": "example-api", "productVersion": "0.1.0", "nodeId": "node-b", "containerStatuses": []map[string]any{{
				"containerId": "api", "liveness": true, "readiness": true,
				"checks": []map[string]any{{"id": "BACKLOG", "message": "queue growing", "params": map[string]any{"depth": 12}, "result": "WARNING"}},
			}}},
		},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("health = %d %s", response.Code, response.Body.String())
	}
	report, err := repository.GetEnvironmentReport(context.Background(), "dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	var api ReportedEntity
	for _, entity := range report.Entities {
		if entity.Product == "app.example:example-api" {
			api = entity
		}
	}
	if api.Health != HealthWarning || api.HealthDetails == nil || api.HealthDetails.Quorum != "ALL" || len(api.HealthDetails.Nodes) != 2 ||
		api.HealthDetails.Nodes[1].Containers[0].Checks[0].Result != HealthWarning {
		t.Fatalf("reported health = %#v", api)
	}

	response = requestApolloStatusJSON(t, handler, "/api/v0/status/environments/dev-use2/entities/health", map[string]any{
		"entitiesHealth": []apolloEntityHealth{{
			ProductGroup: "app.example", ProductName: "example-api", ProductVersion: "0.1.0", NodeID: "bad",
			ContainerStatuses: []apolloContainerStatus{{ContainerID: "api", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{ID: "bad", Message: "bad", Params: map[string]any{}, Result: "INVALID"}}}},
		}},
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid health = %d %s", response.Code, response.Body.String())
	}

	response = requestApolloStatusJSON(t, handler, "/api/v0/status/environments/dev-use2/entities/health", map[string]any{
		"entitiesHealth": []apolloEntityHealth{{
			ProductGroup: "app.example", ProductName: "example-api", ProductVersion: "0.1.0", NodeID: "large",
			ContainerStatuses: []apolloContainerStatus{{ContainerID: "api", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{ID: "large", Message: strings.Repeat("x", 10_000), Params: map[string]any{}, Result: HealthError}}}},
		}},
	})
	// The 10,000-byte bound is per Entity, so an oversized Entity is a
	// validation failure like the native path's, not a request-size failure.
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("oversized health = %d %s", response.Code, response.Body.String())
	}
}

// A fleet reports one entitiesHealth element per (Product, node), so a bound
// meant for a single Entity must not decide how many replicas an Environment
// may report. Fifty modest node reports are roughly 32 KB.
func TestApolloHealthAcceptsAFleetLargerThanOneEntityBound(t *testing.T) {
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureAgentIdentityHeaders: true, AllowInsecureOperatorIdentityHeaders: true,
	})
	if response := requestJSON(t, handler, http.MethodPost, "/v1/plans", foundation()); response.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", response.Code, response.Body.String())
	}

	entities := make([]apolloEntityHealth, 0, 50)
	for node := range 50 {
		entities = append(entities, apolloEntityHealth{
			ProductGroup: "app.example", ProductName: "example-api", ProductVersion: "0.1.0",
			NodeID: fmt.Sprintf("node-%d", node),
			ContainerStatuses: []apolloContainerStatus{{ContainerID: "api", Liveness: true, Readiness: true,
				Checks: []HealthCheck{
					{ID: "LIVENESS", Message: "ok", Params: map[string]any{}, Result: HealthHealthy},
					{ID: "READINESS", Message: "ok", Params: map[string]any{}, Result: HealthHealthy},
					{ID: "DEPENDENCY", Message: "ok", Params: map[string]any{}, Result: HealthHealthy},
				}}},
		})
	}
	encoded, err := json.Marshal(map[string]any{"entitiesHealth": entities})
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) <= 10_000 {
		t.Fatalf("fleet payload is %d bytes; it must exceed the per-Entity bound to exercise the regression", len(encoded))
	}

	response := requestApolloStatusJSON(t, handler, "/api/v0/status/environments/dev-use2/entities/health",
		map[string]any{"entitiesHealth": entities})
	if response.Code != http.StatusOK {
		t.Fatalf("%d-node fleet (%d bytes) = %d %s", len(entities), len(encoded), response.Code, response.Body.String())
	}
}

func TestApolloPublicAPIEnvironmentInstallationNamespaceAndStatus(t *testing.T) {
	handler := Handler()
	state := foundation()
	state.Environment.Namespaces = []NamespaceTarget{{
		RID: "ri.namespace.main.namespace.123", Name: "example", OntologyCountValid: true,
	}}
	response := requestJSON(t, handler, http.MethodPost, "/v1/plans", state)
	if response.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", response.Code, response.Body.String())
	}

	response = requestJSON(t, handler, http.MethodGet, "/api/v0/environments", nil)
	var environments struct {
		Environments []apolloEnvironment `json:"environments"`
	}
	decodeResponse(t, response, http.StatusOK, &environments)
	// environmentType is deliberately NOT asserted here. It used to be pinned to
	// "production" for an Environment this very test names dev-use2, which is
	// the fabrication issues #57 and #112 reported; it is now omitted, and
	// TestApolloOmitsCoordinatesConduitHoldsNoAnswerFor pins the omission on the
	// wire rather than through a DTO that cannot tell absent from empty.
	if len(environments.Environments) != 1 || environments.Environments[0].Status != "managed" {
		t.Fatalf("environments = %#v", environments)
	}
	environmentRID := environments.Environments[0].RID

	response = requestJSON(t, handler, http.MethodGet, "/api/v0/environments/"+environmentRID, nil)
	var environment apolloEnvironment
	decodeResponse(t, response, http.StatusOK, &environment)
	if environment.Name != "dev-use2" {
		t.Fatalf("environment = %#v", environment)
	}

	response = requestJSON(t, handler, http.MethodGet, "/api/v0/environments/"+environmentRID+"/installations", nil)
	var installations struct {
		Installations []apolloInstallation `json:"installations"`
	}
	decodeResponse(t, response, http.StatusOK, &installations)
	if len(installations.Installations) != 2 {
		t.Fatalf("installations = %#v", installations)
	}
	installation := installations.Installations[0]
	response = requestJSON(t, handler, http.MethodGet,
		"/api/v0/environments/"+environmentRID+"/installations/"+installation.RID, nil)
	var fetched apolloInstallation
	decodeResponse(t, response, http.StatusOK, &fetched)
	if fetched.EnvironmentRID != environmentRID || fetched.ApolloEntityID == "" {
		t.Fatalf("installation = %#v", fetched)
	}

	response = requestJSON(t, handler, http.MethodGet, "/api/v0/namespaces", nil)
	var namespaces struct {
		Namespaces []apolloNamespace `json:"namespaces"`
	}
	decodeResponse(t, response, http.StatusOK, &namespaces)
	if len(namespaces.Namespaces) != 1 || namespaces.Namespaces[0].ID != "example" {
		t.Fatalf("namespaces = %#v", namespaces)
	}

	for _, endpoint := range []string{"info", "health"} {
		response = requestApolloStatusJSON(t, handler,
			"/api/v0/status/environments/dev-use2/entities/"+endpoint,
			map[string]any{"entities" + map[string]string{"info": "Info", "health": "Health"}[endpoint]: []any{}},
		)
		if response.Code != http.StatusOK || response.Body.String() != "{}\n" {
			t.Fatalf("status %s = %d %s", endpoint, response.Code, response.Body.String())
		}
	}
}

func TestApolloPublicAPIServicePublishAndValidation(t *testing.T) {
	handler := Handler()
	payload := map[string]any{"extensions": map[string]any{}}
	response := requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/service/com.example:service:1.2.3/publish",
		payload,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("service publish = %d %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/service/com.example:service:1.2.3/publish", payload)
	if response.Code != http.StatusOK {
		t.Fatalf("idempotent service publish = %d %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/service/com.example:service:1.2.3/publish",
		map[string]any{"extensions": map[string]any{"productDependencies": []map[string]any{{
			"productGroup": "com.example", "productName": "dependency", "minimumVersion": "1.0.0", "maximumVersion": "1.999.999",
		}}}},
	)
	if response.Code != http.StatusConflict {
		t.Fatalf("mutable service republish = %d %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost,
		"/api/v0/products/service/com.example:service:not-semver/publish",
		map[string]any{"extensions": map[string]any{}},
	)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid service publish = %d %s", response.Code, response.Body.String())
	}
}

func TestConnectedPublisherRequiresArtifactProductAndReleaseAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "artifact-only", DisplayName: "Artifact only", AdminUsers: []string{"artifact-only"}, MemberUsers: []string{"artifact-only"}},
		{Name: "release-only", DisplayName: "Release only", AdminUsers: []string{"release-only"}, MemberUsers: []string{"release-only"}},
		{Name: "publisher", DisplayName: "Connected publisher", AdminUsers: []string{"publisher"}, MemberUsers: []string{"publisher"}},
		{Name: "reader", DisplayName: "Catalog reader", AdminUsers: []string{"reader"}, MemberUsers: []string{"reader"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []ResourceGrant{
		{ResourceRID: "global/artifact", Team: "artifact-only", Roles: []string{"apollo-artifact:creator", "apollo-artifact:viewer"}},
		{ResourceRID: "global/product", Team: "release-only", Roles: []string{"product:release-creator", "product:viewer"}},
		{ResourceRID: "global/artifact", Team: "publisher", Roles: []string{"apollo-artifact:creator", "apollo-artifact:viewer"}},
		{ResourceRID: "global/product", Team: "publisher", Roles: []string{"product:creator", "product:release-creator", "product:viewer"}},
		{ResourceRID: "global/product", Team: "reader", Roles: []string{"product:viewer"}},
	} {
		if _, err := governance.PutGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	publish := func(subject, coordinate string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/api/v0/products/service/"+coordinate+"/publish", strings.NewReader(`{"extensions":{}}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Platform-Subject", subject)
		request.Header.Set("X-Platform-Service-Account", "true")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	if response := publish("artifact-only", "com.example:fixture:1.0.0"); response.Code != http.StatusForbidden {
		t.Fatalf("artifact-only publication = %d %s", response.Code, response.Body.String())
	}
	if response := publish("release-only", "com.example:fixture:1.0.0"); response.Code != http.StatusForbidden {
		t.Fatalf("release-only publication = %d %s", response.Code, response.Body.String())
	}
	if response := publish("publisher", "com.example:fixture:1.0.0"); response.Code != http.StatusOK {
		t.Fatalf("fully authorized publication = %d %s", response.Code, response.Body.String())
	}
	// Once the Product exists, a Release Creator with Artifact Creator authority
	// can add a Release without also becoming a Product Creator.
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: "global/artifact", Team: "release-only", Roles: []string{"apollo-artifact:creator", "apollo-artifact:viewer"},
	}); err != nil {
		t.Fatal(err)
	}
	if response := publish("release-only", "com.example:fixture:1.0.1"); response.Code != http.StatusOK {
		t.Fatalf("release creator publication = %d %s", response.Code, response.Body.String())
	}
	if response := publish("release-only", "com.example:new-product:1.0.0"); response.Code != http.StatusForbidden {
		t.Fatalf("release creator created Product = %d %s", response.Code, response.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v0/products", nil)
	request.Header.Set("X-Platform-Subject", "reader")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Product viewer list = %d %s", response.Code, response.Body.String())
	}
	events, err := repository.ListAuthorizationAudit(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	wantAudit := map[string]bool{
		OperationCreateArtifact + ":ALLOW": false,
		OperationCreateArtifact + ":DENY":  false,
		OperationCreateProduct + ":ALLOW":  false,
		OperationCreateProduct + ":DENY":   false,
		OperationCreateRelease + ":ALLOW":  false,
	}
	for _, event := range events {
		key := event.Action + ":" + event.Decision
		if _, tracked := wantAudit[key]; tracked {
			wantAudit[key] = true
		}
	}
	for decision, observed := range wantAudit {
		if !observed {
			t.Fatalf("missing publication authorization audit %s; events=%#v", decision, events)
		}
	}
}

func TestApolloPublicAPIEveryOperationRequiresAuthenticatedIdentity(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())
	operations := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v0/products/helmChart/com.example:fixture:1.2.3/publish"},
		{http.MethodPost, "/api/v0/products/service/com.example:fixture:1.2.3/publish"},
		{http.MethodGet, "/api/v0/products"},
		{http.MethodGet, "/api/v0/products/com.example:fixture"},
		{http.MethodGet, "/api/v0/products/com.example:fixture/releases"},
		{http.MethodGet, "/api/v0/products/com.example:fixture/releases/1.2.3"},
		{http.MethodGet, "/api/v0/environments"},
		{http.MethodGet, "/api/v0/environments/ri.apollo.main.environment.example"},
		{http.MethodGet, "/api/v0/environments/ri.apollo.main.environment.example/installations"},
		{http.MethodGet, "/api/v0/environments/ri.apollo.main.environment.example/installations/installation"},
		{http.MethodPost, "/api/v0/status/environments/dev-use2/entities/info"},
		{http.MethodPost, "/api/v0/status/environments/dev-use2/entities/health"},
		{http.MethodGet, "/api/v0/namespaces"},
	}
	for _, operation := range operations {
		request := httptest.NewRequest(operation.method, operation.path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s = %d %s", operation.method, operation.path, response.Code, response.Body.String())
		}
	}
}

func TestApolloPublicAPIStatusIdentityCannotCrossEnvironments(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{AllowInsecureAgentIdentityHeaders: true})
	request := httptest.NewRequest(http.MethodPost, "/api/v0/status/environments/prod/entities/health", strings.NewReader(`{"entitiesHealth":[]}`))
	request.Header.Set("X-Platform-Environment-ID", "dev-use2")
	request.Header.Set("X-Platform-Agent-ID", "agent-dev")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-Environment status = %d %s", response.Code, response.Body.String())
	}
}

// TestApolloOmitsCoordinatesConduitHoldsNoAnswerFor pins the four optional
// properties Conduit stopped fabricating, and pins the one it cannot stop.
//
// It decodes into map[string]any rather than into the typed DTOs the rest of
// this file uses, and that is the point. A `[]string` field decodes to nil from
// both `"owners": []` and an absent `owners`, so a typed assertion cannot tell
// "no group holds Owner permissions" -- a claim governance never made -- from
// "Conduit holds no answer". The defect issues #57 and #112 reported is exactly
// that conflation, so the test has to read the key set the client receives.
//
// The required/optional split is read from the pinned snapshot rather than
// restated, so this fails if a future snapshot makes an omitted property
// required or relaxes `status`.
func TestApolloOmitsCoordinatesConduitHoldsNoAnswerFor(t *testing.T) {
	required := snapshotRequiredProperties(t)
	for schema, property := range map[string]string{
		"Environment": "environmentType", "Namespace": "owners",
	} {
		if required[schema][property] {
			t.Fatalf("%s.%s is required in the pinned snapshot; omitting it is a contract break, not a fidelity fix",
				schema, property)
		}
	}
	if !required["Environment"]["status"] {
		t.Fatal("Environment.status is no longer required in the pinned snapshot; " +
			"the constant below may now be omitted instead of declared as a divergence")
	}

	handler := Handler()
	state := foundation()
	state.Environment.Namespaces = []NamespaceTarget{{
		RID: "ri.namespace.main.namespace.123", Name: "example", OntologyCountValid: true,
	}}
	if response := requestJSON(t, handler, http.MethodPost, "/v1/plans", state); response.Code != http.StatusCreated {
		t.Fatalf("create plan = %d %s", response.Code, response.Body.String())
	}

	var environments struct {
		Environments []map[string]any `json:"environments"`
	}
	decodeResponse(t, requestJSON(t, handler, http.MethodGet, "/api/v0/environments", nil),
		http.StatusOK, &environments)
	if len(environments.Environments) != 1 {
		t.Fatalf("environments = %#v", environments)
	}
	environment := environments.Environments[0]
	if _, present := environment["environmentType"]; present {
		t.Fatalf("environmentType is on the wire as %#v; Conduit's Environments domain has no "+
			"environment-type concept, and the snapshot does not require the property",
			environment["environmentType"])
	}
	// The one that stays. status is in the snapshot's required set, so the
	// honest option is a declared divergence rather than an omission that breaks
	// the contract.
	if environment["status"] != "managed" {
		t.Fatalf("status = %#v; the declared divergence says it is the constant %q", environment["status"], "managed")
	}

	var namespaces struct {
		Namespaces []map[string]any `json:"namespaces"`
	}
	decodeResponse(t, requestJSON(t, handler, http.MethodGet, "/api/v0/namespaces", nil),
		http.StatusOK, &namespaces)
	if len(namespaces.Namespaces) != 1 || namespaces.Namespaces[0]["id"] != "example" {
		t.Fatalf("namespaces = %#v", namespaces)
	}
	for _, property := range []string{"owners", "editors", "viewers"} {
		if value, present := namespaces.Namespaces[0][property]; present {
			t.Fatalf("namespace %s is on the wire as %#v; an empty array asserts that no group holds "+
				"that permission, which Conduit never established", property, value)
		}
	}
}

// snapshotRequiredProperties reads each named schema's `required` list out of
// the pinned snapshot, line by line for the reason enumConstrainedProperties
// gives above: the document is pinned by digest in its own path and cannot
// drift under the check.
func snapshotRequiredProperties(t *testing.T) map[string]map[string]bool {
	t.Helper()
	contents, err := os.ReadFile(filepath.Clean(apolloSnapshot))
	if err != nil {
		t.Fatal(err)
	}
	result := map[string]map[string]bool{}
	schema, inRequired := "", false
	for _, line := range strings.Split(string(contents), "\n") {
		if match := snapshotSchemaName.FindStringSubmatch(line); match != nil {
			schema, inRequired = match[1], false
			result[schema] = map[string]bool{}
			continue
		}
		if schema == "" {
			continue
		}
		if strings.TrimRight(line, " ") == "      required:" {
			inRequired = true
			continue
		}
		match := snapshotEnumMember.FindStringSubmatch(line)
		if inRequired && match != nil {
			result[schema][strings.TrimSpace(match[1])] = true
			continue
		}
		if match == nil {
			inRequired = false
		}
	}
	if !result["Environment"]["rid"] || !result["Namespace"]["id"] {
		t.Fatalf("the required-property reader found nothing it should have: %#v", result)
	}
	return result
}

func requestJSON(t *testing.T, handler http.Handler, method, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if value != nil {
		if err := json.NewEncoder(&body).Encode(value); err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, path, &body)
	if value != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func requestApolloStatusJSON(t *testing.T, handler http.Handler, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, &body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform-Environment-ID", "dev-use2")
	request.Header.Set("X-Platform-Agent-ID", "agent-dev")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, status int, value any) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), value); err != nil {
		t.Fatal(err)
	}
}

// The generated strict interface is the complete operation inventory. The
// compile-time assertion beside apolloOpenAPIAdapter makes a newly generated
// operation a build failure; this test also reports the exact missing method.
func TestApolloGeneratedStrictAdapterCoversEveryOperation(t *testing.T) {
	t.Parallel()
	contract := reflect.TypeOf((*apollo.StrictServerInterface)(nil)).Elem()
	implementation := reflect.TypeOf((*apolloOpenAPIAdapter)(nil))
	if contract.NumMethod() != 13 {
		t.Fatalf("generated strict interface has %d operations, want the pinned 13", contract.NumMethod())
	}
	for index := range contract.NumMethod() {
		operation := contract.Method(index)
		if _, exists := implementation.MethodByName(operation.Name); !exists {
			t.Errorf("generated operation %s has no adapter method", operation.Name)
		}
	}
}

// aggregateApolloHealth decides whether an Entity reports healthy across its
// replicas, which is what reaches reported state and, through it, the promotion
// gates. Only ALL was ever exercised; the arithmetic behind the other modes --
// and the threshold comparison itself -- went untested, so mutation testing
// moved the quorum edge and incremented the healthy count the wrong way without
// failing anything.
func TestApolloHealthQuorumThresholdsAreExact(t *testing.T) {
	t.Parallel()
	node := func(id string, healthy bool) apolloEntityHealth {
		return apolloEntityHealth{
			ProductGroup: "app.example", ProductName: "example-api", ProductVersion: "0.1.0", NodeID: id,
			ContainerStatuses: []apolloContainerStatus{{
				ContainerID: "api", Liveness: healthy, Readiness: healthy,
			}},
		}
	}
	fleet := func(healthy, unhealthy int) []apolloEntityHealth {
		nodes := make([]apolloEntityHealth, 0, healthy+unhealthy)
		for index := range healthy {
			nodes = append(nodes, node(fmt.Sprintf("ok-%d", index), true))
		}
		for index := range unhealthy {
			nodes = append(nodes, node(fmt.Sprintf("bad-%d", index), false))
		}
		return nodes
	}

	for _, test := range []struct {
		quorum    string
		healthy   int
		unhealthy int
		want      string
	}{
		// ALL needs every node.
		{quorum: "ALL", healthy: 2, unhealthy: 0, want: HealthHealthy},
		{quorum: "ALL", healthy: 1, unhealthy: 1, want: HealthError},
		// ANY and NO_GUARANTEE need exactly one, at the edge.
		{quorum: "ANY", healthy: 1, unhealthy: 3, want: HealthHealthy},
		{quorum: "ANY", healthy: 0, unhealthy: 3, want: HealthError},
		{quorum: "NO_GUARANTEE", healthy: 1, unhealthy: 3, want: HealthHealthy},
		{quorum: "NO_GUARANTEE", healthy: 0, unhealthy: 1, want: HealthError},
		// ALL_BUT_ONE tolerates exactly one failure, and never fewer than one
		// healthy node even when the fleet is a single node.
		{quorum: "ALL_BUT_ONE", healthy: 2, unhealthy: 1, want: HealthHealthy},
		{quorum: "ALL_BUT_ONE", healthy: 1, unhealthy: 2, want: HealthError},
		{quorum: "ALL_BUT_ONE", healthy: 1, unhealthy: 0, want: HealthHealthy},
		{quorum: "ALL_BUT_ONE", healthy: 0, unhealthy: 1, want: HealthError},
		// SIMPLE_MAJORITY is n/2+1, so an even fleet needs a true majority.
		{quorum: "SIMPLE_MAJORITY", healthy: 2, unhealthy: 1, want: HealthHealthy},
		{quorum: "SIMPLE_MAJORITY", healthy: 1, unhealthy: 2, want: HealthError},
		{quorum: "SIMPLE_MAJORITY", healthy: 1, unhealthy: 1, want: HealthError},
		{quorum: "SIMPLE_MAJORITY", healthy: 3, unhealthy: 1, want: HealthHealthy},
		{quorum: "SIMPLE_MAJORITY", healthy: 2, unhealthy: 2, want: HealthError},
		// UNKNOWN, and an unset quorum which defaults to it, never claim health.
		{quorum: "UNKNOWN", healthy: 3, unhealthy: 0, want: HealthIndeterminate},
		{quorum: "", healthy: 3, unhealthy: 0, want: HealthIndeterminate},
	} {
		t.Run(fmt.Sprintf("%s/%dok-%dbad", test.quorum, test.healthy, test.unhealthy), func(t *testing.T) {
			t.Parallel()
			details, result := aggregateApolloHealth(test.quorum, fleet(test.healthy, test.unhealthy))
			if result != test.want {
				t.Fatalf("%s with %d healthy of %d = %s, want %s",
					test.quorum, test.healthy, test.healthy+test.unhealthy, result, test.want)
			}
			if len(details.Nodes) != test.healthy+test.unhealthy {
				t.Fatalf("details carry %d nodes, want %d", len(details.Nodes), test.healthy+test.unhealthy)
			}
		})
	}

	// No evidence is not health, whatever the quorum asks for.
	if _, result := aggregateApolloHealth("ANY", nil); result != HealthUnknown {
		t.Fatalf("an empty fleet reported %s, want %s", result, HealthUnknown)
	}
}

// The status ingestion path validates what a Spoke reports before it becomes
// the Hub's view of a fleet. Its enums and its per-Entity size bound were all
// unpinned at their edges, including the bound this adapter carries precisely
// because it does not route through the native path's equivalent.
func TestApolloStatusIngestionValidatesAtItsEdges(t *testing.T) {
	t.Parallel()

	t.Run("entity type is a closed set", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			entityType string
			wantError  bool
		}{
			{entityType: "HELM_CHART"},
			{entityType: "SERVICE"},
			{entityType: "helm_chart", wantError: true},
			{entityType: "DEPLOYMENT", wantError: true},
			{entityType: "", wantError: true},
		} {
			err := validateApolloEntityInfo([]apolloEntityInfo{{
				EntityType: test.entityType, ID: "example-api",
				ProductGroup: "app.example", ProductName: "example-api",
			}})
			if (err != nil) != test.wantError {
				t.Errorf("entityType %q rejected=%v, want %v", test.entityType, err != nil, test.wantError)
			}
		}
	})

	t.Run("quorum is a closed set and optional", func(t *testing.T) {
		t.Parallel()
		valid := []string{"", "UNKNOWN", "ANY", "ALL", "ALL_BUT_ONE", "SIMPLE_MAJORITY", "NO_GUARANTEE"}
		for _, quorum := range append(valid, "MAJORITY", "all", "ALL_BUT_TWO") {
			wantError := !contains(valid, quorum)
			err := validateApolloEntityInfo([]apolloEntityInfo{{
				EntityType: "SERVICE", ID: "example-api",
				ProductGroup: "app.example", ProductName: "example-api", Quorum: quorum,
			}})
			if (err != nil) != wantError {
				t.Errorf("quorum %q rejected=%v, want %v", quorum, err != nil, wantError)
			}
		}
	})

	// The bound exists because persistReportedState writes straight to the
	// repository, so applyHealthObservations never sees this evidence.
	t.Run("per-Entity size bound", func(t *testing.T) {
		t.Parallel()
		entityOfSize := func(size int) apolloEntityHealth {
			entity := apolloEntityHealth{
				ProductGroup: "app.example", ProductName: "example-api", ProductVersion: "0.1.0", NodeID: "node",
				ContainerStatuses: []apolloContainerStatus{{ContainerID: "api", Liveness: true, Readiness: true,
					Checks: []HealthCheck{{ID: "CHECK", Result: HealthHealthy, Params: map[string]any{}}}}},
			}
			encoded, _ := json.Marshal(entity)
			entity.ContainerStatuses[0].Checks[0].Message = strings.Repeat("x", size-len(encoded))
			return entity
		}
		for _, test := range []struct {
			size      int
			wantError bool
		}{
			{size: 10_000},
			{size: 10_001, wantError: true},
		} {
			entity := entityOfSize(test.size)
			if encoded, _ := json.Marshal(entity); len(encoded) != test.size {
				t.Fatalf("fixture is %d bytes, want %d", len(encoded), test.size)
			}
			err := validateApolloEntityHealth([]apolloEntityHealth{entity})
			if (err != nil) != test.wantError {
				t.Errorf("entity of %d bytes rejected=%v, want %v", test.size, err != nil, test.wantError)
			}
		}
	})

	// A truncated Entity ID still looks like one, so the builder refuses rather
	// than shortening. The edge is the whole point of the rule.
	t.Run("entity id length", func(t *testing.T) {
		t.Parallel()
		fixed := len("aeid:dev-use2:HELM_CHART:")
		for _, test := range []struct {
			nameLength int
			wantError  bool
		}{
			{nameLength: workloadmetadata.ApolloEntityIDMaximumLength - fixed},
			{nameLength: workloadmetadata.ApolloEntityIDMaximumLength - fixed + 1, wantError: true},
		} {
			id, err := workloadmetadata.ApolloEntityID("dev-use2", "HELM_CHART", strings.Repeat("a", test.nameLength))
			if (err != nil) != test.wantError {
				t.Errorf("name of %d chars rejected=%v, want %v (id=%d chars)",
					test.nameLength, err != nil, test.wantError, len(id))
			}
		}
	})
}

// matchApolloInfo and matchApolloHealth decide which reported Apollo state belongs to which Plan
// step. Neither had a direct test — only aggregateApolloHealth did — and a mutation sweep found the
// gap as a cluster of survivors around their call site: the guards could be made always-true and
// nothing noticed.
func TestApolloReportedStateIsMatchedToTheRightEntity(t *testing.T) {
	t.Parallel()

	t.Run("info matches only when BOTH the entity and the product agree", func(t *testing.T) {
		fleet := []apolloEntityInfo{
			{ID: "api", ProductGroup: "com.example", ProductName: "api", Quorum: "ALL"},
			{ID: "web", ProductGroup: "com.example", ProductName: "web", Quorum: "ANY"},
		}
		step := PlanStep{Entity: "api", Product: "com.example:api"}
		if got := matchApolloInfo(fleet, step); got.Quorum != "ALL" {
			t.Errorf("quorum = %q, want the api entry", got.Quorum)
		}
		// The entity name alone must not match: two Entities can carry one name across Products,
		// and taking the first would attribute another Product's nodes to this step.
		wrongProduct := PlanStep{Entity: "api", Product: "com.example:other"}
		if got := matchApolloInfo(fleet, wrongProduct); got.Quorum != "UNKNOWN" {
			t.Errorf("a name-only match was accepted: %#v", got)
		}
		// And the product alone must not either.
		wrongEntity := PlanStep{Entity: "other", Product: "com.example:api"}
		if got := matchApolloInfo(fleet, wrongEntity); got.Quorum != "UNKNOWN" {
			t.Errorf("a product-only match was accepted: %#v", got)
		}
	})

	t.Run("an unmatched step gets a synthesized entry, not a zero value", func(t *testing.T) {
		got := matchApolloInfo(nil, PlanStep{Entity: "api", Product: "com.example:api"})
		if got.ProductGroup != "com.example" || got.ProductName != "api" {
			t.Errorf("product coordinate was not split: %#v", got)
		}
		if got.Quorum != "UNKNOWN" {
			t.Errorf("quorum = %q, want UNKNOWN so an absent report is not read as healthy", got.Quorum)
		}
	})

	t.Run("health is filtered to the product, and to the nodes info names", func(t *testing.T) {
		health := []apolloEntityHealth{
			{ProductGroup: "com.example", ProductName: "api", NodeID: "n2"},
			{ProductGroup: "com.example", ProductName: "api", NodeID: "n1"},
			{ProductGroup: "com.example", ProductName: "web", NodeID: "n1"},
		}
		// With node info present, only those nodes count -- a node the Entity does not run on
		// must not contribute its health.
		info := apolloEntityInfo{NodesInfo: []apolloNodeInfo{{NodeID: "n1"}}}
		got := matchApolloHealth(health, info, "com.example:api")
		if len(got) != 1 || got[0].NodeID != "n1" {
			t.Fatalf("node filtering = %#v", got)
		}
		// With no node info, every report for the product counts -- that is the branch an
		// Entity whose nodes Apollo has not reported yet takes.
		all := matchApolloHealth(health, apolloEntityInfo{}, "com.example:api")
		if len(all) != 2 {
			t.Fatalf("empty node info should not filter: %#v", all)
		}
		// Sorted by node, so the version picked downstream is deterministic.
		if all[0].NodeID != "n1" || all[1].NodeID != "n2" {
			t.Errorf("result is not sorted by NodeID: %#v", all)
		}
	})

	t.Run("a product with no reported state at all leaves the entity untouched", func(t *testing.T) {
		// The call site guards on len(info.NodesInfo) > 0 || len(health) > 0. Made always-true,
		// an Entity Apollo has never reported would be stamped with an observation time and an
		// aggregated health verdict computed from nothing.
		info := matchApolloInfo(nil, PlanStep{Entity: "ghost", Product: "com.example:ghost"})
		health := matchApolloHealth(nil, info, "com.example:ghost")
		if len(info.NodesInfo) != 0 || len(health) != 0 {
			t.Fatalf("the fixture reports something: %#v %#v", info, health)
		}
	})
}

// An Entity Apollo has never reported on must be left as it was found. persistReportedState guards
// the whole enrichment on `len(matchingInfo.NodesInfo) > 0 || len(matchingHealth) > 0`, and with
// that guard always true the Entity is stamped with an observation time and a health verdict
// aggregated from an empty fleet — a confident answer computed from nothing. A mutation sweep found
// it: the boundary and negation mutants at that line both survived the suite.
func TestApolloLeavesAnUnreportedEntityAlone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := assessedFoundation()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	plans, err := repository.ListPlans(ctx, state.Environment.ID)
	if err != nil || len(plans) == 0 {
		t.Fatalf("no plan to report against: %v", err)
	}
	if len(plans[0].Plan.Steps) == 0 {
		t.Skip("the fixture plan has no steps")
	}

	// Deliberately empty: Apollo has told us nothing about this Environment.
	api := newApolloPublicAPI(repository, NewGovernanceService(repository), HandlerOptions{})
	if err := api.application.PutEntityInfo(ctx, state.Environment.ID, nil); err != nil {
		t.Fatal(err)
	}
	report, err := repository.GetEnvironmentReport(ctx, state.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Entities) == 0 {
		t.Fatal("no entities were reported")
	}
	for _, entity := range report.Entities {
		if entity.Health != HealthUnknown {
			t.Errorf("%s health = %q, want UNKNOWN — nothing was reported about it",
				entity.InstallationID, entity.Health)
		}
		if entity.HealthDetails != nil {
			t.Errorf("%s carries health details aggregated from an empty fleet: %#v",
				entity.InstallationID, entity.HealthDetails)
		}
		if entity.Lifecycle != LifecycleUnknown {
			t.Errorf("%s lifecycle = %q, want UNKNOWN — no node info says it is installed",
				entity.InstallationID, entity.Lifecycle)
		}
		if !entity.ObservedAt.IsZero() {
			t.Errorf("%s was stamped observed at %s on no observation",
				entity.InstallationID, entity.ObservedAt)
		}
	}
}

// Health reported without node info is the partial case, and it is the one the remaining guards
// exist for: Apollo knows the Product is alive somewhere but has said nothing about WHERE. The
// namespace must not be overwritten with an empty string, and the Entity must not be called
// installed on the strength of health alone. Both mutants survived until this test.
func TestApolloPartialReportDoesNotInventNamespaceOrLifecycle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := assessedFoundation()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	plans, err := repository.ListPlans(ctx, state.Environment.ID)
	if err != nil || len(plans) == 0 || len(plans[0].Plan.Steps) == 0 {
		t.Fatalf("no plan step to report against: %v", err)
	}
	step := plans[0].Plan.Steps[0]
	group, name, _ := strings.Cut(step.Product, ":")

	api := newApolloPublicAPI(repository, NewGovernanceService(repository), HandlerOptions{})
	info := generatedEntityInfo(apolloEntityInfo{EntityType: "SERVICE", ID: step.Entity, ProductGroup: group, ProductName: name, Quorum: "ANY"})
	if err := api.application.PutEntityInfo(ctx, state.Environment.ID, []apollo.EntityInfo{info}); err != nil {
		t.Fatal(err)
	}
	health := generatedEntityHealth([]apolloEntityHealth{{
		ProductGroup: group, ProductName: name, ProductVersion: step.Version, NodeID: "n1",
	}})
	if err := api.application.PutEntityHealth(ctx, state.Environment.ID, health); err != nil {
		t.Fatal(err)
	}
	report, err := repository.GetEnvironmentReport(ctx, state.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	var reported *ReportedEntity
	for index := range report.Entities {
		if report.Entities[index].InstallationID == step.InstallationID {
			reported = &report.Entities[index]
		}
	}
	if reported == nil {
		t.Fatalf("step %s was not reported", step.InstallationID)
	}
	// Health DID arrive, so the enrichment ran — this is not the empty case over again.
	if reported.ObservedAt.IsZero() {
		t.Fatal("the fixture did not reach the enrichment branch")
	}
	if reported.Namespace != step.Namespace {
		t.Errorf("namespace = %q, want the plan's %q — Apollo reported none, so none was learned",
			reported.Namespace, step.Namespace)
	}
	if reported.Lifecycle == LifecycleInstalled {
		t.Error("lifecycle = INSTALLED on health alone; no node info says the Entity is placed")
	}
}

// persistReportedState seeds each Entity from the report already on file, so fields Apollo does not
// speak to survive a refresh. Read the existing report only when the read FAILED and every refresh
// starts from a blank Entity, silently discarding whatever the previous one had established. A
// mutation sweep found the guard unpinned.
func TestApolloRefreshCarriesTheExistingReportForward(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := assessedFoundation()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	plans, err := repository.ListPlans(ctx, state.Environment.ID)
	if err != nil || len(plans) == 0 || len(plans[0].Plan.Steps) == 0 {
		t.Fatalf("no plan step: %v", err)
	}
	step := plans[0].Plan.Steps[0]

	// A prior report that recorded something Apollo does not re-send.
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: state.Environment.ID, AgentID: "prior", ObservedAt: time.Now().UTC(),
		Entities: []ReportedEntity{{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			Namespace: step.Namespace, Health: HealthUnknown,
			RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigIndeterminate,
			Lifecycle: LifecycleUnknown,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	api := newApolloPublicAPI(repository, NewGovernanceService(repository), HandlerOptions{})
	if err := api.application.PutEntityInfo(ctx, state.Environment.ID, nil); err != nil {
		t.Fatal(err)
	}
	report, err := repository.GetEnvironmentReport(ctx, state.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entity := range report.Entities {
		if entity.InstallationID != step.InstallationID {
			continue
		}
		// RolloutUpToDate was established by the prior report and Apollo said nothing to
		// change it. A fresh Entity would be RolloutOutOfDate.
		if entity.RolloutStatus != RolloutUpToDate {
			t.Errorf("rollout status = %q, want the carried-forward %q; the refresh discarded what "+
				"the previous report established", entity.RolloutStatus, RolloutUpToDate)
		}
		return
	}
	t.Fatalf("step %s absent from the refreshed report", step.InstallationID)
}

// worseHealth breaks a tie toward the LEFT operand, which is what makes folding a fleet's states
// deterministic: the accumulated verdict does not flip when two states rank the same. Apollo can
// send a state this severity table does not list -- both then rank 0 -- and that is the case the
// boundary mutant survived on.
func TestWorseHealthKeepsTheLeftOnATie(t *testing.T) {
	t.Parallel()
	if got := worseHealth(HealthError, HealthWarning); got != HealthError {
		t.Errorf("worseHealth(ERROR, WARNING) = %q, want ERROR", got)
	}
	if got := worseHealth(HealthWarning, HealthError); got != HealthError {
		t.Errorf("worseHealth(WARNING, ERROR) = %q, want ERROR", got)
	}
	// Two states the table does not rank. Equal severity must resolve to the left, or folding a
	// fleet returns whichever node happened to be read last.
	if got := worseHealth("UNRANKED_A", "UNRANKED_B"); got != "UNRANKED_A" {
		t.Errorf("a tie resolved to %q, want the left operand — the fold must be stable", got)
	}
}

// A recall declared through one Hub replica must read as a recall from the
// other. This Hub runs two replicas behind one address, so the POST that
// withdraws a Release and the GET that asks about it are routinely served by
// different processes -- and a restart makes every process the second one.
//
// Apollo's recallStatus was answered from a map on the service struct that only
// syncDesiredState wrote, and syncDesiredState runs in the process that handled
// the POST. The other replica therefore served recalled=false for a Release
// Conduit had withdrawn: the safe-looking answer to an unsafe question, and the
// one direction of this error a client cannot recover from, because it installs
// on the strength of it.
//
// The second API here is constructed independently over the SAME repository,
// which is what a second replica and a restarted process both are.
func TestARecalledReleaseReadsAsRecalledFromASecondReplica(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)

	// The replica that serves the write.
	writer := newApolloPublicAPI(repository, governance, HandlerOptions{})
	if writer.initializationError != nil {
		t.Fatal(writer.initializationError)
	}
	if err := writer.application.PublishNativeHelmChart(ctx, "app.example:example-api:0.9.0",
		apolloapp.NativeHelmPublication{
			ChartName: "example-api", ChartVersion: "0.9.0",
			ChartRepository: "oci://registry.example/charts/example-api",
		}); err != nil {
		t.Fatal(err)
	}

	state := assessedFoundation()
	state.Recalls = []Recall{{
		ID: "recall-api-0.9.0", Product: "app.example:example-api", StartingVersion: "0.9.0",
		SingleVersion: true, Strategy: RecallStrategy{Type: RecallRollForward}, Reason: "withdrawn for cause",
	}}
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	writer.syncDesiredState(ctx, state)

	// Stated rather than assumed: the replica that served the write agrees the
	// Release is recalled, so a failure below is about which process answered
	// and not about the recall itself.
	served, err := writer.application.GetRelease(ctx, "app.example:example-api", "0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if !served.RecallStatus.Recalled {
		t.Fatal("the fixture never established a recall to read back")
	}

	reader := newApolloPublicAPI(repository, governance, HandlerOptions{})
	if reader.initializationError != nil {
		t.Fatal(reader.initializationError)
	}
	got, err := reader.application.GetRelease(ctx, "app.example:example-api", "0.9.0")
	if err != nil {
		t.Fatal(err)
	}
	if !got.RecallStatus.Recalled {
		t.Error("a second replica served a recalled Release as not recalled; the recall " +
			"was answered from process memory only the writing replica held")
	}
	if got.RecallStatus.Reason == nil || *got.RecallStatus.Reason != "withdrawn for cause" {
		t.Errorf("the recall reason did not survive the replica boundary: %#v", got.RecallStatus.Reason)
	}

	// The list route is a separate call site, and half a fix here leaves two
	// read paths disagreeing about whether a Release may be installed.
	listed, err := reader.application.ListReleases(ctx, "app.example:example-api")
	if err != nil {
		t.Fatal(err)
	}
	if listed.Releases == nil {
		t.Fatal("the second replica listed no Releases at all")
	}
	for _, release := range *listed.Releases {
		if release.MavenCoordinate == "app.example:example-api:0.9.0" && !release.RecallStatus.Recalled {
			t.Error("ListReleases on the second replica served the recalled Release as not recalled")
		}
	}
}

// An Entity command stores a desired state with its Recalls stripped, because
// commands deliberately bypass recall eligibility. That state is a Plan like
// any other, so reading simply the newest Plan per Environment as the
// Environment's declaration would make issuing one command report every
// recalled Release in that Environment as not recalled.
func TestACommandPlanDoesNotEraseAnEnvironmentsRecalls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := assessedFoundation()
	state.Recalls = []Recall{{
		ID: "recall-api-0.9.0", Product: "app.example:example-api", StartingVersion: "0.9.0",
		SingleVersion: true, Strategy: RecallStrategy{Type: RecallRollForward}, Reason: "withdrawn for cause",
	}}
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	commandState := cloneJSON(state)
	commandState.Recalls = nil
	if _, err := repository.CreatePlan(ctx, PlanRecord{
		ID: "plan-command", CreatedAt: time.Now().UTC().Add(time.Hour),
		Plan:         Plan{Environment: state.Environment.ID},
		DesiredState: &commandState,
		Command:      &EntityCommand{InstallationID: "example-api", Type: CommandRestartResource},
	}); err != nil {
		t.Fatal(err)
	}

	lifecycle, err := apolloReleaseLifecyclePort{repository: repository}.ReleaseLifecycle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(lifecycle.Recalls) != 1 || lifecycle.Recalls[0].Product != "app.example:example-api" {
		t.Errorf("a command Plan erased the Environment's recalls: %#v", lifecycle.Recalls)
	}
}
