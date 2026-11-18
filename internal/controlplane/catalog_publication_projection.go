package controlplane

import (
	"context"
	"errors"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
)

// publicationCatalogRepository is deliberately not the aggregate Repository.
// The bridge cannot read Plans, write approvals, or execute delivery operations.
type publicationCatalogRepository interface {
	ListProductPublications(context.Context, string) ([]catalog.Publication, error)
	CreateProductPublication(context.Context, catalog.Publication) (catalog.Publication, error)
	GetCatalogRetirement(context.Context, string, string) (CatalogRetirement, error)
}

// publicationCatalogPort maps native publication and retirement results to the
// narrow catalog operations consumed at the compatibility application boundary.
type publicationCatalogPort struct{ repository publicationCatalogRepository }

func (port publicationCatalogPort) ListPublications(ctx context.Context, product string) ([]catalog.Publication, error) {
	return port.repository.ListProductPublications(ctx, product)
}

func (port publicationCatalogPort) CreatePublication(ctx context.Context, publication catalog.Publication) (catalog.Publication, bool, error) {
	stored, err := port.repository.CreateProductPublication(ctx, publication)
	if errors.Is(err, ErrConflict) {
		return catalog.Publication{}, true, nil
	}
	return stored, false, err
}

func (port publicationCatalogPort) CoordinateRetired(ctx context.Context, product, version string) (bool, error) {
	_, err := port.repository.GetCatalogRetirement(ctx, product, version)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
