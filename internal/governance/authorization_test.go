package governance

import (
	"strings"
	"testing"
)

// A grant made on the environment must reach the installations inside it,
// otherwise every installation would need its own restatement of the same
// grant. The global scopes are how a fleet-wide grant reaches anything at all.
func TestInstallationScopesReachItsEnvironmentAndTheGlobalScopes(t *testing.T) {
	t.Parallel()
	scopes := AuthorizationScopes("installation/dev-use2/checkout")
	for _, want := range []string{
		"installation/dev-use2/checkout",
		"environment/dev-use2",
		"global/installation",
		"global/environment",
	} {
		if !contains(scopes, want) {
			t.Errorf("scopes %v missing %s", scopes, want)
		}
	}
}

// A malformed installation RID must not be read as though it named an
// environment: "installation/x" has no environment part, and inventing one
// would widen the scope set beyond what was granted.
func TestShortInstallationRIDNamesNoEnvironment(t *testing.T) {
	t.Parallel()
	for _, scope := range AuthorizationScopes("installation/dev-use2") {
		if strings.HasPrefix(scope, "environment/") {
			t.Fatalf("a two-part installation RID must not yield %s", scope)
		}
	}
}

func TestFamilyScopesGainTheirGlobalScope(t *testing.T) {
	t.Parallel()
	for _, family := range []string{
		"artifact", "environment", "product", "module", "label",
		"release-channel", "vulnerability-suppression", "vulnerability-sla",
	} {
		scopes := AuthorizationScopes(family + "/thing")
		if !contains(scopes, "global/"+family) {
			t.Errorf("%s: scopes %v missing global/%s", family, scopes, family)
		}
	}
}

// An unrecognised family gets its own RID and nothing more. Widening here would
// be the dangerous direction: it grants access that was never given.
func TestUnknownFamilyGainsNoExtraScope(t *testing.T) {
	t.Parallel()
	scopes := AuthorizationScopes("something-else/thing")
	if len(scopes) != 1 || scopes[0] != "something-else/thing" {
		t.Fatalf("an unrecognised family must expand to itself alone, got %v", scopes)
	}
}

func TestRolesAllowOnlyWhatTheRoleConveys(t *testing.T) {
	t.Parallel()
	if !RolesAllow([]string{"environment:viewer"}, OperationReadResource) {
		t.Error("a viewer must be able to read")
	}
	if RolesAllow([]string{"environment:viewer"}, OperationOperate) {
		t.Error("a viewer must not be able to operate")
	}
	if RolesAllow([]string{"environment:viewer"}, OperationManagePermissions) {
		t.Error("a viewer must not be able to manage permissions")
	}
	if !RolesAllow([]string{"environment:viewer", "environment:administrator"}, OperationManagePermissions) {
		t.Error("holding several roles must convey the union of them")
	}
}

// A role nobody has heard of conveys nothing. Failing open here would turn a
// typo in a grant into unlimited authority.
func TestUnknownRoleConveysNothing(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{OperationReadResource, OperationOperate, OperationManagePermissions} {
		if RolesAllow([]string{"environment:emperor"}, operation) {
			t.Errorf("an unknown role must not convey %s", operation)
		}
	}
}

// The restricted contributor is deliberately read-only here: its contribution
// right is ownership-dependent and decided elsewhere, so the generic expansion
// must not hand it out.
func TestRestrictedContributorCannotProposeChanges(t *testing.T) {
	t.Parallel()
	if RolesAllow([]string{"release-channel:restricted-contributor"}, OperationProposeChange) {
		t.Fatal("restricted contribution is ownership-dependent and must not come from the role table")
	}
}

func TestApolloRoleResolvesToItsPlatformEquivalent(t *testing.T) {
	t.Parallel()
	if got := ResolveRole("apollo-environments:viewer"); got != "environment:viewer" {
		t.Fatalf("want environment:viewer, got %s", got)
	}
	if got := ResolveRole("environment:viewer"); got != "environment:viewer" {
		t.Fatalf("a Platform role must pass through unchanged, got %s", got)
	}
}

