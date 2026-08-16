package controlplane

import (
	"context"
	"testing"
)

// TestAnnotateSatisfiabilityFlagsTheUnapprovableRequirement is the read-path
// half of #13. A change carrying a requirement no grant can satisfy is
// unapprovable by anyone; the read path annotates each requirement so an
// operator sees which one, and why, instead of a change that silently will not
// move. A satisfiable requirement is marked satisfiable and carries no reason.
func TestAnnotateSatisfiabilityFlagsTheUnapprovableRequirement(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	if _, err := service.PutTeam(ctx, Team{Name: "ops", DisplayName: "Ops", AdminGroups: []string{"ops"}, MemberGroups: []string{"ops"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("prod"), Team: "ops", Roles: []string{"environment:operator"},
	}); err != nil {
		t.Fatal(err)
	}

	request := ChangeRequest{
		ID: "chg_test", Requirements: []ReviewerRequirement{
			// resource:operate is conveyed by environment:operator on this scope.
			{Kind: RequirementOperation, ResourceRID: environmentRID("prod"), Operation: OperationOperate},
			// Nothing conveys Apollo's published entity:approve-config-update here.
			{Kind: RequirementOperation, ResourceRID: environmentRID("prod"), Operation: "entity:approve-config-update"},
		},
	}

	annotated := annotateSatisfiability(ctx, repository, request)
	if len(annotated.Requirements) != 2 {
		t.Fatalf("requirement count changed: %d", len(annotated.Requirements))
	}
	satisfiable := annotated.Requirements[0]
	if satisfiable.Satisfiable == nil || !*satisfiable.Satisfiable || satisfiable.Unsatisfiable != "" {
		t.Fatalf("a satisfiable requirement was flagged: %+v", satisfiable)
	}
	unsatisfiable := annotated.Requirements[1]
	if unsatisfiable.Satisfiable == nil || *unsatisfiable.Satisfiable {
		t.Fatalf("the unapprovable requirement was not flagged: %+v", unsatisfiable)
	}
	if unsatisfiable.Unsatisfiable == "" {
		t.Fatal("the flagged requirement carries no reason for the operator")
	}
	// The stored request is untouched: annotation returns a copy.
	if request.Requirements[1].Satisfiable != nil {
		t.Fatal("annotation mutated the stored requirement instead of a copy")
	}
}
