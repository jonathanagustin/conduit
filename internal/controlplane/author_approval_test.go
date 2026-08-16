package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

// reviewerRequiredService is the ordinary shape this defect lived in: one team
// owns a resource, policy demands a reviewer from that team, and the people who
// change the resource are in that team. The author being eligible is not an
// unusual configuration -- it is the common one, which is why "is this
// principal in the required team" was never enough on its own.
func reviewerRequiredService(t *testing.T, members ...string) *GovernanceService {
	t.Helper()
	ctx := context.Background()
	service := NewGovernanceService(NewMemoryRepository())
	if _, err := service.PutTeam(ctx, Team{
		Name: "release-managers", DisplayName: "Release managers",
		AdminUsers: members, MemberUsers: members,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "release-review", DisplayName: "Release review", Enabled: true,
		Expression: `require_team("release-managers", "release review")`,
	}); err != nil {
		t.Fatal(err)
	}
	return service
}

// TestAuthorCannotApproveTheirOwnChangeRequest is the defect.
//
// Before the author exclusion this passed every check: the author is in
// release-managers, so requirementSatisfied said yes, the requirement was
// marked satisfied, and the Change Request reached APPROVED with no second
// party involved at any point. The duplicate-review guard did not help -- it
// refuses the same subject reviewing twice, and this is the first review.
func TestAuthorCannotApproveTheirOwnChangeRequest(t *testing.T) {
	ctx := context.Background()
	service := reviewerRequiredService(t, "author", "reviewer")

	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author"})
	if err != nil {
		t.Fatal(err)
	}
	_, request, err := service.ProposePlan(ctx, foundation(), author, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if request.Status != ChangePendingApprovals {
		t.Fatalf("a reviewer requirement was not raised, so this proves nothing: %#v", request)
	}

	_, _, err = service.Approve(ctx, request.ID, author, "approving my own change")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("the author approved their own change request: err=%v", err)
	}
	// The reason is asserted, not just the status. A bare ErrForbidden sends an
	// author looking for a grant no grant would fix.
	if !strings.Contains(err.Error(), "author of a change request cannot approve it") {
		t.Errorf("refusal does not say why: %v", err)
	}

	stored, err := service.repository.GetChangeRequest(ctx, request.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ChangePendingApprovals || len(stored.Reviews) != 0 {
		t.Fatalf("the refused approval still left a mark: %#v", stored)
	}
	for _, requirement := range stored.Requirements {
		if len(requirement.SatisfiedBy) != 0 {
			t.Fatalf("requirement %s was satisfied by the refused approval: %#v", requirement.ID, requirement)
		}
	}

	// The exclusion has to be narrow enough to leave the feature working. A
	// second member of the same team, holding nothing the author did not hold,
	// still approves.
	reviewer, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "reviewer"})
	if err != nil {
		t.Fatal(err)
	}
	approved, plan, err := service.Approve(ctx, request.ID, reviewer, "independent review")
	if err != nil || approved.Status != ChangeApproved || plan.Status != PlanApproved {
		t.Fatalf("a genuine second party could not approve: request=%#v plan=%#v err=%v", approved, plan, err)
	}
}

