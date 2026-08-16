package operator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestHTTPControlPlaneUsesSemanticRoutes(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPut || request.URL.Path != "/v1/teams/release-reviewers" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		var team controlplane.Team
		if err := json.NewDecoder(request.Body).Decode(&team); err != nil {
			t.Fatal(err)
		}
		team.Name = "release-reviewers"
		if err := json.NewEncoder(response).Encode(team); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	team, err := adapter.PutTeam(context.Background(), "release-reviewers", controlplane.Team{DisplayName: "Release reviewers"})
	if err != nil {
		t.Fatal(err)
	}
	if team.Name != "release-reviewers" || team.DisplayName != "Release reviewers" {
		t.Fatalf("unexpected team: %#v", team)
	}
}

func TestHTTPControlPlaneListsArtifactRegistriesThroughSemanticRoute(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet || request.URL.Path != "/v1/artifact-registries" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"artifactRegistries": []controlplane.ArtifactRegistryRecord{{
				Environment: "dev",
				Assignment:  controlplane.ArtifactRegistryAssignment{ID: "primary"},
			}},
		})
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	records, err := adapter.ListArtifactRegistries(context.Background())
	if err != nil || len(records) != 1 || records[0].Assignment.ID != "primary" {
		t.Fatalf("records=%#v err=%v", records, err)
	}
}

func TestHTTPControlPlaneTraversesBoundedCatalogPagesAndRejectsCursorLoops(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		if request.URL.Path != "/v1/products" || request.URL.Query().Get("limit") != "20" || request.URL.Query().Get("view") != "full" {
			t.Fatalf("unexpected request: %s", request.URL.String())
		}
		if request.URL.Query().Get("cursor") == "" {
			_ = json.NewEncoder(response).Encode(map[string]any{
				"products":   []controlplane.ProductCatalogRecord{{Product: "app.example:first"}},
				"nextCursor": "opaque-second",
			})
			return
		}
		_ = json.NewEncoder(response).Encode(map[string]any{
			"products": []controlplane.ProductCatalogRecord{{Product: "app.example:second"}},
		})
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	products, err := adapter.ListProducts(context.Background())
	if err != nil || len(products) != 2 || products[0].Product != "app.example:first" || products[1].Product != "app.example:second" || requests != 2 {
		t.Fatalf("products=%#v requests=%d err=%v", products, requests, err)
	}

	loopServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(response).Encode(map[string]any{"environments": []any{}, "nextCursor": "repeated"})
	}))
	defer loopServer.Close()
	loopAdapter, err := NewHTTPControlPlane(loopServer.URL, loopServer.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loopAdapter.ListEnvironments(context.Background()); err == nil || !strings.Contains(err.Error(), "repeated a catalog cursor") {
		t.Fatalf("cursor loop error=%v", err)
	}
}

