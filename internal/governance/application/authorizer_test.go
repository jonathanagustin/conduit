package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

// recordingGrants is a grant store that answers for exactly one scope at a
// time and remembers what it was asked. Asking is half of what this layer
// does, so a fake that ignored the scope would let a widened lookup pass.
type recordingGrants struct {
	byScope map[string][]domain.ResourceGrant
	asked   []string
	err     error
}

func (grants *recordingGrants) ResourceGrantsInScope(
	_ context.Context, scope string,
) ([]domain.ResourceGrant, error) {
	grants.asked = append(grants.asked, scope)
	if grants.err != nil {
		return nil, grants.err
	}
	return grants.byScope[scope], nil
}

func TestEffectiveGrantsAsksEveryScopeTheResourceExpandsTo(t *testing.T) {
	t.Parallel()
	grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{
		"environment/dev": {{ID: "g1", Team: "ops", Roles: []string{"environment:operator"}}},
		"installation/dev/wordpress": {
			{ID: "g2", Team: "app", Operations: []string{domain.OperationOperate}},
		},
	}}
	effective, err := NewAuthorizer(grants).EffectiveGrants(context.Background(), "installation/dev/wordpress")
	if err != nil {
		t.Fatal(err)
	}
	// A grant on the environment reaches its installations without being
	// restated, which is the whole reason the expansion exists.
	if len(effective) != 2 {
		t.Fatalf("effective grants = %#v, want the installation and environment grants", effective)
	}
	want := domain.AuthorizationScopes("installation/dev/wordpress")
	if len(grants.asked) != len(want) {
		t.Fatalf("asked %v, want exactly the domain's scopes %v", grants.asked, want)
	}
	for index, scope := range want {
		if grants.asked[index] != scope {
			t.Fatalf("asked %v, want exactly the domain's scopes %v", grants.asked, want)
		}
	}
}

func TestAllowsRequiresTeamMembershipAndConveyedOperation(t *testing.T) {
	t.Parallel()
	grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{
		"environment/dev": {
			{ID: "g1", Team: "ops", Roles: []string{"environment:operator"}},
			{ID: "g2", Team: "outsiders", Operations: []string{domain.OperationManagePermissions}},
		},
	}}
	authorizer := NewAuthorizer(grants)
	member := domain.Principal{Username: "kai", Teams: []string{"ops"}}

	// A role conveys its operations without them being listed on the grant.
	if !authorizer.Allows(context.Background(), member, "environment/dev", domain.OperationOperate) {
		t.Fatal("a granted role did not convey the operation it is defined to convey")
	}
	// An operation that role does not convey is refused even to the same team.
	if authorizer.Allows(context.Background(), member, "environment/dev", domain.OperationManagePermissions) {
		t.Fatal("an operation outside the granted role was allowed")
	}
	// The grant that would convey it belongs to a team the principal is not in.
	stranger := domain.Principal{Username: "kai"}
	if authorizer.Allows(context.Background(), stranger, "environment/dev", domain.OperationOperate) {
		t.Fatal("a principal with no team was allowed through another team's grant")
	}
}

// A lookup that fails has not answered the question, and an unanswered
// authorization question is a denial rather than a pass.
func TestAllowsDeniesWhenTheGrantLookupFails(t *testing.T) {
	t.Parallel()
	grants := &recordingGrants{err: errors.New("store unavailable")}
	principal := domain.Principal{Username: "kai", Teams: []string{"ops"}}
	if NewAuthorizer(grants).Allows(context.Background(), principal, "environment/dev", domain.OperationReadResource) {
		t.Fatal("a failed grant lookup was treated as authorization")
	}
	if _, err := NewAuthorizer(grants).EffectiveGrants(context.Background(), "environment/dev"); err == nil {
		t.Fatal("EffectiveGrants swallowed the store failure")
	}
}

