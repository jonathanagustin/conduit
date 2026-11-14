package application

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
)

func TestStartWaitsForADifferentApprover(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC)
	service := &Service{Store: repository, Now: func() time.Time { return now }}
	created, err := service.Create(ctx, "ada", "roll-1", "ledger", "1.2.0", []domain.Wave{
		{Name: "canary", Environments: []string{"east"}, Gate: domain.GateWindow},
	})
	if err != nil || created.Rollout.Status != domain.StatusDraft || created.AuditID == "" {
		t.Fatalf("create = %#v %v", created, err)
	}
	paused, err := service.Pause(ctx, "ada", "roll-1", true)
	if err != nil || paused.Allowed || paused.Reason != "rollout is not running" {
		t.Fatalf("pause before start = %#v %v", paused, err)
	}
	proposed, err := service.Propose(ctx, controlplane.Principal{Username: "ada"}, "roll-1", "start", domain.Facts{})
	if err != nil || proposed.ChangeRequestID == "" || proposed.Rollout.Status != domain.StatusDraft {
		t.Fatalf("propose = %#v %v", proposed, err)
	}
	self, err := service.Approve(ctx, controlplane.Principal{Username: "ada"}, proposed.ChangeRequestID, "own request")
	if err != nil || self.Allowed || self.Reason != "the author cannot approve their own change request" {
		t.Fatalf("self = %#v %v", self, err)
	}
	approved, err := service.Approve(ctx, controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}, proposed.ChangeRequestID, "window is the gate")
	if err != nil || !approved.Allowed || approved.Reason != "time window is closed" || approved.Rollout.Waves[0].Status != domain.WaveBlocked {
		t.Fatalf("approve = %#v %v", approved, err)
	}
	events, err := repository.ListAuthorizationAudit(ctx, "rollout/roll-1")
	if err != nil || len(events) < 3 {
		t.Fatalf("audit = %#v %v", events, err)
	}
}

func TestPauseRequiresConfirmation(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	service := &Service{Store: repository, Now: func() time.Time { return time.Date(2026, 11, 14, 15, 0, 0, 0, time.UTC) }}
	if _, err := service.Create(ctx, "ada", "roll-2", "ledger", "1.2.0", []domain.Wave{
		{Name: "canary", Environments: []string{"east"}, Gate: domain.GateHealth},
	}); err != nil {
		t.Fatal(err)
	}
	proposed, err := service.Propose(ctx, controlplane.Principal{Username: "ada"}, "roll-2", "start", domain.Facts{Healthy: map[string]bool{"east": true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Approve(ctx, controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}, proposed.ChangeRequestID, "healthy"); err != nil {
		t.Fatal(err)
	}
	denied, err := service.Pause(ctx, "ada", "roll-2", false)
	if err != nil || denied.Allowed || denied.Reason != "confirmation is required" {
		t.Fatalf("unconfirmed = %#v %v", denied, err)
	}
	paused, err := service.Pause(ctx, "ada", "roll-2", true)
	if err != nil || !paused.Allowed || paused.Status != domain.StatusPaused {
		t.Fatalf("pause = %#v %v", paused, err)
	}
}
