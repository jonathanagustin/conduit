package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"strings"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	catalogports "github.com/jonathanagustin/conduit/internal/catalog/ports"
)

// Compatibility aliases keep the persistence implementations stable while the
// owning definitions live in Catalog. New code imports the context directly.
//
// ProductPublicationRepository is Catalog's port, aliased so the repository
// implementations below satisfy it without knowing it exists.
type (
	ProductPublication           = catalog.Publication
	ProductPublicationAsset      = catalog.PublicationAsset
	ProductPublicationRepository = catalogports.PublicationRepository
)

func (repository *MemoryRepository) CreateProductPublication(_ context.Context, publication ProductPublication) (ProductPublication, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if existing, exists := repository.productPublications[publication.MavenCoordinate]; exists {
		if existing.PublicationDigest != publication.PublicationDigest {
			return ProductPublication{}, ErrConflict
		}
		return cloneJSON(existing), nil
	}
	repository.productPublications[publication.MavenCoordinate] = cloneJSON(publication)
	return cloneJSON(publication), nil
}

func (repository *MemoryRepository) ListProductPublications(_ context.Context, product string) ([]ProductPublication, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	result := make([]ProductPublication, 0, len(repository.productPublications))
	for _, publication := range repository.productPublications {
		if product == "" || publication.Product == product {
			result = append(result, cloneJSON(publication))
		}
	}
	sortProductPublications(result)
	return result, nil
}

func (repository *FileRepository) CreateProductPublication(_ context.Context, publication ProductPublication) (ProductPublication, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	path := repository.productPublicationPath(publication.MavenCoordinate)
	var existing ProductPublication
	if err := readJSON(path, &existing); err == nil {
		if existing.PublicationDigest != publication.PublicationDigest {
			return ProductPublication{}, ErrConflict
		}
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return ProductPublication{}, err
	}
	if err := atomicWriteJSON(path, publication); err != nil {
		return ProductPublication{}, err
	}
	return publication, nil
}

func (repository *FileRepository) ListProductPublications(_ context.Context, product string) ([]ProductPublication, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	return listJSON[ProductPublication](repository.productPublicationDirectory(), func(value ProductPublication) bool {
		return product == "" || value.Product == product
	}, func(left, right ProductPublication) bool {
		if left.Product != right.Product {
			return left.Product < right.Product
		}
		return compareVersions(left.Version, right.Version) < 0
	})
}

func sortProductPublications(values []ProductPublication) {
	sort.Slice(values, func(left, right int) bool {
		if values[left].Product != values[right].Product {
			return values[left].Product < values[right].Product
		}
		return compareVersions(values[left].Version, values[right].Version) < 0
	})
}

func (repository *FileRepository) productPublicationDirectory() string {
	return filepath.Join(repository.root, "product-publications")
}

func (repository *FileRepository) productPublicationPath(coordinate string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(coordinate)))
	return filepath.Join(repository.productPublicationDirectory(), hex.EncodeToString(digest[:])+".json")
}