// TestAuthorCannotApproveThroughDelegation covers the evasion a username
// comparison would have left open in both directions.
//
// The domain states the rule this rests on: "a service account acting for a
// person is judged as that person, which is why EffectivePrincipal exists
// rather than callers reading Username directly". A check on principal.Username
// alone reads the account's name on one side and the person's on the other, and
// concludes they are different parties.
func TestAuthorCannotApproveThroughDelegation(t *testing.T) {
	ctx := context.Background()

	for _, test := range []struct {
		name             string
		authorClaims     IdentityClaims
		approverClaims   IdentityClaims
		proposedOnBehalf bool
		approvedOnBehalf bool
	}{{
		name:             "the delegate approves what the account proposed for them",
		authorClaims:     IdentityClaims{Username: "delivery-bot", ServiceAccount: true, OnBehalfOf: "developer"},
		approverClaims:   IdentityClaims{Username: "developer"},
		proposedOnBehalf: true,
	}, {
		name:             "the account approves on behalf of the person who proposed",
		authorClaims:     IdentityClaims{Username: "developer"},
		approverClaims:   IdentityClaims{Username: "delivery-bot", ServiceAccount: true, OnBehalfOf: "developer"},
		approvedOnBehalf: true,
	}} {
		t.Run(test.name, func(t *testing.T) {
			service := reviewerRequiredService(t, "developer", "delivery-bot")
			if _, err := service.PutGrant(ctx, ResourceGrant{
				ResourceRID: "platform/root", Team: "release-managers", Operations: []string{OperationActOnBehalf},
			}); err != nil {
				t.Fatal(err)
			}
			author, err := service.ResolvePrincipal(ctx, test.authorClaims)
			if err != nil {
				t.Fatal(err)
			}
			if (author.OnBehalfOf != nil) != test.proposedOnBehalf {
				t.Fatalf("the author's delegation is not the shape this case tests: %#v", author)
			}
			_, request, err := service.ProposePlan(ctx, foundation(), author, "", "")
			if err != nil {
				t.Fatal(err)
			}
			if request.Status != ChangePendingApprovals {
				t.Fatalf("no reviewer requirement was raised: %#v", request)
			}
			approver, err := service.ResolvePrincipal(ctx, test.approverClaims)
			if err != nil {
				t.Fatal(err)
			}
			if (approver.OnBehalfOf != nil) != test.approvedOnBehalf {
				t.Fatalf("the approver's delegation is not the shape this case tests: %#v", approver)
			}
			// The usernames differ, which is exactly why this case needs a test:
			// a comparison of them would let this through.
			if approver.Username == author.Username {
				t.Fatalf("this case does not test delegation if the usernames match: %q", author.Username)
			}
			if _, _, err := service.Approve(ctx, request.ID, approver, "delegated self-approval"); !errors.Is(err, ErrForbidden) {
				t.Fatalf("delegation carried a self-approval past the author check: err=%v", err)
			}
		})
	}
}

// TestOptimizerRevalidationSurvivesTheAuthorExclusion pins the one exemption.
//
// The OPTIMIZER_REVALIDATION requirement is appended unconditionally by
// evaluatePlan and no policy can waive it, and what it asks for is machine
// evidence that is still fresh -- not a second party. Excluding the author from
// it would leave an operator permanently unable to present the revalidation
// receipt for their own plan, including in a deployment whose policy raised no
// reviewer requirement at all: wedged by a rule meant to require a reviewer
// nobody had asked for.
//
// So the author may satisfy that requirement and only that one. The assertion
// is both halves: the receipt is accepted, and the reviewer requirement it sits
// beside is still unsatisfied afterwards.
func TestOptimizerRevalidationSurvivesTheAuthorExclusion(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
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
	proposal, _, _, baseline := governedOptimizerFixture(t)
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	_, request, err := service.ProposeOptimizerPlan(ctx, proposal, author, "Optimizer shadow proposal", "review exact evidence")
	if err != nil {
		t.Fatal(err)
	}

	knownAt := baseline.Add(6 * time.Second)
	approval, acquisition := authoritativeOptimizerRevalidation(t, proposal, optimization.ProposalRevalidationApproval, knownAt, false)
	service.now = func() time.Time { return knownAt.Add(time.Second) }
	request, _, err = service.ApproveOptimizerProposal(ctx, request.ID, proposal, approval, acquisition, author, "the author's own revalidation receipt")
	if err != nil {
		t.Fatalf("the author could not present a revalidation receipt for their own plan: %v", err)
	}

	revalidated, reviewed := false, false
	for _, requirement := range request.Requirements {
		if requirement.Kind == "OPTIMIZER_REVALIDATION" {
			revalidated = len(requirement.SatisfiedBy) == 1 && requirement.SatisfiedBy[0] == author.Username
			continue
		}
		if len(requirement.SatisfiedBy) != 0 {
			reviewed = true
		}
	}
	if !revalidated {
		t.Errorf("the revalidation requirement was not satisfied by the author: %#v", request.Requirements)
	}
	// The exemption is for the receipt, not for the review. If this ever flips,
	// the author has satisfied a policy-raised reviewer requirement by routing
	// through the optimizer path.
	if reviewed {
		t.Errorf("the author satisfied a policy-raised requirement via the permit: %#v", request.Requirements)
	}
	if request.Status == ChangeApproved {
		t.Errorf("the change request approved with no independent reviewer: %#v", request)
	}
}
