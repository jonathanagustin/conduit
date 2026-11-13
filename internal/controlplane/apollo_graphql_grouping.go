package controlplane

import (
	"context"
	"errors"

	graphqlports "github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

// The binder owns the authenticated carrier and reuses the existing REST
// Bundle read authority. Only stable IDs cross into the GraphQL context.
type nativeGraphQLGrouping struct {
	principal     Principal
	configuration HandlerOptions
	repository    Repository
	reads         func(string) bool
}

func (source nativeGraphQLGrouping) SpaceID(ctx context.Context, kind, id string) (string, error) {
	if source.configuration.SpaceIDLookup == nil {
		return "", errors.New("native Space data is not configured")
	}
	return source.configuration.SpaceIDLookup(ctx, source.principal, kind, id)
}
func (source nativeGraphQLGrouping) BundleIDs(ctx context.Context) ([]string, error) {
	if !source.reads("platform/root") {
		return nil, graphqlports.ErrNotAuthorized
	}
	bundles, err := source.repository.ListBundles(ctx, "")
	if err != nil {
		return nil, errors.New("native Bundle data could not be read")
	}
	ids := make([]string, 0, len(bundles))
	for _, bundle := range bundles {
		ids = append(ids, bundle.Manifest.ID)
	}
	return ids, nil
}
