package controlplane

import "context"

// planVisibilityReader grants only the Change Request lookup used by the
// existing Plan visibility predicate. It carries no lifecycle write authority.
type planVisibilityReader interface {
	GetChangeRequest(context.Context, string) (ChangeRequest, error)
}

func canReadPlan(
	ctx context.Context, repository planVisibilityReader, governance *GovernanceService,
	principal Principal, configuration HandlerOptions, record PlanRecord,
) bool {
	if record.ChangeRequestID != "" {
		changeRequest, err := repository.GetChangeRequest(ctx, record.ChangeRequestID)
		if err != nil {
			return false
		}
		return canReadChangeRequest(ctx, governance, principal, configuration, changeRequest)
	}
	return isGovernanceAdministrator(ctx, governance, principal, configuration) ||
		governance.Authorize(ctx, effectivePrincipal(principal), environmentRID(record.Plan.Environment), OperationReadResource)
}
