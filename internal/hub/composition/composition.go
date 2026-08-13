// Package composition is the Hub composition root.
//
// It is deliberately thin: concrete adapters are assembled here, while domain
// decisions remain in their owning bounded contexts. During the incremental
// migration it delegates to the legacy controlplane facade so routes and
// persistence remain byte-compatible.
package composition

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// NewHubHandler assembles the Hub HTTP surface from explicitly supplied ports.
// The controlplane dependency is a temporary compatibility boundary, not a
// license for new domain behavior to enter the legacy package.
func NewHubHandler(repository controlplane.Repository, options controlplane.HandlerOptions) http.Handler {
	return controlplane.NewHandler(repository, options)
}