func TestRequirementSatisfactionByTeamRoleAndOperation(t *testing.T) {
	t.Parallel()
	grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{
		"product/wordpress": {
			{ID: "g1", Team: "release", Roles: []string{"product:editor"}},
		},
	}}
	authorizer := NewAuthorizer(grants)
	reviewer := domain.Principal{Username: "sam", Teams: []string{"release"}}

	team := domain.ReviewerRequirement{Kind: domain.RequirementTeam, Team: "release"}
	if !authorizer.RequirementSatisfied(context.Background(), team, reviewer) {
		t.Fatal("a member did not satisfy their own team's requirement")
	}
	if authorizer.RequirementSatisfied(context.Background(), team, domain.Principal{Username: "sam"}) {
		t.Fatal("a non-member satisfied a team requirement")
	}

	// A role requirement names the role literally: holding a role that conveys
	// the same operations is not holding the named role.
	role := domain.ReviewerRequirement{
		Kind: domain.RequirementRole, ResourceRID: "product/wordpress", Role: "product:editor",
	}
	if !authorizer.RequirementSatisfied(context.Background(), role, reviewer) {
		t.Fatal("the granted role did not satisfy the role requirement")
	}
	other := domain.ReviewerRequirement{
		Kind: domain.RequirementRole, ResourceRID: "product/wordpress", Role: "product:administrator",
	}
	if authorizer.RequirementSatisfied(context.Background(), other, reviewer) {
		t.Fatal("a role requirement was satisfied by a different role")
	}

	// An operation requirement is satisfied through the role that conveys it.
	operation := domain.ReviewerRequirement{
		Kind: domain.RequirementOperation, ResourceRID: "product/wordpress", Operation: domain.OperationOperate,
	}
	if !authorizer.RequirementSatisfied(context.Background(), operation, reviewer) {
		t.Fatal("an operation conveyed by the granted role did not satisfy the requirement")
	}
	unconveyed := domain.ReviewerRequirement{
		Kind: domain.RequirementOperation, ResourceRID: "product/wordpress", Operation: domain.OperationManagePermissions,
	}
	if authorizer.RequirementSatisfied(context.Background(), unconveyed, reviewer) {
		t.Fatal("an operation the granted role does not convey satisfied the requirement")
	}
}

func TestRequireAnyIsDisjunctiveAndPrincipalCanReviewIsNotAllOrNothing(t *testing.T) {
	t.Parallel()
	grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{
		"product/wordpress": {{ID: "g1", Team: "release", Roles: []string{"product:editor"}}},
	}}
	authorizer := NewAuthorizer(grants)
	reviewer := domain.Principal{Username: "sam", Teams: []string{"release"}}

	anyOf := domain.ReviewerRequirement{Kind: domain.RequirementAny, Alternatives: []domain.ReviewerRequirement{
		{Kind: domain.RequirementTeam, Team: "security"},
		{Kind: domain.RequirementRole, ResourceRID: "product/wordpress", Role: "product:editor"},
	}}
	if !authorizer.RequirementSatisfied(context.Background(), anyOf, reviewer) {
		t.Fatal("one satisfied alternative did not satisfy require_any")
	}
	noneOf := domain.ReviewerRequirement{Kind: domain.RequirementAny, Alternatives: []domain.ReviewerRequirement{
		{Kind: domain.RequirementTeam, Team: "security"},
		{Kind: domain.RequirementTeam, Team: "compliance"},
	}}
	if authorizer.RequirementSatisfied(context.Background(), noneOf, reviewer) {
		t.Fatal("require_any was satisfied with no satisfied alternative")
	}

	// Reviewing is per-requirement: satisfying one of several outstanding
	// requirements makes someone a reviewer. Whether the change is then fully
	// reviewed is domain.AllRequirementsSatisfied's question, not this one.
	outstanding := []domain.ReviewerRequirement{
		{Kind: domain.RequirementTeam, Team: "security"},
		{Kind: domain.RequirementTeam, Team: "release"},
	}
	if !authorizer.PrincipalCanReview(context.Background(), outstanding, reviewer) {
		t.Fatal("a reviewer who satisfies one requirement was refused")
	}
	if authorizer.PrincipalCanReview(context.Background(), outstanding, domain.Principal{Username: "sam"}) {
		t.Fatal("a principal satisfying no requirement was accepted as a reviewer")
	}
	if authorizer.PrincipalCanReview(context.Background(), nil, reviewer) {
		t.Fatal("a change demanding nothing accepted a reviewer")
	}
}

