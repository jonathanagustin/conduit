// Package ports describes immutable asset storage, catalogue receipts and
// caller-bound authority without importing transport or legacy implementations.
package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/productassets/domain"
	"time"
)

type Store interface {
	Prepare(context.Context, domain.Prepared) (domain.Prepared, error)
	Get(context.Context, string) (domain.Prepared, error)
}

// Receipt is the catalogue's immutable publication acknowledgement. Asset
// metadata alone is only prepared intent and must never satisfy a published read.
type Receipt struct {
	Product, Version, Kind, Digest string
	PublishedAt                    time.Time
	Channels                       []string
}
type Catalogue interface {
	Publish(context.Context, string, string, string) (Receipt, error)
	Get(context.Context, string, string) (Receipt, error)
	List(context.Context, string) ([]Receipt, error)
}
type Authority interface {
	RequirePublication(context.Context, string) (string, error)
	RequireRead(context.Context, string) error
}

// Grouping is optional and bound to a validated request selection. It grants no
// resource authority and reserves grouping only after publication is authorized.
type Grouping interface {
	Reserve(context.Context, string) error
}
