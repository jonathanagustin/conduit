package controlplane

import "context"

// AssistantActionTarget is distinct from historical intent-only PLAN records.
const AssistantActionTarget = "CONDUIT_NATIVE_ASSISTANT_ACTION"

// AfterChangeApproved dispatches an already committed review. Transaction-bound
// services never inherit this callback: external worker calls cannot commit or
// roll back together with the database approval.
type AfterChangeApproved func(context.Context, ChangeRequest) (ChangeRequest, error)

func (service *GovernanceService) SetAfterChangeApproved(callback AfterChangeApproved) {
	service.afterChangeApproved = callback
}

func (service *GovernanceService) dispatchApproved(ctx context.Context, request ChangeRequest) (ChangeRequest, error) {
	if request.Status != ChangeApproved {
		return request, nil
	}
	if service.afterChangeApproved == nil {
		if request.TargetKind == AssistantActionTarget {
			return request, ErrInvalidTransition
		}
		return request, nil
	}
	return service.afterChangeApproved(ctx, request)
}
