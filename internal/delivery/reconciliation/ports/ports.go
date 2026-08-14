// Package ports declares durable reconciliation capabilities without choosing
// a database, planner, transport, or clock implementation.
package ports

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
)

type WorkLease struct {
	Key   domain.Key
	Token string
	// Revision fences a decision against input events that arrive while the
	// worker holds the lease. A changed revision keeps the key queued after the
	// old decision commits, so a concurrent desired/report update cannot be
	// acknowledged accidentally.
	Revision  uint64
	Attempt   uint32
	ExpiresAt time.Time
}

type ProposalLease struct {
	Proposal  domain.Proposal
	Token     string
	Attempt   uint32
	ExpiresAt time.Time
}

type Store interface {
	LeaseWork(context.Context, time.Time, time.Duration) (WorkLease, bool, error)
	LoadInput(context.Context, domain.Key) (domain.Input, error)
	CommitDecision(context.Context, WorkLease, domain.Decision, *domain.Proposal) error
	FailWork(context.Context, WorkLease, string, time.Time, bool) error
	RepairDroppedWork(context.Context, time.Time, int) (int, error)
	LeaseProposal(context.Context, time.Time, time.Duration) (ProposalLease, bool, error)
	CompleteProposal(context.Context, ProposalLease, domain.PlanReceipt) error
	FailProposal(context.Context, ProposalLease, string, time.Time, bool) error
}

type Planner interface {
	Propose(context.Context, domain.Proposal) (domain.PlanReceipt, error)
}

type Clock interface {
	Now() time.Time
}
