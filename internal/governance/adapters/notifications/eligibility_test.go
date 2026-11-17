package notificationsadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/governance/application"
	"github.com/jonathanagustin/conduit/internal/governance/domain"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
)

type eligibilityAuthority struct{ err error }

func (source *eligibilityAuthority) ListTeams(context.Context) ([]domain.Team, error) {
	return []domain.Team{{Name: "root"}, {Name: "readers", MemberUsers: []string{"alice"}}}, source.err
}

func (source *eligibilityAuthority) ResourceGrantsInScope(_ context.Context, scope string) ([]domain.ResourceGrant, error) {
	if scope == "environment/dev" {
		return []domain.ResourceGrant{{Team: "readers", Roles: []string{"environment:viewer"}}}, source.err
	}
	return nil, source.err
}

func TestRecipientEligibilityPreservesOwnerKindAndCurrentAuthorityErrors(t *testing.T) {
	source := &eligibilityAuthority{}
	access, err := application.NewRecipientAccess(source, source, application.AdministratorPolicy{BootstrapAdministrators: []string{"root"}})
	if err != nil {
		t.Fatal(err)
	}
	eligibility, err := NewRecipientEligibility(access)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []struct {
		owner notifications.Owner
		want  bool
	}{
		{notifications.Owner{Kind: notifications.PrincipalUser, ID: "root"}, true},
		{notifications.Owner{Kind: notifications.PrincipalTeam, ID: "root"}, false},
		{notifications.Owner{Kind: notifications.PrincipalUser, ID: "alice"}, true},
		{notifications.Owner{Kind: notifications.PrincipalTeam, ID: "readers"}, true},
		{notifications.Owner{Kind: "SERVICE", ID: "root"}, false},
	} {
		if allowed, err := eligibility.Check(t.Context(), value.owner, "environment/dev"); err != nil || allowed != value.want {
			t.Errorf("%#v read = %v, %v; want %v", value.owner, allowed, err, value.want)
		}
	}
	source.err = errors.New("authority unavailable")
	if allowed, err := eligibility.Check(t.Context(), notifications.Owner{Kind: notifications.PrincipalUser, ID: "alice"}, "environment/dev"); allowed || !errors.Is(err, source.err) {
		t.Fatalf("authority failure was lost: %v, %v", allowed, err)
	}
	if _, err := NewRecipientEligibility(nil); err == nil {
		t.Fatal("nil governance access was accepted")
	}
}
