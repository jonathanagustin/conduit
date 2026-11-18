package application

import (
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
	"testing"
)

func TestPendingRolloutProposalCannotReplaceReviewedAction(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	service := &Service{Store: repository}
	if _, err := service.Create(t.Context(), "ada", "rollout", "ledger", "1", []domain.Wave{{Name: "first", Environments: []string{"east"}, Gate: domain.GateHealth}}); err != nil {
		t.Fatal(err)
	}
	original, err := service.Propose(t.Context(), controlplane.Principal{Username: "ada"}, "rollout", "start", domain.Facts{Healthy: map[string]bool{"east": true}})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := service.Propose(t.Context(), controlplane.Principal{Username: "mallory"}, "rollout", "rollback", domain.Facts{})
	if err == nil && replacement.Allowed {
		t.Error("second proposal replaced pending action", replacement)
	}
	request, err := repository.GetChangeRequest(t.Context(), original.ChangeRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if request.Author.Username != "ada" || request.Changes[0].Request["action"] != "start" {
		t.Fatal("original native proposal changed", request)
	}
	approved, err := service.Approve(t.Context(), controlplane.Principal{Username: "bea", Teams: []string{"release-reviewers"}}, original.ChangeRequestID, "reviewed start")
	if err != nil || !approved.Allowed || approved.Status != domain.StatusRunning {
		t.Fatal("approval did not apply original start", approved, err)
	}
}