func TestHTTPControlPlaneInstallsModuleAndPublishesRelease(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		switch requests {
		case 1:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/environments/dev-use2/module-installations" {
				t.Fatalf("unexpected install request: %s %s", request.Method, request.URL.Path)
			}
			var body struct {
				Module    string            `json:"module"`
				Version   string            `json:"version"`
				Variables map[string]string `json:"variables"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Module != "com.example:foundation" || body.Version != "1.2.3" || body.Variables["environmentId"] != "dev-use2" {
				t.Fatalf("install body=%#v", body)
			}
			_ = json.NewEncoder(response).Encode(controlplane.ModuleInstallation{ID: "mod_one", Environment: "dev-use2"})
		case 2:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/product-publications/helm-chart" {
				t.Fatalf("unexpected publication request: %s %s", request.Method, request.URL.Path)
			}
			var body controlplane.HelmProductReleaseBundle
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Manifest.Extensions.Artifacts) != 1 || body.Manifest.Extensions.Artifacts[0].URI != "registry.example/application:1.2.3@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
				t.Fatalf("publication body=%#v", body)
			}
			if string(body.DefaultConfig) != "replicaCount: 3\n" || string(body.ConfigSchema) != `{"type":"object"}` {
				t.Fatalf("platform bundle=%#v", body)
			}
			_ = json.NewEncoder(response).Encode(struct{}{})
		default:
			t.Fatal("unexpected extra request")
		}
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.InstallModule(context.Background(), "dev-use2", "com.example:foundation", "1.2.3", "", map[string]string{"environmentId": "dev-use2"}); err != nil {
		t.Fatal(err)
	}
	publication, err := adapter.PublishProductRelease(context.Background(), controlplane.HelmProductReleaseBundle{Manifest: controlplane.ApolloProductManifest{
		ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "application", ProductVersion: "1.2.3", ProductType: "helm-chart.v1",
		Extensions: controlplane.ApolloProductManifestExtensions{
			HelmChart: &controlplane.ApolloManifestHelmChart{Name: "application", Version: "1.2.3", RepositoryURL: "oci://registry.example/charts/application:1.2.3@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Artifacts: []controlplane.ApolloManifestArtifact{{Type: "oci", URI: "registry.example/application:1.2.3@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		},
	}, DefaultConfig: []byte("replicaCount: 3\n"), ConfigSchema: []byte(`{"type":"object"}`)})
	if err != nil || !publication.Created || publication.MavenCoordinate != "com.example:application:1.2.3" {
		t.Fatalf("publication=%#v err=%v", publication, err)
	}
}

func TestHTTPControlPlaneKeepsManifestOnlyPublicationOnPinnedPublicRoute(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v0/products/helmChart/com.example:application:1.2.3/publish" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		extensions := body["extensions"].(map[string]any)
		if _, leaked := extensions["platformBundle"]; leaked {
			t.Fatalf("public request leaked Platform-native field: %#v", body)
		}
		_ = json.NewEncoder(response).Encode(struct{}{})
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	_, err = adapter.PublishProductRelease(context.Background(), controlplane.HelmProductReleaseBundle{Manifest: controlplane.ApolloProductManifest{
		ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "application", ProductVersion: "1.2.3", ProductType: "helm-chart.v1",
		Extensions: controlplane.ApolloProductManifestExtensions{
			HelmChart: &controlplane.ApolloManifestHelmChart{Name: "application", Version: "1.2.3", RepositoryURL: "oci://registry.example/charts/application:1.2.3@" + digest},
			Artifacts: []controlplane.ApolloManifestArtifact{{Type: "oci", URI: "registry.example/application:1.2.3@" + digest}},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestHTTPControlPlaneReturnsBoundedProblem(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusForbidden)
		_, _ = response.Write([]byte(`{"error":"operator is not authorized"}`))
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = adapter.GetPlan(context.Background(), "plan-1")
	if err == nil || err.Error() != "operator control plane returned 403: operator is not authorized" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewHTTPControlPlaneRejectsNonHTTPURL(t *testing.T) {
	t.Parallel()
	if _, err := NewHTTPControlPlane("file:///tmp/platform", nil); err == nil {
		t.Fatal("expected non-HTTP URL to be rejected")
	}
}

// Request paths are absolute, so ResolveReference drops a base path instead of
// prefixing it. Accepting such a URL would send every call to the wrong path
// with no error to show for it.
func TestNewHTTPControlPlaneRejectsABaseURLThatCannotBePrefixed(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"https://hub.example/platform",
		"https://hub.example/platform/",
		"https://hub.example/?tenant=a",
		"https://hub.example/#fragment",
	} {
		if _, err := NewHTTPControlPlane(rawURL, nil); err == nil {
			t.Fatalf("%s was accepted; its path would be silently discarded", rawURL)
		}
	}
	for _, rawURL := range []string{"https://hub.example", "https://hub.example/", "http://127.0.0.1:8081"} {
		if _, err := NewHTTPControlPlane(rawURL, nil); err != nil {
			t.Fatalf("%s rejected: %v", rawURL, err)
		}
	}
}

func TestHTTPControlPlaneManagesSuppressionsThroughSemanticRoutes(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		switch requests {
		case 1:
			if request.Method != http.MethodGet || request.URL.Path != "/v1/environments/dev-use2/suppressions" {
				t.Fatalf("unexpected list request: %s %s", request.Method, request.URL.Path)
			}
			_ = json.NewEncoder(response).Encode([]controlplane.SuppressionRecord{{ID: "sup_one", Environment: "dev-use2"}})
		case 2:
			if request.Method != http.MethodDelete || request.URL.Path != "/v1/suppressions/sup_one" {
				t.Fatalf("unexpected remove request: %s %s", request.Method, request.URL.Path)
			}
			_ = json.NewEncoder(response).Encode(controlplane.SuppressionRecord{ID: "sup_one", Environment: "dev-use2"})
		default:
			t.Fatalf("unexpected extra request")
		}
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	records, err := adapter.ListSuppressions(context.Background(), "dev-use2")
	if err != nil || len(records) != 1 || records[0].ID != "sup_one" {
		t.Fatalf("unexpected suppressions: %#v, %v", records, err)
	}
	removed, err := adapter.RemoveSuppression(context.Background(), "sup_one")
	if err != nil || removed.ID != "sup_one" {
		t.Fatalf("unexpected removed suppression: %#v, %v", removed, err)
	}
}

func TestHTTPControlPlaneCancelsPlanThroughSemanticRoute(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/plans/pln_one/cancel" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["reason"] != "superseded by complete boot floor" {
			t.Fatalf("unexpected reason: %#v", body)
		}
		_ = json.NewEncoder(response).Encode(controlplane.PlanRecord{ID: "pln_one", Status: controlplane.PlanCancelled})
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	record, err := adapter.CancelPlan(context.Background(), "pln_one", "superseded by complete boot floor")
	if err != nil || record.Status != controlplane.PlanCancelled {
		t.Fatalf("unexpected cancelled plan: %#v, %v", record, err)
	}
}

func TestHTTPControlPlaneUsesChangeRequestRoutesAndFilters(t *testing.T) {
	t.Parallel()
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests++
		switch requests {
		case 1:
			if request.Method != http.MethodGet || request.URL.Path != "/v1/change-requests" || request.URL.Query().Get("environment") != "dev/use2" {
				t.Fatalf("unexpected list request: %s %s", request.Method, request.URL.String())
			}
			_ = json.NewEncoder(response).Encode(map[string]any{
				"changeRequests": []controlplane.ChangeRequest{{ID: "cr_one", Environment: "dev/use2"}},
			})
		case 2:
			if request.Method != http.MethodPost || request.URL.Path != "/v1/change-requests/cr_one/approve" {
				t.Fatalf("unexpected approve request: %s %s", request.Method, request.URL.Path)
			}
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["rationale"] != "digest reviewed" {
				t.Fatalf("body=%#v", body)
			}
			_ = json.NewEncoder(response).Encode(ChangeRequestActionResult{
				ChangeRequest: controlplane.ChangeRequest{ID: "cr_one", Status: "APPROVED"},
			})
		default:
			t.Fatal("unexpected extra request")
		}
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := adapter.ListChangeRequests(context.Background(), "dev/use2")
	if err != nil || len(listed) != 1 || listed[0].ID != "cr_one" {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
	approved, err := adapter.ApproveChangeRequest(context.Background(), "cr_one", "digest reviewed")
	if err != nil || approved.ChangeRequest.Status != "APPROVED" {
		t.Fatalf("approved=%#v err=%v", approved, err)
	}
}

// The 1 MiB response bound is what stops a compromised or malfunctioning Hub
// from making the operator read an unbounded body into memory, and nothing
// referenced it. Mutation testing kept both the bound and the extra byte alive:
// with LimitReader capped at the bound rather than one past it, an oversized
// response can never be detected, and every test still passed.
func TestHTTPControlPlaneBoundsTheResponseItWillRead(t *testing.T) {
	t.Parallel()
	// Padding keeps the document valid JSON while fixing its exact length.
	atLimit := func(size int) []byte {
		document := []byte(`{"id":"pln_bounded"}`)
		return append(document, bytes.Repeat([]byte(" "), size-len(document))...)
	}

	for _, test := range []struct {
		name      string
		size      int
		wantError bool
	}{
		{name: "exactly at the bound is read", size: maxOperatorResponseBytes},
		{name: "one byte past the bound is refused", size: maxOperatorResponseBytes + 1, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write(atLimit(test.size))
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			record, err := adapter.GetPlan(context.Background(), "pln_bounded")
			if test.wantError {
				if err == nil {
					t.Fatal("an oversized response was accepted")
				}
				if !strings.Contains(err.Error(), "exceeds") {
					t.Fatalf("error does not attribute the size bound: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("a response exactly at the bound was refused: %v", err)
			}
			if record.ID != "pln_bounded" {
				t.Fatalf("decoded record = %#v", record)
			}
		})
	}
}

// 2xx is the success range, and its edges were never asserted: 299 must be
// accepted and 300 must not. Mutation testing moved both boundaries without
// failing a test.
func TestHTTPControlPlaneTreatsTheSuccessRangeExactly(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		status    int
		wantError bool
	}{
		{status: http.StatusOK},
		{status: 299},
		{status: 300, wantError: true},
		{status: http.StatusInternalServerError, wantError: true},
	} {
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(`{"id":"pln_status"}`))
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			_, err = adapter.GetPlan(context.Background(), "pln_status")
			if test.wantError && err == nil {
				t.Fatalf("status %d was treated as success", test.status)
			}
			if !test.wantError && err != nil {
				t.Fatalf("status %d was treated as failure: %v", test.status, err)
			}
		})
	}
}

// version and releaseChannel are optional, and "optional" here means the key is
// absent rather than empty: the Hub distinguishes an unset channel from one set
// to "". Only the omission was exercised, so inverting the guard that omits it
// changed nothing any test observed.
func TestInstallModuleOmitsUnsetOptionalCoordinates(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name           string
		version        string
		releaseChannel string
		wantKeys       map[string]any
	}{
		{name: "both unset", wantKeys: map[string]any{}},
		{name: "channel only", releaseChannel: "RELEASE", wantKeys: map[string]any{"releaseChannel": "RELEASE"}},
		{
			name: "both set", version: "1.2.3", releaseChannel: "DEV",
			wantKeys: map[string]any{"version": "1.2.3", "releaseChannel": "DEV"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var received map[string]any
			var contentType string
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				contentType = request.Header.Get("Content-Type")
				if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
					t.Error(err)
				}
				_, _ = response.Write([]byte(`{}`))
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := adapter.InstallModule(
				context.Background(), "dev-use2", "example", test.version, test.releaseChannel, nil,
			); err != nil {
				t.Fatal(err)
			}
			// A request carrying a body must declare it; a bodyless one must not.
			if contentType != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", contentType)
			}
			for _, key := range []string{"version", "releaseChannel"} {
				want, expected := test.wantKeys[key]
				got, present := received[key]
				if expected != present {
					t.Errorf("key %q present=%v, want present=%v (body=%v)", key, present, expected, received)
				}
				if expected && got != want {
					t.Errorf("key %q = %v, want %v", key, got, want)
				}
			}
		})
	}
}

// fakeSPIFFESource satisfies the source interface without a Workload API. The
// constructor never dereferences it -- it only hands it to the TLS config -- so
// returning errors is enough to prove the client was built around it.
type fakeSPIFFESource struct{}

func (fakeSPIFFESource) GetX509SVID() (*x509svid.SVID, error) {
	return nil, errors.New("no SVID in this test")
}

func (fakeSPIFFESource) GetX509BundleForTrustDomain(spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	return nil, errors.New("no bundle in this test")
}

// NewSPIFFEHTTPClient is the production outbound transport: every operator call
// to the Hub goes through it. It pins the peer to one exact SPIFFE ID, which is
// what stops the operator authenticating to a Hub that merely holds a valid
// certificate from the same trust domain. None of it was covered.
func TestNewSPIFFEHTTPClientPinsTheHubIdentity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		source    SPIFFEX509Source
		hubID     string
		wantError string
	}{
		{name: "the pinned Hub", source: fakeSPIFFESource{}, hubID: controlplane.HubSPIFFEID},
		{
			name:   "surrounding whitespace is tolerated",
			source: fakeSPIFFESource{}, hubID: "  " + controlplane.HubSPIFFEID + "  ",
		},
		{name: "no source", hubID: controlplane.HubSPIFFEID, wantError: "source is required"},
		{
			name: "not a SPIFFE ID", source: fakeSPIFFESource{},
			hubID: "https://hub.example", wantError: "parse control plane SPIFFE ID",
		},
		{name: "empty", source: fakeSPIFFESource{}, wantError: "parse control plane SPIFFE ID"},
		// A valid SPIFFE ID in the right trust domain is still refused unless it
		// is the Hub's: holding a certificate the trust domain issued does not
		// make a peer the control plane.
		{
			name: "another workload in the same trust domain", source: fakeSPIFFESource{},
			hubID:     "spiffe://platform.delivery/environment/dev/agent/spoke-dev",
			wantError: "must be " + controlplane.HubSPIFFEID,
		},
		{
			name: "the Hub path in another trust domain", source: fakeSPIFFESource{},
			hubID: "spiffe://attacker.example/hub", wantError: "must be " + controlplane.HubSPIFFEID,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client, err := NewSPIFFEHTTPClient(test.source, test.hubID)
			if test.wantError != "" {
				if err == nil {
					t.Fatalf("%q was accepted", test.hubID)
				}
				if !strings.Contains(err.Error(), test.wantError) {
					t.Fatalf("error %q does not mention %q", err, test.wantError)
				}
				return
			}
			if err != nil {
				t.Fatalf("the pinned Hub ID was refused: %v", err)
			}
			// The timeout is the operator's only bound on a Hub that accepts a
			// connection and then does not answer.
			if client.Timeout != 30*time.Second {
				t.Fatalf("timeout = %v, want 30s", client.Timeout)
			}
			if client.Transport == nil {
				t.Fatal("client has no transport, so it would not use the SVID")
			}
		})
	}
}

// Every path this adapter builds interpolates an identifier the operator
// supplied -- a Product coordinate, an Environment name, a Plan id. Each is
// escaped, and that escaping is the only thing keeping the identifier inside
// its own path segment: resolving "/v1/products/../../v1/plans/pln_other/releases"
// against the base yields "/v1/plans/pln_other/releases", a different endpoint
// entirely. None of it was covered.
func TestOperatorPathsKeepIdentifiersInOneSegment(t *testing.T) {
	t.Parallel()
	const hostile = "../../v1/plans/pln_other"

	for _, test := range []struct {
		name string
		call func(*HTTPControlPlane) error
		want string
	}{
		{
			name: "product releases",
			call: func(a *HTTPControlPlane) error {
				_, err := a.ListProductReleases(context.Background(), hostile)
				return err
			},
			want: "/v1/products/" + url.PathEscape(hostile) + "/releases",
		},
		{
			name: "product release",
			call: func(a *HTTPControlPlane) error {
				_, err := a.GetProductRelease(context.Background(), hostile, hostile)
				return err
			},
			want: "/v1/products/" + url.PathEscape(hostile) + "/releases/" + url.PathEscape(hostile),
		},
		{
			name: "environment",
			call: func(a *HTTPControlPlane) error { _, err := a.GetEnvironment(context.Background(), hostile); return err },
			want: "/v1/environments/" + url.PathEscape(hostile),
		},
		{
			name: "environment entities",
			call: func(a *HTTPControlPlane) error { _, err := a.ListEntities(context.Background(), hostile); return err },
			want: "/v1/environments/" + url.PathEscape(hostile) + "/entities",
		},
		{
			name: "plan",
			call: func(a *HTTPControlPlane) error { _, err := a.GetPlan(context.Background(), hostile); return err },
			want: "/v1/plans/" + url.PathEscape(hostile),
		},
		{
			name: "reported state",
			call: func(a *HTTPControlPlane) error {
				_, err := a.GetReportedState(context.Background(), hostile)
				return err
			},
			want: "/v1/environments/" + url.PathEscape(hostile) + "/reported-state",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var raw string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// RequestURI is the unmodified target; r.URL.Path is already
				// decoded and would hide the escaping being tested.
				raw = r.RequestURI
				_, _ = w.Write([]byte(`{}`))
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := test.call(adapter); err != nil {
				t.Fatal(err)
			}
			if raw != test.want {
				t.Fatalf("requested %q, want %q -- the identifier escaped its segment", raw, test.want)
			}
		})
	}
}

// UploadArtifact does not go through request(): it streams a body, so it
// carries its own copy of the response bound, the success range and the decode.
// Pinning those on request() said nothing about this path, which is the one
// that moves artifact bytes.
func TestUploadArtifactStreamsAndBoundsItsResponse(t *testing.T) {
	t.Parallel()

	t.Run("streams the body and declares its media type", func(t *testing.T) {
		t.Parallel()
		var seenType, seenBody, seenPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seenType, seenPath = r.Header.Get("Content-Type"), r.URL.Path
			body, _ := io.ReadAll(r.Body)
			seenBody = string(body)
			_ = json.NewEncoder(w).Encode(controlplane.BlobDescriptor{
				Digest: "sha256:" + strings.Repeat("a", 64), Size: 5, MediaType: "application/gzip",
			})
		}))
		defer server.Close()
		adapter, err := NewHTTPControlPlane(server.URL, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		descriptor, err := adapter.UploadArtifact(
			context.Background(), "application/gzip", strings.NewReader("bytes"))
		if err != nil {
			t.Fatal(err)
		}
		if seenPath != "/v1/artifacts/blobs" {
			t.Errorf("path = %q", seenPath)
		}
		// The media type travels with the bytes; the Hub stores it alongside the
		// digest and cannot infer it from the content.
		if seenType != "application/gzip" {
			t.Errorf("content type = %q", seenType)
		}
		if seenBody != "bytes" {
			t.Errorf("body = %q", seenBody)
		}
		if descriptor.Digest != "sha256:"+strings.Repeat("a", 64) {
			t.Errorf("descriptor = %#v", descriptor)
		}
	})

	t.Run("the response bound is its own", func(t *testing.T) {
		t.Parallel()
		// A descriptor is a few hundred bytes; anything at this size is a Hub
		// misbehaving, and reading it unbounded is the thing being prevented.
		for _, test := range []struct {
			size      int
			wantError bool
		}{
			{size: maxOperatorResponseBytes},
			{size: maxOperatorResponseBytes + 1, wantError: true},
		} {
			document := []byte(`{"digest":"sha256:` + strings.Repeat("a", 64) + `"}`)
			padded := append(document, bytes.Repeat([]byte(" "), test.size-len(document))...)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(padded)
			}))
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				server.Close()
				t.Fatal(err)
			}
			_, err = adapter.UploadArtifact(context.Background(), "application/gzip", strings.NewReader("x"))
			server.Close()
			if (err != nil) != test.wantError {
				t.Errorf("a %d byte response errored=%v, want %v (%v)", test.size, err != nil, test.wantError, err)
			}
		}
	})

	t.Run("the success range is its own", func(t *testing.T) {
		t.Parallel()
		for _, test := range []struct {
			status    int
			wantError bool
		}{
			{status: http.StatusOK},
			{status: 299},
			{status: 300, wantError: true},
			{status: http.StatusRequestEntityTooLarge, wantError: true},
		} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(`{"digest":"sha256:` + strings.Repeat("a", 64) + `","error":"refused"}`))
			}))
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				server.Close()
				t.Fatal(err)
			}
			_, err = adapter.UploadArtifact(context.Background(), "application/gzip", strings.NewReader("x"))
			server.Close()
			if (err != nil) != test.wantError {
				t.Errorf("status %d errored=%v, want %v", test.status, err != nil, test.wantError)
			}
			// A refusal has to carry what the Hub said, or an operator sees a
			// bare status with no reason.
			if test.wantError && err != nil && !strings.Contains(err.Error(), "refused") {
				t.Errorf("status %d error %q omits the Hub's response", test.status, err)
			}
		}
	})
}

// A nil client is the documented way to take the default. The timeout that
// default carries is the only thing bounding an operator command against an
// unresponsive Hub, and nothing asserted either the substitution or the value.
func TestNewHTTPControlPlaneSuppliesABoundedDefaultClient(t *testing.T) {
	t.Parallel()

	t.Run("a nil client becomes one with a timeout", func(t *testing.T) {
		t.Parallel()
		adapter, err := NewHTTPControlPlane("https://hub.example", nil)
		if err != nil {
			t.Fatal(err)
		}
		if adapter.client == nil {
			t.Fatal("a nil client was left nil; the first request would panic")
		}
		// Without this an operator command hangs indefinitely against a Hub
		// that accepts the connection and never answers.
		if adapter.client.Timeout != 30*time.Second {
			t.Errorf("default timeout = %v, want 30s", adapter.client.Timeout)
		}
	})

	t.Run("a supplied client is used as given", func(t *testing.T) {
		t.Parallel()
		// Callers pass a SPIFFE-authenticated client here. Replacing it with a
		// default would drop the identity and talk to the Hub unauthenticated.
		supplied := &http.Client{Timeout: 7 * time.Second}
		adapter, err := NewHTTPControlPlane("https://hub.example", supplied)
		if err != nil {
			t.Fatal(err)
		}
		if adapter.client != supplied {
			t.Error("the supplied client was replaced")
		}
		if adapter.client.Timeout != 7*time.Second {
			t.Errorf("timeout = %v, want the supplied 7s", adapter.client.Timeout)
		}
	})
}

// The public publish body carries only the four fields ProductDependencyLocator
// declares. The adapter used to hand the native ApolloManifestDependency to the
// public route whole, and its recommendedVersion and optional tags are both
// omitempty -- so a manifest that happened not to set them looked fine, which is
// how TestHTTPControlPlaneKeepsManifestOnlyPublicationOnPinnedPublicRoute missed
// it: that fixture declares no dependencies at all. This one declares a plain
// dependency and asserts the projection field by field.
//
// On its own this case does not discriminate -- a dependency with neither extra
// field set serializes identically whether or not it is projected, because both
// tags are omitempty. It guards the projection as defence in depth, so that
// removing the routing predicate below degrades to a dropped field rather than
// to a 400. TestPublishRoutesRicherDependenciesToTheNativeRoute is the case that
// actually kills the bug.
func TestPublicPublishSendsOnlyContractDependencyFields(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v0/products/helmChart/com.example:application:1.2.3/publish" {
			t.Fatalf("a plain dependency should stay on the public route, got %s", request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		extensions := body["extensions"].(map[string]any)
		dependencies := extensions["productDependencies"].([]any)
		if len(dependencies) != 1 {
			t.Fatalf("dependencies = %#v", dependencies)
		}
		dependency := dependencies[0].(map[string]any)
		// The Hub decodes this body with DisallowUnknownFields, so any key the
		// contract does not declare is a 400 rather than a cosmetic extra.
		for _, contractField := range []string{"productGroup", "productName", "minimumVersion", "maximumVersion"} {
			if _, present := dependency[contractField]; !present {
				t.Errorf("contract field %q missing: %#v", contractField, dependency)
			}
		}
		if len(dependency) != 4 {
			t.Errorf("public dependency carries fields the contract does not declare: %#v", dependency)
		}
		_ = json.NewEncoder(response).Encode(struct{}{})
	}))
	defer server.Close()
	adapter, err := NewHTTPControlPlane(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.PublishProductRelease(context.Background(), publishBundleWithDependency(controlplane.ApolloManifestDependency{
		ProductGroup: "com.example", ProductName: "library", MinimumVersion: "1.0.0", MaximumVersion: "2.0.0",
	})); err != nil {
		t.Fatal(err)
	}
}

// A dependency the public contract cannot express goes to the Conduit-native
// route, which persists the whole thing. Dropping the flag to keep the public
// route would silently turn an optional dependency into a required one, which
// changes what the published Release means.
func TestPublishRoutesRicherDependenciesToTheNativeRoute(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name       string
		dependency controlplane.ApolloManifestDependency
	}{
		{"optional", controlplane.ApolloManifestDependency{
			ProductGroup: "com.example", ProductName: "library", MinimumVersion: "1.0.0", MaximumVersion: "2.0.0", Optional: true,
		}},
		{"recommended version", controlplane.ApolloManifestDependency{
			ProductGroup: "com.example", ProductName: "library", MinimumVersion: "1.0.0", MaximumVersion: "2.0.0", RecommendedVersion: "1.5.0",
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				if request.URL.Path != "/v1/product-publications/helm-chart" {
					t.Errorf("%s should route natively, got %s", testCase.name, request.URL.Path)
				}
				_ = json.NewEncoder(response).Encode(struct{}{})
			}))
			defer server.Close()
			adapter, err := NewHTTPControlPlane(server.URL, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := adapter.PublishProductRelease(context.Background(), publishBundleWithDependency(testCase.dependency)); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func publishBundleWithDependency(dependency controlplane.ApolloManifestDependency) controlplane.HelmProductReleaseBundle {
	digest := "sha256:" + strings.Repeat("a", 64)
	return controlplane.HelmProductReleaseBundle{Manifest: controlplane.ApolloProductManifest{
		ManifestVersion: "1.0", ProductGroup: "com.example", ProductName: "application", ProductVersion: "1.2.3", ProductType: "helm-chart.v1",
		Extensions: controlplane.ApolloProductManifestExtensions{
			HelmChart:           &controlplane.ApolloManifestHelmChart{Name: "application", Version: "1.2.3", RepositoryURL: "oci://registry.example/charts/application:1.2.3@" + digest},
			Artifacts:           []controlplane.ApolloManifestArtifact{{Type: "oci", URI: "registry.example/application:1.2.3@" + digest}},
			ProductDependencies: []controlplane.ApolloManifestDependency{dependency},
		},
	}}
}
