package composition

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	notificationhttp "github.com/jonathanagustin/conduit/internal/notifications/adapters/httpapi"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

const environmentRID = "ri.apollo.main.environment.d41d8cd98f00"

// notificationStore is the notification component without a database, keyed on
// the same tuple the PostgreSQL store's UNIQUE constraint uses.
type notificationStore struct {
	events      []domain.Event
	entries     []domain.InboxEntry
	preferences map[string]domain.Preference
}

var _ ports.Store = (*notificationStore)(nil)

func newNotificationStore() *notificationStore {
	return &notificationStore{preferences: map[string]domain.Preference{}}
}

func (store *notificationStore) RecordEvent(
	_ context.Context, event domain.Event, entries []domain.InboxEntry,
) error {
	store.events = append(store.events, event)
	store.entries = append(store.entries, entries...)
	return nil
}

func (store *notificationStore) PreferencesFor(
	_ context.Context, resourceRID string,
) ([]domain.Preference, error) {
	found := []domain.Preference{}
	for _, preference := range store.preferences {
		if preference.ResourceRID == resourceRID {
			found = append(found, preference)
		}
	}
	return found, nil
}

func (store *notificationStore) SavePreference(_ context.Context, preference domain.Preference) error {
	if err := preference.Validate(); err != nil {
		return err
	}
	store.preferences[strings.Join([]string{
		string(preference.Owner.Kind), preference.Owner.ID,
		preference.ResourceRID, string(preference.Channel),
	}, "\x00")] = preference
	return nil
}

func (store *notificationStore) Inbox(
	_ context.Context, recipient domain.Recipient, limit int,
) ([]domain.InboxEntry, error) {
	found := []domain.InboxEntry{}
	for _, entry := range store.entries {
		if entry.Recipient == recipient && len(found) < limit {
			found = append(found, entry)
		}
	}
	return found, nil
}

func (store *notificationStore) EventsForResource(
	_ context.Context, resourceRID string, limit int,
) ([]domain.Event, error) {
	found := []domain.Event{}
	for _, event := range store.events {
		if event.ResourceRID == resourceRID && len(found) < limit {
			found = append(found, event)
		}
	}
	return found, nil
}

// hub assembles the real Hub -- the real repository, the real Governance
// service, the real identity path -- over an in-memory notification store, so
// what is under test is the wiring rather than a rehearsal of it.
func hub(t *testing.T) (http.Handler, *notificationStore) {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	ctx := t.Context()
	for _, team := range []controlplane.Team{
		{Name: "platform", DisplayName: "Platform", AdminUsers: []string{"alice"}, MemberUsers: []string{"alice"}},
		{Name: "security", DisplayName: "Security", AdminUsers: []string{"carol"}, MemberUsers: []string{"carol"}},
		{Name: "readers", DisplayName: "Readers", AdminUsers: []string{"bob"}, MemberUsers: []string{"bob"}},
	} {
		if _, err := service.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	// alice and bob both read the Environment; carol does not, which is what
	// makes the resource half of the authorization observable.
	for _, team := range []string{"platform", "readers"} {
		if _, err := service.PutGrant(ctx, controlplane.ResourceGrant{
			ResourceRID: environmentRID, Team: team,
			Operations: []string{governance.OperationReadResource},
		}); err != nil {
			t.Fatal(err)
		}
	}
	store := newNotificationStore()
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		// Named so that the "local-developer" administrator shortcut is off and
		// every decision below comes from a grant.
		BootstrapAdministrators: []string{"root"},
	}, Options{NotificationStore: store})
	if err != nil {
		t.Fatal(err)
	}
	return handler, store
}

func request(
	t *testing.T, handler http.Handler, method, target, subject, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	httpRequest := httptest.NewRequest(method, target, strings.NewReader(body))
	httpRequest.Header.Set("X-Platform-Subject", subject)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httpRequest)
	return response
}

func seed(store *notificationStore, owner domain.Owner, channel domain.Channel, id string) {
	event := domain.Event{
		ID: id, Type: domain.EventPlanFailures, ResourceRID: environmentRID,
		Summary: "Reconciliation exhausted its retry budget", OccurredAt: at(),
	}
	store.events = append(store.events, event)
	store.entries = append(store.entries, domain.InboxEntry{
		Recipient: domain.Recipient{Owner: owner, Channel: channel}, Event: event, PreferenceID: "npref_seed",
	})
}

