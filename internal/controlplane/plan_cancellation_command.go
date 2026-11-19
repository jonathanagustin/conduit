package controlplane

import (
	"context"
	"fmt"
	"time"
)

// PlanCancellationCommand is the existing operator cancellation request. The
// caller supplies intent, never an identity or additional authority.
type PlanCancellationCommand struct {
	Reason                     string `json:"reason"`
	CreateSuppression          bool   `json:"createSuppression,omitempty"`
	SuppressionDurationSeconds int    `json:"suppressionDurationSeconds,omitempty"`
	InstallationID             string `json:"installationId,omitempty"`
}

// PlanCancellationValidationError identifies request validation before effects.
// Adapters retain their existing validation status and response envelope.
type PlanCancellationValidationError string

func (err PlanCancellationValidationError) Error() string { return string(err) }

// CancelPlanAsOperator shares the existing cancellation policy and effect order.
// Authenticate the original carrier before calling. Authorship admits governed
// cancellation, but creating a suppression separately requires Operate. Effects
// remain sequential: an error after cancellation does not imply no write occurred.
// Wire adapters must redact lease credentials from any successful result.
func CancelPlanAsOperator(ctx context.Context, repository PlanDetailReader, lifecycle *Lifecycle, governance *GovernanceService, principal Principal, options HandlerOptions, planID string, command PlanCancellationCommand) (PlanRecord, error) {
	subject := principal.Username
	existing, err := repository.GetPlan(ctx, planID)
	if err != nil {
		return PlanRecord{}, err
	}
	if existing.ChangeRequestID != "" {
		changeRequest, err := repository.GetChangeRequest(ctx, existing.ChangeRequestID)
		if err != nil {
			return PlanRecord{}, err
		}
		if changeRequest.Author.Username != principal.Username {
			return PlanRecord{}, ErrForbidden
		}
	} else {
		// A Plan with no Change Request is cancelled by Lifecycle.CancelPlan,
		// which performs no eligibility check: authentication alone therefore
		// let any operator cancel such a Plan, in any Environment, including
		// one they hold nothing on, and optionally plant a suppression.
		//
		// The check is the one the approval route for the same ungoverned
		// branch already makes. It applies to this branch only: a governed
		// Plan answers to its author, and requiring Operate here as well
		// would refuse an author who may cancel their own Change Request
		// without holding the Environment itself.
		if !governance.AuthorizeResourceOperation(ctx, principal, options,
			environmentRID(existing.Plan.Environment), OperationOperate) {
			return PlanRecord{}, ErrForbidden
		}
	}
	duration := command.SuppressionDurationSeconds
	if command.CreateSuppression {
		// A suppression is durable Environment state that outlives the Plan
		// which planted it, and POST /v1/environments/{id}/suppressions
		// gates that same effect at Operate. Authorship is standing over a
		// Change Request and not over an Environment, so the governed branch
		// above -- which asks only whether the author is calling -- let an
		// author who never held more than ProposeChange reach the effect at
		// the lower of two bars (#150).
		//
		// Only the suppression is gated and not the cancellation: requiring
		// Operate for the whole route would refuse an author cancelling
		// their own Change Request in an Environment they hold nothing on,
		// which governance admits on purpose. Stated once, after the branch
		// dispatch, so that both branches answer to it.
		if !governance.AuthorizeResourceOperation(ctx, principal, options,
			environmentRID(existing.Plan.Environment), OperationOperate) {
			return PlanRecord{}, fmt.Errorf("%w: creating a suppression through Plan cancellation requires %s on %s",
				ErrForbidden, OperationOperate, environmentRID(existing.Plan.Environment))
		}
		if duration == 0 {
			duration = 30 * 60
		}
		if duration < 60 || duration > 30*24*60*60 {
			return PlanRecord{}, PlanCancellationValidationError("suppression duration must be between one minute and 30 days")
		}
		if command.InstallationID != "" && !existing.Plan.ContainsInstallation(command.InstallationID) {
			return PlanRecord{}, PlanCancellationValidationError("installation is not in the Plan")
		}
	}
	record, err := lifecycle.CancelPlan(ctx, planID, subject, command.Reason)
	if err != nil {
		return PlanRecord{}, err
	}
	if record.ChangeRequestID != "" {
		if _, err := governance.Cancel(ctx, record.ChangeRequestID, principal, command.Reason); err != nil {
			return PlanRecord{}, err
		}
	}
	if command.CreateSuppression {
		startsAt := time.Now().UTC()
		endsAt := startsAt.Add(time.Duration(duration) * time.Second)
		if _, err := lifecycle.CreateManualSuppression(
			ctx, record.Plan.Environment, command.InstallationID,
			"Plan cancellation: "+command.Reason, subject, startsAt, &endsAt,
		); err != nil {
			return PlanRecord{}, err
		}
	}
	return record, nil
}
