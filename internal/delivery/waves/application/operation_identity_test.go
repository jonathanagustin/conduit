package application

import (
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
	"testing"
)

func TestResumeReceivesFreshReviewWithoutReplacingCompletedStart(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	service := &Service{Store: repository}
	author := controlplane.Principal{Username: "ada"}
	reviewer := controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}
	if _, err := service.Create(t.Context(), author.Username, "rollout", "ledger", "1", []domain.Wave{{Name: "first", Environments: []string{"east"}, Gate: domain.GateHealth}}); err != nil {
		t.Fatal(err)
	}
	start, err := service.Propose(t.Context(), author, "rollout", "start", domain.Facts{Healthy: map[string]bool{"east": true}})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := service.Approve(t.Context(), reviewer, start.ChangeRequestID, "start checked"); err != nil || !result.Allowed || result.Status != domain.StatusRunning {
		t.Fatal(result, err)
	}
	if result, err := service.Pause(t.Context(), author.Username, "rollout", true); err != nil || !result.Allowed {
		t.Fatal(result, err)
	}
	resume, err := service.Propose(t.Context(), author, "rollout", "resume", domain.Facts{Healthy: map[string]bool{"east": true}})
	if err != nil {
		t.Fatal(err)
	}
	if !resume.Allowed || resume.ChangeRequestID == start.ChangeRequestID || resume.Status != controlplane.ChangePendingApprovals {
		t.Error("resume reused completed authority", resume)
	}
	old, err := repository.GetChangeRequest(t.Context(), start.ChangeRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if old.Status != controlplane.ChangeApproved || len(old.Reviews) != 1 || old.Changes[0].Request["action"] != "start" {
		t.Fatal("completed start was changed", old)
	}
	pending, err := repository.GetChangeRequest(t.Context(), resume.ChangeRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Status != controlplane.ChangePendingApprovals || len(pending.Reviews) != 0 || pending.Changes[0].Request["action"] != "resume" {
		t.Error("resume lacks fresh independent review", pending)
	}
	if rows := service.List(); len(rows) != 1 || rows[0].Status != domain.StatusPaused {
		t.Fatal("proposal applied before review", rows)
	}
	if result, err := service.Approve(t.Context(), reviewer, resume.ChangeRequestID, "resume checked"); err != nil || !result.Allowed {
		t.Fatal("fresh independent review could not apply resume", result, err)
	}
}

func TestEachWaveAdvanceReceivesFreshReview(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	service := &Service{Store: repository}
	author := controlplane.Principal{Username: "ada"}
	reviewer := controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}
	if _, err := service.Create(t.Context(), author.Username, "rollout", "ledger", "1", []domain.Wave{{Name: "first", Environments: []string{"east"}, Gate: domain.GateHealth}}); err != nil {
		t.Fatal(err)
	}
	first, err := service.Propose(t.Context(), author, "rollout", "start", domain.Facts{Healthy: map[string]bool{"east": true}})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := service.Approve(t.Context(), reviewer, first.ChangeRequestID, "open wave"); err != nil || !result.Allowed {
		t.Fatal(result, err)
	}
	next, err := service.Propose(t.Context(), author, "rollout", "start", domain.Facts{Healthy: map[string]bool{"east": true}})
	if err != nil {
		t.Fatal(err)
	}
	if !next.Allowed || next.ChangeRequestID == first.ChangeRequestID || next.Status != controlplane.ChangePendingApprovals {
		t.Error("wave advance reused completed authority", next)
	}
	request, err := repository.GetChangeRequest(t.Context(), next.ChangeRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Reviews) != 0 || request.Changes[0].InstallationID != "rollout" || request.Changes[0].ReferencedResources["environment"].RID != "environment/east" || request.Changes[0].ReferencedResources["product"].RID != "product/ledger" {
		t.Error("fresh operation lost review or resource binding", request)
	}
	if result, err := service.Approve(t.Context(), reviewer, next.ChangeRequestID, "advance checked"); err != nil || !result.Allowed || result.Status != domain.StatusCompleted {
		t.Fatal("independent review could not complete wave", result, err)
	}
}
