package controlplane

import (
	"context"
	"errors"
	"testing"

	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

func TestLiveEventVisibilityMatchesNativeResourceReads(t *testing.T) {
	ctx := context.Background()
	repository, plans := planIndexFixture(t)
	service := NewGovernanceService(repository)
	if _, err := repository.PutResourceGrant(ctx, ResourceGrant{Revision: 1, ID: "dev-read", Team: "readers", ResourceRID: environmentRID(plans[0].Plan.Environment), Roles: []string{"environment:viewer"}}); err != nil {
		t.Fatal(err)
	}
	cr, err := repository.CreateChangeRequest(ctx, ChangeRequest{ID: "cr_events", Environment: "prod", Author: Principal{Username: "author"}, Requirements: []ReviewerRequirement{{Kind: RequirementTeam, Team: "reviewers"}}})
	if err != nil {
		t.Fatal(err)
	}
	governed, err := repository.CreatePlan(ctx, PlanRecord{ID: "pln_governed", ChangeRequestID: cr.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{Environment: plans[0].Plan.Environment}); err != nil {
		t.Fatal(err)
	}
	options := HandlerOptions{BootstrapAdministrators: []string{"administrator"}}
	for _, tc := range []struct {
		name               string
		principal          Principal
		ordinary, governed bool
	}{
		{"outsider", Principal{Username: "outsider"}, false, false},
		{"environment-reader", Principal{Username: "reader", Teams: []string{"readers"}}, true, false},
		{"request-author", Principal{Username: "author"}, false, true},
		{"request-reviewer", Principal{Username: "reviewer", Teams: []string{"reviewers"}}, false, true},
		{"carrier-administrator", Principal{Username: "administrator", OnBehalfOf: &Principal{Username: "outsider"}}, true, true},
		{"represented-bootstrap-is-not-carrier", Principal{Username: "service", ServiceAccount: true, OnBehalfOf: &Principal{Username: "administrator"}}, false, false},
		{"delegated-reader", Principal{Username: "service", ServiceAccount: true, OnBehalfOf: &Principal{Username: "reader", Teams: []string{"readers"}}}, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, subject := range []live.Subject{{Type: "plan", ID: plans[0].ID}, {Type: "environment", ID: plans[0].Plan.Environment}, {Type: "change-request", ID: cr.ID}, {Type: "plan", ID: governed.ID}} {
				want := tc.ordinary
				if subject.ID == cr.ID || subject.ID == governed.ID {
					want = tc.governed
				}
				visible, err := EventResourceVisible(ctx, repository, service, tc.principal, options, subject)
				if err != nil || visible != want {
					t.Fatalf("%+v visible=%v want=%v err=%v", subject, visible, want, err)
				}
			}
			for _, subject := range []live.Subject{{Type: "plan", ID: "missing"}, {Type: "change-request", ID: "missing"}, {Type: "environment", ID: "missing"}, {Type: "packaging-run", ID: "unknown"}} {
				visible, err := EventResourceVisible(ctx, repository, service, tc.principal, options, subject)
				if err != nil || visible {
					t.Fatalf("missing or unowned subject admitted: %+v %v", subject, err)
				}
			}
		})
	}
}

type unavailableEventRepository struct {
	Repository
	err error
}

func (r unavailableEventRepository) GetChangeRequest(context.Context, string) (ChangeRequest, error) {
	return ChangeRequest{}, r.err
}
func (r unavailableEventRepository) ListResourceGrants(context.Context, string) ([]ResourceGrant, error) {
	return nil, r.err
}
func TestLiveEventVisibilityPropagatesUnavailableNativeStores(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryRepository()
	plan, err := base.CreatePlan(ctx, PlanRecord{ID: "pln_events", ChangeRequestID: "cr_events"})
	if err != nil {
		t.Fatal(err)
	}
	unavailable := errors.New("store unavailable")
	repository := unavailableEventRepository{base, unavailable}
	service := NewGovernanceService(repository)
	principal := Principal{Username: "administrator"}
	options := HandlerOptions{BootstrapAdministrators: []string{"administrator"}}
	if visible, err := EventResourceVisible(ctx, repository, service, principal, options, live.Subject{Type: "plan", ID: plan.ID}); visible || !errors.Is(err, unavailable) {
		t.Fatalf("unavailable linked request visible=%v err=%v", visible, err)
	}
	if _, err := service.EventSubscriberAccess(ctx, principal, options); !errors.Is(err, unavailable) {
		t.Fatal("unavailable grant snapshot accepted")
	}
}

func TestEventSubscriberAccessUsesNativeReadGrantsAndCarrierAdministrator(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	for _, grant := range []ResourceGrant{
		{Revision: 1, ID: "read", Team: "readers", ResourceRID: "environment/dev", Roles: []string{"environment:viewer"}},
		{Revision: 1, ID: "delegate", Team: "carriers", ResourceRID: "platform/root", Operations: []string{OperationActOnBehalf}},
		{Revision: 1, ID: "other", Team: "strangers", ResourceRID: "environment/prod", Roles: []string{"environment:viewer"}},
	} {
		if _, err := repository.PutResourceGrant(ctx, grant); err != nil {
			t.Fatal(err)
		}
	}
	principal := Principal{Username: "service", Teams: []string{"carriers"}, OnBehalfOf: &Principal{Username: "reader", Teams: []string{"readers"}}}
	options := HandlerOptions{BootstrapAdministrators: []string{"administrator"}}
	access, err := service.EventSubscriberAccess(ctx, principal, options)
	if err != nil || !access.Allowed || access.Administrator || len(access.Grants) != 2 {
		t.Fatalf("access=%+v err=%v", access, err)
	}
	principal.OnBehalfOf = &Principal{Username: "administrator"}
	access, err = service.EventSubscriberAccess(ctx, principal, options)
	if err != nil || access.Allowed || access.Administrator || len(access.Grants) != 1 {
		t.Fatalf("represented bootstrap user inherited administrator: %+v %v", access, err)
	}
	principal.Username = "administrator"
	access, err = service.EventSubscriberAccess(ctx, principal, options)
	if err != nil || !access.Allowed || !access.Administrator {
		t.Fatalf("carrier administrator refused: %+v %v", access, err)
	}
	principal.OnBehalfOf = &principal
	if _, err := service.EventSubscriberAccess(ctx, principal, options); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cyclic delegation not bounded: %v", err)
	}
}
