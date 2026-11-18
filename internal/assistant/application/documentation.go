package application

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/assistant/domain"
)

// Documentation is the selected native product-reference corpus. Queries do
// not name filesystem paths or remote URLs and cannot read arbitrary files.
type Documentation interface {
	Search(context.Context, string) (DocumentationPage, error)
}

// DocumentationPage is a bounded selection of exact source excerpts.
type DocumentationPage struct {
	Sources      []domain.Citation
	MatchedCount int
	Limited      bool
}
