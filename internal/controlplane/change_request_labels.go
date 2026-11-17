package controlplane

import (
	"context"
	"errors"
	"fmt"
)

var errConcurrentLabelUpdate = errors.New("change request changed before label mutation")

// MutateChangeRequestLabel transforms one existing label within the serialized
// request/Plan write. The legacy Do not merge label is built in; other names
// refer to active label-definition IDs. Attached legacy names remain removable;
// a preview performs no repository writes.
func (service *GovernanceService) MutateChangeRequestLabel(ctx context.Context, id string, principal Principal, name, operation string, dryRun bool) (ChangeRequest, PlanRecord, bool, error) {
	existing, err := service.RequireChangeRequestAnnotation(ctx, id, principal)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, false, err
	}
	if name != "Do not merge" && !contains(existing.Labels, name) {
		definition, err := service.repository.GetLabelDefinition(ctx, name)
		if err != nil {
			return ChangeRequest{}, PlanRecord{}, false, err
		}
		if definition.ID != name || (definition.Status != LabelDefinitionActive && definition.Status != LabelDefinitionTombstoned) {
			return ChangeRequest{}, PlanRecord{}, false, errors.New("label definition is inconsistent")
		}
		if definition.Status != LabelDefinitionActive && !(operation == "REMOVE" && definition.Status == LabelDefinitionTombstoned) {
			return ChangeRequest{}, PlanRecord{}, false, ErrNotFound
		}
	}
	if operation != "ADD" && operation != "REMOVE" {
		return ChangeRequest{}, PlanRecord{}, false, fmt.Errorf("%w: expected ADD or REMOVE label operation", ErrConflict)
	}
	return service.transformLabels(ctx, id, principal, func(previous []string) []string {
		if operation == "ADD" {
			return append(append([]string(nil), previous...), name)
		}
		result := make([]string, 0, len(previous))
		for _, label := range previous {
			if label != name {
				result = append(result, label)
			}
		}
		return result
	}, dryRun)
}

func (service *GovernanceService) transformLabels(ctx context.Context, id string, principal Principal, transform func([]string) []string, dryRun bool) (ChangeRequest, PlanRecord, bool, error) {
	for range 64 {
		if err := ctx.Err(); err != nil {
			return ChangeRequest{}, PlanRecord{}, false, err
		}
		candidate, err := service.repository.GetChangeRequest(ctx, id)
		if err != nil {
			return ChangeRequest{}, PlanRecord{}, false, err
		}
		if candidate.Author.Username != principal.Username && !principalCanReview(ctx, service.repository, candidate.Requirements, principal) {
			return ChangeRequest{}, PlanRecord{}, false, ErrForbidden
		}
		approved := false
		mutate := func(request *ChangeRequest, plan *PlanRecord) error {
			if request.Revision != candidate.Revision {
				return errConcurrentLabelUpdate
			}
			if request.Status == ChangeRejected || request.Status == ChangeCancelled || request.Status == ChangeError {
				return ErrInvalidTransition
			}
			wasApproved := request.Status == ChangeApproved
			request.Labels = normalizedStrings(transform(request.Labels))
			request.Status = ChangePendingApprovals
			if request.RequirementsSatisfied && !hasDoNotMerge(request.Labels) {
				request.Status = ChangeApproved
			}
			approved = !wasApproved && request.Status == ChangeApproved
			request.UpdatedAt = service.now().UTC()
			if plan != nil {
				return projectChangeRequestPlan(plan, *request, request.Status)
			}
			return nil
		}
		if dryRun {
			var plan PlanRecord
			var target *PlanRecord
			if candidate.PlanID != "" {
				plan, err = service.repository.GetPlan(ctx, candidate.PlanID)
				if err != nil {
					return ChangeRequest{}, PlanRecord{}, false, err
				}
				target = &plan
			}
			if err := mutate(&candidate, target); err != nil {
				return ChangeRequest{}, PlanRecord{}, false, err
			}
			return candidate, plan, approved, nil
		}
		request, plan, err := service.repository.UpdateChangeRequestAndPlan(ctx, id, true, mutate)
		if errors.Is(err, errConcurrentLabelUpdate) {
			continue
		}
		if err != nil {
			return ChangeRequest{}, PlanRecord{}, false, err
		}
		if err := service.audit(ctx, principal.Username, "CHANGE_REQUEST_LABELS", id, "ALLOW", "labels updated", map[string]any{"labels": request.Labels}); err != nil {
			return ChangeRequest{}, PlanRecord{}, false, fmt.Errorf("persist label mutation audit: %w", err)
		}
		if approved {
			request, err = service.dispatchApproved(ctx, request)
			if err != nil {
				return request, plan, approved, err
			}
		}
		return request, plan, approved, nil
	}
	return ChangeRequest{}, PlanRecord{}, false, ErrConflict
}
