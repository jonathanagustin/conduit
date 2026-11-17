package composition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func seedNotificationEnvironment(t *testing.T, repository controlplane.Repository, environment string) {
	t.Helper()
	state := reviewedState("alias-baseline-" + environment)
	state.Environment.ID = environment
	if _, err := controlplane.NewLifecycle(repository).CreatePlan(t.Context(), state); err != nil {
		t.Fatal(err)
	}
}

type unavailableNotificationInventory struct {
	controlplane.Repository
	err   error
	reads int
}

func (source *unavailableNotificationInventory) ListPlans(context.Context, string) ([]controlplane.PlanRecord, error) {
	source.reads++
	return nil, source.err
}

func TestNotificationResourceAliasInventoryFailuresStayClosedAndNativeScopesStayNative(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	nativeNotificationViewer(t, repository, "alias-dev", "alice")
	failure := errors.New("environment inventory unavailable")
	source := &unavailableNotificationInventory{Repository: repository, err: failure}
	eligibility, err := NewNotificationRecipientEligibility(source, controlplane.HandlerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owner := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	if allowed, err := eligibility.Check(t.Context(), owner, "environment/alias-dev"); err != nil || !allowed || source.reads != 0 {
		t.Fatalf("native policy acquired an inventory dependency: %v %v reads=%d", allowed, err, source.reads)
	}
	resource := apolloapplication.EnvironmentRID("alias-dev")
	if allowed, err := eligibility.Check(t.Context(), owner, resource); !errors.Is(err, failure) || allowed {
		t.Fatalf("inventory error became permission or an empty audience: %v %v", allowed, err)
	}
	handler, err := NewHub(source, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	}, Options{NotificationStore: newNotificationStore()})
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, handler, http.MethodGet, "/v1/notifications/events?resourceRid="+resource, "root", ""); response.Code != http.StatusForbidden {
		t.Fatalf("HTTP lookup outage failed open: %d %s", response.Code, response.Body)
	}
	assertNotificationAliasCapabilities(t, handler, "root", resource, false)
}

func TestNotificationResourceAliasesRequireOneExactValidInventoryIdentity(t *testing.T) {
	for _, scenario := range []string{"unknown", "empty", "non-DNS", "slash", "opaque", "ambiguous", "padded RID", "forged RID"} {
		t.Run(scenario, func(t *testing.T) {
			repository := controlplane.NewMemoryRepository()
			resource := apolloapplication.EnvironmentRID("z-dev")
			var reportIDs []string
			switch scenario {
			case "unknown":
			case "empty":
				resource, reportIDs = apolloapplication.EnvironmentRID(""), []string{""}
			case "non-DNS":
				resource, reportIDs = apolloapplication.EnvironmentRID("BAD_env"), []string{"BAD_env"}
			case "slash":
				resource, reportIDs = apolloapplication.EnvironmentRID("dev/extra"), []string{"dev/extra"}
			case "opaque":
				reportIDs = []string{resource}
			case "ambiguous":
				// EnvironmentRID preserves an already-opaque ID. Together with
				// z-dev this is a real forward collision; its order is significant
				// because the opaque spelling sorts before z-dev in the catalog.
				seedNotificationEnvironment(t, repository, "z-dev")
				reportIDs = []string{resource}
			case "padded RID":
				seedNotificationEnvironment(t, repository, "z-dev")
				resource += " "
			case "forged RID":
				seedNotificationEnvironment(t, repository, "z-dev")
				resource += "/environment/z-dev"
			}
			for _, id := range reportIDs {
				if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: id}); err != nil {
					t.Fatal(err)
				}
			}
			eligibility, err := NewNotificationRecipientEligibility(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}})
			if err != nil {
				t.Fatal(err)
			}
			if allowed, err := eligibility.Check(t.Context(), domain.Owner{Kind: domain.PrincipalUser, ID: "root"}, resource); err != nil || allowed {
				t.Fatalf("invalid alias %q acquired authority: %v %v", resource, allowed, err)
			}
		})
	}
}

