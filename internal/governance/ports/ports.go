// Package ports declares what Governance needs from the rest of the system in
// order to decide who may do what, and nothing more.
//
// There are two, and both are deliberately narrow. Governance does not want a
// repository, a transaction, or a change request — it wants the grants recorded
// against one scope, and somewhere to file what it decided. Naming exactly that
// keeps the store replaceable, keeps the scope expansion (which is a rule, not
// a query) inside the domain, and means a caller cannot reach a second answer
// to "what may this principal do" by going round the side.
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

// AuditSink files an authorization decision where it can be read back.
//
// It is a port for the same reason GrantSource is, and it is required rather
// than optional on purpose: the decision and its record are taken together, so
// a surface cannot acquire the first without the second. #241 was what the
// alternative looked like — a second surface that reached the decision, reached
// a wider answer than the REST routes, and filed nothing, leaving the widening
// with no symptom anybody could have queried for.
type AuditSink interface {
	AppendAuthorizationAudit(
		ctx context.Context, event domain.AuthorizationAuditEvent,
	) (domain.AuthorizationAuditEvent, error)
}
