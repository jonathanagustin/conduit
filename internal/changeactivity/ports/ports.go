// Package ports isolates discussion persistence and existing request authority.
package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/changeactivity/domain"
)

type Store interface {
	Append(context.Context, domain.Comment) error
	List(context.Context, string) ([]domain.Comment, error)
}
type Writer struct{ Author, Actor string }

type Authority interface {
	RequireAnnotation(context.Context, string) (Writer, error)
	RequireRead(context.Context, string) error
}
