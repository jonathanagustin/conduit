package composition

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func TestNotificationEligibilityUsesConfiguredNativeAuthorityWithoutCapturedClaims(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	governance := controlplane.NewGovernanceService(repository)
	team, err := governance.PutTeam(t.Context(), controlplane.Team{
		Name: "readers", DisplayName: "Readers", AdminUsers: []string{"owner"}, MemberUsers: []string{"alice"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{
		ResourceRID: "environment/dev", Team: "readers", Roles: []string{"environment:viewer"},
	}); err != nil {
		t.Fatal(err)
	}
	eligibility, err := NewNotificationRecipientEligibility(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}})
	if err != nil {
		t.Fatal(err)
	}
	owner := notifications.Owner{Kind: notifications.PrincipalUser, ID: "alice"}
	if allowed, err := eligibility.Check(t.Context(), owner, "environment/dev"); err != nil || !allowed {
		t.Fatalf("current native subscriber was refused: %v, %v", allowed, err)
	}
	team.MemberUsers = []string{"replacement"}
	if _, err := governance.PutTeam(t.Context(), team); err != nil {
		t.Fatal(err)
	}
	if allowed, err := eligibility.Check(t.Context(), owner, "environment/dev"); err != nil || allowed {
		t.Fatalf("checker captured old membership: %v, %v", allowed, err)
	}
	if allowed, err := eligibility.Check(t.Context(), notifications.Owner{Kind: notifications.PrincipalUser, ID: "root"}, "product/private"); err != nil || !allowed {
		t.Fatalf("bootstrap configuration was not bound: %v, %v", allowed, err)
	}
	if _, err := NewNotificationRecipientEligibility(nil, controlplane.HandlerOptions{}); err == nil {
		t.Fatal("nil repository was accepted")
	}
}
