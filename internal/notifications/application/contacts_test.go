package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// memoryContacts is the address book with the database taken out, keyed on the
// same (owner, kind) tuple the PostgreSQL primary key uses -- so a test that
// passes here is not passing because the fake forgot the grain.
type memoryContacts struct {
	rows   map[string]domain.Contact
	readAt error
	saveAt error
}

var _ ports.Contacts = (*memoryContacts)(nil)

func newContacts() *memoryContacts {
	return &memoryContacts{rows: map[string]domain.Contact{}}
}

func contactKey(owner domain.Owner, kind domain.ContactKind) string {
	return string(owner.Kind) + "\x00" + owner.ID + "\x00" + string(kind)
}

func (contacts *memoryContacts) SaveContact(_ context.Context, contact domain.Contact) error {
	if contacts.saveAt != nil {
		return contacts.saveAt
	}
	if err := contact.Validate(); err != nil {
		return err
	}
	contacts.rows[contactKey(contact.Owner, contact.Kind)] = contact
	return nil
}

func (contacts *memoryContacts) ContactsFor(
	_ context.Context, owner domain.Owner,
) ([]domain.Contact, error) {
	if contacts.readAt != nil {
		return nil, contacts.readAt
	}
	held := []domain.Contact{}
	for _, kind := range []domain.ContactKind{
		domain.ContactEmail, domain.ContactPagerDuty, domain.ContactSlack,
	} {
		if contact, found := contacts.rows[contactKey(owner, kind)]; found {
			held = append(held, contact)
		}
	}
	return held, nil
}

func (contacts *memoryContacts) DeleteContact(
	_ context.Context, owner domain.Owner, kind domain.ContactKind,
) (bool, error) {
	key := contactKey(owner, kind)
	if _, found := contacts.rows[key]; !found {
		return false, nil
	}
	delete(contacts.rows, key)
	return true, nil
}