// The artifact this change exists to produce, driven through the assembled Hub
// rather than through a harness: whatever Governance says about a resource,
// bob does not get alice's mail and cannot change what she is subscribed to.
func TestThroughTheHubOnePrincipalIsRefusedAnotherPrincipalsInboxAndPreference(t *testing.T) {
	t.Parallel()
	handler, store := hub(t)
	seed(store, domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, domain.ChannelEmail, "nev-alice")

	// bob is entitled to read the Environment -- the resource check passes and
	// the refusal below is therefore about whose inbox it is, which is the
	// distinction this test would be worthless without.
	if response := request(t, handler, http.MethodGet,
		"/v1/notifications/events?resourceRid="+environmentRID, "bob", ""); response.Code != http.StatusOK {
		t.Fatalf("bob cannot read the resource at all, so the refusals below prove nothing: %d %s",
			response.Code, response.Body.String())
	}

	for _, refusal := range []struct{ name, method, target, body string }{
		{
			"alice's inbox", http.MethodGet,
			"/v1/notifications/inbox?channel=EMAIL&ownerKind=USER&ownerId=alice", "",
		},
		{
			"alice's preference, read", http.MethodGet,
			"/v1/notifications/preferences?resourceRid=" + environmentRID + "&channel=EMAIL&ownerId=alice", "",
		},
		{
			"alice's preference, written", http.MethodPut, "/v1/notifications/preferences",
			fmt.Sprintf(
				`{"owner":{"kind":"USER","id":"alice"},"resourceRid":%q,"channel":"EMAIL","allFutureTypes":true}`,
				environmentRID),
		},
		{
			"the inbox of a team bob is not on", http.MethodGet,
			"/v1/notifications/inbox?channel=EMAIL&ownerKind=TEAM&ownerId=platform", "",
		},
		{
			"a preference for a team bob is not on", http.MethodPut, "/v1/notifications/preferences",
			fmt.Sprintf(
				`{"owner":{"kind":"TEAM","id":"platform"},"resourceRid":%q,"channel":"EMAIL","allFutureTypes":true,"contactId":"c1"}`,
				environmentRID),
		},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			response := request(t, handler, refusal.method, refusal.target, "bob", refusal.body)
			if response.Code != http.StatusForbidden {
				t.Fatalf("answered %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "nev-alice") {
				t.Fatalf("the refusal disclosed the inbox it refused: %s", response.Body.String())
			}
		})
	}
	if len(store.preferences) != 0 {
		t.Fatalf("a refused write reached the store: %#v", store.preferences)
	}

	// The same requests as alice, to show the refusals are about identity and
	// not about the routes being inert.
	own := request(t, handler, http.MethodGet, "/v1/notifications/inbox?channel=EMAIL", "alice", "")
	if own.Code != http.StatusOK {
		t.Fatalf("alice was refused her own inbox: %d %s", own.Code, own.Body.String())
	}
	var inbox struct {
		Entries []domain.InboxEntry `json:"entries"`
	}
	if err := json.Unmarshal(own.Body.Bytes(), &inbox); err != nil {
		t.Fatal(err)
	}
	if len(inbox.Entries) != 1 || inbox.Entries[0].Event.ID != "nev-alice" {
		t.Fatalf("alice's own inbox returned %#v", inbox.Entries)
	}
	written := request(t, handler, http.MethodPut, "/v1/notifications/preferences", "alice",
		fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","allFutureTypes":true}`, environmentRID))
	if written.Code != http.StatusOK {
		t.Fatalf("alice was refused her own preference: %d %s", written.Code, written.Body.String())
	}
	if len(store.preferences) != 1 {
		t.Fatalf("alice's write did not reach the store: %#v", store.preferences)
	}
}

// A Governance administrator may read any resource's events, because that is
// what a resource grant conveys and administrators hold it everywhere. They
// still may not read a person's inbox, and the difference is deliberate: no
// grant is over a person.
func TestAnAdministratorReadsEveryResourceAndNobodysInbox(t *testing.T) {
	t.Parallel()
	handler, store := hub(t)
	seed(store, domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, domain.ChannelEmail, "nev-alice")
	if response := request(t, handler, http.MethodGet,
		"/v1/notifications/events?resourceRid="+environmentRID, "root", ""); response.Code != http.StatusOK {
		t.Fatalf("an administrator was refused a resource read: %d %s", response.Code, response.Body.String())
	}
	response := request(t, handler, http.MethodGet,
		"/v1/notifications/inbox?channel=EMAIL&ownerId=alice", "root", "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("an administrator read another principal's inbox: %d %s", response.Code, response.Body.String())
	}
}

