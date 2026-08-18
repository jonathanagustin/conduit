package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// apolloProtocolFixture builds a handler holding one published Product and two
// narrowly scoped principals, so the same read can be asked over REST and over
// GraphQL and the two answers compared.
//
// The two shapes are the ones #209 names and the existing tests do not cover:
// a principal holding only platform/root read, and a principal holding only
// product read. Neither is an administrator, so nothing short-circuits the
// grant lookup.
func apolloProtocolFixture(t *testing.T) http.Handler {
	t.Helper()
	ctx := context.Background()
	repository := NewMemoryRepository()
	governance := NewGovernanceService(repository)
	for _, team := range []Team{
		{Name: "platform-readers", DisplayName: "Platform readers",
			AdminUsers: []string{"platform-reader"}, MemberUsers: []string{"platform-reader"}},
		{Name: "product-readers", DisplayName: "Product readers",
			AdminUsers: []string{"product-reader"}, MemberUsers: []string{"product-reader"}},
		{Name: "publishers", DisplayName: "Publishers",
			AdminUsers: []string{"publisher"}, MemberUsers: []string{"publisher"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []ResourceGrant{
		{ResourceRID: "platform/root", Team: "platform-readers", Operations: []string{OperationReadResource}},
		{ResourceRID: "global/product", Team: "product-readers", Operations: []string{OperationReadResource}},
		{ResourceRID: "global/artifact", Team: "publishers", Roles: []string{"apollo-artifact:creator", "apollo-artifact:viewer"}},
		{ResourceRID: "global/product", Team: "publishers", Roles: []string{"product:creator", "product:release-creator", "product:viewer"}},
	} {
		if _, err := governance.PutGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"admin"},
	})
	publish := httptest.NewRequest(http.MethodPost,
		"/api/v0/products/service/com.example:app:1.0.0/publish", strings.NewReader(`{"extensions":{}}`))
	publish.Header.Set("Content-Type", "application/json")
	publish.Header.Set("X-Platform-Subject", "publisher")
	publish.Header.Set("X-Platform-Service-Account", "true")
	published := httptest.NewRecorder()
	handler.ServeHTTP(published, publish)
	if published.Code != http.StatusOK {
		t.Fatalf("fixture publication = %d %s", published.Code, published.Body.String())
	}
	return handler
}

func apolloRESTGet(t *testing.T, handler http.Handler, subject, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Platform-Subject", subject)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// graphqlServedProducts reports whether a GraphQL response actually carried
// Product records, as opposed to refusing. A 403 carries none; a 200 with the
// projection bound carries the catalogue.
func graphqlServedProducts(t *testing.T, response *httptest.ResponseRecorder) (int, []string) {
	t.Helper()
	if response.Code != http.StatusOK {
		return response.Code, nil
	}
	var decoded struct {
		Data struct {
			Apollo struct {
				Products struct {
					Products []struct {
						ProductID string `json:"productId"`
					} `json:"products"`
				} `json:"products"`
			} `json:"apollo"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode GraphQL response: %v (%s)", err, response.Body.String())
	}
	served := []string{}
	for _, product := range decoded.Data.Apollo.Products.Products {
		served = append(served, product.ProductID)
	}
	return response.Code, served
}

// The invariant internal/apollographql/ports states outright: "a client cannot
// get a different answer by choosing a protocol." #209 claims two principal
// shapes break it in opposite directions, and the disclosure direction is the
// one that matters:
//
//   - a principal holding only platform/root read is refused the Products
//     coordinate over REST (it authorizes global/product, and platform/root
//     expands to itself alone) but served the whole catalogue over GraphQL,
//     because the GraphQL guard bars on platform/root and nothing else;
//   - a principal holding only product read is served over REST and refused
//     over GraphQL by that same single bar.
//
// The first row is a leak. Products carry their Releases, so a platform-scoped
// reader reaches release manifests GraphQL has no business handing them.
func TestApolloGraphQLProductReadsAgreeWithREST(t *testing.T) {
	t.Parallel()
	handler := apolloProtocolFixture(t)
	const productsQuery = `{ apollo { products(pageSize: 50) { products { productId } } } }`

	// Direction one: platform-scoped. REST refuses; GraphQL must not serve.
	restPlatform := apolloRESTGet(t, handler, "platform-reader", "/api/v0/products")
	graphqlPlatformStatus, graphqlPlatformProducts := graphqlServedProducts(
		t, graphqlRequest(t, handler, "platform-reader", productsQuery))
	restPlatformServed := restPlatform.Code == http.StatusOK
	graphqlPlatformServed := graphqlPlatformStatus == http.StatusOK && len(graphqlPlatformProducts) > 0
	if restPlatformServed != graphqlPlatformServed {
		t.Errorf(
			"a platform-scoped principal got a different answer by choosing a protocol: "+
				"REST /api/v0/products = %d, GraphQL = %d serving %v",
			restPlatform.Code, graphqlPlatformStatus, graphqlPlatformProducts)
	}

	// Direction two: product-scoped. REST serves; GraphQL must serve too.
	restProduct := apolloRESTGet(t, handler, "product-reader", "/api/v0/products")
	graphqlProductStatus, graphqlProductProducts := graphqlServedProducts(
		t, graphqlRequest(t, handler, "product-reader", productsQuery))
	restProductServed := restProduct.Code == http.StatusOK
	graphqlProductServed := graphqlProductStatus == http.StatusOK && len(graphqlProductProducts) > 0
	if restProductServed != graphqlProductServed {
		t.Errorf(
			"a product-scoped principal got a different answer by choosing a protocol: "+
				"REST /api/v0/products = %d, GraphQL = %d serving %v",
			restProduct.Code, graphqlProductStatus, graphqlProductProducts)
	}
}

// The same comparison one coordinate deeper. GetProduct and ListReleases
// authorize product/<id> over REST, so a platform-scoped principal is refused
// the Release manifests; GraphQL reaches the same manifests through
// products { releases }, and must refuse them for the same principal.
func TestApolloGraphQLReleaseReadsAgreeWithREST(t *testing.T) {
	t.Parallel()
	handler := apolloProtocolFixture(t)
	const releasesQuery = `{ apollo { products(pageSize: 50) { products { productId productReleases(pageSize: 50) { productReleases { mavenCoordinate } } } } } }`

	for _, principal := range []struct {
		subject string
		name    string
	}{
		{subject: "platform-reader", name: "platform-scoped"},
		{subject: "product-reader", name: "product-scoped"},
	} {
		rest := apolloRESTGet(t, handler, principal.subject, "/api/v0/products/com.example:app/releases")
		response := graphqlRequest(t, handler, principal.subject, releasesQuery)
		restServed := rest.Code == http.StatusOK
		graphqlServed := response.Code == http.StatusOK && strings.Contains(response.Body.String(), `"com.example:app:1.0.0"`)
		if restServed != graphqlServed {
			t.Errorf(
				"a %s principal got a different answer for Releases by choosing a protocol: "+
					"REST = %d, GraphQL = %d %s",
				principal.name, rest.Code, response.Code, response.Body.String())
		}
	}
}

// The Environment and namespace coordinates authorize platform/root over REST,
// so the agreement runs the other way there: the platform-scoped principal is
// served and the product-scoped one refused. Pinning it keeps a fix for the
// Product rows from being written as "authorize global/product for everything".
func TestApolloGraphQLEnvironmentReadsAgreeWithREST(t *testing.T) {
	t.Parallel()
	handler := apolloProtocolFixture(t)
	const environmentsQuery = `{ apollo { environments(pageSize: 50) { environments { rid } } } }`

	for _, principal := range []string{"platform-reader", "product-reader"} {
		rest := apolloRESTGet(t, handler, principal, "/api/v0/environments")
		response := graphqlRequest(t, handler, principal, environmentsQuery)
		restServed := rest.Code == http.StatusOK
		graphqlServed := response.Code == http.StatusOK && !strings.Contains(response.Body.String(), `"errors"`)
		if restServed != graphqlServed {
			t.Errorf(
				"principal %q got a different answer for Environments by choosing a protocol: "+
					"REST = %d, GraphQL = %d %s",
				principal, rest.Code, response.Code, response.Body.String())
		}
	}
}
