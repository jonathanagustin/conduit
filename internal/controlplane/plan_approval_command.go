package controlplane

import "context"

// PlanApprovalHandoffError reports a failed sequencing handoff after approval
// has already been recorded. Retrying the approval is not a recovery action.
type PlanApprovalHandoffError struct {
	PlanID string
	Cause  error
}

func (err *PlanApprovalHandoffError) Error() string {
	return "plan sequencing failed: " + err.Cause.Error()
}
func (err *PlanApprovalHandoffError) Unwrap() error { return err.Cause }

// ApprovePlanAsOperator is the existing native approval command shared by wire
// adapters. Keep the original carrier: governed reviewer policy and resource
// authorization own their respective decisions, including audit identity.
func ApprovePlanAsOperator(ctx context.Context, repository Repository, lifecycle *Lifecycle, governance *GovernanceService, principal Principal, options HandlerOptions, planID, rationale string) (PlanRecord, error) {
	existing, err := repository.GetPlan(ctx, planID)
	if err != nil {
		return PlanRecord{}, err
	}
	var record PlanRecord
	if existing.ChangeRequestID != "" {
		_, record, err = governance.Approve(ctx, existing.ChangeRequestID, principal, rationale)
	} else {
		// Reconciliation and command Plans can legitimately lack a Change
		// Request. Lifecycle alone checks no eligibility, so this branch needs
		// Operate on the Environment. Do not impose that extra grant on a
		// governed reviewer whom the Change Request policy deliberately admits.
		if !governance.AuthorizeResourceOperation(ctx, principal, options, environmentRID(existing.Plan.Environment), OperationOperate) {
			return PlanRecord{}, ErrForbidden
		}
		record, err = lifecycle.ApprovePlan(ctx, existing.ID, principal.Username, rationale)
	}
	if err != nil {
		return PlanRecord{}, err
	}
	if err := notifyPlanApproved(ctx, options, record); err != nil {
		return record, &PlanApprovalHandoffError{PlanID: record.ID, Cause: err}
	}
	return record, nil
}
