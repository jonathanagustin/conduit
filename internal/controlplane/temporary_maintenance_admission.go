package controlplane

import (
	"context"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	"sort"
	"time"
)

// TemporaryMaintenanceAdmission serves maintenance-aware Agents. The base Plan
// is never rewritten with the temporary grant: ordinary leases and old readers
// always see the original schedule.
// CONDUIT-INVENTED: maintenance.EphemeralLeaseAdmission
// Displaced-by: a published or sanitized temporary maintenance override Agent admission protocol with durable approval lookup and last-safe-point expiry.
type TemporaryMaintenanceAdmission struct {
	repository Repository
	now        func() time.Time
}

func NewTemporaryMaintenanceAdmission(repository Repository) *TemporaryMaintenanceAdmission {
	return &TemporaryMaintenanceAdmission{repository, time.Now}
}
func (s *TemporaryMaintenanceAdmission) within(ctx context.Context, environment string, run func(*Lifecycle) error) error {
	return moduleOverrideBoundary(ctx, s.repository, func(repository Repository) error {
		requests, err := repository.ListChangeRequests(ctx, environment)
		if err != nil {
			return err
		}
		view := &temporaryMaintenanceView{Repository: repository, now: s.now}
		for _, request := range requests {
			if request.TargetKind != TemporaryMaintenanceTarget || request.Environment != environment || request.Status != ChangeApproved || !request.RequirementsSatisfied || hasDoNotMerge(request.Labels) {
				continue
			}
			window, err := decodeTemporaryMaintenance(request)
			if err != nil {
				return err
			}
			view.windows = append(view.windows, approvedTemporaryMaintenance{window, request.ID})
		}
		lifecycle := NewLifecycle(view)
		lifecycle.now = s.now
		return run(lifecycle)
	})
}
func (s *TemporaryMaintenanceAdmission) Lease(ctx context.Context, environment, agentID string, duration time.Duration) (PlanRecord, error) {
	var result PlanRecord
	err := s.within(ctx, environment, func(lifecycle *Lifecycle) error {
		var err error
		result, err = lifecycle.LeaseNextPlan(ctx, environment, agentID, duration)
		if err != nil {
			return err
		}
		return lifecycle.repository.(*temporaryMaintenanceView).auditAdmission(ctx, agentID, result, "TEMPORARY_ENTITY_MAINTENANCE_LEASE_ADMITTED")
	})
	return result, err
}
func (s *TemporaryMaintenanceAdmission) Start(ctx context.Context, environment, agentID, planID, token string) (PlanRecord, error) {
	var result PlanRecord
	err := s.within(ctx, environment, func(lifecycle *Lifecycle) error {
		record, err := lifecycle.repository.GetPlan(ctx, planID)
		if err != nil {
			return err
		}
		if record.Plan.Environment != environment || record.Lease == nil || record.Lease.AgentID != agentID {
			return ErrForbidden
		}
		result, err = lifecycle.StartPlan(ctx, planID, token)
		if err != nil {
			return err
		}
		return lifecycle.repository.(*temporaryMaintenanceView).auditAdmission(ctx, agentID, result, "TEMPORARY_ENTITY_MAINTENANCE_START_ADMITTED")
	})
	return result, err
}

type approvedTemporaryMaintenance struct {
	window    temporaryMaintenanceWindow
	requestID string
}
type temporaryMaintenanceView struct {
	Repository
	windows []approvedTemporaryMaintenance
	now     func() time.Time
}

func (v *temporaryMaintenanceView) project(record PlanRecord) PlanRecord {
	record = cloneJSON(record)
	// Imperative commands retain their existing separate policy and never gain
	// native temporary maintenance authority through this admission path.
	if record.Command != nil {
		return record
	}
	for i, step := range record.Plan.Steps {
		for _, approved := range v.windows {
			window := approved.window
			now := v.now().UTC()
			if window.Environment != record.Plan.Environment || window.Entity != step.Entity || window.InstallationID != step.InstallationID || window.Product != step.Product || now.Before(window.StartsAt) || !now.Before(window.EndsAt) {
				continue
			}
			record.Plan.Steps[i].Maintenance.Overrides = append(record.Plan.Steps[i].Maintenance.Overrides, MaintenanceOverride{InstallationID: step.InstallationID, Type: window.Type, StartsAt: window.StartsAt, EndsAt: window.EndsAt, Reason: window.Reason, ApprovedBy: "change-request:" + approved.requestID})
		}
	}
	return record
}
func (v *temporaryMaintenanceView) ListPlans(ctx context.Context, environment string) ([]PlanRecord, error) {
	records, err := v.Repository.ListPlans(ctx, environment)
	if err != nil {
		return nil, err
	}
	for i := range records {
		records[i] = v.project(records[i])
	}
	return records, nil
}
func (v *temporaryMaintenanceView) GetPlan(ctx context.Context, id string) (PlanRecord, error) {
	record, err := v.Repository.GetPlan(ctx, id)
	if err != nil {
		return record, err
	}
	return v.project(record), nil
}
func (v *temporaryMaintenanceView) UpdatePlan(ctx context.Context, id string, update func(*PlanRecord) error) (PlanRecord, error) {
	record, err := v.Repository.UpdatePlan(ctx, id, func(record *PlanRecord) error {
		base := cloneJSON(record.Plan)
		projected := v.project(*record)
		record.Plan = projected.Plan
		defer func() { record.Plan = base }()
		return update(record)
	})
	if err != nil {
		return record, err
	}
	return v.project(record), nil
}

func (v *temporaryMaintenanceView) auditAdmission(ctx context.Context, agentID string, record PlanRecord, action string) error {
	if record.Command != nil {
		return nil
	}
	ids := map[string]bool{}
	for _, approved := range v.windows {
		for _, step := range record.Plan.Steps {
			for _, window := range step.Maintenance.Overrides {
				if window.ApprovedBy == "change-request:"+approved.requestID {
					ids[approved.requestID] = true
				}
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	requests := make([]string, 0, len(ids))
	for id := range ids {
		requests = append(requests, id)
	}
	sort.Strings(requests)
	event := governance.NewAuthorizationCheckEvent(v.now().UTC(), agentID, action, environmentRID(record.Plan.Environment), "ALLOW", "maintenance-aware admission using current approved Entity windows", map[string]any{"planId": record.ID, "changeRequestIds": requests, "valueTier": "CONDUIT_NATIVE"})
	_, err := v.Repository.AppendAuthorizationAudit(ctx, event)
	return err
}
