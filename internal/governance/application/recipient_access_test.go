package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
)

type recipientTeams struct {
	values []domain.Team
	err    error
}

func (source *recipientTeams) ListTeams(context.Context) ([]domain.Team, error) {
	return source.values, source.err
}

func recipientAccessFixture(t *testing.T, policy AdministratorPolicy) (*RecipientAccess, *recipientTeams, *recordingGrants) {
	t.Helper()
	teams := &recipientTeams{values: []domain.Team{{Name: "readers", MemberUsers: []string{"alice"}, AdminUsers: []string{"owner"}}}}
	grants := &recordingGrants{byScope: map[string][]domain.ResourceGrant{
		"environment/dev": {{Team: "readers", Roles: []string{"environment:viewer"}}},
	}}
	access, err := NewRecipientAccess(teams, grants, policy)
	if err != nil {
		t.Fatal(err)
	}
	return access, teams, grants
}

func TestRecipientAccessRereadsNativeMembershipAndGrants(t *testing.T) {
	access, teams, grants := recipientAccessFixture(t, AdministratorPolicy{})
	for _, user := range []string{"alice", "owner"} {
		if allowed, err := access.UserCanRead(t.Context(), user, "installation/dev/payments"); err != nil || !allowed {
			t.Fatalf("current %s membership did not inherit Environment read: %v, %v", user, allowed, err)
		}
	}
	teams.values[0].MemberUsers = []string{"replacement"}
	if allowed, err := access.UserCanRead(t.Context(), "alice", "installation/dev/payments"); err != nil || allowed {
		t.Fatalf("removed user retained access: %v, %v", allowed, err)
	}
	if allowed, err := access.TeamCanRead(t.Context(), "readers", "installation/dev/payments"); err != nil || !allowed {
		t.Fatalf("named team's authority incorrectly depended on removed user: %v, %v", allowed, err)
	}
	grants.byScope["environment/dev"][0].Roles = nil
	grants.byScope["environment/dev"][0].Operations = []string{domain.OperationProposeChange}
	for _, check := range []func(context.Context, string, string) (bool, error){access.UserCanRead, access.TeamCanRead} {
		name := "readers"
		if allowed, err := check(t.Context(), name, "installation/dev/payments"); err != nil || allowed {
			t.Fatalf("non-read grant was accepted: %v, %v", allowed, err)
		}
	}
	if allowed, err := access.UserCanRead(t.Context(), "owner", "installation/dev/payments"); err != nil || allowed {
		t.Fatalf("current member retained revoked read: %v, %v", allowed, err)
	}
	grants.byScope["environment/dev"][0].Operations = []string{domain.OperationReadResource}
	if allowed, err := access.TeamCanRead(t.Context(), "readers", "installation/dev/payments"); err != nil || !allowed {
		t.Fatalf("explicit current read operation was refused: %v, %v", allowed, err)
	}
	teams.values = nil
	if allowed, err := access.TeamCanRead(t.Context(), "readers", "installation/dev/payments"); err != nil || allowed {
		t.Fatalf("a dangling grant kept a nonexistent team eligible: %v, %v", allowed, err)
	}
}

func TestRecipientBootstrapPolicyNeverConfusesUsersWithTeams(t *testing.T) {
	policy := AdministratorPolicy{BootstrapAdministrators: []string{" root "}, AllowLocalDeveloperFallback: true}
	access, teams, grants := recipientAccessFixture(t, policy)
	teams.values = []domain.Team{{Name: "root"}, {Name: "local-developer"}}
	grants.byScope = nil
	policy.BootstrapAdministrators[0] = "mallory"
	if allowed, err := access.UserCanRead(t.Context(), "root", "product/private"); err != nil || !allowed {
		t.Fatalf("configured bootstrap user was refused: %v, %v", allowed, err)
	}
	for _, name := range []string{"root", "local-developer"} {
		if allowed, err := access.TeamCanRead(t.Context(), name, "product/private"); err != nil || allowed {
			t.Fatalf("team %q acquired bootstrap username authority: %v, %v", name, allowed, err)
		}
	}
	if allowed, err := access.UserCanRead(t.Context(), "local-developer", "product/private"); err != nil || allowed {
		t.Fatalf("development fallback survived an explicit bootstrap list: %v, %v", allowed, err)
	}
	fallback, err := NewRecipientAccess(teams, grants, AdministratorPolicy{AllowLocalDeveloperFallback: true})
	if err != nil {
		t.Fatal(err)
	}
	if allowed, err := fallback.UserCanRead(t.Context(), "local-developer", "product/private"); err != nil || !allowed {
		t.Fatalf("explicit workstation fallback was lost: %v, %v", allowed, err)
	}
}

func TestRecipientGrantAdministratorUsesCanonicalRootScope(t *testing.T) {
	access, _, grants := recipientAccessFixture(t, AdministratorPolicy{})
	grants.byScope = map[string][]domain.ResourceGrant{
		"platform/root": {{Team: "readers", Operations: []string{domain.OperationManagePermissions}}},
	}
	if allowed, err := access.UserCanRead(t.Context(), "alice", "product/private"); err != nil || !allowed {
		t.Fatalf("current granted administrator could not read: %v, %v", allowed, err)
	}
	if allowed, err := access.TeamCanRead(t.Context(), "readers", "product/private"); err != nil || !allowed {
		t.Fatalf("current administrator team could not read: %v, %v", allowed, err)
	}
	grants.byScope = map[string][]domain.ResourceGrant{
		"environment/dev": {{Team: "readers", Operations: []string{domain.OperationManagePermissions}}},
	}
	if allowed, err := access.UserCanRead(t.Context(), "alice", "product/private"); err != nil || allowed {
		t.Fatalf("environment-local permission management became global read: %v, %v", allowed, err)
	}
}

