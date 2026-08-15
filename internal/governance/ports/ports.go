// Package ports declares what Governance needs from the rest of the system in
// order to decide who may do what, and nothing more.
//
// There is one port, and it is deliberately narrow. Governance does not want a
// repository, a transaction, or a change request — it wants the grants recorded
// against one scope. Naming exactly that keeps the store replaceable, keeps the
// scope expansion (which is a rule, not a query) inside the domain, and means a
// caller cannot reach a second answer to "what may this principal do" by going
// round the side.
package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

// GrantSource answers which grants exist against one authorization scope.
//
// A scope is not a resource identifier: AuthorizationScopes expands the
// resource into every scope a covering grant could have been made against, and
// this port is asked once per scope. An implementation must return only the
// grants recorded against exactly the scope it is given, because widening here
// silently widens every authorization decision above it.
type GrantSource interface {
	ResourceGrantsInScope(ctx context.Context, scope string) ([]domain.ResourceGrant, error)
}
