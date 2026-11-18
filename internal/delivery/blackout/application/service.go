// Package application stores blackouts. Declaring one requires confirmation.
// Clearing one opens a change request that the author cannot approve.
package application

import (
	"context"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/blackout/domain"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

// Result is one mutation.
type Result struct {
	Allowed         bool
	Reason          string
	ChangeRequestID string
	Blackouts       []domain.Blackout
	AuditID         string
}

// Governance records proposals and reviews through the Hub's configured service.
type Governance interface {
	ProposeHumanChange(context.Context, string, string, string, []controlplane.EntityChange, controlplane.Principal, string, string) (controlplane.ChangeRequest, error)
	Approve(context.Context, string, controlplane.Principal, string) (controlplane.ChangeRequest, controlplane.PlanRecord, error)
}

// Service stores blackouts for the process and audits each decision.
type Service struct {
	Governance Governance
	Store      controlplane.Repository
	Now        func() time.Time

	mu        sync.Mutex
	blackouts []domain.Blackout
	pending   map[string]int
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

// Active is the operator reason at a moment, or empty when no blackout applies.
func (service *Service) Active(environment string, at time.Time) string {
	service.mu.Lock()
	defer service.mu.Unlock()
	return domain.Reason(service.blackouts, environment, at)
}

// Declare stores a blackout when the caller confirms.
func (service *Service) Declare(ctx context.Context, actor, environment, reason string, starts, ends time.Time, confirmed bool) (Result, error) {
	if !confirmed {
		auditID, err := service.audit(ctx, actor, environment, governance.DecisionDeny, "confirmation is required")
		if err != nil {
			return Result{}, err
		}
		return Result{Reason: "confirmation is required", AuditID: auditID}, nil
	}
	blackout, err := domain.New(environment, actor, reason, starts, ends)
	if err != nil {
		return Result{}, err
	}
	// A refused audit must leave the active blackout set unchanged. Publish the
	// in-process declaration only after its decision receipt has been recorded.
	auditID, err := service.audit(ctx, actor, environment, governance.DecisionAllow, blackout.Reason)
	if err != nil {
		return Result{}, err
	}
	service.mu.Lock()
	service.blackouts = append(service.blackouts, blackout)
	copyOf := append([]domain.Blackout(nil), service.blackouts...)
	service.mu.Unlock()
	return Result{Allowed: true, Reason: blackout.Reason, Blackouts: copyOf, AuditID: auditID}, nil
}

// ProposeClear opens a change request to remove one blackout.
func (service *Service) ProposeClear(ctx context.Context, actor controlplane.Principal, environment string) (Result, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	index := -1
	for i := range service.blackouts {
		if service.blackouts[i].Environment == environment {
			index = i
			break
		}
	}
	if index < 0 {
		return Result{Reason: "no blackout is stored"}, nil
	}
	governanceService := service.governance()
	request, err := governanceService.ProposeHumanChange(ctx, "BLACKOUT", environment, environment, []controlplane.EntityChange{{
		ID: "clear-blackout", Type: controlplane.ChangeTypeUpdateEnvironmentSettings,
		Request: map[string]any{"deploymentId": environment, "settings": map[string]any{"blackout": "clear"}},
		ReferencedResources: map[string]controlplane.ReferencedResource{
			"environment": {RID: "environment/" + environment, Name: environment},
		},
	}}, actor, "Clear blackout on "+environment, service.blackouts[index].Reason)
	if err != nil {
		return Result{}, err
	}
	if service.pending == nil {
		service.pending = map[string]int{}
	}
	service.pending[request.ID] = index
	auditID, err := service.audit(ctx, actor.Username, environment, governance.DecisionAllow, "proposed clear")
	if err != nil {
		return Result{}, err
	}
	return Result{Allowed: true, Reason: "proposed clear", ChangeRequestID: request.ID, AuditID: auditID}, nil
}

// ApproveClear removes the blackout when the caller is not the author.
func (service *Service) ApproveClear(ctx context.Context, approver controlplane.Principal, changeID, rationale string) (Result, error) {
	service.mu.Lock()
	defer service.mu.Unlock()
	index, ok := service.pending[changeID]
	if !ok || rationale == "" {
		return Result{Reason: "change request is not pending"}, nil
	}
	if service.blackouts[index].Author == approver.Username {
		auditID, err := service.audit(ctx, approver.Username, service.blackouts[index].Environment, governance.DecisionDeny, "the author cannot approve their own change request")
		return Result{Reason: "the author cannot approve their own change request", AuditID: auditID}, err
	}
	if _, _, err := service.governance().Approve(ctx, changeID, approver, rationale); err != nil {
		return Result{}, err
	}
	environment := service.blackouts[index].Environment
	service.blackouts = append(service.blackouts[:index], service.blackouts[index+1:]...)
	delete(service.pending, changeID)
	auditID, err := service.audit(ctx, approver.Username, environment, governance.DecisionAllow, "blackout cleared")
	if err != nil {
		return Result{}, err
	}
	return Result{Allowed: true, Reason: "blackout cleared", AuditID: auditID}, nil
}

func (service *Service) audit(ctx context.Context, actor, environment, decision, reason string) (string, error) {
	event := governance.NewAuthorizationCheckEvent(service.now(), actor, "blackout.decision", "environment/"+environment, decision, reason, nil)
	saved, err := service.Store.AppendAuthorizationAudit(ctx, event)
	if err != nil {
		return "", err
	}
	return saved.ID, nil
}
