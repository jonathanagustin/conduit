package controlplane

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestApprovalWithoutRationaleExplainsTheRefusal(t *testing.T) {
	ctx := context.Background()
	service := NewGovernanceService(mustFileRepository(t))
	mustTeamAndPolicy(t, service)
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer@example.test", Groups: []string{"release-reviewers"}})
	_, request, err := service.ProposePlan(ctx, foundation(), author, "Foundation rollout", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Approve(ctx, request.ID, reviewer, "   ")
	if !errors.Is(err, ErrInvalidTransition) || !strings.Contains(err.Error(), "approval rationale is required") {
		t.Fatalf("empty rationale error = %v", err)
	}
	stored, err := service.repository.GetChangeRequest(ctx, request.ID)
	if err != nil || stored.Status != ChangePendingApprovals {
		t.Fatalf("request should remain pending: %#v %v", stored, err)
	}
	_, _, err = service.Approve(ctx, request.ID, reviewer, "verified rollout")
	if err != nil {
		t.Fatal(err)
	}
}

func TestRejectionWithoutRationaleExplainsTheRefusal(t *testing.T) {
	ctx := context.Background()
	service := NewGovernanceService(mustFileRepository(t))
	mustTeamAndPolicy(t, service)
	author, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	reviewer, _ := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer@example.test", Groups: []string{"release-reviewers"}})
	_, request, err := service.ProposePlan(ctx, foundation(), author, "Foundation rollout", "")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Reject(ctx, request.ID, reviewer, "")
	if !errors.Is(err, ErrInvalidTransition) || !strings.Contains(err.Error(), "rejection rationale is required") {
		t.Fatalf("empty rationale error = %v", err)
	}
}

func mustFileRepository(t *testing.T) *FileRepository {
	t.Helper()
	repository, err := NewFileRepository(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	return repository
}

func mustTeamAndPolicy(t *testing.T, service *GovernanceService) {
	t.Helper()
	ctx := context.Background()
	if _, err := service.PutTeam(ctx, Team{
		Name: "release-managers", DisplayName: "Release managers",
		AdminGroups: []string{"release-admins"}, MemberGroups: []string{"release-reviewers"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "release-review", DisplayName: "Release review", Enabled: true,
		Expression: `type == "CREATE_ENTITY" ? require_team("release-managers", "release review") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
}
