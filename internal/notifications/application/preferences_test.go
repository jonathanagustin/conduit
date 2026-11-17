package application

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func TestUnavailablePreferenceSelectionIsRefusedWithoutChangingLegacyState(t *testing.T) {
	t.Parallel()
	legacy := domain.Preference{
		ID: "legacy", Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail,
		Types:     []domain.EventType{domain.EventPlanFailures, domain.EventRBACChange},
		CreatedAt: at(), UpdatedAt: at(), Revision: 3,
	}
	store := &fakeStore{preferences: map[string][]domain.Preference{preferenceResource: {legacy}}}
	preferences := newPreferences(t, store)
	for _, types := range [][]domain.EventType{
		{domain.EventRBACChange}, {domain.EventPlanFailures, domain.EventRBACChange},
	} {
		_, err := preferences.Save(t.Context(), domain.Preference{
			Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail, Types: types,
		})
		if err == nil || !strings.Contains(err.Error(), "not yet emitted") {
			t.Fatalf("unavailable selection was not refused: %v", err)
		}
	}
	got, err := preferences.Get(t.Context(), preferenceResource, alice(), domain.ChannelEmail)
	if err != nil || !reflect.DeepEqual(got, legacy) || len(store.saved) != 0 {
		t.Fatalf("refusal changed the saved preference: %#v, %v, writes=%d", got, err, len(store.saved))
	}
	legacy.Types = []domain.EventType{domain.EventPlanFailures}
	if _, err := preferences.Save(t.Context(), legacy); err != nil {
		t.Fatalf("removing the unavailable selection failed: %v", err)
	}
	legacy.Types = []domain.EventType{domain.EventRBACChange}
	legacy.AllFutureTypes = true
	if _, err := preferences.Save(t.Context(), legacy); err != nil {
		t.Fatalf("forward consent with an inactive named selection failed: %v", err)
	}
}

const preferenceResource = "ri.apollo.main.environment.abc"

func alice() domain.Owner { return domain.Owner{Kind: domain.PrincipalUser, ID: "alice"} }

func newPreferences(t *testing.T, store *fakeStore) *Preferences {
	t.Helper()
	preferences, err := NewPreferences(store, fixedAt{})
	if err != nil {
		t.Fatal(err)
	}
	return preferences
}

func TestAPreferenceServiceWithNoStoreIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := NewPreferences(nil, fixedAt{}); err == nil {
		t.Fatal("a preference service with nowhere to write was constructed")
	}
	// A nil clock is the ordinary case, not a misconfiguration: only a test
	// needs to fix the moment.
	if _, err := NewPreferences(&fakeStore{}, nil); err != nil {
		t.Fatalf("a preference service without an injected clock was refused: %v", err)
	}
}

// Absent and "enables nothing" are different states, and only one of them can
// be stored, so a caller rendering a settings page has to be able to tell them
// apart.
func TestAnUnsetPreferenceIsDistinguishableFromAnEmptyOne(t *testing.T) {
	t.Parallel()
	preferences := newPreferences(t, &fakeStore{})
	_, err := preferences.Get(t.Context(), preferenceResource, alice(), domain.ChannelEmail)
	if !errors.Is(err, ErrNoPreference) {
		t.Fatalf("an unset preference answered %v, want ErrNoPreference", err)
	}
}

