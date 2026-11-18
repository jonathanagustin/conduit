package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
)

type refusingRolloutAudit struct {
	controlplane.Repository
	fail bool
}

func (r *refusingRolloutAudit) AppendAuthorizationAudit(ctx context.Context, event controlplane.AuthorizationAuditEvent) (controlplane.AuthorizationAuditEvent, error) {
	if r.fail {
		return controlplane.AuthorizationAuditEvent{}, errors.New("synthetic audit refusal")
	}
	return r.Repository.AppendAuthorizationAudit(ctx, event)
}

func TestCreateAuditFailureDoesNotPublishDraftOrPoisonRetry(t *testing.T) {
	repository := &refusingRolloutAudit{Repository: controlplane.NewMemoryRepository(), fail: true}
	service := &Service{Store: repository}
	waves := []domain.Wave{{Name: "canary", Environments: []string{"east"}, Gate: domain.GateHealth}}
	result, err := service.Create(t.Context(), "ada", "rollout", "ledger", "1", waves)
	if err == nil || result.Allowed || result.AuditID != "" {
		t.Fatal("failed audit acknowledged", result, err)
	}
	if len(service.List()) != 0 {
		t.Error("failed audit published draft")
	}
	repository.fail = false
	result, err = service.Create(t.Context(), "ada", "rollout", "ledger", "1", waves)
	if err != nil || !result.Allowed || result.AuditID == "" || len(service.List()) != 1 {
		t.Fatal("same-target retry failed", result, err)
	}
}

func TestPauseAuditFailureDoesNotPublishTransitionAndCanRetry(t *testing.T) {
	repository := &refusingRolloutAudit{Repository: controlplane.NewMemoryRepository()}
	service := &Service{Store: repository}
	if _, err := service.Create(t.Context(), "ada", "rollout", "ledger", "1", []domain.Wave{{Name: "canary", Environments: []string{"east"}, Gate: domain.GateHealth}}); err != nil {
		t.Fatal(err)
	}
	proposal, err := service.Propose(t.Context(), controlplane.Principal{Username: "ada"}, "rollout", "start", domain.Facts{Healthy: map[string]bool{"east": true}})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.Approve(t.Context(), controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}, proposal.ChangeRequestID, "healthy")
	if err != nil || started.Status != domain.StatusRunning {
		t.Fatal("running fixture", started, err)
	}
	repository.fail = true
	result, err := service.Pause(t.Context(), "ada", "rollout", true)
	if err == nil || result.Allowed || result.AuditID != "" {
		t.Fatal("failed audit acknowledged", result, err)
	}
	rows := service.List()
	if len(rows) != 1 || rows[0].Status != domain.StatusRunning || rows[0].PausedBy != "" {
		t.Error("failed audit published pause", rows)
	}
	repository.fail = false
	result, err = service.Pause(t.Context(), "ada", "rollout", true)
	if err != nil || !result.Allowed || result.Status != domain.StatusPaused || result.AuditID == "" {
		t.Fatal("retry failed", result, err)
	}
}
