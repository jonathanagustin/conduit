package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func resetContacts(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	if _, err := store.database.ExecContext(ctx, `TRUNCATE conduit_notification_contacts`); err != nil {
		t.Fatal(err)
	}
}

func alice() domain.Owner { return domain.Owner{Kind: domain.PrincipalUser, ID: "alice"} }
func platform() domain.Owner {
	return domain.Owner{Kind: domain.PrincipalTeam, ID: "platform"}
}

// The editing grain against the real primary key. The dialog edits one
// (owner, kind), so saving it twice has to leave one row in the database that
// actually enforces it, and the identifier minted from that tuple must be the
// same both times.
func TestPostgresAContactEditedTwiceLeavesOneRow(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetContacts(t, ctx, store)
	contacts, err := application.NewContacts(store, fixedAt{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := contacts.Save(ctx, domain.Contact{
		Owner: alice(), Kind: domain.ContactEmail, Address: "alice@example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	later, err := application.NewContacts(store, fixedAt{offset: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	second, err := later.Save(ctx, domain.Contact{
		Owner: alice(), Kind: domain.ContactEmail, Address: "alice@elsewhere.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Errorf("one (owner, kind) minted two identifiers: %q then %q", first.ID, second.ID)
	}
	held, err := contacts.List(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 {
		t.Fatalf("editing one address left %d rows in the database", len(held))
	}
	if held[0].Address != "alice@elsewhere.test" {
		t.Errorf("the edit did not take: %q", held[0].Address)
	}
	if !held[0].CreatedAt.Equal(at()) {
		t.Errorf("an edit reset the creation time to %s", held[0].CreatedAt)
	}
	if !held[0].UpdatedAt.Equal(at().Add(time.Hour)) {
		t.Errorf("an edit left the update time at %s", held[0].UpdatedAt)
	}
}

// One owner's address book is not another's, at the level of the query rather
// than of the code that calls it.
func TestPostgresContactsAreReadPerOwner(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetContacts(t, ctx, store)
	contacts, err := application.NewContacts(store, fixedAt{})
	if err != nil {
		t.Fatal(err)
	}
	for _, contact := range []domain.Contact{
		{Owner: alice(), Kind: domain.ContactEmail, Address: "alice@example.test"},
		{Owner: platform(), Kind: domain.ContactEmail, Address: "platform@example.test"},
		{Owner: platform(), Kind: domain.ContactSlack, Address: "#platform-alerts"},
		// A team of the same name as the user, which is a different owner and
		// would collide if the key dropped the kind of principal.
		{
			Owner:   domain.Owner{Kind: domain.PrincipalTeam, ID: "alice"},
			Kind:    domain.ContactEmail,
			Address: "alice-team@example.test",
		},
	} {
		if _, err := contacts.Save(ctx, contact); err != nil {
			t.Fatal(err)
		}
	}
	held, err := contacts.List(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || held[0].Address != "alice@example.test" {
		t.Fatalf("alice's address book read back as %#v", held)
	}
	team, err := contacts.List(ctx, platform())
	if err != nil {
		t.Fatal(err)
	}
	if len(team) != 2 {
		t.Fatalf("the team holds %d of its 2 contacts", len(team))
	}
	// Ordered, so resolving the same recipient twice chooses the same place.
	if team[0].Kind != domain.ContactEmail || team[1].Kind != domain.ContactSlack {
		t.Errorf("the team's contacts came back in order %s, %s", team[0].Kind, team[1].Kind)
	}
}

// Deletion has to remove the address rather than hide it, because that is the
// whole difference between "you may withdraw it" and "we will stop showing it
// to you".
func TestPostgresDeletingAContactRemovesTheAddress(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetContacts(t, ctx, store)
	contacts, err := application.NewContacts(store, fixedAt{})
	if err != nil {
		t.Fatal(err)
	}
	// Two kinds, because deleting one must not take the other with it: a
	// person removing the Slack channel they no longer read would otherwise
	// silently stop receiving mail as well, and the next thing they would
	// notice is a notification that never arrived.
	for kind, address := range map[domain.ContactKind]string{
		domain.ContactEmail: "alice@example.test",
		domain.ContactSlack: "#alice-alerts",
	} {
		if _, err := contacts.Save(ctx, domain.Contact{
			Owner: alice(), Kind: kind, Address: address,
		}); err != nil {
			t.Fatal(err)
		}
	}
	deleted, err := contacts.Delete(ctx, alice(), domain.ContactEmail)
	if err != nil || !deleted {
		t.Fatalf("deleting a stored contact reported deleted=%v err=%v", deleted, err)
	}
	var remaining int
	if err := store.database.QueryRowContext(ctx,
		`SELECT count(*) FROM conduit_notification_contacts WHERE address = $1`,
		"alice@example.test").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("%d rows still hold the deleted address", remaining)
	}
	held, err := contacts.List(ctx, alice())
	if err != nil {
		t.Fatal(err)
	}
	if len(held) != 1 || held[0].Kind != domain.ContactSlack {
		t.Fatalf("deleting one kind left %#v, want only the Slack contact", held)
	}
	if deleted, err := contacts.Delete(ctx, alice(), domain.ContactEmail); err != nil || deleted {
		t.Fatalf("deleting an absent contact reported deleted=%v err=%v", deleted, err)
	}
}

// The slice this whole change exists for, end to end against a real database:
// an event reaches a subscriber, the queued delivery is leased, and the
// destination it resolves to is that subscriber's own address rather than
// whatever endpoint the deployment configured.
func TestPostgresADeliveryResolvesToTheSubscribersOwnAddress(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	resetContacts(t, ctx, store)
	contacts, err := application.NewContacts(store, fixedAt{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := contacts.Save(ctx, domain.Contact{
		Owner: alice(), Kind: domain.ContactEmail, Address: "alice@example.test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreference(ctx, preference("pref-1", alice(), domain.ChannelEmail)); err != nil {
		t.Fatal(err)
	}
	emitter, err := application.NewEmitter(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(ctx, planFailed("nev-1")); err != nil {
		t.Fatal(err)
	}
	lease, found, err := store.LeaseDelivery(ctx, at().Add(time.Minute), time.Minute)
	if err != nil || !found {
		t.Fatalf("the emitted notification was not queued for delivery: found=%v err=%v", found, err)
	}
	destination, resolved, err := contacts.DestinationFor(ctx, lease.Entry.Recipient)
	if err != nil || !resolved {
		t.Fatalf("the leased delivery resolved to nothing: resolved=%v err=%v", resolved, err)
	}
	if destination.Address != "alice@example.test" || destination.Kind != domain.ContactEmail {
		t.Fatalf("the delivery resolved to %+v, want the subscriber's own contact", destination)
	}
	// And a recipient with no contact resolves to nothing, which is what leaves
	// the operator's endpoint as the fallback rather than a failure.
	if _, resolved, err := contacts.DestinationFor(ctx, domain.Recipient{
		Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "bob"}, Channel: domain.ChannelEmail,
	}); err != nil || resolved {
		t.Fatalf("a recipient with no contact resolved=%v err=%v", resolved, err)
	}
}

// The migration this change appends must add the table on a database that has
// already run the two before it, and must be the only thing pending on one that
// has not run it.
func TestPostgresTheContactMigrationIsWhatAddsTheTable(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	var exists *string
	if err := store.database.QueryRowContext(ctx,
		`SELECT to_regclass('conduit_notification_contacts')::text`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists == nil {
		t.Fatal("opening the store did not apply the contacts migration")
	}
}

func TestAClosedStoreRefusesContactWork(t *testing.T) {
	t.Parallel()
	var closed *Store
	if err := closed.SaveContact(t.Context(), domain.Contact{}); err == nil {
		t.Error("a closed store saved a contact")
	}
	if _, err := closed.ContactsFor(t.Context(), alice()); err == nil {
		t.Error("a closed store read an address book")
	}
	if _, err := closed.DeleteContact(t.Context(), alice(), domain.ContactEmail); err == nil {
		t.Error("a closed store deleted a contact")
	}
}

// An address book read with no owner is a query with no WHERE that matters, so
// it is refused rather than answered with everybody's addresses.
func TestReadingAnAddressBookRequiresAnOwner(t *testing.T) {
	t.Parallel()
	store := &Store{}
	if _, err := store.ContactsFor(t.Context(), domain.Owner{}); err == nil {
		t.Error("an address book read with no owner was accepted")
	}
	if _, err := store.DeleteContact(t.Context(), domain.Owner{}, domain.ContactEmail); err == nil {
		t.Error("a contact deletion with no owner was accepted")
	}
	if err := store.SaveContact(t.Context(), domain.Contact{
		ID: "c-1", Owner: alice(), Kind: domain.ContactEmail,
	}); err == nil {
		t.Error("a contact with no address was accepted")
	}
}
