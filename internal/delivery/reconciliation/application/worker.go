// Package application owns the durable reconciliation worker and outbox
// dispatcher. It depends only on reconciliation domain types and ports.
package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/ports"
)

var ErrRetryExhausted = errors.New("reconciliation retry budget exhausted")

type Config struct {
	LeaseDuration         time.Duration
	MaximumObservationAge time.Duration
	MaximumAttempts       uint32
	BaseRetryDelay        time.Duration
	RepairBatchSize       int
}

type Worker struct {
	store       ports.Store
	planner     ports.Planner
	clock       ports.Clock
	escalations ports.Escalations
	config      Config
}

// NewWorker requires an Escalations, and it is required rather than optional on
// purpose. A worker that can exhaust a proposal and tell nobody is precisely
// the gap recorded as Tier 1 gap 1 in docs/operations/PRODUCTION-READINESS.md;
// making the dependency optional would let a deployment reintroduce it by
// omission, silently, and the symptom would be silence.
func NewWorker(
	store ports.Store, planner ports.Planner, clock ports.Clock,
	escalations ports.Escalations, config Config,
) (*Worker, error) {
	if store == nil || planner == nil || clock == nil || escalations == nil {
		return nil, errors.New("reconciliation Store, Planner, Clock, and Escalations are required")
	}
	if config.LeaseDuration <= 0 || config.MaximumAttempts == 0 || config.BaseRetryDelay <= 0 || config.RepairBatchSize <= 0 {
		return nil, errors.New("reconciliation lease, attempts, retry delay, and repair batch must be positive")
	}
	return &Worker{store: store, planner: planner, clock: clock, escalations: escalations, config: config}, nil
}

func (worker *Worker) ProcessOne(ctx context.Context) (bool, error) {
	leaseAt := worker.clock.Now().UTC()
	lease, found, err := worker.store.LeaseWork(ctx, leaseAt, worker.config.LeaseDuration)
	if err != nil || !found {
		return found, err
	}
	input, err := worker.store.LoadInput(ctx, lease.Key)
	if err != nil {
		return true, worker.failWork(ctx, lease, fmt.Sprintf("load input: %v", err))
	}
	decideAt := worker.clock.Now().UTC()
	decision, proposal, err := domain.Decide(input, lease.Attempt, decideAt, worker.config.MaximumObservationAge)
	if err != nil {
		return true, worker.failWork(ctx, lease, fmt.Sprintf("decide: %v", err))
	}
	if err := worker.store.CommitDecision(ctx, lease, decision, proposal); err != nil {
		return true, err
	}
	return true, nil
}

func (worker *Worker) DispatchOne(ctx context.Context) (bool, error) {
	now := worker.clock.Now().UTC()
	lease, found, err := worker.store.LeaseProposal(ctx, now, worker.config.LeaseDuration)
	if err != nil || !found {
		return found, err
	}
	receipt, err := worker.planner.Propose(ctx, lease.Proposal)
	if err != nil {
		if errors.Is(err, ports.ErrProposalSuperseded) {
			// Retired, not exhausted. The row carries the same boolean either
			// way, but the meanings are opposite: a newer work revision is
			// already queued, so nothing is stuck and nobody needs telling.
			// Escalating here would page an operator for the system working.
			return true, worker.store.FailProposal(ctx, lease, err.Error(), now, true)
		}
		return true, worker.failProposal(ctx, lease, err.Error(), now)
	}
	if receipt.ProposalID != lease.Proposal.ID || receipt.PlanID == "" || receipt.CreatedAt.IsZero() {
		return true, worker.failProposal(ctx, lease, "planner returned an invalid receipt", now)
	}
	if err := worker.store.CompleteProposal(ctx, lease, receipt); err != nil {
		return true, err
	}
	return true, nil
}

func (worker *Worker) Repair(ctx context.Context) (int, error) {
	return worker.store.RepairDroppedWork(ctx, worker.clock.Now().UTC(), worker.config.RepairBatchSize)
}

func (worker *Worker) Drain(ctx context.Context) error {
	for {
		processed, err := worker.ProcessOne(ctx)
		if err != nil {
			return err
		}
		if !processed {
			break
		}
	}
	for {
		processed, err := worker.DispatchOne(ctx)
		if err != nil {
			return err
		}
		if !processed {
			return nil
		}
	}
}

// failProposal records a dispatch failure, and escalates first when it is the
// last one.
//
// Escalation happens before the store write, and the order is the whole point.
// An exhausted proposal is never leased again -- the lease query filters on
// exhausted = false -- so a crash after marking it and before telling anyone
// loses the escalation permanently, with no queue entry left to retry from.
// Escalating first inverts the failure: a crash in the same window costs a
// duplicate notification instead, which the store deduplicates on the event's
// identity. Duplicate over lost is the right trade for the one signal that says
// convergence has stopped.
//
// An escalation that fails is returned rather than swallowed, so the proposal
// keeps its lease, expires, and is dispatched again. That leaves a proposal
// retrying past its budget while the notification store is down, which is a
// visibly stuck queue -- and a stuck queue an operator can see beats a terminal
// failure nobody hears about, which is the state this whole path exists to end.
func (worker *Worker) failProposal(
	ctx context.Context, lease ports.ProposalLease, reason string, now time.Time,
) error {
	exhausted := lease.Attempt >= worker.config.MaximumAttempts
	if exhausted {
		if err := worker.escalations.ProposalExhausted(
			ctx, lease.Proposal, reason, lease.Attempt, now,
		); err != nil {
			return fmt.Errorf("escalate exhausted reconciliation proposal %s: %w", lease.Proposal.ID, err)
		}
	}
	return worker.store.FailProposal(ctx, lease, reason, worker.retryAt(now, lease.Attempt), exhausted)
}

// failWork records a decision failure. It does not escalate yet, and the
// asymmetry with failProposal is scope rather than judgement: exhausted work is
// the second thing an operator needs telling about and it belongs to its own
// change, alongside the events raised outside this context.
func (worker *Worker) failWork(ctx context.Context, lease ports.WorkLease, reason string) error {
	exhausted := lease.Attempt >= worker.config.MaximumAttempts
	if err := worker.store.FailWork(ctx, lease, reason, worker.retryAt(worker.clock.Now().UTC(), lease.Attempt), exhausted); err != nil {
		return err
	}
	if exhausted {
		return fmt.Errorf("%w: %s", ErrRetryExhausted, reason)
	}
	return nil
}

func (worker *Worker) retryAt(now time.Time, attempt uint32) time.Time {
	delay := worker.config.BaseRetryDelay
	for count := uint32(1); count < attempt && delay < time.Hour; count++ {
		delay *= 2
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return now.Add(delay)
}
