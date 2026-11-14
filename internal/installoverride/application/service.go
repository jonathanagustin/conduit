// Package application coordinates bound review through caller-owned authority.
package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/installoverride/domain"
	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
)

type Service struct {
	authority ports.Authority
	now       func() time.Time
}

func New(authority ports.Authority) (*Service, error) {
	if authority == nil {
		return nil, errors.New("installation overrides require caller-bound authority")
	}
	return &Service{authority: authority, now: time.Now}, nil
}

type Intent struct {
	Target         domain.Selector
	Flags          domain.Flags
	Rationale      string
	ReviewLifetime time.Duration
}
type ProposedReview struct {
	Proposal domain.Proposal
	ReviewID string
	Status   string
}

func (s *Service) Propose(ctx context.Context, intent Intent) (ProposedReview, error) {
	if err := ctx.Err(); err != nil {
		return ProposedReview{}, err
	}
	if intent.Target.Validate() != nil || intent.ReviewLifetime <= 0 || intent.ReviewLifetime > domain.MaximumReviewLifetime {
		return ProposedReview{}, domain.ErrInvalid
	}
	snapshot, author, err := s.authority.ResolveProposal(ctx, intent.Target)
	if err != nil {
		return ProposedReview{}, err
	}
	if snapshot.Target.Selector() != intent.Target {
		return ProposedReview{}, domain.ErrCorrupt
	}
	now := s.now().UTC()
	proposal, err := domain.NewProposal(snapshot, intent.Flags, author, intent.Rationale, now, now.Add(intent.ReviewLifetime))
	if err != nil {
		return ProposedReview{}, err
	}
	if err := ctx.Err(); err != nil {
		return ProposedReview{}, err
	}
	ack, err := s.authority.CreateReview(ctx, domain.CopyProposal(proposal))
	if err != nil {
		return ProposedReview{}, err
	}
	if !domain.ValidID(ack.ReviewID) || ack.ProposalID != proposal.ID || ack.Status != "PENDING_APPROVALS" {
		return ProposedReview{}, domain.ErrCorrupt
	}
	return ProposedReview{domain.CopyProposal(proposal), ack.ReviewID, ack.Status}, nil
}

// WithPermit never invokes the effect for a missing, expired, drifted or
// non-independent review. The authority adapter owns the critical section.
func (s *Service) WithPermit(ctx context.Context, reviewID string, target domain.Selector, effect func(domain.Permit) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !domain.ValidID(reviewID) || target.Validate() != nil || effect == nil {
		return domain.ErrInvalid
	}
	var mutex sync.Mutex
	called, closed := false, false
	var callbackErr error
	operationErr := s.authority.WithApproved(ctx, reviewID, target, func(proposal domain.Proposal, approval domain.Approval, current domain.Snapshot) error {
		mutex.Lock()
		defer mutex.Unlock()
		if closed || called {
			callbackErr = domain.ErrCorrupt
			return callbackErr
		}
		called = true
		callbackErr = func() error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if approval.ReviewID != reviewID || current.Target.Selector() != target {
				return domain.ErrCorrupt
			}
			permit, err := domain.Revalidate(proposal, approval, current, s.now().UTC())
			if err != nil {
				return err
			}
			if !permit.ValidAt(s.now().UTC()) {
				return domain.ErrBlocked
			}
			return effect(permit)
		}()
		return callbackErr
	})
	mutex.Lock()
	defer mutex.Unlock()
	closed = true
	if operationErr != nil {
		return operationErr
	}
	if !called {
		return domain.ErrCorrupt
	}
	return callbackErr
}
