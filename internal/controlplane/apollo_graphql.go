package controlplane

import (
	"fmt"
	"net/http"

	apollo "github.com/jonathanagustin/conduit/internal/apollo"
	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"

	"github.com/jonathanagustin/conduit/internal/apollographql/adapters/httpgraphql"
	graphqlapp "github.com/jonathanagustin/conduit/internal/apollographql/application"
	graphqlports "github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

// registerApolloGraphQL mounts the Apollo-shaped GraphQL interface.
//
// Two things are worth noting at the mount point, because they are easy to lose
// once the routes are just two more lines in a long list.
//
// The interface is part recovered and part invented, and it says which per
// field. GET .../schema serves the annotated SDL and every response discloses
// the invented coordinates it served, so a client never has to take Conduit's
// Apollo shape as an Apollo compatibility claim.
//
// Sources are bound per request from the authenticated principal, via the same
// constructor the console's overview uses. The resolver never sees a reader's
// identity and never filters: what a caller may see was decided before the
// query was parsed.
func registerApolloGraphQL(
	mux *http.ServeMux,
	api *apolloPublicAPI,
	repository Repository,
	governance *GovernanceService,
	configuration HandlerOptions,
) error {
	resolver, err := graphqlapp.New()
	if err != nil {
		return err
	}
	handler := httpgraphql.New(resolver, func(
		writer http.ResponseWriter, request *http.Request,
	) (graphqlports.Sources, bool) {
		principal, allowed := requireOperatorPrincipal(writer, request, governance, configuration)
		if !allowed {
			return graphqlports.Sources{}, false
		}
		// The same bar the Apollo REST surface sets, because this serves the
		// same data. Every Apollo OpenAPI read calls authorizeOperator with
		// OperationReadResource against platform/root; without this, a
		// principal holding nothing got the whole projection -- every
		// Environment, installation, namespace and recall the Hub governs --
		// by choosing GraphQL over REST. That is precisely what
		// internal/apollographql/ports says cannot happen: "a client cannot
		// get a different answer by choosing a protocol."
		//
		// Refusing the request rather than binding a nil or empty projection
		// is deliberate. ListEnvironments and its siblings return a response
		// with no error, so an unauthorized caller handed an empty projection
		// would read "no environments" -- the unknown-reads-as-none
		// conflation the same file says the provenance model exists to
		// prevent. A refusal a client can see is the only honest answer this
		// interface can give.
		//
		// It gates the Overview source with it as well, which is a widening
		// this endpoint can afford: nothing in web/src calls
		// /v1/apollo/graphql, so the console reaches overview data through its
		// own authorized routes rather than through here.
		if !requirePlatformOperation(
			writer, request, governance, principal, configuration, OperationReadResource,
		) {
			return graphqlports.Sources{}, false
		}
		return graphqlports.Sources{
			// The Apollo projection is nil when its initialization failed. A nil
			// source refuses its half of the interface rather than taking the
			// process down, which is the same choice the REST surface makes.
			Apollo:   apolloProjectionOrNil(api),
			Overview: newOverviewSource(request.Context(), repository, governance, principal, configuration),
		}, true
	})
	mux.HandleFunc("GET /v1/apollo/graphql/schema", handler.Schema)
	mux.HandleFunc("POST /v1/apollo/graphql", handler.Query)
	return nil
}

func apolloProjectionOrNil(api *apolloPublicAPI) graphqlports.ApolloProjection {
	if api == nil || api.application == nil {
		return nil
	}
	return apolloProjectionAdapter{service: api.application}
}

// apolloProjectionAdapter translates the Apollo application's error kinds into
// the one distinction the GraphQL port draws: a record that is absent, versus a
// failure to answer.
//
// It exists because the two contexts spell "not found" differently and neither
// should learn the other's spelling. The port declares ErrRecordNotFound; the
// Apollo application declares Error{Kind: NotFound}; this is the composition
// point that already binds them, so it is where the translation belongs.
//
// Everything that is not a not-found passes through unchanged, so the resolver
// propagates it. That is the direction that matters: before this, the resolver
// turned every error into a null, and a store that was down was served as an
// environment that does not exist.
type apolloProjectionAdapter struct{ service *apolloapp.Service }

func (adapter apolloProjectionAdapter) ListEnvironments() apollo.ListEnvironmentsResponse {
	return adapter.service.ListEnvironments()
}

func (adapter apolloProjectionAdapter) ListInstallations(environmentRID string) apollo.ListInstallationsResponse {
	return adapter.service.ListInstallations(environmentRID)
}

func (adapter apolloProjectionAdapter) ListProducts() apollo.ListProductsResponse {
	return adapter.service.ListProducts()
}

func (adapter apolloProjectionAdapter) ListReleases(product string) apollo.ListReleasesResponse {
	return adapter.service.ListReleases(product)
}

func (adapter apolloProjectionAdapter) GetEnvironment(rid string) (apollo.Environment, error) {
	value, err := adapter.service.GetEnvironment(rid)
	return value, apolloProjectionError(err)
}

func (adapter apolloProjectionAdapter) GetProduct(product string) (apollo.Product, error) {
	value, err := adapter.service.GetProduct(product)
	return value, apolloProjectionError(err)
}

func (adapter apolloProjectionAdapter) GetRelease(product, version string) (apollo.Release, error) {
	value, err := adapter.service.GetRelease(product, version)
	return value, apolloProjectionError(err)
}

func apolloProjectionError(err error) error {
	if err == nil {
		return nil
	}
	if apolloapp.IsKind(err, apolloapp.NotFound) {
		return fmt.Errorf("%w: %s", graphqlports.ErrRecordNotFound, err.Error())
	}
	return err
}
