package controlplane

import (
	"context"
	"strings"
)

// RequireChangeRequestAnnotation reuses the request author/reviewer mutation
// authority used by label review operations. Discussion never supplies approval.
func (service *GovernanceService) RequireChangeRequestAnnotation(ctx context.Context, id string, principal Principal) (ChangeRequest, error) {
	if err := ctx.Err(); err != nil {
		return ChangeRequest{}, err
	}
	request, err := service.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return ChangeRequest{}, err
	}
	if request.Author.Username != principal.Username && !principalCanReview(ctx, service.repository, request.Requirements, principal) {
		return ChangeRequest{}, ErrForbidden
	}
	return request, nil
}

// GetReadableChangeRequest applies the existing Change Request read policy with
// the original credential carrier, including its delegation restrictions.
func (service *GovernanceService) GetReadableChangeRequest(ctx context.Context, id string, principal Principal, options HandlerOptions) (ChangeRequest, error) {
	if err := ctx.Err(); err != nil {
		return ChangeRequest{}, err
	}
	request, err := service.repository.GetChangeRequest(ctx, id)
	if err != nil {
		return ChangeRequest{}, err
	}
	if !canReadChangeRequest(ctx, service, principal, options, request) {
		return ChangeRequest{}, ErrForbidden
	}
	return request, nil
}

// FinalizeApprovedChangeRequest preserves the existing reviewed-target handoff
// after a native label mutation, while refusing a stale request snapshot.
func (service *BundleService) FinalizeApprovedChangeRequest(ctx context.Context, request ChangeRequest, approvedByThisCall bool) (*BundleImport, error) {
	if request.TargetKind != "FLEET_ACTION" && request.TargetKind != "BUNDLE_IMPORT" && !strings.HasPrefix(request.TargetKind, "EXPORT_PIPELINE_LIFECYCLE_") {
		return nil, nil
	}
	current, err := service.repository.GetChangeRequest(ctx, request.ID)
	if err != nil {
		return nil, err
	}
	if current.Revision != request.Revision || current.Status != request.Status {
		return nil, ErrConflict
	}
	return finalizeApprovedChangeRequest(ctx, service, current, approvedByThisCall)
}
