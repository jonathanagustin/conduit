// Package compatibilityprojection binds compatibility application reads to
// GraphQL's read port without importing the native control-plane implementation.
package compatibilityprojection

import (
	"context"
	"fmt"
	apollo "github.com/jonathanagustin/conduit/internal/apollo"
	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
	graphqlports "github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

// Reader grants only the seven projection reads, never compatibility commands.
type Reader interface {
	ListEnvironments(context.Context) (apollo.ListEnvironmentsResponse, error)
	ListInstallations(context.Context, string) (apollo.ListInstallationsResponse, error)
	ListProducts() apollo.ListProductsResponse
	ListReleases(context.Context, string) (apollo.ListReleasesResponse, error)
	GetEnvironment(context.Context, string) (apollo.Environment, error)
	GetProduct(string) (apollo.Product, error)
	GetRelease(context.Context, string, string) (apollo.Release, error)
}

var _ Reader = (*apolloapp.Service)(nil)
var _ graphqlports.ApolloProjection = projection{}

// New binds a projection to the authenticated request's context. An absent
// reader leaves this projection unavailable rather than manufacturing empty data.
func New(ctx context.Context, reader Reader) graphqlports.ApolloProjection {
	if reader == nil {
		return nil
	}
	return projection{ctx: ctx, reader: reader}
}

// projection translates the Apollo application's error kinds into
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
type projection struct {
	ctx    context.Context
	reader Reader
}

func (adapter projection) ListEnvironments() (apollo.ListEnvironmentsResponse, error) {
	return adapter.reader.ListEnvironments(adapter.ctx)
}

func (adapter projection) ListInstallations(rid string) (apollo.ListInstallationsResponse, error) {
	return adapter.reader.ListInstallations(adapter.ctx, rid)
}

func (adapter projection) ListProducts() (apollo.ListProductsResponse, error) {
	return adapter.reader.ListProducts(), nil
}

func (adapter projection) ListReleases(product string) (apollo.ListReleasesResponse, error) {
	return adapter.reader.ListReleases(adapter.ctx, product)
}

func (adapter projection) GetEnvironment(rid string) (apollo.Environment, error) {
	value, err := adapter.reader.GetEnvironment(adapter.ctx, rid)
	return value, projectionError(err)
}

func (adapter projection) GetProduct(product string) (apollo.Product, error) {
	value, err := adapter.reader.GetProduct(product)
	return value, projectionError(err)
}

func (adapter projection) GetRelease(product, version string) (apollo.Release, error) {
	value, err := adapter.reader.GetRelease(adapter.ctx, product, version)
	return value, projectionError(err)
}

func projectionError(err error) error {
	if err == nil {
		return nil
	}
	if apolloapp.IsKind(err, apolloapp.NotFound) {
		return fmt.Errorf("%w: %s", graphqlports.ErrRecordNotFound, err.Error())
	}
	return err
}