func TestNotificationResourceAliasLookupObservesNewDurableInventory(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	nativeNotificationViewer(t, repository, "alias-dev", "alice")
	eligibility, err := NewNotificationRecipientEligibility(repository, controlplane.HandlerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	owner := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	resource := apolloapplication.EnvironmentRID("alias-dev")
	if allowed, err := eligibility.Check(t.Context(), owner, resource); err != nil || allowed {
		t.Fatalf("unknown alias acquired the native grant: %v %v", allowed, err)
	}
	seedNotificationEnvironment(t, repository, "alias-dev")
	if allowed, err := eligibility.Check(t.Context(), owner, resource); err != nil || !allowed {
		t.Fatalf("new durable identity was hidden by stale resolver state: %v %v", allowed, err)
	}
}

func nativeNotificationViewer(t *testing.T, repository controlplane.Repository, environment, username string) controlplane.ResourceGrant {
	t.Helper()
	service := controlplane.NewGovernanceService(repository)
	if _, err := service.PutTeam(t.Context(), controlplane.Team{
		Name: username + "-readers", DisplayName: "Readers", AdminUsers: []string{"team-admin"}, MemberUsers: []string{username},
	}); err != nil {
		t.Fatal(err)
	}
	grant, err := service.PutGrant(t.Context(), controlplane.ResourceGrant{
		ResourceRID: "environment/" + environment, Team: username + "-readers", Roles: []string{"environment:viewer"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func proposeAliasPlan(t *testing.T, handler http.Handler, environment, generation string) {
	t.Helper()
	state := reviewedState(generation)
	state.Environment.ID = environment
	body, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, http.MethodPost, "/v1/plans", "root", string(body))
	if response.Code != http.StatusCreated {
		t.Fatalf("propose plan = %d %s", response.Code, response.Body)
	}
}

func assertNotificationAliasCapabilities(t *testing.T, handler http.Handler, actor, resource string, readable bool) {
	t.Helper()
	response := request(t, handler, http.MethodGet, "/v1/session?resourceRid="+resource, actor, "")
	var session controlplane.SessionProjection
	if err := json.Unmarshal(response.Body.Bytes(), &session); err != nil || response.Code != http.StatusOK || len(session.Resources) != 1 || session.Resources[0].ResourceRID != resource {
		t.Fatalf("session did not retain the queried alias: %d %s %v", response.Code, response.Body, err)
	}
	if got := slices.Contains(session.Resources[0].Operations, controlplane.OperationReadResource); got != readable {
		t.Fatalf("session resource:read for %s = %v, want %v: %#v", resource, got, readable, session.Resources[0])
	}
	if !readable && len(session.Resources[0].Operations) != 0 {
		t.Fatalf("ineligible alias retained capabilities: %#v", session.Resources[0])
	}
}

// The role is native, the stored notification key is opaque, and the owner is
// an ordinary viewer. Neither an administrator nor a grant on the opaque RID
// may hide a missing identifier join.
func TestNotificationEnvironmentAliasUsesNativeViewerAuthority(t *testing.T) {
	const environment = "alias-dev"
	resource := apolloapplication.EnvironmentRID(environment)
	repository := controlplane.NewMemoryRepository()
	seedNotificationEnvironment(t, repository, environment)
	nativeNotificationViewer(t, repository, environment, "alice")
	governance := controlplane.NewGovernanceService(repository)
	if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{
		ID: "review", DisplayName: "Independent review", Enabled: true, Expression: `require_team("alice-readers", "review")`,
	}); err != nil {
		t.Fatal(err)
	}
	store := newNotificationStore()
	if err := store.RecordEvent(t.Context(), domain.Event{
		ID: "existing-event", ResourceRID: resource, Environment: environment, Type: domain.EventPlanFailures,
		Summary: "Previously recorded failure", OccurredAt: time.Now().UTC(),
	}, nil); err != nil {
		t.Fatal(err)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	}, Options{NotificationStore: store})
	if err != nil {
		t.Fatal(err)
	}
	assertNotificationAliasCapabilities(t, handler, "alice", resource, true)
	assertNotificationAliasCapabilities(t, handler, "alice", "environment/"+environment, true)
	assertNotificationAliasCapabilities(t, handler, "root", apolloapplication.EnvironmentRID("unknown"), false)
	query := "/v1/notifications/preferences?resourceRid=" + resource + "&channel=IN_APP"
	if response := request(t, handler, http.MethodGet, query, "alice", ""); response.Code != http.StatusNotFound {
		t.Fatalf("native viewer cannot read missing opaque preference: %d %s", response.Code, response.Body)
	}
	response := request(t, handler, http.MethodPut, "/v1/notifications/preferences", "alice",
		fmt.Sprintf(`{"resourceRid":%q,"channel":"IN_APP","types":["CHANGE_REQUEST_PENDING_APPROVAL"]}`, resource))
	if response.Code != http.StatusOK {
		t.Fatalf("native viewer cannot subscribe: %d %s", response.Code, response.Body)
	}
	var preference domain.Preference
	if err := json.Unmarshal(response.Body.Bytes(), &preference); err != nil {
		t.Fatal(err)
	}
	if preference.ResourceRID != resource || preference.ID != notifications.PreferenceID(resource, domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, domain.ChannelInApp) {
		t.Fatalf("authorization rewrote the stored preference identity: %#v", preference)
	}
	eventsPath := "/v1/notifications/events?resourceRid=" + resource
	response = request(t, handler, http.MethodGet, eventsPath, "alice", "")
	var events struct {
		Events []domain.Event `json:"events"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &events); err != nil || response.Code != http.StatusOK || len(events.Events) != 1 || events.Events[0].ID != "existing-event" || events.Events[0].ResourceRID != resource {
		t.Fatalf("existing opaque event was not read unchanged: %d %s %v", response.Code, response.Body, err)
	}
	proposeAliasPlan(t, handler, environment, "alias-pending")
	if len(store.entries) != 1 || store.entries[0].Recipient.Owner.ID != "alice" || store.entries[0].PreferenceID != preference.ID || store.entries[0].Event.ResourceRID != resource {
		t.Fatalf("ordinary native viewer did not receive the new opaque event: %#v", store.entries)
	}
	before := store.entries[0].Event.ID
	team, err := repository.GetTeam(t.Context(), "alice-readers")
	if err != nil {
		t.Fatal(err)
	}
	team.MemberUsers = []string{"another-viewer"}
	team, err = governance.PutTeam(t.Context(), team)
	if err != nil {
		t.Fatal(err)
	}
	if response := request(t, handler, http.MethodGet, eventsPath, "alice", ""); response.Code != http.StatusForbidden {
		t.Fatalf("revoked native viewer still reads alias: %d %s", response.Code, response.Body)
	}
	assertNotificationAliasCapabilities(t, handler, "alice", resource, false)
	proposeAliasPlan(t, handler, environment, "alias-after-revocation")
	if len(store.entries) != 1 || store.entries[0].Event.ID != before || len(store.events) != 3 {
		t.Fatalf("revocation lost history or allowed new fan-out: entries=%#v events=%#v", store.entries, store.events)
	}
	team.MemberUsers = []string{"alice"}
	if _, err := governance.PutTeam(t.Context(), team); err != nil {
		t.Fatal(err)
	}
	proposeAliasPlan(t, handler, environment, "alias-after-revocation")
	if len(store.entries) != 1 || len(store.events) != 3 {
		t.Fatalf("restoring access retroactively changed the audience: %#v", store.entries)
	}
}

func TestNotificationEnvironmentAliasDoesNotTrustEventMetadataOrUnknownIDs(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	for _, environment := range []string{"alias-dev", "alias-prod"} {
		seedNotificationEnvironment(t, repository, environment)
	}
	nativeNotificationViewer(t, repository, "alias-dev", "alice")
	eligibility, err := NewNotificationRecipientEligibility(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"root"}})
	if err != nil {
		t.Fatal(err)
	}
	store := newNotificationStore()
	for _, environment := range []string{"alias-dev", "alias-prod", "unknown"} {
		resource := apolloapplication.EnvironmentRID(environment)
		if err := store.SavePreference(t.Context(), domain.Preference{
			ID: "legacy-" + environment, Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, ResourceRID: resource,
			Channel: domain.ChannelInApp, AllFutureTypes: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	emitter, err := notifications.NewEmitter(store, eligibility)
	if err != nil {
		t.Fatal(err)
	}
	for _, environment := range []string{"alias-prod", "unknown", "alias-dev"} {
		if err := emitter.Emit(t.Context(), domain.Event{
			ID: "event-" + environment, Type: domain.EventPlanFailures,
			ResourceRID: apolloapplication.EnvironmentRID(environment), Environment: "alias-dev",
			Summary: "Untrusted environment metadata is not an authority source", OccurredAt: time.Now().UTC(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if len(store.entries) != 1 || store.entries[0].Event.ID != "event-alias-dev" {
		t.Fatalf("an alias or forged metadata conferred unrelated authority: %#v", store.entries)
	}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	}, Options{NotificationStore: store})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"alice", "root"} {
		resource := apolloapplication.EnvironmentRID("unknown")
		if response := request(t, handler, http.MethodGet, "/v1/notifications/events?resourceRid="+resource, actor, ""); response.Code != http.StatusForbidden {
			t.Fatalf("unknown opaque RID was authorized for %s: %d %s", actor, response.Code, response.Body)
		}
		assertNotificationAliasCapabilities(t, handler, actor, resource, false)
	}
	if allowed, err := eligibility.Check(t.Context(), domain.Owner{Kind: domain.PrincipalUser, ID: "root"}, apolloapplication.EnvironmentRID("unknown")); err != nil || allowed {
		t.Fatalf("unknown alias bypassed inventory for bootstrap recipient: %v %v", allowed, err)
	}
}