// An Apollo role with no alias conveys nothing at all. That is the fail-closed
// direction, and it is the point of the map being policy rather than evidence.
func TestUnaliasedApolloRoleConveysNothing(t *testing.T) {
	t.Parallel()
	for _, refused := range []string{"apollo-terminal:terminal-user", "apollo-artifact:deleter"} {
		if RolesAllow([]string{refused}, OperationReadResource) {
			t.Errorf("%s has no alias and must convey nothing", refused)
		}
	}
}

// A delegation is judged as the person delegated to. Judging it as the carrying
// account would let a service account's own authority stand in for a human's.
func TestDelegationIsJudgedAsThePersonDelegatedTo(t *testing.T) {
	t.Parallel()
	human := Principal{Username: "operator", Teams: []string{"sre"}}
	robot := Principal{Username: "ci", ServiceAccount: true, OnBehalfOf: &human}
	if got := EffectivePrincipal(robot); got.Username != "operator" {
		t.Fatalf("want operator, got %s", got.Username)
	}
	if got := EffectivePrincipal(human); got.Username != "operator" {
		t.Fatalf("an undelegated principal must be itself, got %s", got.Username)
	}
}

func TestCanonicalRoleScopeRejectsTheWrongResourceKind(t *testing.T) {
	t.Parallel()
	if err := ValidateCanonicalRoleScope("environment:viewer", "environment/dev-use2"); err != nil {
		t.Errorf("an environment role on an environment must be allowed: %v", err)
	}
	if err := ValidateCanonicalRoleScope("environment:viewer", "product/checkout"); err == nil {
		t.Error("an environment role on a product must be refused")
	}
	if err := ValidateCanonicalRoleScope("environment:viewer", "global/environment"); err != nil {
		t.Errorf("a global scope for the same family must be allowed: %v", err)
	}
}

// Entity roles are granted on installations. The two names are the same thing,
// and forgetting that refuses every entity grant ever made.
func TestEntityRolesAreGrantedOnInstallations(t *testing.T) {
	t.Parallel()
	if err := ValidateCanonicalRoleScope("entity:viewer", "installation/dev-use2/checkout"); err != nil {
		t.Fatalf("an entity role must be grantable on an installation: %v", err)
	}
}

// Permission management is root-only. Anywhere else it would be authority over
// a scope the grantor may not hold.
func TestPermissionRolesRequireTheRootResource(t *testing.T) {
	t.Parallel()
	if err := ValidateCanonicalRoleScope("permissions:administrator", "platform/root"); err != nil {
		t.Errorf("permissions roles must be grantable at root: %v", err)
	}
	if err := ValidateCanonicalRoleScope("permissions:administrator", "environment/dev-use2"); err == nil {
		t.Error("permissions roles must be refused anywhere but root")
	}
}

// A role Platform does not recognise is not this check's business. Refusing it
// here would reject grants that convey nothing anyway, and hide the real
// problem — that the role name is wrong.
func TestUnknownRoleScopeIsNotThisChecksBusiness(t *testing.T) {
	t.Parallel()
	if err := ValidateCanonicalRoleScope("something:else", "product/checkout"); err != nil {
		t.Fatalf("an unrecognised role must pass this check: %v", err)
	}
}

// The alias resolves before the scope is checked, or every Apollo grant would
// be refused for naming a role the table does not contain.
func TestApolloRoleScopeIsCheckedAfterResolution(t *testing.T) {
	t.Parallel()
	if err := ValidateCanonicalRoleScope("apollo-environments:viewer", "environment/dev-use2"); err != nil {
		t.Errorf("an aliased role must be checked as its Platform equivalent: %v", err)
	}
	if err := ValidateCanonicalRoleScope("apollo-environments:viewer", "product/checkout"); err == nil {
		t.Error("an aliased role must still be refused on the wrong resource kind")
	}
}
