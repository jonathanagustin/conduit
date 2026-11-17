package composition

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	assetmemory "github.com/jonathanagustin/conduit/internal/productassets/adapters/memory"
	assetapp "github.com/jonathanagustin/conduit/internal/productassets/application"
	assets "github.com/jonathanagustin/conduit/internal/productassets/domain"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
	spaces "github.com/jonathanagustin/conduit/internal/spaces/domain"
)

func assetManifest() assets.Manifest {
	return assets.Manifest{APIVersion: assets.SchemaVersion, ProductGroup: "io.conduit", ProductName: "cli", Version: "1.2.3", Assets: []assets.Asset{{Name: "cli-linux.tar.gz", Target: "linux/amd64", URI: "https://example.org/cli.tar.gz", SHA256: strings.Repeat("a", 64)}}}
}
func assetBody(t *testing.T, m assets.Manifest) string {
	t.Helper()
	b, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func TestNativeDownloadCatalogueUsesPublicationPermissionsAndCannotExecute(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	store := assetmemory.New()
	governance := controlplane.NewGovernanceService(repository)
	if _, e := governance.PutTeam(t.Context(), controlplane.Team{Name: "writers", DisplayName: "Writers", AdminUsers: []string{"writer"}, MemberUsers: []string{"writer"}}); e != nil {
		t.Fatal(e)
	}
	for _, grant := range []controlplane.ResourceGrant{{ResourceRID: "global/artifact", Team: "writers", Operations: []string{controlplane.OperationCreateArtifact}}, {ResourceRID: "product/io.conduit:cli", Team: "writers", Operations: []string{controlplane.OperationCreateRelease}}} {
		if _, e := governance.PutGrant(t.Context(), grant); e != nil {
			t.Fatal(e)
		}
	}
	handler, e := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, Options{ProductAssets: store})
	if e != nil {
		t.Fatal(e)
	}
	m := assetManifest()
	body := assetBody(t, m)
	endpoint := "/v1/product-publications/download-assets"
	if response := spaceRequest(handler, "writer", http.MethodPost, endpoint, body); response.Code != http.StatusForbidden {
		t.Fatalf("new Product creation bypassed: %d %s", response.Code, response.Body)
	}
	if _, e := store.Get(t.Context(), m.Coordinate()); !errors.Is(e, assets.ErrNotFound) {
		t.Fatal("denied creation prepared intent", e)
	}
	response := spaceRequest(handler, "admin", http.MethodPost, endpoint, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("publish: %d %s", response.Code, response.Body)
	}
	var acknowledgement assetapp.PublicationResult
	if e := json.Unmarshal(response.Body.Bytes(), &acknowledgement); e != nil {
		t.Fatal(e)
	}
	if acknowledgement.Status != "CATALOGUED_NOT_EXECUTABLE" || acknowledgement.ValueTier != "CONDUIT_NATIVE" {
		t.Fatal(response.Body)
	}
	response = spaceRequest(handler, "writer", http.MethodPost, endpoint, body)
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "preparedBy") {
		t.Fatalf("write-only retry: %d %s", response.Code, response.Body)
	}
	path := "/v1/products/" + m.Product() + "/releases/" + m.Version + "/download-assets"
	if response := spaceRequest(handler, "writer", http.MethodGet, path, ""); response.Code != http.StatusForbidden {
		t.Fatalf("write implies read: %d %s", response.Code, response.Body)
	}
	response = spaceRequest(handler, "admin", http.MethodGet, path, "")
	var published assetapp.Published
	if e := json.Unmarshal(response.Body.Bytes(), &published); e != nil {
		t.Fatal(e)
	}
	if response.Code != http.StatusOK || published.Metadata.PreparedBy != "admin" || published.Executable || len(published.Channels) != 3 {
		t.Fatalf("published metadata: %d %s", response.Code, response.Body)
	}
	records, e := repository.ListProductPublications(t.Context(), m.Product())
	if e != nil || len(records) != 1 || records[0].Release != nil || records[0].Kind != assets.Kind {
		t.Fatal("download catalogue acquired executable release", records, e)
	}
	catalogue, e := controlplane.ListProductCatalog(t.Context(), repository)
	if e != nil || len(catalogue) != 1 || len(catalogue[0].Releases) != 0 {
		t.Fatal("download declaration became executable inventory", catalogue, e)
	}
	m.Assets[0].SHA256 = strings.Repeat("b", 64)
	response = spaceRequest(handler, "admin", http.MethodPost, endpoint, assetBody(t, m))
	if response.Code != http.StatusConflict {
		t.Fatalf("immutable release changed: %d %s", response.Code, response.Body)
	}
	for _, body := range []string{strings.TrimSuffix(body, "}") + `,"preparedBy":"forged"}`, body + ` {}`, `{"private":"PVT_ASSET_PAYLOAD"`} {
		response = spaceRequest(handler, "admin", http.MethodPost, endpoint, body)
		if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "PVT_ASSET_PAYLOAD") {
			t.Fatalf("invalid declaration or diagnostic leak: %d %s", response.Code, response.Body)
		}
	}
}
func TestDownloadPublicationSpaceSelectionCannotMoveGrouping(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	store := assetmemory.New()
	grouping := spacememory.New()
	handler, e := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, Options{ProductAssets: store, Spaces: grouping})
	if e != nil {
		t.Fatal(e)
	}
	first, second := createNativeSpace(t, handler, "First"), createNativeSpace(t, handler, "Second")
	m := assetManifest()
	body := assetBody(t, m)
	endpoint := "/v1/product-publications/download-assets?spaceId="
	response := spaceRequest(handler, "admin", http.MethodPost, endpoint+first.ID, body)
	if response.Code != http.StatusCreated {
		t.Fatalf("selected publication: %d %s", response.Code, response.Body)
	}
	response = spaceRequest(handler, "admin", http.MethodPost, endpoint+second.ID, body)
	if response.Code != http.StatusConflict {
		t.Fatalf("grouping moved: %d %s", response.Code, response.Body)
	}
	saved, e := grouping.GetBinding(t.Context(), spaces.Resource{Kind: spaces.Product, ID: m.Product()})
	if e != nil || saved.SpaceID != first.ID || saved.CreatedBy != "admin" {
		t.Fatal(saved, e)
	}
	m.Version = "1.2.4"
	for _, query := range []string{"%zz", first.ID + "%zz", first.ID + ";ignored"} {
		response = spaceRequest(handler, "admin", http.MethodPost, endpoint+query, assetBody(t, m))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("malformed selection accepted: %d %s", response.Code, response.Body)
		}
		if _, err := store.Get(t.Context(), m.Coordinate()); !errors.Is(err, assets.ErrNotFound) {
			t.Fatalf("malformed selection prepared metadata: %v", err)
		}
	}
	response = spaceRequest(handler, "admin", http.MethodPost, endpoint+"invalid", assetBody(t, m))
	if response.Code != http.StatusBadRequest {
		t.Fatal(response.Code, response.Body)
	}
	if _, e := store.Get(t.Context(), m.Coordinate()); !errors.Is(e, assets.ErrNotFound) {
		t.Fatal("invalid selection prepared metadata", e)
	}
}
func TestDownloadRoutesSanitizeIdentityFailuresAndRemainAbsentWithoutStorage(t *testing.T) {
	for _, configured := range []bool{false, true} {
		options := Options{}
		if configured {
			options.ProductAssets = assetmemory.New()
		}
		handler, e := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
			return controlplane.IdentityClaims{}, errors.New("PVT_IDENTITY_PROVIDER")
		}}, options)
		if e != nil {
			t.Fatal(e)
		}
		request := httptest.NewRequest(http.MethodGet, "/v1/products/io.conduit:cli/download-releases", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		expected := http.StatusNotFound
		if configured {
			expected = http.StatusUnauthorized
		}
		if response.Code != expected || strings.Contains(response.Body.String(), "PVT_IDENTITY_PROVIDER") {
			t.Fatal(response.Code, response.Body)
		}
	}
}

