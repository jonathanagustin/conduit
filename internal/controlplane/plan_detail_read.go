package controlplane

import "context"

// PlanDetailReader grants only the two reads needed to authorize a Plan detail.
// Lifecycle commands, leases and writes are not part of this projection port.
type PlanDetailReader interface {
	GetPlan(context.Context, string) (PlanRecord, error)
	GetChangeRequest(context.Context, string) (ChangeRequest, error)
}

// ReadAuthorizedPlan is the existing single-Plan read shared by wire adapters.
// Callers authenticate the original carrier first and apply request-specific
// Space selection afterward. A readable Change Request does not require an
// additional Environment grant. The returned lease never exposes its token.
func ReadAuthorizedPlan(ctx context.Context, repository PlanDetailReader, governance *GovernanceService, principal Principal, options HandlerOptions, planID string) (PlanRecord, error) {
	record, err := repository.GetPlan(ctx, planID)
	if err != nil {
		return PlanRecord{}, err
	}
	if record.ChangeRequestID != "" {
		request, err := repository.GetChangeRequest(ctx, record.ChangeRequestID)
		if err != nil {
			return PlanRecord{}, err
		}
		if !canReadChangeRequest(ctx, governance, principal, options, request) {
			return PlanRecord{}, ErrForbidden
		}
	} else if !governance.AuthorizeResourceOperation(ctx, principal, options, environmentRID(record.Plan.Environment), OperationReadResource) {
		return PlanRecord{}, ErrForbidden
	}
	return redactLeaseToken(record), nil
}
