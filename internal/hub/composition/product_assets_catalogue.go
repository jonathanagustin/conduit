package composition

import (
	"context"
	"errors"
	"time"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	assets "github.com/jonathanagustin/conduit/internal/productassets/domain"
	assetports "github.com/jonathanagustin/conduit/internal/productassets/ports"
)

// assetCatalogue exchanges immutable receipts with Catalog, never executable
// Release definitions. Download declarations cannot become Agent workloads.
type assetCatalogue struct{ repository controlplane.Repository }

func (c assetCatalogue) retired(ctx context.Context, product, version string) (bool, error) {
	for _, v := range []string{"", version} {
		if _, err := c.repository.GetCatalogRetirement(ctx, product, v); err == nil {
			return true, nil
		} else if !errors.Is(err, controlplane.ErrNotFound) {
			return false, err
		}
	}
	return false, nil
}
func assetReceipt(p controlplane.ProductPublication) assetports.Receipt {
	return assetports.Receipt{Product: p.Product, Version: p.Version, Kind: p.Kind, Digest: p.PublicationDigest, PublishedAt: p.CreatedAt, Channels: append([]string(nil), p.ReleaseChannels...)}
}
func (c assetCatalogue) Publish(ctx context.Context, product, version, digest string) (assetports.Receipt, error) {
	if retired, err := c.retired(ctx, product, version); err != nil {
		return assetports.Receipt{}, err
	} else if retired {
		return assetports.Receipt{}, assets.ErrConflict
	}
	publication := controlplane.ProductPublication{MavenCoordinate: product + ":" + version, Product: product, Version: version, Kind: assets.Kind, PublicationDigest: digest, ReleaseChannels: catalog.DefaultChannelsForVersion(version), CreatedAt: time.Now().UTC()}
	saved, err := c.repository.CreateProductPublication(ctx, publication)
	if errors.Is(err, controlplane.ErrConflict) {
		return assetports.Receipt{}, assets.ErrConflict
	}
	if err != nil {
		return assetports.Receipt{}, err
	}
	// The legacy repository's idempotency key is its digest. The receipt still
	// has to describe this non-executable kind, even if a foreign writer reused it.
	if saved.Kind != assets.Kind || saved.Release != nil {
		return assetports.Receipt{}, assets.ErrCorrupt
	}
	return assetReceipt(saved), nil
}
func (c assetCatalogue) Get(ctx context.Context, product, version string) (assetports.Receipt, error) {
	if retired, err := c.retired(ctx, product, version); err != nil {
		return assetports.Receipt{}, err
	} else if retired {
		return assetports.Receipt{}, assets.ErrNotFound
	}
	publications, err := c.repository.ListProductPublications(ctx, product)
	if err != nil {
		return assetports.Receipt{}, err
	}
	for _, p := range publications {
		if p.Product != product {
			return assetports.Receipt{}, assets.ErrCorrupt
		}
		if p.Version == version {
			if p.Kind == assets.Kind && p.Release != nil {
				return assetports.Receipt{}, assets.ErrCorrupt
			}
			return assetReceipt(p), nil
		}
	}
	return assetports.Receipt{}, assets.ErrNotFound
}
func (c assetCatalogue) List(ctx context.Context, product string) ([]assetports.Receipt, error) {
	publications, err := c.repository.ListProductPublications(ctx, product)
	if err != nil {
		return nil, err
	}
	result := make([]assetports.Receipt, 0, len(publications))
	for _, p := range publications {
		if p.Product != product {
			return nil, assets.ErrCorrupt
		}
		if retired, err := c.retired(ctx, product, p.Version); err != nil {
			return nil, err
		} else if retired {
			continue
		}
		if p.Kind == assets.Kind && p.Release != nil {
			return nil, assets.ErrCorrupt
		}
		result = append(result, assetReceipt(p))
	}
	return result, nil
}
