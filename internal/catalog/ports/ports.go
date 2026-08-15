// Package ports declares what Catalog needs from the rest of the system, and
// nothing more.
//
// There is one, and it is an outbound persistence port rather than a query the
// domain makes: a Publication is a coordinate the catalogue mints and must
// never mint twice, so the store is the thing that has to enforce immutability
// and the domain cannot.
package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/catalog/domain"
)

// PublicationRepository records where a Release was published.
//
// Implementations enforce coordinate immutability: a Publication for a
// product/version/registry triple that already exists is not overwritten. That
// is a store-level guarantee because it is a uniqueness guarantee, and no
// amount of checking before the write can make one out of two operations.
type PublicationRepository interface {
	CreateProductPublication(ctx context.Context, publication domain.Publication) (domain.Publication, error)
	ListProductPublications(ctx context.Context, product string) ([]domain.Publication, error)
}
