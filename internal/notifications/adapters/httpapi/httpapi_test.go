package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

const resource = "ri.apollo.main.environment.d41d8cd98f00"

func at() time.Time { return time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC) }

// memoryStore is the notification store with the database taken out. It keys
// preferences on the same tuple the PostgreSQL store's UNIQUE constraint does,
// so a test that passes here is not passing because the fake forgot the grain.
type memoryStore struct {
	events      []domain.Event
	entries     []domain.InboxEntry
	preferences map[string]domain.Preference
	lastLimit   int
	failWith    error
}

var _ ports.Store = (*memoryStore)(nil)

func newStore() *memoryStore {
	return &memoryStore{preferences: map[string]domain.Preference{}}
}

func preferenceKey(preference domain.Preference) string {
	return strings.Join([]string{
		string(preference.Owner.Kind), preference.Owner.ID,
		preference.ResourceRID, string(preference.Channel),
	}, "\x00")
}

func (store *memoryStore) RecordEvent(
	_ context.Context, event domain.Event, entries []domain.InboxEntry,
) error {
	store.events = append(store.events, event)
	store.entries = append(store.entries, entries...)
	return nil
}

func (store *memoryStore) PreferencesFor(
	_ context.Context, resourceRID string,
) ([]domain.Preference, error) {
	if store.failWith != nil {
		return nil, store.failWith
	}
	found := []domain.Preference{}
	for _, preference := range store.preferences {
		if preference.ResourceRID == resourceRID {
			found = append(found, preference)
		}
	}
	return found, nil
}

func (store *memoryStore) SavePreference(_ context.Context, preference domain.Preference) error {
	if err := preference.Validate(); err != nil {
		return err
	}
	store.preferences[preferenceKey(preference)] = preference
	return nil
}

func (store *memoryStore) Inbox(
	_ context.Context, recipient domain.Recipient, limit int,
) ([]domain.InboxEntry, error) {
	if store.failWith != nil {
		return nil, store.failWith
	}
	store.lastLimit = limit
	found := []domain.InboxEntry{}
	for _, entry := range store.entries {
		if entry.Recipient == recipient && len(found) < limit {
			found = append(found, entry)
		}
	}
	return found, nil
}

func (store *memoryStore) EventsForResource(
	_ context.Context, resourceRID string, limit int,
) ([]domain.Event, error) {
	store.lastLimit = limit
	found := []domain.Event{}
	for _, event := range store.events {
		if event.ResourceRID == resourceRID && len(found) < limit {
			found = append(found, event)
		}
	}
	return found, nil
}

// harness mounts the routes over a store, with identity and authorization
// answered by the test rather than by Governance -- the point being that this
// package's refusals must not depend on which of them said no.
type harness struct {
	handler http.Handler
	store   *memoryStore
	caller  Principal
	denyRID map[string]bool
	resolve func(*http.Request) (Principal, error)
}

func newHarness(t *testing.T, caller Principal) *harness {
	t.Helper()
	store := newStore()
	reader, err := application.NewReader(store)
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := application.NewPreferences(store, at)
	if err != nil {
		t.Fatal(err)
	}
	test := &harness{store: store, caller: caller, denyRID: map[string]bool{}}
	mux := http.NewServeMux()
	if err := Register(mux, Config{
		Reader: reader, Preferences: preferences,
		Resolve: func(request *http.Request) (Principal, error) {
			if test.resolve != nil {
				return test.resolve(request)
			}
			return test.caller, nil
		},
		Authorize: func(_ context.Context, _ Principal, resourceRID, operation string) bool {
			if operation != OperationReadResource {
				return false
			}
			return !test.denyRID[resourceRID]
		},
	}); err != nil {
		t.Fatal(err)
	}
	test.handler = mux
	return test
}

func (test *harness) do(method, target, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	response := httptest.NewRecorder()
	test.handler.ServeHTTP(response, request)
	return response
}

