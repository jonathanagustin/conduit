package controlplane

import (
	"context"
	"fmt"
	"net/http"

	apollo "github.com/jonathanagustin/conduit/internal/apollo"
	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"

	"github.com/jonathanagustin/conduit/internal/apollographql/adapters/authorizedprojection"
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
		return graphqlports.Sources{
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
	return apolloProjectionAdapter{ctx: ctx, service: api.application}
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
// The request's context is carried on the struct because the GraphQL projection
// port takes none and the resolver has none to give: the reads it fronts now
// reach the repository, and a query the client abandoned should not outlive it.
// The projection is already bound per request, so this is the context of the
// request being answered and not a longer-lived one.
type apolloProjectionAdapter struct {
	ctx     context.Context
	service *apolloapp.Service
}

func (adapter apolloProjectionAdapter) ListEnvironments() (apollo.ListEnvironmentsResponse, error) {
	return adapter.service.ListEnvironments(), nil
}

func (adapter apolloProjectionAdapter) ListInstallations(rid string) (apollo.ListInstallationsResponse, error) {
	return adapter.service.ListInstallations(rid), nil
}

func (adapter apolloProjectionAdapter) ListProducts() (apollo.ListProductsResponse, error) {
	return adapter.service.ListProducts(), nil
}

func (adapter apolloProjectionAdapter) ListReleases(product string) (apollo.ListReleasesResponse, error) {
	return adapter.service.ListReleases(adapter.ctx, product)
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
	value, err := adapter.service.GetRelease(adapter.ctx, product, version)
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