// carol is on no team holding a grant on the Environment, so the resource half
// of the authorization refuses her even though the owner half would not.
func TestAPreferenceOnAResourceTheCallerCannotReadIsRefused(t *testing.T) {
	t.Parallel()
	handler, store := hub(t)
	for _, refusal := range []struct{ method, target, body string }{
		{http.MethodGet, "/v1/notifications/events?resourceRid=" + environmentRID, ""},
		{
			http.MethodGet,
			"/v1/notifications/preferences?resourceRid=" + environmentRID + "&channel=EMAIL", "",
		},
		{
			http.MethodPut, "/v1/notifications/preferences",
			fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","allFutureTypes":true}`, environmentRID),
		},
	} {
		response := request(t, handler, refusal.method, refusal.target, "carol", refusal.body)
		if response.Code != http.StatusForbidden {
			t.Errorf("%s %s answered %d, want %d: %s",
				refusal.method, refusal.target, response.Code, http.StatusForbidden, response.Body.String())
		}
	}
	if len(store.preferences) != 0 {
		t.Fatalf("a write by a caller who cannot read the resource stored something: %#v", store.preferences)
	}
}

// The notification routes are not a second front door. They authenticate the
// way the rest of the Hub does, and the assertion is against an existing route
// rather than against a remembered status code -- if internal/controlplane's
// operator identity path changes, this fails instead of drifting.
func TestNotificationRoutesAuthenticateLikeEveryOtherHubRoute(t *testing.T) {
	t.Parallel()
	for _, mode := range []struct {
		name    string
		options controlplane.HandlerOptions
		subject string
	}{
		{
			name:    "no credential and no development opt-in",
			options: controlplane.HandlerOptions{},
		},
		{
			name:    "development headers, opted in",
			options: controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true},
			subject: "alice",
		},
		{
			name: "a resolver that refuses",
			options: controlplane.HandlerOptions{
				OperatorIdentityResolver: func(*http.Request) (controlplane.IdentityClaims, error) {
					return controlplane.IdentityClaims{}, controlplane.ErrUnauthorized
				},
			},
		},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			handler, err := NewHub(controlplane.NewMemoryRepository(), mode.options,
				Options{NotificationStore: newNotificationStore()})
			if err != nil {
				t.Fatal(err)
			}
			session := request(t, handler, http.MethodGet, "/v1/session", mode.subject, "")
			inbox := request(t, handler, http.MethodGet,
				"/v1/notifications/inbox?channel=EMAIL", mode.subject, "")
			sessionAuthenticated := session.Code != http.StatusUnauthorized
			inboxAuthenticated := inbox.Code != http.StatusUnauthorized
			if sessionAuthenticated != inboxAuthenticated {
				t.Fatalf("/v1/session answered %d and the inbox answered %d; the two identity paths disagree",
					session.Code, inbox.Code)
			}
		})
	}
}

// The operation the notification adapter asks for is spelled in its own
// package so that it depends on no other context. This is where the two meet,
// so this is where a rename has to be noticed.
func TestTheNotificationAdapterAsksForTheOperationGovernanceGrants(t *testing.T) {
	t.Parallel()
	if notificationhttp.OperationReadResource != governance.OperationReadResource {
		t.Fatalf("the notification routes ask for %q and Governance grants %q; no grant would ever match",
			notificationhttp.OperationReadResource, governance.OperationReadResource)
	}
}

