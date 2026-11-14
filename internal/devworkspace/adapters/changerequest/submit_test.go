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
