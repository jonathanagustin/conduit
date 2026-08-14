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
	store   ports.Store
	planner ports.Planner
	clock   ports.Clock
	config  Config
}

func NewWorker(store ports.Store, planner ports.Planner, clock ports.Clock, config Config) (*Worker, error) {
	if store == nil || planner == nil || clock == nil {
		return nil, errors.New("reconciliation Store, Planner, and Clock are required")
	}
	if config.LeaseDuration <= 0 || config.MaximumAttempts == 0 || config.BaseRetryDelay <= 0 || config.RepairBatchSize <= 0 {
		return nil, errors.New("reconciliation lease, attempts, retry delay, and repair batch must be positive")
	}
	return &Worker{store: store, planner: planner, clock: clock, config: config}, nil
}

func (worker *Worker) ProcessOne(ctx context.Context) (bool, error) {
	now := worker.clock.Now().UTC()
	lease, found, err := worker.store.LeaseWork(ctx, now, worker.config.LeaseDuration)
	if err != nil || !found {
		return found, err
	}
	input, err := worker.store.LoadInput(ctx, lease.Key)
	if err != nil {
		return true, worker.failWork(ctx, lease, fmt.Sprintf("load input: %v", err))
	}
	decision, proposal, err := domain.Decide(input, lease.Attempt, now, worker.config.MaximumObservationAge)
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
		exhausted := lease.Attempt >= worker.config.MaximumAttempts
		return true, worker.store.FailProposal(ctx, lease, err.Error(), worker.retryAt(now, lease.Attempt), exhausted)
	}
	if receipt.ProposalID != lease.Proposal.ID || receipt.PlanID == "" || receipt.CreatedAt.IsZero() {
		return true, worker.store.FailProposal(ctx, lease, "planner returned an invalid receipt", worker.retryAt(now, lease.Attempt), lease.Attempt >= worker.config.MaximumAttempts)
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
