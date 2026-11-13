package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

// CONDUIT-INVENTED: spaces.GraphQLGrouping
// Displaced-by: a published or sanitized Apollo record establishing Space identity per Environment, Entity, Product, Release and Bundle, plus Bundle lookup semantics.
// Space IDs come from persisted native assignments, never from a default
// constant, an Apollo RID, or an inferred Foundry namespace.
func (resolver *bound) spaceID(ctx context.Context, kind, id string) (any, error) {
	if resolver.sources.Grouping == nil {
		return nil, errNoSource
	}
	value, err := resolver.sources.Grouping.SpaceID(ctx, kind, id)
	if err != nil {
		return nil, err
	}
	if value == "" {
		return nil, nil
	} // The configured store observed no assignment.
	return value, nil
}

type bundleSource struct{ id string }

func (resolver *bound) bundleByRID(ctx context.Context, rid string) (any, error) {
	if resolver.sources.Grouping == nil {
		return nil, errNoSource
	}
	// Ask the authorized source even for a foreign RID, so a denied catalogue
	// cannot turn into a seemingly successful missing-record answer.
	ids, err := resolver.sources.Grouping.BundleIDs(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if bundleRID(id) == rid {
			return child{typeName: "ApolloBundle", source: bundleSource{id: id}}, nil
		}
	}
	return nil, ports.ErrRecordNotFound
}
func (resolver *bound) resolveBundle(ctx context.Context, field string, source any) (any, error) {
	bundle, ok := source.(bundleSource)
	if !ok {
		return nil, fmt.Errorf("ApolloBundle.%s resolved against %T", field, source)
	}
	switch field {
	case "rid":
		return bundleRID(bundle.id), nil
	case "spaceId":
		return resolver.spaceID(ctx, "BUNDLE", bundle.id)
	}
	return nil, fmt.Errorf("ApolloBundle.%s is covered but this resolver has no case for it", field)
}
func (resolver *bound) queryBundle(ctx context.Context, field string, arguments map[string]any) (any, error) {
	argument := "rid"
	if field == "bundles" {
		argument = "rids"
	}
	rid, err := stringArgument(arguments, argument)
	if err != nil {
		return nil, err
	}
	value, err := resolver.bundleByRID(ctx, rid)
	if errors.Is(err, ports.ErrRecordNotFound) {
		return nil, nil
	}
	return value, err
}
