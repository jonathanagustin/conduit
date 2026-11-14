package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/maintenance"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

const TemporaryMaintenanceTarget = "CONDUIT_NATIVE_TEMPORARY_ENTITY_MAINTENANCE"

type temporaryMaintenanceWindow struct {
	Environment      string    `json:"environment"`
	Entity           string    `json:"entity"`
	InstallationID   string    `json:"installationId"`
	Product          string    `json:"product"`
	StartsAt         time.Time `json:"startsAt"`
	EndsAt           time.Time `json:"endsAt"`
	Type             string    `json:"type"`
	Reason           string    `json:"reason"`
	ComplianceTicket string    `json:"complianceTicket,omitempty"`
}

// TemporaryMaintenanceService submits bounded per-Entity windows to normal governance.
// CONDUIT-INVENTED: maintenance.ReviewedEntityWindow
// Displaced-by: a published or sanitized temporary Entity maintenance override transport, persisted approval binding and lease admission contract.
type TemporaryMaintenanceService struct {
	repository Repository
	options    HandlerOptions
	actor      Principal
	now        func() time.Time
}

func NewTemporaryMaintenanceService(repository Repository, options HandlerOptions, actor Principal) *TemporaryMaintenanceService {
	return &TemporaryMaintenanceService{repository, options, cloneJSON(actor), time.Now}
}
func (s *TemporaryMaintenanceService) CreateMaintenanceOverrides(ctx context.Context, draft maintenance.OverrideDraft) (maintenance.OverrideBatch, error) {
	duration, err := maintenance.ValidateDraft(draft)
	result := maintenance.OverrideBatch{ValueTier: maintenance.Native, DryRun: draft.DryRun, Requests: []maintenance.OverrideReceipt{}}
	if err != nil {
		return result, err
	}
	start := s.now().UTC()
	seen := map[maintenance.EntityTarget]bool{}
	for _, target := range draft.Entities {
		if seen[target] {
			continue
		}
		seen[target] = true
		var receipt maintenance.OverrideReceipt
		submit := func(repository Repository) error {
			service := NewGovernanceService(repository)
			service.now = s.now
			if !service.AuthorizeResourceOperation(ctx, s.actor, s.options, environmentRID(target.Environment), OperationProposeChange) {
				return ErrForbidden
			}
			plans, err := repository.ListPlans(ctx, target.Environment)
			if err != nil {
				return err
			}
			latest, ok := latestPlansByEnvironment(plans)[target.Environment]
			if !ok || latest.DesiredState == nil {
				return ErrNotFound
			}
			var step *PlanStep
			for _, candidate := range latest.Plan.Steps {
				if candidate.Entity == target.Entity {
					copy := candidate
					step = &copy
					break
				}
			}
			if step == nil {
				return ErrNotFound
			}
			kind := maintenance.OverrideNoDowntime
			if draft.Downtime {
				kind = maintenance.OverrideDowntime
			}
			window := temporaryMaintenanceWindow{target.Environment, target.Entity, step.InstallationID, step.Product, start, start.Add(duration), kind, strings.TrimSpace(draft.Reason), draft.ComplianceTicket}
			document, err := json.Marshal(window)
			if err != nil {
				return err
			}
			id := temporaryMaintenanceID(window, s.actor)
			change := EntityChange{ID: "chg_" + stableShortID(id), Type: ChangeTypeCreateEntityMaintenanceWindowOverride, Request: map[string]any{"entity": target.Entity, "startTime": window.StartsAt, "endTime": window.EndsAt, "type": kind, "nativeWindow": string(document)}, Metadata: map[string]any{"complianceTicket": draft.ComplianceTicket, "valueTier": maintenance.Native}, ReferencedResources: map[string]ReferencedResource{
				"environment":  service.referencedResource(ctx, environmentRID(target.Environment), target.Environment, effectivePrincipal(s.actor)),
				"installation": service.referencedResource(ctx, installationRID(target.Environment, step.InstallationID), step.InstallationID, effectivePrincipal(s.actor)),
				"product":      service.referencedResource(ctx, productRID(step.Product), step.Product, effectivePrincipal(s.actor)),
			}}
			var request ChangeRequest
			if draft.DryRun {
				request, err = service.PreviewChanges(ctx, TemporaryMaintenanceTarget, id, target.Environment, []EntityChange{change}, s.actor, "Temporary Entity maintenance override", window.Reason)
			} else {
				request, _, err = service.ProposeChanges(ctx, TemporaryMaintenanceTarget, id, target.Environment, []EntityChange{change}, s.actor, "Temporary Entity maintenance override", window.Reason)
			}
			if err != nil {
				return err
			}
			if !draft.DryRun {
				event := governance.NewAuthorizationCheckEvent(s.now().UTC(), s.actor.Username, "TEMPORARY_ENTITY_MAINTENANCE_PROPOSED", environmentRID(target.Environment), "ALLOW", "governed bounded Entity maintenance override", map[string]any{"changeRequestId": request.ID, "entity": target.Entity, "startsAt": start, "endsAt": window.EndsAt, "type": kind, "valueTier": maintenance.Native})
				if _, err = repository.AppendAuthorizationAudit(ctx, event); err != nil {
					return err
				}
			}
			receipt = maintenance.OverrideReceipt{ChangeRequestID: request.ID, EntityTarget: target, Status: request.Status, StartsAt: start, EndsAt: window.EndsAt, Type: kind}
			return nil
		}
		if draft.DryRun {
			err = submit(s.repository)
		} else {
			err = moduleOverrideBoundary(ctx, s.repository, submit)
		}
		if err != nil {
			result.FailedTarget = &target
			switch {
			case errors.Is(err, ErrForbidden):
				result.Failure = "maintenance override proposal forbidden"
			case errors.Is(err, ErrNotFound):
				result.Failure = "Entity not found in current desired state"
			default:
				result.Failure = "maintenance override submission failed"
			}
			if len(result.Requests) == 0 {
				return result, err
			}
			return result, nil
		}
		result.Requests = append(result.Requests, receipt)
	}
	return result, nil
}
func temporaryMaintenanceID(window temporaryMaintenanceWindow, author Principal) string {
	return "tmw_" + stableShortID(string(mustModuleOverrideJSON(struct {
		Window temporaryMaintenanceWindow
		Author Principal
	}{window, author})))
}
func decodeTemporaryMaintenance(request ChangeRequest) (temporaryMaintenanceWindow, error) {
	var window temporaryMaintenanceWindow
	if request.TargetKind != TemporaryMaintenanceTarget || len(request.Changes) != 1 || request.Changes[0].Type != ChangeTypeCreateEntityMaintenanceWindowOverride {
		return window, ErrInvalidTransition
	}
	document, ok := request.Changes[0].Request["nativeWindow"].(string)
	if !ok || len(document) > 16<<10 {
		return window, ErrConflict
	}
	decoder := json.NewDecoder(bytes.NewBufferString(document))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&window) != nil || decoder.Decode(new(any)) != io.EOF {
		return window, ErrConflict
	}
	if window.Environment != request.Environment || request.Description != window.Reason || request.TargetID != temporaryMaintenanceID(window, request.Author) || window.InstallationID == "" || window.Product == "" || window.StartsAt.IsZero() || !window.EndsAt.After(window.StartsAt) || window.EndsAt.Sub(window.StartsAt) > maintenance.MaximumOverrideDuration || strings.TrimSpace(window.Reason) == "" || (window.Type != maintenance.OverrideDowntime && window.Type != maintenance.OverrideNoDowntime) {
		return window, ErrConflict
	}
	return window, nil
}
