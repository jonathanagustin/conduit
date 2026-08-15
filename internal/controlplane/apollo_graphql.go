package controlplane

import (
	"net/http"

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
	return api.application
}
