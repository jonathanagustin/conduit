package changerequest

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func TestSubmitUsesChangeRequestAndAuthorCannotApprove(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, controlplane.Team{
		Name: "release-reviewers", DisplayName: "Release reviewers",
		AdminUsers: []string{"author", "reviewer"}, MemberUsers: []string{"author", "reviewer"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, controlplane.ApprovalPolicy{
		ID: "release-review", DisplayName: "Release review", Enabled: true,
		Expression: `type != "" ? require_team("release-reviewers", "release review") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	submitter := New(governance)
	receipt, err := submitter.Submit(ctx, domain.Caller{Username: "author", Teams: []string{"release-reviewers"}}, domain.Submission{
		WorkspaceID: "wsp_1", EnvironmentID: "dev", Revision: "rev-1", SourceDigest: "sha256:abc", Title: "Review workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != controlplane.ChangePendingApprovals || receipt.ChangeRequestID == "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if _, _, err := governance.Approve(ctx, receipt.ChangeRequestID, controlplane.Principal{Username: "author", Teams: []string{"release-reviewers"}}, "self"); !errors.Is(err, controlplane.ErrForbidden) {
		t.Fatalf("author approval: %v", err)
	}
	plans, err := repository.ListPlans(ctx, "dev")
	if err != nil || len(plans) != 0 {
		t.Fatalf("plans = %#v %v", plans, err)
	}
}

func TestSubmitRequiresHumanEvenWhenPolicyApprovesAndBindsDelegation(t *testing.T) {
	ctx := t.Context()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	if _, err := service.PutPolicy(ctx, controlplane.ApprovalPolicy{ID: "automatic", DisplayName: "Automatic", Enabled: true, Expression: `approve("automatic policy")`}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutTeam(ctx, controlplane.Team{Name: "release-reviewers", DisplayName: "Reviewers", AdminUsers: []string{"human"}, MemberUsers: []string{"human", "independent"}}); err != nil {
		t.Fatal(err)
	}
	caller := domain.Caller{Username: "workspace-owner", ServiceAccount: true, OnBehalfOf: &domain.Caller{Username: "human", Teams: []string{"release-reviewers"}}}
	draft := domain.Submission{WorkspaceID: "workspace-1", EnvironmentID: "env-a", Revision: "rev-1", SourceDigest: "sha256:source-a"}
	submitter := New(service)
	receipt, err := submitter.Submit(ctx, caller, draft)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != controlplane.ChangePendingApprovals {
		t.Fatalf("automatic policy escaped human gate: %#v", receipt)
	}
	request, err := repository.GetChangeRequest(ctx, receipt.ChangeRequestID)
	if err != nil {
		t.Fatal(err)
	}
	if request.RequirementsSatisfied || len(request.Reviews) != 0 || len(request.Requirements) == 0 {
		t.Fatalf("not held for a person: %#v", request)
	}
	if !request.Author.ServiceAccount || request.Author.OnBehalfOf == nil || request.Author.OnBehalfOf.Username != "human" {
		t.Fatalf("author context lost: %#v", request.Author)
	}
	if _, _, err := service.Approve(ctx, receipt.ChangeRequestID, controlplane.Principal{Username: "human", Teams: []string{"release-reviewers"}}, "self"); !errors.Is(err, controlplane.ErrForbidden) {
		t.Fatalf("represented author review: %v", err)
	}
	retry, err := submitter.Submit(ctx, caller, draft)
	if err != nil || retry.ChangeRequestID != receipt.ChangeRequestID {
		t.Fatalf("exact retry changed identity: %#v %v", retry, err)
	}
	ids := map[string]bool{receipt.ChangeRequestID: true}
	variants := []struct {
		caller domain.Caller
		draft  domain.Submission
	}{
		{domain.Caller{Username: "workspace-owner", ServiceAccount: true, OnBehalfOf: &domain.Caller{Username: "other-human"}}, draft},
		{caller, domain.Submission{WorkspaceID: draft.WorkspaceID, EnvironmentID: "env-b", Revision: draft.Revision, SourceDigest: draft.SourceDigest}},
		{caller, domain.Submission{WorkspaceID: draft.WorkspaceID, EnvironmentID: draft.EnvironmentID, Revision: draft.Revision, SourceDigest: "sha256:source-b"}},
	}
	for _, variant := range variants {
		receipt, err := submitter.Submit(ctx, variant.caller, variant.draft)
		if err != nil {
			t.Fatal(err)
		}
		if ids[receipt.ChangeRequestID] {
			t.Fatal("different author or scope reused a request")
		}
		ids[receipt.ChangeRequestID] = true
	}
	plans, err := repository.ListPlans(ctx, "")
	if err != nil || len(plans) != 0 {
		t.Fatalf("submission created execution: %#v %v", plans, err)
	}
}
