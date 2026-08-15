package domain

import (
	"github.com/jonathanagustin/conduit/internal/apollo"
	"sort"
	"strings"
	"testing"
)

// The alias map is policy, so it is hand-authored. That makes it the one part
// of the Apollo surface a typo can reach: a misspelled key resolves nothing and
// a misspelled value expands to no operations, and both fail silently in the
// safe-looking direction. These gates validate the policy against the evidence.

func TestApolloRoleAliasesAreValid(t *testing.T) {
	for apolloRoleID, platformRole := range ApolloRolePlatformAliases {
		if _, known := apollo.Roles[apolloRoleID]; !known {
			t.Errorf("alias key %q is not a role the captured tenant serves", apolloRoleID)
		}
		if _, known := CanonicalRoleOperations[platformRole]; !known {
			t.Errorf("alias %q resolves to %q, which is not a Platform canonical role",
				apolloRoleID, platformRole)
		}
		if !strings.HasPrefix(apolloRoleID, "apollo-") {
			t.Errorf("alias key %q is not an Apollo identifier", apolloRoleID)
		}
		if strings.HasPrefix(platformRole, "apollo-") {
			t.Errorf("alias %q resolves to another Apollo identifier %q rather than a Platform role",
				apolloRoleID, platformRole)
		}
	}
}

// The leaf name must match on both sides. Aliasing apollo-environments:viewer
// onto environment:administrator would be a silent privilege escalation that no
// other check in this package would notice.
func TestApolloRoleAliasesPreserveTheLeafName(t *testing.T) {
	for apolloRoleID, platformRole := range ApolloRolePlatformAliases {
		_, apolloLeaf, _ := strings.Cut(apolloRoleID, ":")
		_, platformLeaf, _ := strings.Cut(platformRole, ":")
		if apolloLeaf != platformLeaf {
			t.Errorf("alias %q resolves to %q: leaf %q does not match %q",
				apolloRoleID, platformRole, apolloLeaf, platformLeaf)
		}
	}
}

// An Apollo role Platform has not explicitly aliased must convey nothing. This
// is the fail-closed property the whole split depends on, so it is asserted
// over the entire captured role set rather than sampled.
func TestUnaliasedApolloRolesConveyNoPlatformAuthority(t *testing.T) {
	everyOperation := []string{
		OperationActOnBehalf, OperationProposeChange, OperationReadResource,
		OperationOperate, OperationImportBundle, OperationBulkChange,
		OperationManagePermissions, OperationApplyLabel, OperationCreateArtifact,
		OperationCreateProduct, OperationCreateRelease,
	}
	unaliased := 0
	for apolloRoleID := range apollo.Roles {
		if _, aliased := ApolloRolePlatformAliases[apolloRoleID]; aliased {
			continue
		}
		unaliased++
		for _, operation := range everyOperation {
			if RolesAllow([]string{apolloRoleID}, operation) {
				t.Errorf("unaliased Apollo role %q granted %q", apolloRoleID, operation)
			}
		}
	}
	if unaliased == 0 {
		t.Fatal("no unaliased Apollo roles remain, so this gate proves nothing")
	}
	t.Logf("%d unaliased Apollo roles convey no Platform authority", unaliased)
}

// Terminal access is refused deliberately (H30). If someone later adds an
// alias for it, that is a boundary decision and must not pass as a routine
// vocabulary extension.
func TestTerminalAccessApolloRolesRemainUnaliased(t *testing.T) {
	for _, refused := range []string{
		"apollo-environments:terminal-user",
		"apollo-environments:terminal-access-approver",
	} {
		if _, known := apollo.Roles[refused]; !known {
			t.Fatalf("%q is no longer in the captured graph; this gate needs rebasing", refused)
		}
		if platformRole, aliased := ApolloRolePlatformAliases[refused]; aliased {
			t.Errorf("%q is aliased onto %q, but debug terminal access is the H30 boundary Platform refuses",
				refused, platformRole)
		}
	}
}

// Apollo roles grant operations owned by other products - containers:push,
// ccs:create-namespace, connectivityconfig:*. Platform operates none of those
// systems, so no alias may ever expand into one. If a Platform operation
// constant is ever spelled like a borrowed identifier, this catches it before
// a grant starts implying registry or networking authority.
func TestNoAliasConveysBorrowedProductAuthority(t *testing.T) {
	borrowedPrefixes := []string{"containers:", "ccs:", "connectivityconfig:", "repositories:"}
	var offenders []string
	for apolloRoleID, platformRole := range ApolloRolePlatformAliases {
		for _, operation := range CanonicalRoleOperations[platformRole] {
			for _, prefix := range borrowedPrefixes {
				if strings.HasPrefix(operation, prefix) {
					offenders = append(offenders,
						apolloRoleID+" -> "+platformRole+" -> "+operation)
				}
			}
		}
	}
	sort.Strings(offenders)
	for _, offender := range offenders {
		t.Errorf("alias conveys authority over a system Platform does not operate: %s", offender)
	}
}