func (test *harness) seedEntry(t *testing.T, owner domain.Owner, channel domain.Channel, id string) {
	t.Helper()
	event := domain.Event{
		ID: id, Type: domain.EventPlanFailures, ResourceRID: resource,
		Summary: "Reconciliation exhausted its retry budget", OccurredAt: at(),
	}
	test.store.events = append(test.store.events, event)
	test.store.entries = append(test.store.entries, domain.InboxEntry{
		Recipient: domain.Recipient{Owner: owner, Channel: channel}, Event: event, PreferenceID: "npref_seed",
	})
}

func (test *harness) seedPreference(t *testing.T, owner domain.Owner, channel domain.Channel) {
	t.Helper()
	if err := test.store.SavePreference(context.Background(), domain.Preference{
		ID: application.PreferenceID(resource, owner, channel), Owner: owner, ResourceRID: resource,
		Channel: channel, Types: []domain.EventType{domain.EventPlanFailures},
		CreatedAt: at(), UpdatedAt: at(), Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
}

func alice() Principal { return Principal{Username: "alice", Teams: []string{"platform"}} }

func user(name string) domain.Owner {
	return domain.Owner{Kind: domain.PrincipalUser, ID: name}
}

// The test this whole surface exists to earn.
//
// An inbox is per-principal and a preference is per-principal, and both are
// reachable by naming an owner in a request that is otherwise identical to a
// legitimate one. Every way of naming somebody else is enumerated here in one
// place, because a refusal that holds on three routes and not the fourth is the
// same bug as no refusal at all.
func TestAPrincipalIsRefusedAnotherPrincipalsInboxAndPreference(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	bob := user("bob")
	test.seedEntry(t, bob, domain.ChannelEmail, "nev-bob")
	test.seedPreference(t, bob, domain.ChannelEmail)

	for _, refusal := range []struct {
		name, method, target, body string
	}{
		{
			name:   "reading another user's inbox",
			method: http.MethodGet,
			target: "/v1/notifications/inbox?channel=EMAIL&ownerKind=USER&ownerId=bob",
		},
		{
			name:   "reading another user's inbox without naming the kind",
			method: http.MethodGet,
			target: "/v1/notifications/inbox?channel=EMAIL&ownerId=bob",
		},
		{
			name:   "reading the inbox of a team the caller is not on",
			method: http.MethodGet,
			target: "/v1/notifications/inbox?channel=EMAIL&ownerKind=TEAM&ownerId=security",
		},
		{
			name:   "reading another user's preference",
			method: http.MethodGet,
			target: "/v1/notifications/preferences?resourceRid=" + resource + "&channel=EMAIL&ownerId=bob",
		},
		{
			name:   "writing another user's preference",
			method: http.MethodPut,
			target: "/v1/notifications/preferences",
			body: fmt.Sprintf(
				`{"owner":{"kind":"USER","id":"bob"},"resourceRid":%q,"channel":"EMAIL","allFutureTypes":true}`,
				resource),
		},
		{
			name:   "writing a preference for a team the caller is not on",
			method: http.MethodPut,
			target: "/v1/notifications/preferences",
			body: fmt.Sprintf(
				`{"owner":{"kind":"TEAM","id":"security"},"resourceRid":%q,"channel":"EMAIL","allFutureTypes":true,"contactId":"contact-1"}`,
				resource),
		},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			response := test.do(refusal.method, refusal.target, refusal.body)
			if response.Code != http.StatusForbidden {
				t.Fatalf("%s answered %d, want %d; body %s",
					refusal.name, response.Code, http.StatusForbidden, response.Body.String())
			}
			// A refusal that leaked the thing it refused would be worse than
			// none, because the caller would have what they asked for and a
			// status code saying they did not.
			if strings.Contains(response.Body.String(), "nev-bob") {
				t.Fatalf("%s refused and disclosed anyway: %s", refusal.name, response.Body.String())
			}
		})
	}

	// And the refusal was about who was asking, not about the request being
	// malformed: bob's own preference was never touched.
	stored, err := test.store.PreferencesFor(context.Background(), resource)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Owner != bob || stored[0].AllFutureTypes {
		t.Fatalf("a refused write changed stored state: %#v", stored)
	}
}

