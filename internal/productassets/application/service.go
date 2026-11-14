// Package application publishes immutable download metadata through a separate
// store and a catalogue receipt, without adding any artifact execution authority.
package application

import (
	"context"
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/productassets/domain"
	"github.com/jonathanagustin/conduit/internal/productassets/ports"
)

type Service struct {
	store     ports.Store
	catalogue ports.Catalogue
	authority ports.Authority
	grouping  ports.Grouping
	now       func() time.Time
}

func New(store ports.Store, catalogue ports.Catalogue, authority ports.Authority, grouping ports.Grouping) (*Service, error) {
	if store == nil || catalogue == nil || authority == nil {
		return nil, errors.New("download assets require storage, catalogue and caller authority")
	}
	return &Service{store: store, catalogue: catalogue, authority: authority, grouping: grouping, now: time.Now}, nil
}

type PublicationResult struct {
	MavenCoordinate string `json:"mavenCoordinate"`
	Digest          string `json:"digest"`
	Status          string `json:"status"`
	ValueTier       string `json:"valueTier"`
}
type Published struct {
	Metadata    domain.Prepared `json:"metadata"`
	PublishedAt time.Time       `json:"publishedAt"`
	Channels    []string        `json:"releaseChannels"`
	Executable  bool            `json:"executable"`
	Status      string          `json:"status"`
}

func (service *Service) Publish(ctx context.Context, manifest domain.Manifest) (PublicationResult, error) {
	if err := ctx.Err(); err != nil {
		return PublicationResult{}, err
	}
	canonical, err := manifest.Canonical()
	if err != nil {
		return PublicationResult{}, err
	}
	subject, err := service.authority.RequirePublication(ctx, canonical.Product())
	if err != nil {
		return PublicationResult{}, err
	}
	record, err := domain.NewPrepared(canonical, subject, service.now())
	if err != nil {
		return PublicationResult{}, err
	}
	saved, err := service.store.Prepare(ctx, record)
	if err != nil {
		return PublicationResult{}, err
	}
	if err := saved.Validate(); err != nil {
		return PublicationResult{}, domain.ErrCorrupt
	}
	if saved.Manifest.Coordinate() != record.Manifest.Coordinate() || saved.Digest != record.Digest {
		return PublicationResult{}, domain.ErrCorrupt
	}
	if service.grouping != nil {
		if err := service.grouping.Reserve(ctx, canonical.Product()); err != nil {
			return PublicationResult{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return PublicationResult{}, err
	}
	receipt, err := service.catalogue.Publish(ctx, canonical.Product(), canonical.Version, saved.Digest)
	if err != nil {
		return PublicationResult{}, err
	}
	if !matches(saved, receipt) {
		return PublicationResult{}, domain.ErrCorrupt
	}
	// Creation provenance belongs to an independently authorized read. A writer
	// retry must not disclose the original preparer's identity without read access.
	return PublicationResult{MavenCoordinate: canonical.Coordinate(), Digest: saved.Digest, Status: "CATALOGUED_NOT_EXECUTABLE", ValueTier: domain.ValueTier}, nil
}
func matches(record domain.Prepared, receipt ports.Receipt) bool {
	return receipt.Product == record.Manifest.Product() && receipt.Version == record.Manifest.Version && receipt.Kind == domain.Kind && receipt.Digest == record.Digest && !receipt.PublishedAt.IsZero()
}
func (service *Service) Get(ctx context.Context, product, version string) (Published, error) {
	if !domain.ValidProduct(product) || !domain.ValidVersion(version) {
		return Published{}, domain.ErrInvalid
	}
	if err := service.authority.RequireRead(ctx, product); err != nil {
		return Published{}, err
	}
	receipt, err := service.catalogue.Get(ctx, product, version)
	if err != nil {
		return Published{}, err
	}
	return service.readPublished(ctx, product, version, receipt)
}
func (service *Service) readPublished(ctx context.Context, product, version string, receipt ports.Receipt) (Published, error) {
	if receipt.Product != product || receipt.Version != version || receipt.Kind == "" {
		return Published{}, domain.ErrCorrupt
	}
	if receipt.Kind != domain.Kind {
		return Published{}, domain.ErrNotFound
	}
	record, err := service.store.Get(ctx, product+":"+version)
	// A committed receipt with missing metadata is a broken publication, not an
	// observed absence that could be reported as an empty catalogue.
	if errors.Is(err, domain.ErrNotFound) {
		return Published{}, domain.ErrCorrupt
	}
	if err != nil {
		return Published{}, err
	}
	if err := record.Validate(); err != nil {
		return Published{}, domain.ErrCorrupt
	}
	if !matches(record, receipt) {
		return Published{}, domain.ErrCorrupt
	}
	return Published{Metadata: domain.Clone(record), PublishedAt: receipt.PublishedAt, Channels: append([]string(nil), receipt.Channels...), Executable: false, Status: "CATALOGUED_NOT_EXECUTABLE"}, nil
}
func (service *Service) List(ctx context.Context, product string) ([]Published, error) {
	if !domain.ValidProduct(product) {
		return nil, domain.ErrInvalid
	}
	if err := service.authority.RequireRead(ctx, product); err != nil {
		return nil, err
	}
	receipts, err := service.catalogue.List(ctx, product)
	if err != nil {
		return nil, err
	}
	result := make([]Published, 0, len(receipts))
	seen := map[string]bool{}
	for _, receipt := range receipts {
		if receipt.Product != product || receipt.Kind == "" {
			return nil, domain.ErrCorrupt
		}
		if receipt.Kind != domain.Kind {
			continue
		}
		if seen[receipt.Version] {
			return nil, domain.ErrCorrupt
		}
		seen[receipt.Version] = true
		value, err := service.readPublished(ctx, product, receipt.Version, receipt)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}
