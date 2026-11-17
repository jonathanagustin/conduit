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

// These ownership fixtures use a native scope. Opaque Environment aliases are
// tested separately against genuine inventory and ordinary native roles.
const environmentRID = "environment/notification-test"

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

// RecordEvent is idempotent on the event's id, which is what ports.Store
// requires of an implementation and what the PostgreSQL store gets from
// ON CONFLICT (id) DO NOTHING. The fake carried it as a plain append until a
// second emit site needed it: an emitter that restates the same event -- after
// a crash, or because the fact it describes is resubmitted with every
// DesiredState -- must not produce a second inbox row.
func (store *notificationStore) RecordEvent(
	_ context.Context, event domain.Event, entries []domain.InboxEntry,
) error {
	for _, recorded := range store.events {
		if recorded.ID == event.ID {
			return nil
		}
	}
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

// contactBook is the address book without a database, keyed on the same
// (owner, kind) tuple the PostgreSQL primary key uses.
type contactBook struct {
	rows map[string]domain.Contact
}

var _ ports.Contacts = (*contactBook)(nil)

func contactKey(owner domain.Owner, kind domain.ContactKind) string {
	return string(owner.Kind) + "\x00" + owner.ID + "\x00" + string(kind)
}

func (book *contactBook) SaveContact(_ context.Context, contact domain.Contact) error {
	if err := contact.Validate(); err != nil {
		return err
	}
	book.rows[contactKey(contact.Owner, contact.Kind)] = contact
	return nil
}

func (book *contactBook) ContactsFor(
	_ context.Context, owner domain.Owner,
) ([]domain.Contact, error) {
	held := []domain.Contact{}
	for _, kind := range []domain.ContactKind{
		domain.ContactEmail, domain.ContactPagerDuty, domain.ContactSlack,
	} {
		if contact, found := book.rows[contactKey(owner, kind)]; found {
			held = append(held, contact)
		}
	}
	return held, nil
}

func (book *contactBook) DeleteContact(
	_ context.Context, owner domain.Owner, kind domain.ContactKind,
) (bool, error) {
	key := contactKey(owner, kind)
	if _, found := book.rows[key]; !found {
		return false, nil
	}
	delete(book.rows, key)
	return true, nil
}

// hub assembles the real Hub -- the real repository, the real Governance
// service, the real identity path -- over an in-memory notification store, so
// what is under test is the wiring rather than a rehearsal of it.
func hub(t *testing.T) (http.Handler, *notificationStore) {
	handler, store, _ := hubWithContacts(t)
	return handler, store
}

// hubWithContacts is the same Hub with its address book handed back, for the
// tests that are about who may write an address.
func hubWithContacts(t *testing.T) (http.Handler, *notificationStore, *contactBook) {
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
	book := &contactBook{rows: map[string]domain.Contact{}}
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		// Named so that the "local-developer" administrator shortcut is off and
		// every decision below comes from a grant.
		BootstrapAdministrators: []string{"root"},
	}, Options{NotificationStore: store, NotificationContacts: book})
	if err != nil {
		t.Fatal(err)
	}
	return handler, store, book
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

