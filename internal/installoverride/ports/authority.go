// Package ports isolates override review from repositories and execution.
package ports

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/installoverride/domain"
)

type ReviewAck struct{ ReviewID, ProposalID, Status string }

// Authority supplies authenticated identities, sanitized current facts and
// durable reviews. CreateReview must impose independent review even when
// ordinary policy would auto-approve. WithApproved must authorize the caller,
// read current review/target/grant facts and hold their serialization boundary
// through the callback. Audit and guarded effects must persist together before
// acknowledgment; a callback error must not acknowledge an applied effect.
type Authority interface {
	ResolveProposal(context.Context, domain.Selector) (domain.Snapshot, domain.Identity, error)
	CreateReview(context.Context, domain.Proposal) (ReviewAck, error)
	WithApproved(context.Context, string, domain.Selector, func(domain.Proposal, domain.Approval, domain.Snapshot) error) error
}