// The other half of the same property: the refusals above are not the route
// being broken for everybody.
func TestACallerReadsTheirOwnInboxAndTheirTeamsWithoutNamingEither(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	test.seedEntry(t, user("alice"), domain.ChannelEmail, "nev-alice")
	test.seedEntry(t, domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"}, domain.ChannelEmail, "nev-team")
	test.seedEntry(t, user("bob"), domain.ChannelEmail, "nev-bob")

	response := test.do(http.MethodGet, "/v1/notifications/inbox?channel=EMAIL", "")
	if response.Code != http.StatusOK {
		t.Fatalf("own inbox answered %d: %s", response.Code, response.Body.String())
	}
	var own struct {
		Owner   domain.Owner        `json:"owner"`
		Limit   int                 `json:"limit"`
		Entries []domain.InboxEntry `json:"entries"`
	}
	decode(t, response, &own)
	if own.Owner != user("alice") {
		t.Errorf("an inbox read with no owner named did not default to the caller: %#v", own.Owner)
	}
	if len(own.Entries) != 1 || own.Entries[0].Event.ID != "nev-alice" {
		t.Fatalf("own inbox returned %#v", own.Entries)
	}

	response = test.do(http.MethodGet, "/v1/notifications/inbox?channel=EMAIL&ownerKind=TEAM&ownerId=platform", "")
	if response.Code != http.StatusOK {
		t.Fatalf("a member was refused their own team's inbox: %d %s", response.Code, response.Body.String())
	}
	decode(t, response, &own)
	if len(own.Entries) != 1 || own.Entries[0].Event.ID != "nev-team" {
		t.Fatalf("team inbox returned %#v", own.Entries)
	}
}

// The ceiling the application layer already applies has to be visible, or a
// caller cannot tell a truncated page from the end of their inbox.
func TestAnInboxReadIsBoundedAndSaysWhatBoundItWasGiven(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	for index := range 3 {
		test.seedEntry(t, user("alice"), domain.ChannelInApp, fmt.Sprintf("nev-%d", index))
	}
	for _, testCase := range []struct {
		query         string
		wantLimit     int
		wantEntries   int
		wantStoreSees int
	}{
		{query: "", wantLimit: application.MaximumLimit, wantEntries: 3, wantStoreSees: application.MaximumLimit},
		{query: "&limit=2", wantLimit: 2, wantEntries: 2, wantStoreSees: 2},
		{query: "&limit=5000", wantLimit: application.MaximumLimit, wantEntries: 3, wantStoreSees: application.MaximumLimit},
	} {
		t.Run("limit"+testCase.query, func(t *testing.T) {
			response := test.do(http.MethodGet, "/v1/notifications/inbox?channel=IN_APP"+testCase.query, "")
			if response.Code != http.StatusOK {
				t.Fatalf("answered %d: %s", response.Code, response.Body.String())
			}
			var body struct {
				Limit   int                 `json:"limit"`
				Entries []domain.InboxEntry `json:"entries"`
			}
			decode(t, response, &body)
			if body.Limit != testCase.wantLimit {
				t.Errorf("reported limit %d, want %d", body.Limit, testCase.wantLimit)
			}
			if len(body.Entries) != testCase.wantEntries {
				t.Errorf("returned %d entries, want %d", len(body.Entries), testCase.wantEntries)
			}
			if test.store.lastLimit != testCase.wantStoreSees {
				t.Errorf("store was asked for %d rows, want %d", test.store.lastLimit, testCase.wantStoreSees)
			}
		})
	}
	if response := test.do(http.MethodGet, "/v1/notifications/inbox?channel=IN_APP&limit=fifty", ""); response.Code != http.StatusBadRequest {
		t.Errorf("an unparseable limit answered %d, want %d", response.Code, http.StatusBadRequest)
	}
}

// A preference is about a resource, and subscribing to a resource you may not
// read would have events copied into your inbox one row at a time.
func TestEveryRouteAsksGovernanceAboutTheResourceItNames(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	test.denyRID[resource] = true
	for _, refusal := range []struct{ name, method, target, body string }{
		{"events", http.MethodGet, "/v1/notifications/events?resourceRid=" + resource, ""},
		{
			"preference read", http.MethodGet,
			"/v1/notifications/preferences?resourceRid=" + resource + "&channel=EMAIL", "",
		},
		{
			"preference write", http.MethodPut, "/v1/notifications/preferences",
			fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","allFutureTypes":true}`, resource),
		},
	} {
		t.Run(refusal.name, func(t *testing.T) {
			response := test.do(refusal.method, refusal.target, refusal.body)
			if response.Code != http.StatusForbidden {
				t.Fatalf("answered %d, want %d: %s", response.Code, http.StatusForbidden, response.Body.String())
			}
		})
	}
	if len(test.store.preferences) != 0 {
		t.Fatalf("a write refused by Governance stored something: %#v", test.store.preferences)
	}
}

// An inbox read is deliberately not authorized against a resource: an inbox
// spans resources, and asking Governance about one of them would be asking the
// wrong question. The owner check is the whole authorization, which is why the
// test above exists.
func TestAnInboxReadIsNotGatedOnAnyResource(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	test.denyRID[resource] = true
	test.seedEntry(t, user("alice"), domain.ChannelEmail, "nev-alice")
	response := test.do(http.MethodGet, "/v1/notifications/inbox?channel=EMAIL", "")
	if response.Code != http.StatusOK {
		t.Fatalf("own inbox answered %d: %s", response.Code, response.Body.String())
	}
}

func TestAPreferenceRoundTripsAtTheTupleTheStoreKeysOn(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	body := fmt.Sprintf(
		`{"resourceRid":%q,"channel":"EMAIL","types":["PLAN_FAILURES"]}`, resource)
	response := test.do(http.MethodPut, "/v1/notifications/preferences", body)
	if response.Code != http.StatusOK {
		t.Fatalf("write answered %d: %s", response.Code, response.Body.String())
	}
	var written domain.Preference
	decode(t, response, &written)
	if written.Owner != user("alice") {
		t.Errorf("a write with no owner named did not default to the caller: %#v", written.Owner)
	}
	if written.ID != application.PreferenceID(resource, user("alice"), domain.ChannelEmail) {
		t.Errorf("identifier %q is not the one the tuple produces", written.ID)
	}
	if written.Revision != 1 {
		t.Errorf("first write is revision %d, want 1", written.Revision)
	}

	// Saving the same page twice leaves one row, which is the property the
	// store's UNIQUE constraint exists for and the reason the identifier is
	// derived rather than minted.
	second := test.do(http.MethodPut, "/v1/notifications/preferences",
		fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","allFutureTypes":true}`, resource))
	if second.Code != http.StatusOK {
		t.Fatalf("second write answered %d: %s", second.Code, second.Body.String())
	}
	var replaced domain.Preference
	decode(t, second, &replaced)
	if replaced.ID != written.ID || replaced.Revision != 2 || replaced.CreatedAt != written.CreatedAt {
		t.Errorf("a second save of one tuple did not replace the first: %#v", replaced)
	}
	if len(test.store.preferences) != 1 {
		t.Fatalf("two rows for one tuple: %#v", test.store.preferences)
	}

	read := test.do(http.MethodGet,
		"/v1/notifications/preferences?resourceRid="+resource+"&channel=EMAIL", "")
	if read.Code != http.StatusOK {
		t.Fatalf("read answered %d: %s", read.Code, read.Body.String())
	}
	var got domain.Preference
	decode(t, read, &got)
	if !got.AllFutureTypes || got.Revision != 2 {
		t.Errorf("read did not return the second write: %#v", got)
	}

	// The other channel is a different tuple and has nothing stored, which is
	// distinguishable from a tuple whose preference enables nothing -- a state
	// the domain refuses to store at all.
	missing := test.do(http.MethodGet,
		"/v1/notifications/preferences?resourceRid="+resource+"&channel=IN_APP", "")
	if missing.Code != http.StatusNotFound {
		t.Fatalf("an unset channel answered %d, want %d", missing.Code, http.StatusNotFound)
	}
}

func TestAnUnauthenticatedCallerReachesNoNotificationRoute(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	test.resolve = func(*http.Request) (Principal, error) {
		return Principal{}, StatusError{Status: http.StatusUnauthorized, Message: "unauthorized"}
	}
	for _, route := range []struct{ method, target string }{
		{http.MethodGet, "/v1/notifications/inbox?channel=EMAIL"},
		{http.MethodGet, "/v1/notifications/events?resourceRid=" + resource},
		{http.MethodGet, "/v1/notifications/preferences?resourceRid=" + resource + "&channel=EMAIL"},
		{http.MethodPut, "/v1/notifications/preferences"},
	} {
		if response := test.do(route.method, route.target, "{}"); response.Code != http.StatusUnauthorized {
			t.Errorf("%s %s answered %d, want %d",
				route.method, route.target, response.Code, http.StatusUnauthorized)
		}
	}

	// A resolver that says nothing about the failure still refuses, and a
	// resolver that returns a blank username is not an anonymous inbox owner.
	test.resolve = func(*http.Request) (Principal, error) { return Principal{}, errors.New("no credential") }
	if response := test.do(http.MethodGet, "/v1/notifications/inbox?channel=EMAIL", ""); response.Code != http.StatusUnauthorized {
		t.Errorf("an unclassified identity failure answered %d, want %d", response.Code, http.StatusUnauthorized)
	}
	test.resolve = func(*http.Request) (Principal, error) { return Principal{Username: "  "}, nil }
	if response := test.do(http.MethodGet, "/v1/notifications/inbox?channel=EMAIL", ""); response.Code != http.StatusUnauthorized {
		t.Errorf("a blank username answered %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestARequestThatNamesNoChannelOrResourceIsRefusedRatherThanGuessed(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	for _, bad := range []struct{ name, method, target, body string }{
		{"inbox with no channel", http.MethodGet, "/v1/notifications/inbox", ""},
		{"inbox with an invented channel", http.MethodGet, "/v1/notifications/inbox?channel=SLACK", ""},
		{"inbox with an invented owner kind", http.MethodGet, "/v1/notifications/inbox?channel=EMAIL&ownerKind=ROBOT&ownerId=x", ""},
		{"a team inbox with no team", http.MethodGet, "/v1/notifications/inbox?channel=EMAIL&ownerKind=TEAM", ""},
		{"events with no resource", http.MethodGet, "/v1/notifications/events", ""},
		{"a preference with no resource", http.MethodGet, "/v1/notifications/preferences?channel=EMAIL", ""},
		{"a written preference with no resource", http.MethodPut, "/v1/notifications/preferences", `{"channel":"EMAIL","allFutureTypes":true}`},
		{"a written preference that is not JSON", http.MethodPut, "/v1/notifications/preferences", `{`},
		{
			"a written preference naming a field this build does not have", http.MethodPut,
			"/v1/notifications/preferences",
			fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL","severity":"HIGH"}`, resource),
		},
	} {
		t.Run(bad.name, func(t *testing.T) {
			if response := test.do(bad.method, bad.target, bad.body); response.Code != http.StatusBadRequest {
				t.Fatalf("answered %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
	// A preference the domain refuses is a different answer from one this
	// package could not read: the caller said what they meant and it cannot be
	// stored.
	unstorable := test.do(http.MethodPut, "/v1/notifications/preferences",
		fmt.Sprintf(`{"resourceRid":%q,"channel":"EMAIL"}`, resource))
	if unstorable.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a preference enabling nothing answered %d, want %d",
			unstorable.Code, http.StatusUnprocessableEntity)
	}
}

func TestEventsForAResourceAreReadableAndBounded(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	test.seedEntry(t, user("bob"), domain.ChannelEmail, "nev-1")
	response := test.do(http.MethodGet, "/v1/notifications/events?resourceRid="+resource+"&limit=1", "")
	if response.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", response.Code, response.Body.String())
	}
	var body struct {
		ResourceRID string         `json:"resourceRid"`
		Limit       int            `json:"limit"`
		Events      []domain.Event `json:"events"`
	}
	decode(t, response, &body)
	if body.ResourceRID != resource || body.Limit != 1 || len(body.Events) != 1 {
		t.Fatalf("resource events returned %#v", body)
	}
	// Reading a resource's events is not reading anybody's inbox: the entry
	// above belongs to bob and the event is still legible to alice, because the
	// question "did the Hub raise this at all" is about the resource.
	if body.Events[0].ID != "nev-1" {
		t.Errorf("events returned %#v", body.Events)
	}
}

func TestRegisterRefusesAPartialSurface(t *testing.T) {
	t.Parallel()
	store := newStore()
	reader, err := application.NewReader(store)
	if err != nil {
		t.Fatal(err)
	}
	preferences, err := application.NewPreferences(store, at)
	if err != nil {
		t.Fatal(err)
	}
	resolve := ResolvePrincipal(func(*http.Request) (Principal, error) { return alice(), nil })
	authorize := Authorize(func(context.Context, Principal, string, string) bool { return true })
	for _, missing := range []struct {
		name   string
		mux    *http.ServeMux
		config Config
	}{
		{"no mux", nil, Config{Reader: reader, Preferences: preferences, Resolve: resolve, Authorize: authorize}},
		{"no reader", http.NewServeMux(), Config{Preferences: preferences, Resolve: resolve, Authorize: authorize}},
		{"no preferences", http.NewServeMux(), Config{Reader: reader, Resolve: resolve, Authorize: authorize}},
		{"no resolver", http.NewServeMux(), Config{Reader: reader, Preferences: preferences, Authorize: authorize}},
		{"no authorizer", http.NewServeMux(), Config{Reader: reader, Preferences: preferences, Resolve: resolve}},
	} {
		t.Run(missing.name, func(t *testing.T) {
			if err := Register(missing.mux, missing.config); err == nil {
				t.Fatal("a surface that could not refuse anybody was mounted anyway")
			}
		})
	}
}

func TestAStoreFailureIsNotAnEmptyInbox(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	test.store.failWith = errors.New("connection refused")
	response := test.do(http.MethodGet, "/v1/notifications/inbox?channel=EMAIL", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("a broken store answered %d, want %d: %s",
			response.Code, http.StatusInternalServerError, response.Body.String())
	}
	response = test.do(http.MethodGet,
		"/v1/notifications/preferences?resourceRid="+resource+"&channel=EMAIL", "")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("a broken store answered %d for a preference read, want %d",
			response.Code, http.StatusInternalServerError)
	}
}

func decode(t *testing.T, response *httptest.ResponseRecorder, into any) {
	t.Helper()
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type %q, want application/json", got)
	}
	if err := json.Unmarshal(response.Body.Bytes(), into); err != nil {
		t.Fatalf("decode %s: %v", response.Body.String(), err)
	}
}

// A verb these routes do not serve is answered here rather than left to fall
// through to whatever else the Hub mounts, because a 404 would tell a caller
// the route does not exist.
func TestAnUnservedMethodOrPathUnderThePrefixIsAnsweredHere(t *testing.T) {
	t.Parallel()
	test := newHarness(t, alice())
	for _, unsupported := range []struct {
		method, target string
		status         int
		allow          string
	}{
		{http.MethodDelete, "/v1/notifications/inbox", http.StatusMethodNotAllowed, "GET"},
		{http.MethodPost, "/v1/notifications/preferences", http.StatusMethodNotAllowed, "GET, PUT"},
		{http.MethodPut, "/v1/notifications/events", http.StatusMethodNotAllowed, "GET"},
		{http.MethodGet, "/v1/notifications/outbox", http.StatusNotFound, ""},
	} {
		response := test.do(unsupported.method, unsupported.target, "")
		if response.Code != unsupported.status {
			t.Errorf("%s %s answered %d, want %d",
				unsupported.method, unsupported.target, response.Code, unsupported.status)
		}
		if got := response.Header().Get("Allow"); got != unsupported.allow {
			t.Errorf("%s %s carried Allow %q, want %q",
				unsupported.method, unsupported.target, got, unsupported.allow)
		}
	}
}
