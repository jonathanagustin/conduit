package application

import (
	"context"
	"errors"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

type localTurnRun struct {
	state  string
	cancel context.CancelFunc
}

// CancelTurn fences a turn in this development owner's process-local store.
// Production uses workflow history; this store is never its fallback.
func (s *Service) CancelTurn(ctx context.Context, sessionID, runID string) (*domain.TurnCancellationReceipt, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !domain.ValidTurnRunID(runID) {
		return nil, domain.IntakeError{Reason: "invalid planning run id"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, err := s.mutable(sessionID)
	if err != nil {
		return nil, err
	}
	runs := s.turnRuns[sessionID]
	run := runs[runID]
	if run == nil {
		if session.Status == domain.StatusAccepted {
			return nil, domain.ErrAlreadyAccepted
		}
		if len(runs) >= domain.MaxTurnRuns {
			return nil, domain.ErrRunCapacity
		}
		if runs == nil {
			runs = map[string]*localTurnRun{}
			s.turnRuns[sessionID] = runs
		}
		run = &localTurnRun{state: "cancelled"}
		runs[runID] = run
	} else if run.state == "running" {
		run.state = "cancellation_requested"
		if run.cancel != nil {
			run.cancel()
		}
	}
	return &domain.TurnCancellationReceipt{RunID: runID, State: run.state, ValueTier: "CONDUIT_NATIVE"}, nil
}

// beginTurnLocked reserves a logical id while the session mutex is held.
func (s *Service) beginTurnLocked(ctx context.Context, sessionID string) (context.Context, *localTurnRun, error) {
	_, id := RunIdentifiers(ctx)
	if id == "" {
		return ctx, nil, nil
	}
	if !domain.ValidTurnRunID(id) {
		return ctx, nil, domain.IntakeError{Reason: "invalid planning run id"}
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	runs := s.turnRuns[sessionID]
	if err := localRunError(runs[id]); err != nil {
		return ctx, nil, err
	}
	if len(runs) >= domain.MaxTurnRuns {
		return ctx, nil, domain.ErrRunCapacity
	}
	if runs == nil {
		runs = map[string]*localTurnRun{}
		s.turnRuns[sessionID] = runs
	}
	child, cancel := context.WithCancel(ctx)
	run := &localTurnRun{state: "running", cancel: cancel}
	runs[id] = run
	return child, run, nil
}

func localRunError(run *localTurnRun) error {
	if run == nil {
		return nil
	}
	if run.state == "cancelled" || run.state == "cancellation_requested" {
		return domain.ErrRunCancelled
	}
	return domain.ErrRunConflict
}

func (s *Service) finishTurn(sessionID string, run *localTurnRun, turnErr *error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.activeTurns, sessionID)
	if run == nil {
		return
	}
	if run.state == "cancellation_requested" {
		run.state = "cancelled"
		*turnErr = domain.ErrRunCancelled
	} else if run.state == "running" {
		if errors.Is(*turnErr, context.Canceled) {
			run.state = "cancelled"
		} else if *turnErr != nil {
			run.state = "failed"
		} else {
			run.state = "completed"
		}
	}
	if run.cancel != nil {
		run.cancel()
		run.cancel = nil
	}
}
