package composition

import (
	"context"
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	planningapp "github.com/jonathanagustin/conduit/internal/planning/application"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
)

const computeLease = 2 * time.Minute

// computeExecutionStore is the compare-and-set reservation shared by the
// memory, file, and SQL repositories.
type computeExecutionStore interface {
	ReserveComputeExecution(ctx context.Context, sessionID string, now time.Time, lease time.Duration) (controlplane.ComputeReservation, error)
	RenewComputeExecution(ctx context.Context, sessionID, token string, now time.Time, lease time.Duration) error
	CompleteComputeExecution(ctx context.Context, sessionID, token string, receipt controlplane.ComputeReceipt, now time.Time) (controlplane.ComputeReservation, error)
	ReleaseComputeExecution(ctx context.Context, sessionID, token string) error
}

type computeGuard struct {
	store computeExecutionStore
	inner planningapp.AppgenComputer
	lease time.Duration
	now   func() time.Time
}

func guardCompute(store computeExecutionStore, inner planningapp.AppgenComputer) planningapp.AppgenComputer {
	guard := &computeGuard{store: store, inner: inner, lease: computeLease, now: time.Now}
	return guard.run
}

func (guard *computeGuard) run(ctx context.Context, session *planningdomain.Session) error {
	if session == nil || session.Binding == nil {
		return planningdomain.ErrComputeBeforeAccept
	}
	now := guard.now().UTC()
	reservation, err := guard.store.ReserveComputeExecution(ctx, session.ID, now, guard.lease)
	if errors.Is(err, controlplane.ErrComputeBusy) {
		return planningdomain.ErrComputeInProgress
	}
	if err != nil {
		return err
	}
	if reservation.Status == controlplane.ComputeStatusCompleted && reservation.Receipt != nil {
		session.ComputeReceipt = planningReceipt(*reservation.Receipt)
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := make(chan struct{})
	defer close(stop)
	go guard.heartbeat(session.ID, reservation.Token, stop, cancel)
	if err := guard.inner(runCtx, session); err != nil {
		_ = guard.store.ReleaseComputeExecution(context.WithoutCancel(ctx), session.ID, reservation.Token)
		return err
	}
	receipt := controlplane.ComputeReceipt{
		SessionID:      session.ID,
		PlanRevisionID: session.ID,
		PlanDigest:     session.Binding.Plan.SHA256,
		Outcome:        "completed",
		CompletedAt:    guard.now().UTC(),
	}
	completed, err := guard.store.CompleteComputeExecution(ctx, session.ID, reservation.Token, receipt, guard.now().UTC())
	if err != nil {
		return err
	}
	if completed.Receipt != nil {
		session.ComputeReceipt = planningReceipt(*completed.Receipt)
	}
	return nil
}

func (guard *computeGuard) heartbeat(sessionID, token string, stop <-chan struct{}, cancel context.CancelFunc) {
	interval := guard.lease / 3
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if err := guard.store.RenewComputeExecution(context.Background(), sessionID, token, guard.now().UTC(), guard.lease); err != nil {
				cancel()
				return
			}
		}
	}
}

func planningReceipt(receipt controlplane.ComputeReceipt) *planningdomain.ComputeReceipt {
	return &planningdomain.ComputeReceipt{
		SessionID:      receipt.SessionID,
		PlanRevisionID: receipt.PlanRevisionID,
		PlanDigest:     receipt.PlanDigest,
		Outcome:        receipt.Outcome,
		CompletedAt:    receipt.CompletedAt,
	}
}
