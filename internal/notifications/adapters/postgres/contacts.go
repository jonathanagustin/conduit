package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

var _ ports.Contacts = (*Store)(nil)

// SaveContact stores or replaces one owner's destination of one kind.
//
// Keyed on (owner, kind) rather than on the contact's own identifier, for the
// reason SavePreference is keyed on its tuple: that is what the dialog edits,
// and an ID-keyed upsert would leave two rows the moment a caller minted a
// fresh identifier for the same edit. The identifier is derived from the same
// tuple by application.ContactID, so it is stable across an edit anyway and the
// two keys can never disagree.
//
// created_at is left alone on an update. The application layer already carries
// the original forward, and the guard here means a caller that did not is
// corrected by the database rather than silently resetting when somebody
// changed their address.
func (store *Store) SaveContact(ctx context.Context, contact domain.Contact) error {
	if store == nil || store.database == nil {
		return errors.New("notification store is closed")
	}
	if err := contact.Validate(); err != nil {
		return fmt.Errorf("refuse notification contact: %w", err)
	}
	updatedAt := contact.UpdatedAt
	if updatedAt.IsZero() {
		updatedAt = contact.CreatedAt
	}
	if _, err := store.database.ExecContext(ctx, `
INSERT INTO conduit_notification_contacts
    (owner_kind, owner_id, kind, id, address, opt_into_future_resources, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (owner_kind, owner_id, kind) DO UPDATE SET
    id = EXCLUDED.id,
    address = EXCLUDED.address,
    opt_into_future_resources = EXCLUDED.opt_into_future_resources,
    updated_at = EXCLUDED.updated_at`,
		string(contact.Owner.Kind), contact.Owner.ID, string(contact.Kind), contact.ID,
		contact.Address, contact.OptIntoFutureResources,
		contact.CreatedAt.UTC(), updatedAt.UTC()); err != nil {
		// The failure names the owner and the kind and never the address. A
		// unique-violation message from the driver would quote the key, which is
		// why the key is the tuple rather than anything derived from what it
		// holds.
		return fmt.Errorf("save the %s notification contact for %s %s: %w",
			contact.Kind, contact.Owner.Kind, contact.Owner.ID, err)
	}
	return nil
}

// ContactsFor returns every contact one owner holds, ordered by kind so two
// reads of an unchanged table resolve to the same destination.
func (store *Store) ContactsFor(
	ctx context.Context, owner domain.Owner,
) ([]domain.Contact, error) {
	if store == nil || store.database == nil {
		return nil, errors.New("notification store is closed")
	}
	if owner.Kind == "" || owner.ID == "" {
		return nil, errors.New("reading notification contacts requires the owner they belong to")
	}
	rows, err := store.database.QueryContext(ctx, `
SELECT id, kind, address, opt_into_future_resources, created_at, updated_at
FROM conduit_notification_contacts
WHERE owner_kind = $1 AND owner_id = $2
ORDER BY kind`, string(owner.Kind), owner.ID)
	if err != nil {
		return nil, fmt.Errorf("read notification contacts for %s %s: %w", owner.Kind, owner.ID, err)
	}
	defer rows.Close()
	contacts := []domain.Contact{}
	for rows.Next() {
		contact := domain.Contact{Owner: owner}
		if err := rows.Scan(&contact.ID, &contact.Kind, &contact.Address,
			&contact.OptIntoFutureResources, &contact.CreatedAt, &contact.UpdatedAt); err != nil {
			return nil, fmt.Errorf("read notification contacts for %s %s: %w", owner.Kind, owner.ID, err)
		}
		contact.CreatedAt = contact.CreatedAt.UTC()
		contact.UpdatedAt = contact.UpdatedAt.UTC()
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read notification contacts for %s %s: %w", owner.Kind, owner.ID, err)
	}
	return contacts, nil
}

// DeleteContact removes one owner's destination of one kind, reporting whether
// there was one to remove.
//
// A DELETE rather than a tombstone column, and that is the point of it: the
// row holds an address, and "deleted" has to mean the address is gone rather
// than hidden. The inbox entries it delivered are untouched, because they are
// the record that somebody was told, and they hold no address.
func (store *Store) DeleteContact(
	ctx context.Context, owner domain.Owner, kind domain.ContactKind,
) (bool, error) {
	if store == nil || store.database == nil {
		return false, errors.New("notification store is closed")
	}
	if owner.Kind == "" || owner.ID == "" {
		return false, errors.New("deleting a notification contact requires the owner it belongs to")
	}
	result, err := store.database.ExecContext(ctx, `
DELETE FROM conduit_notification_contacts
WHERE owner_kind = $1 AND owner_id = $2 AND kind = $3`,
		string(owner.Kind), owner.ID, string(kind))
	if err != nil {
		return false, fmt.Errorf("delete the %s notification contact for %s %s: %w",
			kind, owner.Kind, owner.ID, err)
	}
	return affected(result)
}
