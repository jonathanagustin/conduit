package controlplane

import (
	"context"
	"net/http"

	"github.com/jonathanagustin/conduit/internal/apollographql/adapters/authorizedprojection"
	"github.com/jonathanagustin/conduit/internal/apollographql/adapters/compatibilityprojection"
	"github.com/jonathanagustin/conduit/internal/apollographql/adapters/httpgraphql"
	"github.com/jonathanagustin/conduit/internal/apollographql/adapters/nativegrouping"
	graphqlapp "github.com/jonathanagustin/conduit/internal/apollographql/application"
	graphqlports "github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

// graphQLProjectionReader supplies only snapshots used by the read binder.
// Governance and compatibility projections are supplied separately.
type graphQLProjectionReader interface {
	OverviewProjectionReader
	ListBundles(context.Context, string) ([]BundleEnvelope, error)
}

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
// authority-bearing principal and never filters: what a caller may see was
// decided before the query was parsed. It additionally receives only the caller-bound native
// current-user snapshot, never an unresolved identity header.
func registerApolloGraphQL(
	mux *http.ServeMux,
	api *apolloPublicAPI,
	repository graphQLProjectionReader,
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
		// Authorization matches the Apollo REST surface read for read, which it
		// did not before: this barred every query on platform/root while REST
		// authorizes Environments there, the catalogue on global/product and one
		// Product on product/<id>. Governance expands neither coordinate into
		// the other, so the surfaces refused each other's readers in opposite
		// directions -- the measured table is in
		// internal/apollographql/domain/authorization.go. The rule now lives
		// beside the port it guards, so this binder names no coordinate itself.
		reads := func(resourceRID string) bool {
			return requireResourceOperation(newApolloResponseCapture(), request, governance,
				principal, configuration, resourceRID, OperationReadResource)
		}
		// One decision is still about the request rather than a field: a reader
		// who may see nothing at all gets Governance's status, because a
		// document of nothing but refusals is a worse answer than the refusal.
		projection := apolloProjectionOrNil(request.Context(), api)
		if !authorizedprojection.AnyReadable(projection, reads) {
			writeLifecycleError(writer, ErrForbidden)
			return graphqlports.Sources{}, false
		}
		var lookupSpaceID func(context.Context, string, string) (string, error)
		if configuration.SpaceIDLookup != nil {
			lookupSpaceID = func(ctx context.Context, kind, id string) (string, error) {
				return configuration.SpaceIDLookup(ctx, principal, kind, id)
			}
		}
		return graphqlports.Sources{
			CurrentUser: &graphqlports.CurrentUser{ID: principal.Username},
			Grouping: nativegrouping.Source{
				LookupSpaceID: lookupSpaceID,
				CanReadRoot:   func() bool { return reads("platform/root") },
				ReadBundleIDs: func(ctx context.Context) ([]string, error) {
					bundles, err := repository.ListBundles(ctx, "")
					if err != nil {
						return nil, err
					}
					ids := make([]string, 0, len(bundles))
					for _, bundle := range bundles {
						ids = append(ids, bundle.Manifest.ID)
					}
					return ids, nil
				},
			},
			// The Apollo projection is nil when its initialization failed. A nil
			// source refuses its half of the interface rather than taking the
			// process down, which is the same choice the REST surface makes.
			Apollo:   authorizedprojection.New(projection, reads),
			Overview: newOverviewSource(request.Context(), repository, governance, principal, configuration),
		}, true
	})
	mux.HandleFunc("GET /v1/apollo/graphql/schema", handler.Schema)
	mux.HandleFunc("POST /v1/apollo/graphql", handler.Query)
	return nil
}

func apolloProjectionOrNil(ctx context.Context, api *apolloPublicAPI) graphqlports.ApolloProjection {
	if api == nil || api.application == nil {
		return nil
	}
	return compatibilityprojection.New(ctx, api.application)
}