// delegationHub is the configuration #241 was reproduced under: alice is a
// bootstrap administrator, robot is a service account holding act-on-behalf and
// nothing else, and the Environment named below is one nobody has been granted
// anything on. Both surfaces are asked about that same coordinate.
func delegationHub(t *testing.T) (http.Handler, *controlplane.MemoryRepository, string) {
	t.Helper()
	repository := controlplane.NewMemoryRepository()
	service := controlplane.NewGovernanceService(repository)
	ctx := t.Context()
	// alice is on the delegating team as well as robot, so the credential that
	// widens can be exercised alongside the one that narrows: alice is the
	// administrator *and* a service account able to act for somebody who is not.
	if _, err := service.PutTeam(ctx, controlplane.Team{
		Name: "automation", DisplayName: "Automation",
		AdminUsers: []string{"robot"}, MemberUsers: []string{"robot", "alice"},
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
	secret := "environment/secret-env"
	store.events = append(store.events, domain.Event{
		ID: "nev-secret", Type: domain.EventPlanFailures, ResourceRID: secret,
		Summary: "the confidential body a refusal must not serve", OccurredAt: at(),
	})
	handler, err := NewHub(repository, controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"},
	}, Options{NotificationStore: store})
	if err != nil {
		t.Fatal(err)
	}
	return handler, repository, secret
}

func as(
	t *testing.T, handler http.Handler, target, subject, onBehalfOf string,
) *httptest.ResponseRecorder {
	t.Helper()
	httpRequest := httptest.NewRequest(http.MethodGet, target, nil)
	httpRequest.Header.Set("X-Platform-Subject", subject)
	if onBehalfOf != "" {
		httpRequest.Header.Set("X-Platform-Service-Account", "true")
		httpRequest.Header.Set("X-Platform-On-Behalf-Of", onBehalfOf)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httpRequest)
	return response
}

// The notification routes and the controlplane's REST routes must reach the
// same answer about the same coordinate, and a delegated credential is where
// they stopped doing so.
//
// #241: the notification surface matched the bootstrap administrator list
// against the *effective* principal, so `robot` -- holding act-on-behalf and no
// grant at all -- became an administrator by naming alice, and read an
// Environment's events. The same credential on the REST route for the same
// Environment was refused. The assertion is that the two agree, not that either
// answers a remembered status: if the administrator rule changes, both move
// together or this fails.
//
// Moving the test from the effective principal to the carrier is symmetric, and
// the last row is here because the widening direction deserves a test rather
// than to be discovered. An administrator's own delegated credential -- alice,
// named in BootstrapAdministrators, acting for bob who is named nowhere -- was
// refused by these routes before and is served now. That is not a regression
// being tolerated; it is the REST behaviour, which has always matched the
// carrier, and a surface that kept refusing it would be the same disagreement
// pointing the other way. Both rows fail if either surface moves alone.
func TestBothSurfacesAgreeAboutADelegatedAdministrator(t *testing.T) {
	t.Parallel()
	handler, _, secret := delegationHub(t)
	events := "/v1/notifications/events?resourceRid=" + secret
	rest := "/v1/environments/secret-env"
	for _, credential := range []struct {
		name, subject, onBehalfOf string
		refused                   bool
	}{
		{name: "the carrier alone", subject: "robot", refused: true},
		{name: "the carrier acting for an administrator", subject: "robot", onBehalfOf: "alice", refused: true},
		{name: "the administrator herself", subject: "alice", refused: false},
		{
			name:    "the administrator's own delegated credential",
			subject: "alice", onBehalfOf: "bob", refused: false,
		},
	} {
		t.Run(credential.name, func(t *testing.T) {
			notifications := as(t, handler, events, credential.subject, credential.onBehalfOf)
			controlplaneREST := as(t, handler, rest, credential.subject, credential.onBehalfOf)
			// Refusal rather than the whole status: an authorized read of an
			// Environment that was never created is a 404 on the REST route and an
			// empty page on the notification route, and those two disagreeing about
			// a body is not what this is about.
			notificationsRefused := notifications.Code == http.StatusForbidden
			restRefused := controlplaneREST.Code == http.StatusForbidden
			if notificationsRefused != restRefused {
				t.Fatalf("the notification routes answered %d and the REST routes answered %d for %s;"+
					" the two surfaces disagree about one coordinate\n%s",
					notifications.Code, controlplaneREST.Code, secret, notifications.Body.String())
			}
			if notificationsRefused != credential.refused {
				t.Fatalf("both surfaces agreed on %d, but this credential should have been refused=%v",
					notifications.Code, credential.refused)
			}
			if credential.refused && strings.Contains(notifications.Body.String(), "nev-secret") {
				t.Fatalf("a refused read served the event body it refused: %s", notifications.Body.String())
			}
		})
	}
}

// An authorization decision that is not recorded cannot be reviewed afterwards,
// which is the half of #241 that had no symptom: the notification routes took
// the decision the REST routes file for every ALLOW and every DENY, and filed
// nothing. The record has to name the carrier as the subject and the delegation
// as context, because "robot acting for alice" is the only form of this event
// that answers who actually presented a credential.
func TestTheNotificationRoutesRecordTheAuthorizationTheyDecided(t *testing.T) {
	t.Parallel()
	handler, repository, secret := delegationHub(t)
	if response := as(t, handler, "/v1/notifications/events?resourceRid="+secret,
		"robot", "alice"); response.Code != http.StatusForbidden {
		t.Fatalf("the delegated read answered %d, want %d: %s",
			response.Code, http.StatusForbidden, response.Body.String())
	}
	records, err := repository.ListAuthorizationAudit(t.Context(), secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		if record.Subject != "robot" || record.Decision != governance.DecisionDeny {
			continue
		}
		if record.Action != governance.OperationReadResource {
			t.Fatalf("the decision was filed as %q, not the operation it was taken on", record.Action)
		}
		if record.Context["onBehalfOf"] != "alice" {
			t.Fatalf("the record does not say who the carrier acted for: %#v", record.Context)
		}
		return
	}
	t.Fatalf("the notification routes refused a read of %s and filed no record of it: %#v", secret, records)
}

func at() time.Time { return time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC) }

