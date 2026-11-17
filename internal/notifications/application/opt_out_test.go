package application

import (
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func TestOptOutKeepsTheTupleAndHistoryAndStopsNewNotifications(t *testing.T) {
	store := &fakeStore{}
	preferences := newPreferences(t, store)
	first, err := preferences.Save(t.Context(), domain.Preference{
		Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail,
		Types: []domain.EventType{domain.EventPlanFailures}, ContactID: "chosen-contact",
	})
	if err != nil {
		t.Fatal(err)
	}
	emitter, err := NewEmitter(store, allowRecipients{})
	if err != nil {
		t.Fatal(err)
	}
	emit := func(id string) {
		t.Helper()
		event := planFailed()
		event.ID, event.ResourceRID = id, preferenceResource
		if err := emitter.Emit(t.Context(), event); err != nil {
			t.Fatal(err)
		}
	}
	emit("before-off")
	preferences.clock = fixedAt{offset: time.Hour}
	off, err := preferences.Save(t.Context(), domain.Preference{
		Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail,
		Types: []domain.EventType{}, AllFutureTypes: false, ContactID: first.ContactID,
	})
	if err != nil {
		t.Fatalf("opt-out was refused: %v", err)
	}
	loaded, err := preferences.Get(t.Context(), preferenceResource, alice(), domain.ChannelEmail)
	if err != nil || loaded.ID != first.ID || loaded.Revision != first.Revision+1 || loaded.CreatedAt != first.CreatedAt || !loaded.UpdatedAt.After(first.UpdatedAt) || loaded.AllFutureTypes || len(loaded.Types) != 0 || loaded.ContactID != first.ContactID {
		t.Fatalf("off consent lost tuple, metadata or chosen contact: %#v %v", loaded, err)
	}
	emit("after-off")
	if len(store.events) != 2 || len(store.entries) != 1 || store.entries[0].Event.ID != "before-off" {
		t.Fatalf("off consent deleted history or received a new event: %#v %#v", store.events, store.entries)
	}
	preferences.clock = fixedAt{offset: 2 * time.Hour}
	on, err := preferences.Save(t.Context(), domain.Preference{
		Owner: alice(), ResourceRID: preferenceResource, Channel: domain.ChannelEmail,
		Types: []domain.EventType{domain.EventPlanFailures}, ContactID: loaded.ContactID,
	})
	if err != nil || on.ID != off.ID || on.Revision != off.Revision+1 {
		t.Fatalf("resubscribe = %#v, %v", on, err)
	}
	emit("after-resubscribe")
	if len(store.entries) != 2 || store.entries[1].Event.ID != "after-resubscribe" {
		t.Fatalf("resubscribe did not affect the next new event: %#v", store.entries)
	}
}

func TestATeamCanOptOutAfterItsOnlyLegacySelectionOrContactDisappears(t *testing.T) {
	for _, contact := range []string{"", "deleted-contact"} {
		legacy := domain.Preference{
			ID:    PreferenceID(preferenceResource, domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"}, domain.ChannelEmail),
			Owner: domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"}, ResourceRID: preferenceResource,
			Channel: domain.ChannelEmail, Types: []domain.EventType{domain.EventRBACChange},
			ContactID: contact, Revision: 4, CreatedAt: at(), UpdatedAt: at(),
		}
		store := &fakeStore{preferences: map[string][]domain.Preference{preferenceResource: {legacy}}}
		preferences := newPreferences(t, store)
		legacy.Types = nil
		off, err := preferences.Save(t.Context(), legacy)
		if err != nil || off.ID != legacy.ID || off.Revision != 5 || off.ContactID != contact {
			t.Fatalf("team opt-out with contact %q = %#v, %v", contact, off, err)
		}
	}
}
