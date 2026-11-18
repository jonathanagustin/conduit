// Package nativegrouping binds native grouping reads to a caller's authority.
package nativegrouping

import (
	"context"
	"errors"

	"github.com/jonathanagustin/conduit/internal/apollographql/ports"
)

// Source adapts caller-bound native lookups without retaining a principal or
// legacy repository. CanReadRoot must authorize the same native root read used
// by the REST Bundle listing, before ReadBundleIDs is invoked.
type Source struct {
	LookupSpaceID func(context.Context, string, string) (string, error)
	CanReadRoot   func() bool
	ReadBundleIDs func(context.Context) ([]string, error)
}

func (source Source) SpaceID(ctx context.Context, kind, id string) (string, error) {
	if source.LookupSpaceID == nil {
		return "", errors.New("native Space data is not configured")
	}
	return source.LookupSpaceID(ctx, kind, id)
}

func (source Source) BundleIDs(ctx context.Context) ([]string, error) {
	if source.CanReadRoot == nil || !source.CanReadRoot() {
		return nil, ports.ErrNotAuthorized
	}
	if source.ReadBundleIDs == nil {
		return nil, errors.New("native Bundle data could not be read")
	}
	ids, err := source.ReadBundleIDs(ctx)
	if err != nil {
		return nil, errors.New("native Bundle data could not be read")
	}
	return ids, nil
}

var _ ports.NativeGrouping = Source{}
