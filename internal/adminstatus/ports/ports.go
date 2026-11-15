package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/adminstatus/domain"
)

type Source interface {
	Read(context.Context) (domain.Summary, error)
}

// CapacityReader reads a configured backing filesystem without exposing paths.
type CapacityReader interface {
	Capacity(context.Context) (used, total int64, err error)
}
