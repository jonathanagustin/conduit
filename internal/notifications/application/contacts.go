package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// Contacts reads and edits the address book at the grain the store keys on:
// one destination per (owner, kind).
//
// It is the service the HTTP surface calls and the resolver the delivery
// dispatcher reads, and those are deliberately the same object seen through two
// interfaces. One place decides what a contact is worth storing, so a
// destination that could not be saved through the routes cannot appear at
// delivery time either.
type Contacts struct {
	store ports.Contacts
	clock ports.Clock
}

var _ ports.DestinationResolver = (*Contacts)(nil)

// NewContacts refuses a service with nowhere to write. The clock is optional
// and defaults to the system one, exactly as NewPreferences' does; only a test
// needs to fix the moment.
func NewContacts(store ports.Contacts, clock ports.Clock) (*Contacts, error) {
	if store == nil {
		return nil, errors.New("a notification contact store is required; an address book with nowhere to write stores nothing")
	}
	if clock == nil {
		clock = SystemClock{}
	}
	return &Contacts{store: store, clock: clock}, nil
}

// List returns every contact one owner holds.
//
// It does not filter out the undeliverable ones. A PagerDuty contact that
// vanished from the owner's own settings page would be a contact they cannot
// delete, and Apollo's dialog creates one; ResolveDestination is where
// undeliverable means anything.
func (contacts *Contacts) List(ctx context.Context, owner domain.Owner) ([]domain.Contact, error) {
	if err := validateOwner(owner); err != nil {
		return nil, err
	}
	held, err := contacts.store.ContactsFor(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("read notification contacts for %s %s: %w", owner.Kind, owner.ID, err)
	}
	return held, nil
}

// Save stores one owner's destination of one kind, replacing whatever they had
// of that kind.
//
// The identifier and the timestamps are minted here rather than accepted, on
// the same reasoning Preferences.Save mints its own: a caller that chose the ID
// could write two rows for one (owner, kind) against a store keyed differently,
// and a caller that chose CreatedAt could backdate somebody else's consent to
// being contacted.
//
// CreatedAt is preserved across an edit and UpdatedAt moves, which is what
// makes "this address changed at 03:00" answerable without an audit trail this
// context does not have.
func (contacts *Contacts) Save(
	ctx context.Context, proposed domain.Contact,
) (domain.Contact, error) {
	if err := validateOwner(proposed.Owner); err != nil {
		return domain.Contact{}, err
	}
	if !proposed.Kind.Valid() {
		return domain.Contact{}, fmt.Errorf(
			"notification contact kind %q is not one Apollo offers; use EMAIL, SLACK or PAGERDUTY", proposed.Kind)
	}
	now := contacts.clock.Now().UTC()
	stored := domain.Contact{
		ID:                     ContactID(proposed.Owner, proposed.Kind),
		Owner:                  proposed.Owner,
		Kind:                   proposed.Kind,
		Address:                proposed.Address,
		OptIntoFutureResources: proposed.OptIntoFutureResources,
		CreatedAt:              now,
		UpdatedAt:              now,
	}
	existing, found, err := contacts.contact(ctx, proposed.Owner, proposed.Kind)
	if err != nil {
		return domain.Contact{}, err
	}
	if found {
		stored.CreatedAt = existing.CreatedAt
	}
	if err := stored.Validate(); err != nil {
		return domain.Contact{}, err
	}
	if err := contacts.store.SaveContact(ctx, stored); err != nil {
		return domain.Contact{}, err
	}
	return stored, nil
}

// Delete removes one owner's destination of one kind and reports whether there
// was one.
func (contacts *Contacts) Delete(
	ctx context.Context, owner domain.Owner, kind domain.ContactKind,
) (bool, error) {
	if err := validateOwner(owner); err != nil {
		return false, err
	}
	if !kind.Valid() {
		return false, fmt.Errorf(
			"notification contact kind %q is not one Apollo offers; use EMAIL, SLACK or PAGERDUTY", kind)
	}
	deleted, err := contacts.store.DeleteContact(ctx, owner, kind)
	if err != nil {
		return false, fmt.Errorf("delete the %s notification contact for %s %s: %w",
			kind, owner.Kind, owner.ID, err)
	}
	return deleted, nil
}

// DestinationFor answers where a recipient's own contacts point, so the
// dispatcher can target the person rather than one endpoint.
//
// The rule is the domain's -- this reads the contacts and hands them over. The
// error path returns no destination *and* an error, and the difference between
// that and (Destination{}, false, nil) is the one the dispatcher has to honour:
// the first means "the address book could not be read", which must not be
// treated as "this person has no address" and quietly delivered elsewhere.
func (contacts *Contacts) DestinationFor(
	ctx context.Context, recipient domain.Recipient,
) (domain.Destination, bool, error) {
	if err := validateOwner(recipient.Owner); err != nil {
		return domain.Destination{}, false, err
	}
	held, err := contacts.store.ContactsFor(ctx, recipient.Owner)
	if err != nil {
		return domain.Destination{}, false, fmt.Errorf(
			"read notification contacts for %s %s: %w", recipient.Owner.Kind, recipient.Owner.ID, err)
	}
	destination, found := domain.ResolveDestination(recipient, held)
	return destination, found, nil
}

// contact reads one owner's contact of one kind out of the set they hold.
//
// Read through ContactsFor rather than through a second store method, so the
// port stays the three operations an address book needs. The sets involved are
// at most one per kind.
func (contacts *Contacts) contact(
	ctx context.Context, owner domain.Owner, kind domain.ContactKind,
) (domain.Contact, bool, error) {
	held, err := contacts.store.ContactsFor(ctx, owner)
	if err != nil {
		return domain.Contact{}, false, fmt.Errorf(
			"read notification contacts for %s %s: %w", owner.Kind, owner.ID, err)
	}
	for _, contact := range held {
		if contact.Kind == kind {
			return contact, true, nil
		}
	}
	return domain.Contact{}, false, nil
}

// ContactID is the identifier the (owner, kind) pair always produces.
//
// A digest rather than a counter, for the reason PreferenceID is one: every
// replica has to mint the same value without storing it, and the same edit sent
// twice has to be the same contact rather than two. The input is the owner and
// the kind and deliberately not the address -- an identifier derived from an
// address would leak it wherever the identifier travels, and changing an
// address would change the identity of the contact holding it.
func ContactID(owner domain.Owner, kind domain.ContactKind) string {
	digest := sha256.Sum256([]byte(
		string(owner.Kind) + "\x00" + owner.ID + "\x00" + string(kind)))
	return "ncontact_" + hex.EncodeToString(digest[:12])
}

// validateOwner refuses a request that does not name one address book.
func validateOwner(owner domain.Owner) error {
	if owner.Kind != domain.PrincipalUser && owner.Kind != domain.PrincipalTeam {
		return errors.New("a notification contact belongs to a USER or a TEAM")
	}
	if owner.ID == "" {
		return errors.New("a notification contact requires the owner it belongs to")
	}
	return nil
}
