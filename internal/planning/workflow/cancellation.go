package workflow

import (
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/planning/domain"
)

const (
	UpdateCancelTurn    = "planning.CancelTurn"
	ErrTypeRunCancelled = "RunCancelled"
	ErrTypeRunConflict  = "RunConflict"
	ErrTypeRunCapacity  = "RunCapacity"
	maxTurnRuns         = domain.MaxTurnRuns
)

// CancelTurnRequest addresses one logical agent run, never the entire session.
type CancelTurnRequest struct {
	Actor string `json:"actor"`
	RunID string `json:"runId"`
}

// CONDUIT-INVENTED: planning.TurnCancellation
// Displaced-by: An authoritative planning-run cancellation contract defining identity, durable fencing, acknowledgement and retained run identifiers.

// CancelTurnReceipt distinguishes a persisted fence from a finished turn handler.
// Cancellation does not undo a previously committed turn, answer or acceptance.
type CancelTurnReceipt = domain.TurnCancellationReceipt

type turnRuns struct {
	states map[string]string
	active map[string]workflow.CancelFunc
}

func newTurnRuns(ctx workflow.Context, carry map[string]string, actor string, accepted func() bool) (*turnRuns, error) {
	r := &turnRuns{states: make(map[string]string), active: make(map[string]workflow.CancelFunc)}
	if len(carry) > maxTurnRuns {
		return nil, turnRunError(ErrTypeRunCapacity, "planning turn history is full")
	}
	for id, state := range carry {
		if err := validateTurnRunID(id, false); err != nil {
			return nil, err
		}
		switch state {
		case "completed", "failed", "cancelled":
			r.states[id] = state
		default:
			return nil, turnRunError(ErrTypeBadInput, "invalid continued planning turn state")
		}
	}
	err := workflow.SetUpdateHandlerWithOptions(ctx, UpdateCancelTurn,
		func(_ workflow.Context, req CancelTurnRequest) (*CancelTurnReceipt, error) {
			state := r.states[req.RunID]
			if state == "" {
				if accepted() {
					return nil, turnRunError(ErrTypeAlreadyAccepted, domain.ErrAlreadyAccepted.Error())
				}
				if len(r.states) >= maxTurnRuns {
					return nil, turnRunError(ErrTypeRunCapacity, "planning turn history is full")
				}
				// Persist a tombstone even when cancellation arrives before Turn.
				r.states[req.RunID] = "cancelled"
			} else if state == "running" {
				r.states[req.RunID] = "cancellation_requested"
				if cancel := r.active[req.RunID]; cancel != nil {
					cancel()
				}
			}
			return &CancelTurnReceipt{RunID: req.RunID, State: r.states[req.RunID], ValueTier: "CONDUIT_NATIVE"}, nil
		}, workflow.UpdateHandlerOptions{Validator: func(_ workflow.Context, req CancelTurnRequest) error {
			if err := checkActor(actor, req.Actor); err != nil {
				return err
			}
			return validateTurnRunID(req.RunID, false)
		}})
	return r, err
}

func (r *turnRuns) turn(ctx workflow.Context, id string, execute func(workflow.Context, func() bool) (*domain.Session, error)) (*domain.Session, error) {
	if id == "" {
		// Legacy histories retain their activity commands and cannot be targeted
		// by a guessed logical run id. No cancellation guarantee is implied.
		return execute(ctx, func() bool { return true })
	}
	if err := validateTurnRunID(id, false); err != nil {
		return nil, err
	}
	if state := r.states[id]; state != "" {
		if state == "cancelled" || state == "cancellation_requested" {
			return nil, turnRunError(ErrTypeRunCancelled, "planning turn was cancelled")
		}
		return nil, turnRunError(ErrTypeRunConflict, "planning run id has already been used")
	}
	if len(r.states) >= maxTurnRuns {
		return nil, turnRunError(ErrTypeRunCapacity, "planning turn history is full")
	}
	activityCtx, cancel := workflow.WithCancel(ctx)
	r.states[id] = "running"
	r.active[id] = cancel
	permitted := func() bool { return r.states[id] == "running" }
	defer func() { delete(r.active, id); cancel() }()
	result, err := execute(activityCtx, permitted)
	if !permitted() {
		r.states[id] = "cancelled"
		return nil, turnRunError(ErrTypeRunCancelled, "planning turn was cancelled")
	}
	if err != nil {
		r.states[id] = "failed"
	} else {
		r.states[id] = "completed"
	}
	return result, err
}

func (r *turnRuns) snapshot() map[string]string {
	if len(r.states) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.states))
	for id, state := range r.states {
		out[id] = state
	}
	return out
}

func validateTurnRunID(id string, legacy bool) error {
	if legacy && id == "" {
		return nil
	}
	if !domain.ValidTurnRunID(id) {
		return turnRunError(ErrTypeBadInput, "invalid planning run id")
	}
	return nil
}

func turnRunError(kind, message string) error {
	return temporal.NewNonRetryableApplicationError(message, kind, nil)
}