func TestRecipientExternalGroupsRequireCurrentEvidenceOnlyWhenTheyCouldAuthorize(t *testing.T) {
	access, teams, grants := recipientAccessFixture(t, AdministratorPolicy{})
	teams.values = append(teams.values, domain.Team{Name: "external", MemberGroups: []string{"oidc-group"}})
	if allowed, err := access.UserCanRead(t.Context(), "outsider", "environment/dev"); err != nil || allowed {
		t.Fatalf("unrelated external group made a known denial uncertain: %v, %v", allowed, err)
	}
	grants.byScope["environment/dev"] = append(grants.byScope["environment/dev"], domain.ResourceGrant{Team: "external", Operations: []string{domain.OperationReadResource}})
	if allowed, err := access.UserCanRead(t.Context(), "outsider", "environment/dev"); allowed || !errors.Is(err, ErrRecipientGroupsUnavailable) {
		t.Fatalf("unverified current groups did not fail closed: %v, %v", allowed, err)
	}
	if allowed, err := access.UserCanRead(t.Context(), "alice", "environment/dev"); err != nil || !allowed {
		t.Fatalf("independent native allow was blocked by group uncertainty: %v, %v", allowed, err)
	}
	// Group-bound teams may be returned before directly authorized native teams.
	// Their presence must not make a definite allow depend on source ordering.
	teams.values[0], teams.values[1] = teams.values[1], teams.values[0]
	teams.values[0].MemberGroups = nil
	teams.values[0].AdminGroups = []string{"oidc-admin-group"}
	if allowed, err := access.UserCanRead(t.Context(), "alice", "environment/dev"); err != nil || !allowed {
		t.Fatalf("reordered external team hid a definite native allow: %v, %v", allowed, err)
	}
	if allowed, err := access.UserCanRead(t.Context(), "outsider", "environment/dev"); allowed || !errors.Is(err, ErrRecipientGroupsUnavailable) {
		t.Fatalf("external administrator groups were guessed: %v, %v", allowed, err)
	}
	if allowed, err := access.TeamCanRead(t.Context(), "external", "environment/dev"); err != nil || !allowed {
		t.Fatalf("named current team needed a fictional user-group claim: %v, %v", allowed, err)
	}
	grants.byScope = map[string][]domain.ResourceGrant{"platform/root": {{Team: "external", Roles: []string{"permissions:administrator"}}}}
	if allowed, err := access.UserCanRead(t.Context(), "outsider", "product/private"); allowed || !errors.Is(err, ErrRecipientGroupsUnavailable) {
		t.Fatalf("external administrator membership was guessed: %v, %v", allowed, err)
	}
}

func TestRecipientLookupFailuresCancellationAndInvalidIdentifiersNeverAllow(t *testing.T) {
	access, teams, grants := recipientAccessFixture(t, AdministratorPolicy{})
	failure := errors.New("authority unavailable")
	for _, check := range []func(context.Context, string, string) (bool, error){access.UserCanRead, access.TeamCanRead} {
		teams.err = failure
		if allowed, err := check(t.Context(), "readers", "environment/dev"); allowed || !errors.Is(err, failure) {
			t.Fatalf("team lookup failure did not propagate: %v, %v", allowed, err)
		}
		teams.err, grants.err = nil, failure
		if allowed, err := check(t.Context(), "readers", "environment/dev"); allowed || !errors.Is(err, failure) {
			t.Fatalf("grant lookup failure did not propagate: %v, %v", allowed, err)
		}
		grants.err = nil
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if allowed, err := check(ctx, "readers", "environment/dev"); allowed || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled check allowed a recipient: %v, %v", allowed, err)
		}
		for _, name := range []string{"", " alice", "alice "} {
			if allowed, _ := check(t.Context(), name, "environment/dev"); allowed {
				t.Fatalf("noncanonical owner %q was rebound", name)
			}
		}
	}
	if _, err := NewRecipientAccess(nil, grants, AdministratorPolicy{}); err == nil {
		t.Fatal("missing team source was accepted")
	}
	if _, err := NewRecipientAccess(teams, nil, AdministratorPolicy{}); err == nil {
		t.Fatal("missing grant source was accepted")
	}
}

func TestAuthorizerCheckPreservesLookupFailureAndDenial(t *testing.T) {
	failure := errors.New("grants unavailable")
	grants := &recordingGrants{err: failure}
	authorizer := NewAuthorizer(grants)
	if allowed, err := authorizer.Check(t.Context(), domain.Principal{Teams: []string{"readers"}}, "environment/dev", domain.OperationReadResource); allowed || !errors.Is(err, failure) {
		t.Fatalf("lookup failure was collapsed into a verdict: %v, %v", allowed, err)
	}
	grants.err = nil
	if allowed, err := authorizer.Check(t.Context(), domain.Principal{Teams: []string{"readers"}}, "environment/dev", domain.OperationReadResource); allowed || err != nil {
		t.Fatalf("definitive denial became a lookup error: %v, %v", allowed, err)
	}
}
