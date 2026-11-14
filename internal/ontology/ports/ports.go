// Package ports is the ontology catalog's inbound port.
package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/ontology/domain"
)

// Caller is the operator the HTTP adapter already authenticated.
type Caller struct {
	Username string
	Teams    []string
	Groups   []string
}

// Source reads the catalog the caller may see and proposes a change request
// through governance. Apply must not store anything when the mode is
// VALIDATE_ONLY.
type Source interface {
	Catalog(ctx context.Context, allow func(environmentID string) bool) (domain.Catalog, error)
	Apply(ctx context.Context, caller Caller, request domain.ApplyRequest) (domain.ApplyResult, error)
}
