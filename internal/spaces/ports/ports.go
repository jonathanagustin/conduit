package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/spaces/domain"
)

// Store persists immutable Space identities and grouping anchors. A binding
// conveys no resource grant and cannot be moved to another Space implicitly.
type Store interface {
	CreateSpace(context.Context, domain.Space) (domain.Space, error)
	GetSpace(context.Context, string) (domain.Space, error)
	ListSpaces(context.Context) ([]domain.Space, error)
	BindResource(context.Context, domain.Binding) (domain.Binding, error)
	GetBinding(context.Context, domain.Resource) (domain.Binding, error)
	ListBindings(context.Context, string) ([]domain.Binding, error)
}

// Authority is bound to the actual caller, never to fields in a request body.
// Denials use domain.ErrForbidden; infrastructure failures remain failures.
type Authority interface {
	RequireCreation(context.Context) (string, error)
	RequireRead(context.Context, string) error
	RequireBinding(context.Context, string, domain.Resource) (string, error)
	RequireResourceRead(context.Context, domain.Resource) error
}
