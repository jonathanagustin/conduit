// Package ports declares durable reconciliation capabilities without choosing
// a database, planner, transport, or clock implementation.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
)

// ErrProposalSuperseded means the durable desired snapshot changed after an
// outbox entry was created. Retrying that proposal would be stale authority;
// the worker terminally retires it and lets the newer work revision proceed.
var ErrProposalSuperseded = errors.New("reconciliation proposal superseded")

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

// Escalations is told when reconciliation has stopped trying, so that something
// outside this context can tell a person.
//
// It is a port rather than a direct call into notifications because the worker
// must not know what a notification is: what it knows is that a proposal will
// not be retried and nobody has been told. Whether that becomes an inbox entry,
// an email or a metric is the adapter's problem.
//
// Only one caller reaches here, and the reason is the distinction the durable
// state cannot make. A proposal row is marked exhausted for two unrelated
// reasons -- its retry budget ran out, and a newer desired snapshot superseded
// it -- and only the first is an operator's problem. The second is the system
// working: newer work is already queued. The worker knows which it is at the
// moment it decides; the boolean it writes does not carry it, so anything
// reading the table afterwards cannot separate them. That is why this is called
// from the decision and not from a scan of exhausted rows.
type Escalations interface {
	// ProposalExhausted reports a proposal whose retry budget is spent. attempt
	// is the attempt that failed, and reason is the planner's own error text.
	ProposalExhausted(
		ctx context.Context, proposal domain.Proposal, reason string, attempt uint32, at time.Time,
	) error
}

type Clock interface {
	Now() time.Time
}
