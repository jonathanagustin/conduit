package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	catalogv1 "github.com/jonathanagustin/conduit/gen/go/conduit/catalog/v1"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"google.golang.org/protobuf/encoding/protojson"
)

const productSummaryProcedure = "/conduit.catalog.v1.ProductReadService/ListProductSummaries"

func postProductSummaries(t *testing.T, handler http.Handler, actor, represented, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, productSummaryProcedure, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Connect-Protocol-Version", "1")
	if actor != "" {
		request.Header.Set("X-Platform-Subject", actor)
	}
	if represented != "" {
		request.Header.Set("X-Platform-Service-Account", "true")
		request.Header.Set("X-Platform-On-Behalf-Of", represented)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func addSummaryProduct(t *testing.T, repository *controlplane.MemoryRepository, product string, versions ...string) {
	t.Helper()
	if len(versions) == 0 {
		versions = []string{"metadata-only"}
	}
	for _, version := range versions {
		publication := controlplane.ProductPublication{Product: product, Version: version, MavenCoordinate: product + ":" + version, PublicationDigest: "sha256:" + product + version}
		if version != "metadata-only" {
			publication.Release = &controlplane.Release{Product: product, Version: version}
		}
		if _, err := repository.CreateProductPublication(t.Context(), publication); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProductSummaryConnectSharesAuthorizedNativePagination(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "product-readers", DisplayName: "Readers", AdminUsers: []string{"owner"}, MemberUsers: []string{"alice"}}); err != nil {
		t.Fatal(err)
	}
	for _, product := range []string{"team:alpha", "team:beta", "team:metadata"} {
		if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "product/" + product, Team: "product-readers", Operations: []string{controlplane.OperationReadResource}}); err != nil {
			t.Fatal(err)
		}
	}
	addSummaryProduct(t, repository, "team:alpha", "1.9.0", "1.10.0")
	addSummaryProduct(t, repository, "team:beta", "2.0.0")
	addSummaryProduct(t, repository, "team:metadata")
	addSummaryProduct(t, repository, "foreign:secret", "99.0.0")
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, principal := range []struct {
		actor string
		total int64
	}{{"alice", 3}, {"outsider", 0}, {"root", 4}} {
		wire := postProductSummaries(t, handler, principal.actor, "", `{}`)
		page := &catalogv1.ListProductSummariesResponse{}
		if wire.Code != http.StatusOK || protojson.Unmarshal(wire.Body.Bytes(), page) != nil || page.Total != principal.total || int64(len(page.Products)) != principal.total {
			t.Fatalf("%s: %d %s", principal.actor, wire.Code, wire.Body.String())
		}
		if principal.actor != "root" && strings.Contains(wire.Body.String(), "foreign") {
			t.Fatal("foreign product disclosed")
		}
	}
	values := url.Values{"view": {"summary"}, "limit": {"1"}, "search": {" TEAM "}}
	var products []string
	for index := 0; index < 3; index++ {
		native := driveAs(t, handler, http.MethodGet, "/v1/products?"+values.Encode(), "alice", "")
		var expected controlplane.ProductCatalogSummaryPage
		if native.Code != http.StatusOK || json.Unmarshal(native.Body.Bytes(), &expected) != nil {
			t.Fatalf("REST: %d %s", native.Code, native.Body.String())
		}
		body, _ := json.Marshal(map[string]any{"limit": 1, "cursor": values.Get("cursor"), "search": values.Get("search")})
		wire := postProductSummaries(t, handler, "alice", "", string(body))
		actual := &catalogv1.ListProductSummariesResponse{}
		if wire.Code != http.StatusOK || protojson.Unmarshal(wire.Body.Bytes(), actual) != nil {
			t.Fatalf("RPC: %d %s", wire.Code, wire.Body.String())
		}
		if actual.Total != int64(expected.Total) || actual.PageSize != int32(expected.PageSize) || actual.Limit != int32(expected.Limit) || actual.NextCursor != expected.NextCursor || actual.PreviousCursor != expected.PreviousCursor || actual.HasPrevious != expected.HasPrevious || len(actual.Products) != len(expected.Products) {
			t.Fatalf("page metadata changed: REST=%+v RPC=%v", expected, actual)
		}
		for i, product := range expected.Products {
			if actual.Products[i].Product != product.Product || actual.Products[i].ReleaseCount != int64(product.ReleaseCount) || actual.Products[i].LatestVersion != product.LatestVersion {
				t.Fatalf("projection changed: REST=%+v RPC=%v", product, actual.Products[i])
			}
			products = append(products, product.Product)
		}
		if index == 0 {
			if actual.Products[0].ReleaseCount != 2 || actual.Products[0].LatestVersion != "1.10.0" {
				t.Fatal("native semantic version order lost")
			}
			bad, _ := json.Marshal(map[string]any{"limit": 1, "cursor": actual.NextCursor, "search": "beta"})
			if response := postProductSummaries(t, handler, "alice", "", string(bad)); response.Code != http.StatusBadRequest {
				t.Fatalf("cursor not bound to filters: %d %s", response.Code, response.Body.String())
			}
			changed, _ := json.Marshal(map[string]any{"limit": 1, "cursor": actual.NextCursor, "search": "different"})
			if mismatch := postProductSummaries(t, handler, "alice", "", string(changed)); mismatch.Code != http.StatusBadRequest {
				t.Fatalf("cursor changed filters: %d %s", mismatch.Code, mismatch.Body.String())
			}
		}
		values.Set("cursor", expected.NextCursor)
	}
	if strings.Join(products, ",") != "team:alpha,team:beta,team:metadata" || values.Get("cursor") != "" {
		t.Fatalf("order/cursor: %v %v", products, values)
	}
	for _, query := range []struct{ rest, wire string }{{"limit=0", `{"limit":0}`}, {"limit=201", `{"limit":201}`}, {"cursor=malformed", `{"cursor":"malformed"}`}} {
		native := driveAs(t, handler, http.MethodGet, "/v1/products?view=summary&"+query.rest, "alice", "")
		wire := postProductSummaries(t, handler, "alice", "", query.wire)
		if native.Code != http.StatusUnprocessableEntity || wire.Code != http.StatusBadRequest || !strings.Contains(wire.Body.String(), "invalid_argument") {
			t.Fatalf("input parity: REST=%d RPC=%d %s", native.Code, wire.Code, wire.Body.String())
		}
	}
}