// The facade keeps everything it had. A composition root that mounted a new
// surface by replacing the mux would be discovered as a 404 on a route nobody
// touched.
func TestMountingNotificationsLeavesTheLegacySurfaceAlone(t *testing.T) {
	t.Parallel()
	handler, _ := hub(t)
	for _, route := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/health/live", http.StatusOK},
		{http.MethodGet, "/health/ready", http.StatusOK},
		{http.MethodGet, "/version", http.StatusOK},
		{http.MethodPost, "/health/live", http.StatusMethodNotAllowed},
		// The notification paths themselves are method-scoped, so a verb they
		// do not serve is refused rather than falling through to the facade and
		// becoming a 404 that looks like a missing route.
		{http.MethodDelete, "/v1/notifications/inbox", http.StatusMethodNotAllowed},
		{http.MethodPost, "/v1/notifications/preferences", http.StatusMethodNotAllowed},
	} {
		response := request(t, handler, route.method, route.path, "alice", "")
		if response.Code != route.status {
			t.Errorf("%s %s answered %d, want %d", route.method, route.path, response.Code, route.status)
		}
	}
}

// A deployment with no notification component serves no notification routes.
// Answering 500 on a route whose store does not exist would tell an operator
// the Hub is broken rather than that this mode has none.
func TestAHubWithNoNotificationStoreMountsNoNotificationRoutes(t *testing.T) {
	t.Parallel()
	handler := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
	})
	response := request(t, handler, http.MethodGet, "/v1/notifications/inbox?channel=EMAIL", "alice", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("a Hub with no notification store answered %d, want %d: %s",
			response.Code, http.StatusNotFound, response.Body.String())
	}
}

// A service account acting for a person reads that person's inbox and nobody
// else's -- the delegation doctrine the rest of the Hub already uses, resolved
// before the notification adapter ever sees a principal.
func TestADelegatedServiceAccountReadsTheInboxOfWhoeverItActsFor(t *testing.T) {
	t.Parallel()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	ctx := t.Context()
	if _, err := service.PutTeam(ctx, controlplane.Team{
		Name: "automation", DisplayName: "Automation", AdminUsers: []string{"robot"}, MemberUsers: []string{"robot"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutGrant(ctx, controlplane.ResourceGrant{
		ResourceRID: "platform/root", Team: "automation",
		Operations: []string{governance.OperationActOnBehalf},
	}); err != nil {
		t.Fatal(err)
	}
	store := newNotificationStore()
	seed(store, domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, domain.ChannelEmail, "nev-alice")
	seed(store, domain.Owner{Kind: domain.PrincipalUser, ID: "robot"}, domain.ChannelEmail, "nev-robot")
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"root"},
	}, Options{NotificationStore: store})
	if err != nil {
		t.Fatal(err)
	}
	httpRequest := httptest.NewRequest(http.MethodGet, "/v1/notifications/inbox?channel=EMAIL", nil)
	httpRequest.Header.Set("X-Platform-Subject", "robot")
	httpRequest.Header.Set("X-Platform-Service-Account", "true")
	httpRequest.Header.Set("X-Platform-On-Behalf-Of", "alice")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httpRequest)
	if response.Code != http.StatusOK {
		t.Fatalf("a delegated read answered %d: %s", response.Code, response.Body.String())
	}
	var inbox struct {
		Owner   domain.Owner        `json:"owner"`
		Entries []domain.InboxEntry `json:"entries"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &inbox); err != nil {
		t.Fatal(err)
	}
	if inbox.Owner.ID != "alice" || len(inbox.Entries) != 1 || inbox.Entries[0].Event.ID != "nev-alice" {
		t.Fatalf("a service account acting for alice read %#v", inbox)
	}
}

// Every route reads or writes at the grain the store keys on, and the
// identifier a write mints is the one the tuple always produces, so the same
// edit sent twice leaves one row rather than two.
func TestAPreferenceWrittenThroughTheHubIsKeyedOnTheTuple(t *testing.T) {
	t.Parallel()
	handler, store := hub(t)
	body := fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","types":["PLAN_FAILURES"]}`, environmentRID)
	for range 2 {
		if response := request(t, handler, http.MethodPut,
			"/v1/notifications/preferences", "alice", body); response.Code != http.StatusOK {
			t.Fatalf("write answered %d: %s", response.Code, response.Body.String())
		}
	}
	if len(store.preferences) != 1 {
		t.Fatalf("two writes of one tuple left %d rows", len(store.preferences))
	}
	for _, stored := range store.preferences {
		want := notifications.PreferenceID(
			environmentRID, domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, domain.ChannelEmail)
		if stored.ID != want {
			t.Fatalf("stored identifier %q, want %q", stored.ID, want)
		}
	}
}

func at() time.Time { return time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC) }
