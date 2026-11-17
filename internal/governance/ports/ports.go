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
	"time"

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
// than optional on purpose: a *resource* decision and its record are taken
// together, so a surface cannot reach the first without supplying the second.
// #241 was what the alternative looked like — a second surface that reached the
// decision, reached a wider answer than the REST routes, and filed nothing,
// leaving the widening with no symptom anybody could have queried for.
//
// Mutating ResourceDecision.Allow calls require successful persistence; its
// two explicit read operations retain best-effort audit behavior.
// The pairing binds ResourceDecision.Allow and nothing else. The administrator
// predicate beside it is shared between both surfaces but writes no record, so
// a route gated on administrator status alone still leaves none — read that
// function's comment before assuming otherwise.
type AuditSink interface {
	AppendAuthorizationAudit(
		ctx context.Context, event domain.AuthorizationAuditEvent,
	) (domain.AuthorizationAuditEvent, error)
}

// ChangeRequestOpening is what Governance knows at the moment a change request
// starts waiting for a person: which request, what it targets, and who wrote
// it. It carries identifiers rather than the record, because the record is the
// legacy control plane's and a port that named it would import it.
type ChangeRequestOpening struct {
	ID          string
	Environment string
	TargetKind  string
	TargetID    string
	Title       string
	Author      string
	// Revision separates one round of review from the next. A request that is
	// rejected and reopened waits for approval a second time, and that second
	// wait is a new fact; the same round restated is not.
	Revision uint64
	At       time.Time
}

// ChangeRequestOpenings is told when a change request has been committed in a
// state that waits for an approver, so that something outside this context can
// tell one.
//
// A port rather than a call into notifications, for the reason catalog's
// RecallOpenings gives: the caller must not learn what a notification is. It is
// called after the write it describes has been committed and never inside its
// transaction, so a rolled-back proposal announces nothing; and the caller
// drops the error, because the request is stored by then and reporting a
// refusal would misdescribe work that happened.
type ChangeRequestOpenings interface {
	AwaitingApproval(ctx context.Context, opening ChangeRequestOpening) error
}

// GrantChange is one committed write of a resource grant. Before is nil for a
// grant that did not exist.
type GrantChange struct {
	Before *domain.ResourceGrant
	After  domain.ResourceGrant
	// Actor is the principal who made the change, not the team that gained or
	// lost authority; the team is on the grant.
	Actor string
}

// GrantChanges is told when a resource grant was written, after the write and
// its audit record both landed. The same after-commit, dropped-error contract
// as ChangeRequestOpenings.
type GrantChanges interface {
	GrantChanged(ctx context.Context, change GrantChange) error
}
