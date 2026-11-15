package controlplane

import (
	"context"
	"reflect"
)

// UpdateChangeRequestAndPlan serializes a request mutation with its Plan
// projection. With writeRequest false, the callback must leave the request
// unchanged and projects its current stored verdict, never an older caller copy.
func (repository *MemoryRepository) UpdateChangeRequestAndPlan(ctx context.Context, id string, writeRequest bool, update func(*ChangeRequest, *PlanRecord) error) (ChangeRequest, PlanRecord, error) {
	if err := ctx.Err(); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	original, ok := repository.changeRequests[id]
	if !ok {
		return ChangeRequest{}, PlanRecord{}, ErrNotFound
	}
	request := cloneJSON(original)
	var plan *PlanRecord
	if original.PlanID != "" {
		saved, ok := repository.plans[original.PlanID]
		if !ok {
			return ChangeRequest{}, PlanRecord{}, ErrNotFound
		}
		working := clonePlanRecord(saved)
		plan = &working
	}
	if err := update(&request, plan); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if request.ID != original.ID || request.PlanID != original.PlanID || (!writeRequest && !reflect.DeepEqual(request, original)) {
		return ChangeRequest{}, PlanRecord{}, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if writeRequest {
		request.Revision++
		repository.appendNativeInvalidation("change-request", id, request.Revision, "approvals", "overview", "kpi")
		repository.changeRequests[id] = cloneJSON(request)
	}
	if plan != nil {
		plan.Revision++
		repository.appendNativeInvalidation("plan", original.PlanID, plan.Revision, "plans", "approvals", "overview", "kpi")
		repository.plans[original.PlanID] = clonePlanRecord(*plan)
		return cloneJSON(request), clonePlanRecord(*plan), nil
	}
	return cloneJSON(request), PlanRecord{}, nil
}

// UpdateChangeRequestAndPlan serializes both file writes within the
// single-process repository lock.
// Both records must be durable before acknowledgement. An IO failure is an
// error, not a successful multi-file transaction or an execution receipt.
func (repository *FileRepository) UpdateChangeRequestAndPlan(ctx context.Context, id string, writeRequest bool, update func(*ChangeRequest, *PlanRecord) error) (ChangeRequest, PlanRecord, error) {
	if err := ctx.Err(); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var original ChangeRequest
	if err := readJSON(repository.changeRequestPath(id), &original); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	request := cloneJSON(original)
	var plan *PlanRecord
	if original.PlanID != "" {
		saved, err := repository.readPlan(original.PlanID)
		if err != nil {
			return ChangeRequest{}, PlanRecord{}, err
		}
		plan = &saved
	}
	if err := update(&request, plan); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if request.ID != original.ID || request.PlanID != original.PlanID || (!writeRequest && !reflect.DeepEqual(request, original)) {
		return ChangeRequest{}, PlanRecord{}, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if writeRequest {
		request.Revision++
		if err := repository.writeChangeRequestWithOutbox(id, request); err != nil {
			return ChangeRequest{}, PlanRecord{}, err
		}
	}
	if plan != nil {
		plan.Revision++
		if plan.UpdatedAt.Before(plan.CreatedAt) {
			plan.UpdatedAt = plan.CreatedAt
		}
		if err := repository.writePlanWithOutbox(original.PlanID, *plan); err != nil {
			return ChangeRequest{}, PlanRecord{}, err
		}
		return request, *plan, nil
	}
	return request, PlanRecord{}, nil
}