// TestRequirementSatisfiableAnswersCouldItBeMetAtAll pins the PH-6 defect: a
// require_operation naming an operation no grant conveys is unapprovable by
// everyone, and this is what surfaces it. It asks a different question from
// RequirementSatisfied -- not "does this principal meet it" but "could any
// grant meet it" -- so it takes no principal.
func TestRequirementSatisfiableAnswersCouldItBeMetAtAll(t *testing.T) {
	t.Parallel()
	// A grant conveys resource:operate on the environment, and nothing conveys
	// the Apollo-published entity:approve-config-update anywhere in scope.
	grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{
		"environment/prod": {{ID: "g1", Team: "ops", Operations: []string{domain.OperationOperate}}},
	}}
	authorizer := NewAuthorizer(grants)

	satisfiable := domain.ReviewerRequirement{
		Kind: domain.RequirementOperation, ResourceRID: "environment/prod", Operation: domain.OperationOperate,
	}
	if !authorizer.RequirementSatisfiable(context.Background(), satisfiable) {
		t.Fatal("a requirement a grant conveys was reported unsatisfiable")
	}

	unsatisfiable := domain.ReviewerRequirement{
		Kind: domain.RequirementOperation, ResourceRID: "environment/prod", Operation: "entity:approve-config-update",
	}
	if authorizer.RequirementSatisfiable(context.Background(), unsatisfiable) {
		t.Fatal("a requirement no grant conveys was reported satisfiable")
	}
}

// TestRequirementSatisfiableTracksTheGrantTableBothWays is the reason it is
// computed at read time and never stored: adding the grant makes a previously
// unsatisfiable requirement satisfiable, and removing it does the reverse. A
// value frozen at submission would be wrong in both directions.
func TestRequirementSatisfiableTracksTheGrantTableBothWays(t *testing.T) {
	t.Parallel()
	requirement := domain.ReviewerRequirement{
		Kind: domain.RequirementRole, ResourceRID: "environment/prod", Role: "environment:operator",
	}
	grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{}}
	authorizer := NewAuthorizer(grants)

	if authorizer.RequirementSatisfiable(context.Background(), requirement) {
		t.Fatal("satisfiable with no grant present")
	}
	grants.byScope["environment/prod"] = []domain.ResourceGrant{{ID: "g1", Team: "ops", Roles: []string{"environment:operator"}}}
	if !authorizer.RequirementSatisfiable(context.Background(), requirement) {
		t.Fatal("still unsatisfiable after the grant was added -- the answer was frozen")
	}
	grants.byScope["environment/prod"] = nil
	if authorizer.RequirementSatisfiable(context.Background(), requirement) {
		t.Fatal("still satisfiable after the grant was removed -- the answer was frozen")
	}
}

// TestRequirementSatisfiableIsDisjunctiveOverAlternativesAndConservativeOnError
// covers the two remaining shapes: a require_any is satisfiable if any branch
// is, and a grant-lookup failure must not be reported as unsatisfiable -- a
// diagnostic that says "never approvable" when the store hiccupped is worse
// than silence.
func TestRequirementSatisfiableIsDisjunctiveOverAlternativesAndConservativeOnError(t *testing.T) {
	t.Parallel()
	grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{
		"environment/prod": {{ID: "g1", Team: "ops", Operations: []string{domain.OperationOperate}}},
	}}
	authorizer := NewAuthorizer(grants)

	either := domain.ReviewerRequirement{Kind: domain.RequirementAny, Alternatives: []domain.ReviewerRequirement{
		{Kind: domain.RequirementOperation, ResourceRID: "environment/prod", Operation: "nobody:conveys-this"},
		{Kind: domain.RequirementOperation, ResourceRID: "environment/prod", Operation: domain.OperationOperate},
	}}
	if !authorizer.RequirementSatisfiable(context.Background(), either) {
		t.Fatal("require_any with one satisfiable branch reported unsatisfiable")
	}

	// A team requirement is met by membership, which no grant constrains.
	team := domain.ReviewerRequirement{Kind: domain.RequirementTeam, Team: "ops"}
	if !authorizer.RequirementSatisfiable(context.Background(), team) {
		t.Fatal("a team requirement was reported unsatisfiable")
	}

	failing := NewAuthorizer(&recordingGrants{err: errors.New("store down")})
	unknowable := domain.ReviewerRequirement{Kind: domain.RequirementOperation, ResourceRID: "environment/prod", Operation: domain.OperationOperate}
	if !failing.RequirementSatisfiable(context.Background(), unknowable) {
		t.Fatal("a grant-lookup failure was reported as unsatisfiable rather than unknowable")
	}
}