func TestProductSummaryConnectRejectsAnonymousAndUnregisteredRequests(t *testing.T) {
	handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	production, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"", "root"} {
		if wire := postProductSummaries(t, production, actor, "", "not JSON"); wire.Code != http.StatusUnauthorized {
			t.Fatalf("production unauthenticated request: %d %s", wire.Code, wire.Body.String())
		}
	}
	// Explicit insecure development mode retains native REST's local fallback.
	native := driveAs(t, handler, http.MethodGet, "/v1/products?view=summary", "", "")
	wire := postProductSummaries(t, handler, "", "", `{}`)
	if native.Code != http.StatusOK || wire.Code != http.StatusOK {
		t.Fatalf("development fallback parity: REST=%d RPC=%d", native.Code, wire.Code)
	}
	if wire := postProductSummaries(t, handler, "root", "", "not JSON"); wire.Code != http.StatusBadRequest {
		t.Fatalf("malformed request: %d %s", wire.Code, wire.Body.String())
	}
	for _, target := range []struct{ method, path string }{{http.MethodGet, productSummaryProcedure}, {http.MethodPost, "/conduit.catalog.v1.ProductReadService/GetProduct"}} {
		wire := driveAs(t, handler, target.method, target.path, "root", "")
		if wire.Code != http.StatusNotFound && wire.Code != http.StatusMethodNotAllowed {
			t.Fatalf("unregistered route served: %s %s %d %s", target.method, target.path, wire.Code, wire.Body.String())
		}
	}
}

func TestProductSummaryConnectPreservesDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	addSummaryProduct(t, repository, "private:catalog", "1.0.0")
	handler, err := NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		actor, represented string
		count              int64
		status             int
	}{{"alice", "", 1, 200}, {"alice", "bob", 1, 200}, {"robot", "alice", 0, 200}, {"bob", "alice", 0, 403}, {"bob", "", 0, 200}} {
		native := as(t, handler, "/v1/products?view=summary", tc.actor, tc.represented)
		wire := postProductSummaries(t, handler, tc.actor, tc.represented, `{}`)
		if native.Code != tc.status || wire.Code != tc.status {
			t.Fatalf("%s as %s REST=%d %s RPC=%d %s", tc.actor, tc.represented, native.Code, native.Body.String(), wire.Code, wire.Body.String())
		}
		if tc.status == http.StatusOK {
			page := &catalogv1.ListProductSummariesResponse{}
			if protojson.Unmarshal(wire.Body.Bytes(), page) != nil || page.Total != tc.count {
				t.Fatalf("carrier authority changed: %s", wire.Body.String())
			}
		}
	}
}