// The identifier is a function of the tuple, so the same edit sent twice is the
// same row: the store's upsert keys on (owner, resource, channel) and a minted
// identifier would leave two rows on a store that keyed on the identifier.
func TestSavingTheSameTupleTwiceReplacesRatherThanAccumulates(t *testing.T) {
	t.Parallel()
	store := &fakeStore{preferences: map[string][]domain.Preference{}}
	preferences := newPreferences(t, store)
	first, err := preferences.Save(t.Context(), domain.Preference{
		Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail,
		Types: []domain.EventType{domain.EventPlanFailures},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != PreferenceID(preferenceResource, alice(), domain.ChannelEmail) {
		t.Errorf("identifier %q is not the one the tuple produces", first.ID)
	}
	if first.Revision != 1 || first.CreatedAt != at().UTC() || first.UpdatedAt != at().UTC() {
		t.Errorf("first save recorded %#v", first)
	}

	preferences.clock = fixedAt{offset: time.Hour}
	second, err := preferences.Save(t.Context(), domain.Preference{
		Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail,
		AllFutureTypes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Errorf("the same tuple minted two identifiers: %q then %q", first.ID, second.ID)
	}
	if second.Revision != 2 {
		t.Errorf("revision %d after a second save, want 2", second.Revision)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Errorf("an edit moved CreatedAt from %s to %s", first.CreatedAt, second.CreatedAt)
	}
	if !second.UpdatedAt.After(first.UpdatedAt) {
		t.Errorf("an edit left UpdatedAt at %s", second.UpdatedAt)
	}
}

// A caller cannot choose its own identity fields. A client that could set
// CreatedAt could backdate a subscription; one that could set the ID could
// write a second row for a tuple the store means to hold one of.
func TestACallerCannotChooseTheIdentityOfItsOwnPreference(t *testing.T) {
	t.Parallel()
	preferences := newPreferences(t, &fakeStore{preferences: map[string][]domain.Preference{}})
	stored, err := preferences.Save(t.Context(), domain.Preference{
		ID: "npref_chosen-by-the-caller", Owner: alice(), ResourceRID: preferenceResource,
		Channel: domain.ChannelEmail, AllFutureTypes: true,
		CreatedAt: at().Add(-100 * time.Hour), UpdatedAt: at().Add(-100 * time.Hour), Revision: 99,
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID == "npref_chosen-by-the-caller" || stored.Revision != 1 || stored.CreatedAt != at().UTC() {
		t.Fatalf("a caller chose its own identity fields: %#v", stored)
	}
}

func TestAPreferenceThatNamesNoTupleIsRefusedBeforeAnythingIsRead(t *testing.T) {
	t.Parallel()
	store := &fakeStore{preferences: map[string][]domain.Preference{}}
	preferences := newPreferences(t, store)
	for _, incomplete := range []struct {
		name       string
		preference domain.Preference
	}{
		{"no resource", domain.Preference{Owner: alice(), Channel: domain.ChannelEmail, AllFutureTypes: true}},
		{"no owner kind", domain.Preference{
			Owner: domain.Owner{ID: "alice"}, ResourceRID: preferenceResource,
			Channel: domain.ChannelEmail, AllFutureTypes: true,
		}},
		{"no owner id", domain.Preference{
			Owner: domain.Owner{Kind: domain.PrincipalUser}, ResourceRID: preferenceResource,
			Channel: domain.ChannelEmail, AllFutureTypes: true,
		}},
		{"a channel no resource is configured on", domain.Preference{
			Owner: alice(), ResourceRID: preferenceResource, Channel: "PAGERDUTY", AllFutureTypes: true,
		}},
	} {
		t.Run(incomplete.name, func(t *testing.T) {
			if _, err := preferences.Save(t.Context(), incomplete.preference); err == nil {
				t.Fatal("a preference naming no editable tuple was stored")
			}
		})
	}
	if len(store.saved) != 0 {
		t.Fatalf("a refused preference reached the store: %#v", store.saved)
	}
}

// The domain still judges what a preference means. A team preference with no
// contact delivers nowhere, and the service does not get to wave that through
// on the grounds that the tuple was well formed.
func TestThePreferenceServiceDoesNotOverruleTheDomain(t *testing.T) {
	t.Parallel()
	store := &fakeStore{preferences: map[string][]domain.Preference{}}
	preferences := newPreferences(t, store)
	if _, err := preferences.Save(t.Context(), domain.Preference{
		Owner:       domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"},
		ResourceRID: preferenceResource, Channel: domain.ChannelEmail, AllFutureTypes: true,
	}); err == nil {
		t.Fatal("a team preference with no contact was stored")
	}
	if _, err := preferences.Save(t.Context(), domain.Preference{
		Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail,
	}); err == nil {
		t.Fatal("a preference enabling nothing and opting into nothing was stored")
	}
	if len(store.saved) != 0 {
		t.Fatalf("a refused preference reached the store: %#v", store.saved)
	}
}

// Two owners on one resource and one channel are two preferences. A Get that
// matched on the resource alone would hand one owner another's settings, which
// is the same failure the HTTP surface refuses at the door.
func TestGetSelectsTheOwnerAndChannelAsked(t *testing.T) {
	t.Parallel()
	bob := domain.Owner{Kind: domain.PrincipalUser, ID: "bob"}
	store := &fakeStore{preferences: map[string][]domain.Preference{
		preferenceResource: {
			{
				ID: "a", Owner: alice(), ResourceRID: preferenceResource,
				Channel: domain.ChannelEmail, AllFutureTypes: true,
			},
			{
				ID: "b", Owner: bob, ResourceRID: preferenceResource,
				Channel: domain.ChannelEmail, Types: []domain.EventType{domain.EventPlanFailures},
			},
			{
				ID: "c", Owner: alice(), ResourceRID: preferenceResource,
				Channel: domain.ChannelInApp, Types: []domain.EventType{domain.EventRoleChange},
			},
		},
	}}
	preferences := newPreferences(t, store)
	for _, want := range []struct {
		owner   domain.Owner
		channel domain.Channel
		id      string
	}{
		{alice(), domain.ChannelEmail, "a"},
		{bob, domain.ChannelEmail, "b"},
		{alice(), domain.ChannelInApp, "c"},
	} {
		got, err := preferences.Get(t.Context(), preferenceResource, want.owner, want.channel)
		if err != nil {
			t.Fatalf("%s on %s: %v", want.owner.ID, want.channel, err)
		}
		if got.ID != want.id {
			t.Errorf("%s on %s returned %q, want %q", want.owner.ID, want.channel, got.ID, want.id)
		}
	}
}

// A store that cannot answer is not an owner with no preference. Collapsing
// the two would make a database outage look like a settings page nobody had
// opened, and the fix for each is the opposite of the other.
func TestAStoreFailureIsNotAnAbsentPreference(t *testing.T) {
	t.Parallel()
	failure := errors.New("connection refused")
	preferences := newPreferences(t, &fakeStore{preferencesErr: failure})
	_, err := preferences.Get(t.Context(), preferenceResource, alice(), domain.ChannelEmail)
	if errors.Is(err, ErrNoPreference) || !errors.Is(err, failure) {
		t.Fatalf("a broken store answered %v", err)
	}
	if _, err := preferences.Save(t.Context(), domain.Preference{
		Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail, AllFutureTypes: true,
	}); !errors.Is(err, failure) {
		t.Fatalf("a save over a broken store answered %v", err)
	}
}

func TestPreferenceIDSeparatesTupleFieldsThatWouldOtherwiseRunTogether(t *testing.T) {
	t.Parallel()
	// "ab" + "c" and "a" + "bc" are the same bytes concatenated and different
	// tuples, so an identifier that joined without a separator would give one
	// owner another's row.
	left := PreferenceID("c", domain.Owner{Kind: domain.PrincipalUser, ID: "ab"}, domain.ChannelEmail)
	right := PreferenceID("bc", domain.Owner{Kind: domain.PrincipalUser, ID: "a"}, domain.ChannelEmail)
	if left == right {
		t.Fatal("two different tuples produce one identifier")
	}
	if PreferenceID("c", domain.Owner{Kind: domain.PrincipalTeam, ID: "ab"}, domain.ChannelEmail) == left {
		t.Fatal("a USER and a TEAM of the same name share an identifier")
	}
	if PreferenceID("c", domain.Owner{Kind: domain.PrincipalUser, ID: "ab"}, domain.ChannelInApp) == left {
		t.Fatal("two channels share an identifier")
	}
}
