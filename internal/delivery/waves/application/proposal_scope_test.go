package application

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/waves/domain"
)

func TestGovernedRolloutProposalEvaluatesEveryEnvironmentPolicy(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "west-reviewers", DisplayName: "West reviewers", AdminUsers: []string{"root"}, MemberUsers: []string{"west-reviewer"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{ID: "west-only", DisplayName: "West review", Enabled: true, Revision: 1, Expression: `referencedResources.environment.rid == "environment/west" ? require_team("west-reviewers", "west target review") : approve("east target")`}); err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: repository, Governance: governance}
	if _, err := service.Create(t.Context(), "ada", "rollout", "ledger", "1", []domain.Wave{{Name: "first", Environments: []string{"east", "west", "east"}, Gate: domain.GateHealth}}); err != nil {
		t.Fatal(err)
	}
	proposal, err := service.Propose(t.Context(), controlplane.Principal{Username: "ada"}, "rollout", "start", domain.Facts{})
	if err != nil {
		t.Fatal(err)
	}
	request, err := repository.GetChangeRequest(t.Context(), proposal.ChangeRequestID)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, change := range request.Changes {
		targets[change.ReferencedResources["environment"].RID] = true
	}
	if len(request.Changes) != 2 || !targets["environment/east"] || !targets["environment/west"] {
		t.Errorf("distinct target changes omitted: %#v", request.Changes)
	}
	west := false
	for _, requirement := range request.Requirements {
		if requirement.Team == "west-reviewers" {
			west = true
		}
	}
	if !west {
		t.Error("second target policy did not participate", request.Requirements)
	}
}

func TestGovernedRolloutWaitsForEveryTargetReview(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	for _, target := range []string{"east", "west"} {
		if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: target + "-reviewers", DisplayName: target, AdminUsers: []string{"root"}, MemberUsers: []string{target + "-reviewer"}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{ID: "target-review", DisplayName: "Target review", Enabled: true, Revision: 1, Expression: `referencedResources.environment.rid == "environment/west" ? require_team("west-reviewers", "west review") : require_team("east-reviewers", "east review")`}); err != nil {
		t.Fatal(err)
	}
	service := &Service{Store: repository, Governance: governance}
	if _, err := service.Create(t.Context(), "ada", "rollout", "ledger", "1", []domain.Wave{{Name: "first", Environments: []string{"east", "west"}, Gate: domain.GateHealth}}); err != nil {
		t.Fatal(err)
	}
	proposal, err := service.Propose(t.Context(), controlplane.Principal{Username: "ada"}, "rollout", "start", domain.Facts{Healthy: map[string]bool{"east": true, "west": true}})
	if err != nil {
		t.Fatal(err)
	}
	partial, err := service.Approve(t.Context(), controlplane.Principal{Username: "east-reviewer", Teams: []string{"east-reviewers"}}, proposal.ChangeRequestID, "east checked")
	if err != nil {
		t.Fatal(err)
	}
	request, err := repository.GetChangeRequest(t.Context(), proposal.ChangeRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if request.RequirementsSatisfied || request.Status != controlplane.ChangePendingApprovals {
		t.Fatal("fixture did not retain native pending review", request)
	}
	if partial.Allowed || service.List()[0].Status != domain.StatusDraft {
		t.Errorf("partial approval advanced rollout: %#v", partial)
	}
	completed, err := service.Approve(t.Context(), controlplane.Principal{Username: "west-reviewer", Teams: []string{"west-reviewers"}}, proposal.ChangeRequestID, "west checked")
	if err != nil || !completed.Allowed || completed.Status != domain.StatusRunning {
		t.Fatal("remaining independent review did not advance rollout", completed, err)
	}
}