// The artifact this change exists to produce, driven through the assembled Hub
// rather than through a harness. A contact is where somebody's notifications
// are sent, so a write that reached the wrong owner would not leak bob's mail
// to alice -- it would redirect alice's mail to bob.
//
// bob holds a grant on the Environment throughout, so the resource half of the
// authorization passes and every refusal below is about whose address it is.
// There is no administrator override, which the companion test pins.
func TestThroughTheHubOnePrincipalCannotWriteAnotherPrincipalsContact(t *testing.T) {
	t.Parallel()
	handler, _, book := hubWithContacts(t)
	alice := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	if err := book.SaveContact(t.Context(), domain.Contact{
		ID: "ncontact_seed", Owner: alice, Kind: domain.ContactEmail,
		Address: "alice@example.test", CreatedAt: at(), UpdatedAt: at(),
	}); err != nil {
		t.Fatal(err)
	}
	if response := request(t, handler, http.MethodGet,
		"/v1/notifications/events?resourceRid="+environmentRID, "bob", ""); response.Code != http.StatusOK {
		t.Fatalf("bob cannot read the resource at all, so the refusals below prove nothing: %d %s",
			response.Code, response.Body.String())
	}

	for _, refusal := range []struct{ name, method, target, body string }{
		{
			"alice's contact, redirected", http.MethodPut, "/v1/notifications/contacts",
			`{"owner":{"kind":"USER","id":"alice"},"kind":"EMAIL","address":"attacker@example.test"}`,
		},
		{
			"alice's contact, read", http.MethodGet,
			"/v1/notifications/contacts?ownerKind=USER&ownerId=alice", "",
		},
		{
			"alice's contact, deleted", http.MethodDelete,
			"/v1/notifications/contacts?ownerKind=USER&ownerId=alice&kind=EMAIL", "",
		},
		{
			"a contact for a team bob is not on", http.MethodPut, "/v1/notifications/contacts",
			`{"owner":{"kind":"TEAM","id":"platform"},"kind":"SLACK","address":"#attacker"}`,
		},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			response := request(t, handler, refusal.method, refusal.target, "bob", refusal.body)
			if response.Code != http.StatusForbidden {
				t.Fatalf("answered %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "alice@example.test") {
				t.Fatalf("the refusal disclosed the address it refused: %s", response.Body.String())
			}
		})
	}
	// The state, which is what a refusal that answered 403 and wrote anyway
	// would still have changed.
	if len(book.rows) != 1 {
		t.Fatalf("a refused write changed the address book: %#v", book.rows)
	}
	held := book.rows[contactKey(alice, domain.ContactEmail)]
	if held.Address != "alice@example.test" {
		t.Fatalf("alice is now reached at %q", held.Address)
	}

	// And the same requests as alice, to show the refusals are about identity
	// rather than the routes being inert.
	written := request(t, handler, http.MethodPut, "/v1/notifications/contacts", "alice",
		`{"kind":"EMAIL","address":"alice@elsewhere.test"}`)
	if written.Code != http.StatusOK {
		t.Fatalf("alice was refused her own contact: %d %s", written.Code, written.Body.String())
	}
	if book.rows[contactKey(alice, domain.ContactEmail)].Address != "alice@elsewhere.test" {
		t.Fatalf("alice's own write did not reach the store: %#v", book.rows)
	}
}

// A Governance administrator holds every resource grant there is, and still
// holds nothing over a person's address. Deliberate: a grant conveys authority
// over a resource, and "an operator with enough grants can redirect your mail"
// is a property somebody would have to discover by being surprised.
func TestAnAdministratorCannotReadOrRedirectAnybodysContact(t *testing.T) {
	t.Parallel()
	handler, _, book := hubWithContacts(t)
	alice := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	if err := book.SaveContact(t.Context(), domain.Contact{
		ID: "ncontact_seed", Owner: alice, Kind: domain.ContactEmail,
		Address: "alice@example.test", CreatedAt: at(), UpdatedAt: at(),
	}); err != nil {
		t.Fatal(err)
	}
	if response := request(t, handler, http.MethodGet,
		"/v1/notifications/events?resourceRid="+environmentRID, "root", ""); response.Code != http.StatusOK {
		t.Fatalf("the administrator was refused a resource read, so this proves nothing: %d", response.Code)
	}
	for name, refusal := range map[string]struct{ method, target, body string }{
		"read": {
			http.MethodGet, "/v1/notifications/contacts?ownerKind=USER&ownerId=alice", "",
		},
		"redirect": {
			http.MethodPut, "/v1/notifications/contacts",
			`{"owner":{"kind":"USER","id":"alice"},"kind":"EMAIL","address":"root@example.test"}`,
		},
	} {
		response := request(t, handler, refusal.method, refusal.target, "root", refusal.body)
		if response.Code != http.StatusForbidden {
			t.Errorf("an administrator %s another principal's contact: %d %s",
				name, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "alice@example.test") {
			t.Errorf("the refusal disclosed the address: %s", response.Body.String())
		}
	}
	if book.rows[contactKey(alice, domain.ContactEmail)].Address != "alice@example.test" {
		t.Fatalf("an administrator redirected a person's mail: %#v", book.rows)
	}
}

// A Hub assembled without an address book serves every route it did before and
// no contact route, rather than one that answers 500 to the first person who
// edits their own address.
func TestAHubWithNoAddressBookServesNoContactRoutes(t *testing.T) {
	t.Parallel()
	handler, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"root"},
	}, Options{NotificationStore: newNotificationStore()})
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, handler, http.MethodGet, "/v1/notifications/contacts", "alice", "")
	if response.Code != http.StatusNotFound {
		t.Fatalf("a Hub with no address book answered %d, want %d: %s",
			response.Code, http.StatusNotFound, response.Body.String())
	}
	if own := request(t, handler, http.MethodGet,
		"/v1/notifications/inbox?channel=EMAIL", "alice", ""); own.Code != http.StatusOK {
		t.Fatalf("the inbox answered %d without an address book", own.Code)
	}
}