func TestDownloadPublicationDoesNotInheritDelegatedBootstrapAuthority(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	store := assetmemory.New()
	if _, e := governance.PutTeam(t.Context(), controlplane.Team{Name: "automation", DisplayName: "Automation", AdminUsers: []string{"robot"}, MemberUsers: []string{"robot", "admin"}}); e != nil {
		t.Fatal(e)
	}
	if _, e := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "platform/root", Team: "automation", Operations: []string{controlplane.OperationActOnBehalf}}); e != nil {
		t.Fatal(e)
	}
	handler, e := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, Options{ProductAssets: store})
	if e != nil {
		t.Fatal(e)
	}
	m := assetManifest()
	request := httptest.NewRequest(http.MethodPost, "/v1/product-publications/download-assets", strings.NewReader(assetBody(t, m)))
	request.Header.Set("X-Platform-Subject", "robot")
	request.Header.Set("X-Platform-Service-Account", "true")
	request.Header.Set("X-Platform-On-Behalf-Of", "admin")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatal("delegated bootstrap escalation", response.Code, response.Body)
	}
	if _, e := store.Get(t.Context(), m.Coordinate()); !errors.Is(e, assets.ErrNotFound) {
		t.Fatal("denied delegated publication prepared metadata", e)
	}
}

func TestRetiredDownloadCoordinateCannotBeRepublishedOrReadAsActive(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	store := assetmemory.New()
	handler, e := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"admin"}}, Options{ProductAssets: store})
	if e != nil {
		t.Fatal(e)
	}
	m := assetManifest()
	endpoint := "/v1/product-publications/download-assets"
	body := assetBody(t, m)
	response := spaceRequest(handler, "admin", http.MethodPost, endpoint, body)
	if response.Code != http.StatusCreated {
		t.Fatal(response.Code, response.Body)
	}
	if _, e := repository.CreateCatalogRetirement(t.Context(), controlplane.CatalogRetirement{Scope: controlplane.CatalogRetirementRelease, Product: m.Product(), Version: m.Version, Rationale: "obsolete", RetiredBy: "admin", ChangeRequestID: "crq_retire_cli", RetiredAt: time.Now().UTC(), Revision: 1}); e != nil {
		t.Fatal(e)
	}
	response = spaceRequest(handler, "admin", http.MethodPost, endpoint, body)
	if response.Code != http.StatusConflict {
		t.Fatal("retired coordinate republished", response.Code, response.Body)
	}
	response = spaceRequest(handler, "admin", http.MethodGet, "/v1/products/"+m.Product()+"/releases/"+m.Version+"/download-assets", "")
	if response.Code != http.StatusNotFound {
		t.Fatal("retired release served as active", response.Code, response.Body)
	}
	response = spaceRequest(handler, "admin", http.MethodGet, "/v1/products/"+m.Product()+"/download-releases", "")
	if response.Code != http.StatusOK || response.Body.String() != "{\"releases\":[]}\n" {
		t.Fatal("retired release listed", response.Code, response.Body)
	}
	if _, e := store.Get(t.Context(), m.Coordinate()); e != nil {
		t.Fatal("retirement erased metadata evidence", e)
	}
}