func contactService(t *testing.T, store ports.Contacts, offset time.Duration) *application.Contacts {
	t.Helper()
	service, err := application.NewContacts(store, &fixedClock{now: moment().Add(offset)})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func person() domain.Owner { return domain.Owner{Kind: domain.PrincipalUser, ID: "alice"} }

// Saving the same address twice leaves one contact, keeps the moment it was
// first recorded, and moves the moment it was last changed.
func TestAContactSavedTwiceIsOneContactWithItsOriginalCreation(t *testing.T) {
	t.Parallel()
	store := newContacts()
	first, err := contactService(t, store, 0).Save(t.Context(), domain.Contact{
		Owner: person(), Kind: domain.ContactEmail, Address: "alice@example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != application.ContactID(person(), domain.ContactEmail) {
		t.Errorf("the contact was minted as %q rather than from its (owner, kind)", first.ID)
	}
	if !first.CreatedAt.Equal(moment()) || !first.UpdatedAt.Equal(moment()) {
		t.Errorf("the first save timestamped %s / %s", first.CreatedAt, first.UpdatedAt)
	}
	second, err := contactService(t, store, time.Hour).Save(t.Context(), domain.Contact{
		Owner: person(), Kind: domain.ContactEmail, Address: "alice@elsewhere.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.rows) != 1 {
		t.Fatalf("editing one address left %d contacts", len(store.rows))
	}
	if second.ID != first.ID {
		t.Errorf("the same (owner, kind) minted two identifiers: %q then %q", first.ID, second.ID)
	}
	if !second.CreatedAt.Equal(moment()) {
		t.Errorf("an edit reset the creation time to %s", second.CreatedAt)
	}
	if !second.UpdatedAt.Equal(moment().Add(time.Hour)) {
		t.Errorf("an edit left the update time at %s", second.UpdatedAt)
	}
	if second.Address != "alice@elsewhere.test" {
		t.Errorf("the edit did not take: %q", second.Address)
	}
}

// An identifier derived from an address would carry the address wherever the
// identifier travels, and would change identity when somebody moved house.
func TestTheContactIdentifierIsNotDerivedFromTheAddress(t *testing.T) {
	t.Parallel()
	one := application.ContactID(person(), domain.ContactEmail)
	if one != application.ContactID(person(), domain.ContactEmail) {
		t.Fatal("the same (owner, kind) minted two identifiers")
	}
	if one == application.ContactID(person(), domain.ContactSlack) {
		t.Error("two kinds for one owner mint one identifier, so one would replace the other")
	}
	if one == application.ContactID(domain.Owner{Kind: domain.PrincipalTeam, ID: "alice"}, domain.ContactEmail) {
		t.Error("a user and a team of the same name mint one identifier")
	}
	if strings.Contains(one, "example") {
		t.Errorf("the identifier %q carries something other than the tuple it is derived from", one)
	}
}

func TestAnUnusableContactIsRefusedBeforeItIsStored(t *testing.T) {
	t.Parallel()
	store := newContacts()
	service := contactService(t, store, 0)
	for name, proposed := range map[string]domain.Contact{
		"no owner":     {Kind: domain.ContactEmail, Address: "alice@example.test"},
		"unknown kind": {Owner: person(), Kind: "CARRIER_PIGEON", Address: "alice@example.test"},
		"no address":   {Owner: person(), Kind: domain.ContactEmail},
	} {
		if _, err := service.Save(t.Context(), proposed); err == nil {
			t.Errorf("%s: an unusable contact was accepted", name)
		}
	}
	if len(store.rows) != 0 {
		t.Fatalf("a refused save reached the store: %#v", store.rows)
	}
}

// Deletion is a first-class answer rather than an omission: an address Conduit
// may record and its owner may not withdraw is not a defensible position.
func TestAContactCanBeWithdrawn(t *testing.T) {
	t.Parallel()
	store := newContacts()
	service := contactService(t, store, 0)
	if _, err := service.Save(t.Context(), domain.Contact{
		Owner: person(), Kind: domain.ContactEmail, Address: "alice@example.test",
	}); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.Delete(t.Context(), person(), domain.ContactEmail)
	if err != nil || !deleted {
		t.Fatalf("deleting a stored contact reported deleted=%v err=%v", deleted, err)
	}
	if len(store.rows) != 0 {
		t.Fatalf("the address survived its own deletion: %#v", store.rows)
	}
	// Deleting again is not an error and is not a lie either.
	if deleted, err := service.Delete(t.Context(), person(), domain.ContactEmail); err != nil || deleted {
		t.Fatalf("deleting an absent contact reported deleted=%v err=%v", deleted, err)
	}
	if _, err := service.Delete(t.Context(), person(), "CARRIER_PIGEON"); err == nil {
		t.Error("a kind Apollo does not offer was accepted for deletion")
	}
}

// The resolver the dispatcher reads, over the store the routes write.
func TestTheResolverAnswersFromWhatTheOwnerStored(t *testing.T) {
	t.Parallel()
	store := newContacts()
	service := contactService(t, store, 0)
	recipient := domain.Recipient{Owner: person(), Channel: domain.ChannelEmail}
	if _, found, err := service.DestinationFor(t.Context(), recipient); err != nil || found {
		t.Fatalf("an owner with no contact resolved found=%v err=%v", found, err)
	}
	if _, err := service.Save(t.Context(), domain.Contact{
		Owner: person(), Kind: domain.ContactEmail, Address: "alice@example.test",
	}); err != nil {
		t.Fatal(err)
	}
	destination, found, err := service.DestinationFor(t.Context(), recipient)
	if err != nil || !found {
		t.Fatalf("a stored contact resolved found=%v err=%v", found, err)
	}
	if destination.Address != "alice@example.test" {
		t.Errorf("resolved to %+v", destination)
	}
}

// A store that could not be read is not an owner with no address. Reporting the
// first as the second is how a person's notification ends up at an endpoint
// they never chose, so the error is returned rather than folded into "none".
func TestAnUnreadableAddressBookIsAnErrorRatherThanAnEmptyOne(t *testing.T) {
	t.Parallel()
	store := newContacts()
	store.readAt = errors.New("connection reset by peer")
	service := contactService(t, store, 0)
	destination, found, err := service.DestinationFor(t.Context(),
		domain.Recipient{Owner: person(), Channel: domain.ChannelEmail})
	if err == nil {
		t.Fatal("an unreadable address book resolved to a destination decision")
	}
	if found || destination.Named() {
		t.Errorf("a failed read still produced %+v", destination)
	}
	if _, err := service.List(t.Context(), person()); err == nil {
		t.Error("an unreadable address book listed cleanly")
	}
}

// The list is the owner's own settings page, so it shows what they created --
// including the PagerDuty contact that will never receive anything. A list that
// hid it would be a contact they could not find in order to delete.
func TestTheListShowsEveryContactIncludingTheOneThatNeverDelivers(t *testing.T) {
	t.Parallel()
	store := newContacts()
	service := contactService(t, store, 0)
	team := domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"}
	for kind, address := range map[domain.ContactKind]string{
		domain.ContactEmail:     "platform@example.test",
		domain.ContactPagerDuty: "PD-SERVICE-KEY",
	} {
		if _, err := service.Save(t.Context(), domain.Contact{
			Owner: team, Kind: kind, Address: address,
		}); err != nil {
			t.Fatal(err)
		}
	}
	held, err := service.List(t.Context(), team)
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 2 {
		t.Fatalf("the owner's settings page shows %d of their 2 contacts", len(held))
	}
	if _, err := service.List(t.Context(), domain.Owner{Kind: "SERVICE", ID: "runner"}); err == nil {
		t.Error("an owner that is neither a user nor a team was listed")
	}
}

func TestAContactServiceWithNoStoreIsRefused(t *testing.T) {
	t.Parallel()
	if _, err := application.NewContacts(nil, nil); err == nil {
		t.Fatal("an address book with nowhere to write was constructed")
	}
}
