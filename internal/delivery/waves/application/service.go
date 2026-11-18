// Package application stores staged rollouts and gates their mutations.
// Pause applies after confirmation. Start, resume, and roll back open a change
// request and apply only when a different person approves it.
package application

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

// Result is one mutation.
type Result struct {
	Allowed         bool
	Reason          string
	ChangeRequestID string
	Status          string
	Rollout         domain.Rollout
	AuditID         string
}

// Governance records proposals and reviews through the Hub's configured service.
type Governance interface {
	ProposeHumanChange(context.Context, string, string, string, []controlplane.EntityChange, controlplane.Principal, string, string) (controlplane.ChangeRequest, error)
	Approve(context.Context, string, controlplane.Principal, string) (controlplane.ChangeRequest, controlplane.PlanRecord, error)
}

// Service is the rollout store for one process.
type Service struct {
	Governance Governance
	Store      controlplane.Repository
	Now        func() time.Time

	mu       sync.Mutex
	rollouts map[string]domain.Rollout
	pending  map[string]pending
}

type pending struct {
	rolloutID string
	action    string
	author    string
	facts     domain.Facts
}

func (service *Service) governance() Governance {
	if service.Governance != nil {
		return service.Governance
	}
	return controlplane.NewGovernanceService(service.Store)
}

func (service *Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

// Create stores a draft. It does not start the rollout.
func (service *Service) Create(ctx context.Context, actor, id, product, version string, waves []domain.Wave) (Result, error) {
	rollout, err := domain.New(id, product, version, actor, waves)
	if err != nil {
		return Result{}, err
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	if service.rollouts == nil {
		service.rollouts = map[string]domain.Rollout{}
	}
	if _, exists := service.rollouts[rollout.ID]; exists {
		return Result{}, fmt.Errorf("rollout %s already exists", rollout.ID)
	}
	auditID, err := service.audit(ctx, actor, "rollout/"+rollout.ID, governance.DecisionAllow, "rollout drafted")
	if err != nil {
		return Result{}, err
	}
	service.rollouts[rollout.ID] = rollout
	return Result{Allowed: true, Reason: "rollout drafted", Status: rollout.Status, Rollout: rollout, AuditID: auditID}, nil
}

// Pause applies when the caller confirms. It does not open a change request.
func (service *Service) Pause(ctx context.Context, actor, id string, confirmed bool) (Result, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	rollout, ok := service.rollouts[id]
	if !ok {
		return Result{Reason: "rollout was not found"}, nil
	}
	next, reason := domain.Pause(rollout, actor, confirmed)
	decision := governance.DecisionDeny
	if reason == "rollout paused" {
		decision = governance.DecisionAllow
	}
	auditID, err := service.audit(ctx, actor, "rollout/"+id, decision, reason)
	if err != nil {
		return Result{}, err
	}
	if decision == governance.DecisionAllow {
		service.rollouts[id] = next
	}
	return Result{Allowed: decision == governance.DecisionAllow, Reason: reason, Status: next.Status, Rollout: next, AuditID: auditID}, nil
}

// Propose opens a change request for start, resume, or roll back.
func (service *Service) Propose(ctx context.Context, actor controlplane.Principal, id, action string, facts domain.Facts) (Result, error) {
	switch action {
	case "start", "resume", "rollback":
	default:
		return Result{}, fmt.Errorf("unknown rollout action %q", action)
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	rollout, ok := service.rollouts[id]
	if !ok {
		return Result{Reason: "rollout was not found"}, nil
	}
	if service.pending == nil {
		service.pending = map[string]pending{}
	}
	governanceService := service.governance()
	request, err := governanceService.ProposeHumanChange(ctx, "ROLLOUT", id, rollout.Waves[0].Environments[0], []controlplane.EntityChange{{
		ID: action, Type: controlplane.ChangeTypeUpdateEntity, InstallationID: id,
		Request: map[string]any{"action": action, "rollout": id},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment":  {RID: "environment/" + rollout.Waves[0].Environments[0], Name: rollout.Waves[0].Environments[0]},
			"installation": {RID: "installation/" + id, Name: id},
			"product":      {RID: "product/" + rollout.Product, Name: rollout.Product},
		},
	}}, actor, "Rollout "+action+" "+id, action+" "+rollout.Product+" "+rollout.Version)
	if err != nil {
		return Result{}, err
	}
	service.pending[request.ID] = pending{rolloutID: id, action: action, author: actor.Username, facts: facts}
	auditID, err := service.audit(ctx, actor.Username, "rollout/"+id, governance.DecisionAllow, "proposed "+action)
	if err != nil {
		return Result{}, err
	}
	return Result{Allowed: true, Reason: "proposed " + action, ChangeRequestID: request.ID, Status: request.Status, Rollout: rollout, AuditID: auditID}, nil
}

// Approve applies a proposed action when the caller is not its author.
func (service *Service) Approve(ctx context.Context, approver controlplane.Principal, changeID, rationale string) (Result, error) {
	if strings.TrimSpace(rationale) == "" {
		return Result{Reason: "an approval rationale is required"}, nil
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	item, ok := service.pending[changeID]
	if !ok {
		return Result{Reason: "change request is not pending"}, nil
	}
	if item.author == approver.Username {
		auditID, err := service.audit(ctx, approver.Username, "rollout/"+item.rolloutID, governance.DecisionDeny, "the author cannot approve their own change request")
		if err != nil {
			return Result{}, err
		}
		return Result{Reason: "the author cannot approve their own change request", AuditID: auditID}, nil
	}
	governanceService := service.governance()
	if _, _, err := governanceService.Approve(ctx, changeID, approver, rationale); err != nil {
		return Result{}, err
	}
	rollout := service.rollouts[item.rolloutID]
	var reason string
	switch item.action {
	case "start", "resume":
		if rollout.Status == domain.StatusPaused && item.action == "resume" {
			rollout.Status = domain.StatusRunning
		}
		rollout, reason = domain.Advance(rollout, item.facts)
	case "rollback":
		rollout.Status = domain.StatusRolledBack
		reason = "rollout rolled back"
	}
	service.rollouts[item.rolloutID] = rollout
	delete(service.pending, changeID)
	auditID, err := service.audit(ctx, approver.Username, "rollout/"+item.rolloutID, governance.DecisionAllow, reason)
	if err != nil {
		return Result{}, err
	}
	return Result{Allowed: true, Reason: reason, Status: rollout.Status, Rollout: rollout, AuditID: auditID, ChangeRequestID: changeID}, nil
}

// List returns the stored rollouts.
func (service *Service) List() []domain.Rollout {
	service.mu.Lock()
	defer service.mu.Unlock()
	out := make([]domain.Rollout, 0, len(service.rollouts))
	for _, rollout := range service.rollouts {
		out = append(out, rollout)
	}
	return out
}

func (service *Service) audit(ctx context.Context, actor, resource, decision, reason string) (string, error) {
	event := governance.NewAuthorizationCheckEvent(service.now(), actor, "rollout.step", resource, decision, reason, nil)
	saved, err := service.Store.AppendAuthorizationAudit(ctx, event)
	if err != nil {
		return "", err
	}
	return saved.ID, nil
}
