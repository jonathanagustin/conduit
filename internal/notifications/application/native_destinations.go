package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// NativeDestinations honors a preference's selected contact while retaining
// strict owner/channel resolution for user preferences without a contact ID.
type NativeDestinations struct {
	*Contacts
	preferences ports.PreferenceReader
}

func NewNativeDestinations(store ports.Contacts, preferences ports.PreferenceReader, clock ports.Clock) (*NativeDestinations, error) {
	if preferences == nil {
		return nil, errors.New("native notification delivery requires a preference reader")
	}
	contacts, err := NewChannelContacts(store, clock)
	if err != nil {
		return nil, err
	}
	return &NativeDestinations{Contacts: contacts, preferences: preferences}, nil
}

func (resolver *NativeDestinations) DestinationForEntry(ctx context.Context, entry domain.InboxEntry) (domain.Destination, bool, error) {
	if err := validateOwner(entry.Recipient.Owner); err != nil {
		return domain.Destination{}, false, err
	}
	preferences, err := resolver.preferences.PreferencesFor(ctx, entry.Event.ResourceRID)
	if err != nil {
		return domain.Destination{}, false, fmt.Errorf("read notification delivery preference: %w", err)
	}
	var selected *domain.Preference
	for index := range preferences {
		candidate := &preferences[index]
		if candidate.ID == entry.PreferenceID && candidate.ResourceRID == entry.Event.ResourceRID && candidate.Owner == entry.Recipient.Owner && candidate.Channel == entry.Recipient.Channel {
			selected = candidate
			break
		}
	}
	if selected == nil {
		return domain.Destination{}, false, nil
	}
	if selected.ContactID == "" {
		if entry.Recipient.Owner.Kind == domain.PrincipalTeam {
			return domain.Destination{}, false, nil
		}
		return resolver.Contacts.DestinationFor(ctx, entry.Recipient)
	}
	contacts, err := resolver.store.ContactsFor(ctx, entry.Recipient.Owner)
	if err != nil {
		return domain.Destination{}, false, fmt.Errorf("read selected notification contact: %w", err)
	}
	for _, contact := range contacts {
		if contact.ID == selected.ContactID && contact.Owner == entry.Recipient.Owner && string(contact.Kind) == string(entry.Recipient.Channel) && contact.Kind.Deliverable() && contact.Address != "" {
			return domain.Destination{Kind: contact.Kind, Address: contact.Address}, true, nil
		}
	}
	return domain.Destination{}, false, nil
}
