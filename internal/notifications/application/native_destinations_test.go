package application

import (
	"context"
	"errors"
	"testing"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

type selectedContactStore struct {
	preferences                      []domain.Preference
	contacts                         []domain.Contact
	preferenceFailure, errorContacts error
	owner                            domain.Owner
	resource                         string
}

func (*selectedContactStore) SaveContact(context.Context, domain.Contact) error { return nil }
func (store *selectedContactStore) ContactsFor(_ context.Context, owner domain.Owner) ([]domain.Contact, error) {
	store.owner = owner
	return store.contacts, store.errorContacts
}
func (*selectedContactStore) DeleteContact(context.Context, domain.Owner, domain.ContactKind) (bool, error) {
	return false, nil
}
func (store *selectedContactStore) PreferencesFor(_ context.Context, resource string) ([]domain.Preference, error) {
	store.resource = resource
	return store.preferences, store.preferenceFailure
}

func TestNativeResolutionHonorsTheSelectedContactAndItsOwnerAndChannel(t *testing.T) {
	owner := domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"}
	selected := domain.Preference{ID: "preference", Owner: owner, ResourceRID: "resource", Channel: domain.ChannelSlack, ContactID: "selected"}
	store := &selectedContactStore{preferences: []domain.Preference{selected}, contacts: []domain.Contact{
		{ID: "selected", Owner: owner, Kind: domain.ContactSlack, Address: "C-selected"},
		{ID: "email", Owner: owner, Kind: domain.ContactEmail, Address: "mail@example.test"},
		{ID: "foreign", Owner: domain.Owner{Kind: domain.PrincipalTeam, ID: "another"}, Kind: domain.ContactSlack, Address: "C-foreign"},
	}}
	resolver, err := NewNativeDestinations(store, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := domain.InboxEntry{Recipient: domain.Recipient{Owner: owner, Channel: domain.ChannelSlack}, Event: domain.Event{ResourceRID: "resource"}, PreferenceID: "preference"}
	destination, found, err := resolver.DestinationForEntry(t.Context(), entry)
	if err != nil || !found || destination.Address != "C-selected" || store.owner != owner || store.resource != "resource" {
		t.Fatal("selected native contact was not scoped correctly")
	}
	for _, id := range []string{"foreign", "email", "missing", ""} {
		store.preferences[0].ContactID = id
		if _, found, err := resolver.DestinationForEntry(t.Context(), entry); err != nil || found {
			t.Fatalf("invalid selected contact %q resolved: found=%v, error=%v", id, found, err)
		}
	}
	store.preferences[0] = selected
	for _, mutate := range []func(*domain.InboxEntry){
		func(entry *domain.InboxEntry) { entry.PreferenceID = "other-preference" },
		func(entry *domain.InboxEntry) { entry.Recipient.Owner.ID = "another" },
		func(entry *domain.InboxEntry) { entry.Recipient.Channel = domain.ChannelEmail },
		func(entry *domain.InboxEntry) { entry.Event.ResourceRID = "another-resource" },
	} {
		changed := entry
		mutate(&changed)
		if _, found, err := resolver.DestinationForEntry(t.Context(), changed); err != nil || found {
			t.Fatal("another preference/owner/resource/channel was used")
		}
	}
}

func TestNativeDestinationReadFailureDoesNotBecomeAnAddressBookFallback(t *testing.T) {
	failure := errors.New("store unavailable")
	store := &selectedContactStore{preferenceFailure: failure}
	resolver, err := NewNativeDestinations(store, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := domain.InboxEntry{Recipient: domain.Recipient{Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: domain.ChannelEmail}, Event: domain.Event{ResourceRID: "resource"}}
	destination, found, err := resolver.DestinationForEntry(t.Context(), entry)
	if !errors.Is(err, failure) || found || destination.Named() {
		t.Fatal("a failed selection read became a destination")
	}
}

func TestNativeSelectedContactReadFailureIsRetriable(t *testing.T) {
	failure := errors.New("contact store unavailable")
	owner := domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"}
	store := &selectedContactStore{preferences: []domain.Preference{{ID: "preference", Owner: owner, ResourceRID: "resource", Channel: domain.ChannelSlack, ContactID: "selected"}}, errorContacts: failure}
	resolver, err := NewNativeDestinations(store, store, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := domain.InboxEntry{Recipient: domain.Recipient{Owner: owner, Channel: domain.ChannelSlack}, Event: domain.Event{ResourceRID: "resource"}, PreferenceID: "preference"}
	destination, found, err := resolver.DestinationForEntry(t.Context(), entry)
	if !errors.Is(err, failure) || found || destination.Named() {
		t.Fatal("failed contact read became a destination")
	}
}
