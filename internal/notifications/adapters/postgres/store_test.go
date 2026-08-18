package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
)

const resource = "ri.apollo.main.environment.d41d8cd98f00"

func at() time.Time { return time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC) }

// The slice end to end against a real database: a preference is stored, an
// event is emitted, and the person who asked can read it back.
func TestPostgresNotificationsReachTheSubscriberWhoAskedForThem(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	emitter, err := application.NewEmitter(store)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := application.NewReader(store)
	if err != nil {
		t.Fatal(err)
	}
	alice := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	if err := store.SavePreference(ctx, preference("pref-1", alice, domain.ChannelEmail)); err != nil {
		t.Fatal(err)
	}
	// A second owner on a resource nobody raised an event about, so a fan-out
	// that ignored the resource join would show up as an extra entry.
	if err := store.SavePreference(ctx, domain.Preference{
		ID: "pref-2", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "bob"},
		ResourceRID: "ri.apollo.main.environment.other", Channel: domain.ChannelEmail,
		AllFutureTypes: true, CreatedAt: at(), UpdatedAt: at(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(ctx, planFailed("nev-1")); err != nil {
		t.Fatal(err)
	}
	entries, err := reader.Inbox(ctx, domain.Recipient{Owner: alice, Channel: domain.ChannelEmail}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the subscriber's inbox holds %d entries", len(entries))
	}
	if entries[0].Event.Summary != planFailed("nev-1").Summary || entries[0].PreferenceID != "pref-1" {
		t.Errorf("the entry lost the event or the preference behind it: %#v", entries[0])
	}
	if !entries[0].Event.OccurredAt.Equal(at()) {
		t.Errorf("the event moved in time through the database: %s", entries[0].Event.OccurredAt)
	}
	bobsInbox, err := reader.Inbox(ctx, domain.Recipient{
		Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "bob"}, Channel: domain.ChannelEmail,
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(bobsInbox) != 0 {
		t.Errorf("a preference on another resource received %d entries", len(bobsInbox))
	}
	// The other half of the read path: what the Hub raised, subscribed to or
	// not. Without it, "nobody was told" and "nothing happened" read the same.
	events, err := reader.EventsForResource(ctx, resource, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != "nev-1" {
		t.Fatalf("the resource's own record of what happened is %#v", events)
	}
}

// The emit site writes the notification before it marks its work terminal, so a
// crash between the two replays the same event. It has to land as nothing.
func TestPostgresRecordingTheSameEventTwiceTellsNobodyTwice(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	emitter, err := application.NewEmitter(store)
	if err != nil {
		t.Fatal(err)
	}
	alice := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	if err := store.SavePreference(ctx, preference("pref-1", alice, domain.ChannelEmail)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := emitter.Emit(ctx, planFailed("nev-1")); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := store.Inbox(ctx, domain.Recipient{Owner: alice, Channel: domain.ChannelEmail}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("a replayed escalation told one person %d times", len(entries))
	}
	events, err := store.EventsForResource(ctx, resource, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Errorf("a replayed escalation was recorded %d times", len(events))
	}
}

// Saving the settings page twice leaves one row. The key is the tuple Apollo
// edits, not the identifier a caller happens to mint, or a second save would
// double every subsequent notification.
func TestPostgresSavingAPreferenceTwiceLeavesOneSubscription(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	alice := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	if err := store.SavePreference(ctx, preference("pref-1", alice, domain.ChannelEmail)); err != nil {
		t.Fatal(err)
	}
	second := preference("pref-2", alice, domain.ChannelEmail)
	second.AllFutureTypes = true
	second.UpdatedAt = at().Add(time.Hour)
	if err := store.SavePreference(ctx, second); err != nil {
		t.Fatal(err)
	}
	preferences, err := store.PreferencesFor(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	if len(preferences) != 1 {
		t.Fatalf("one owner on one channel holds %d preferences", len(preferences))
	}
	if !preferences[0].AllFutureTypes || preferences[0].ID != "pref-2" {
		t.Errorf("the edit did not replace the stored preference: %#v", preferences[0])
	}
	// The same owner on the other channel is a different subscription: the
	// settings page configures email and in-app independently.
	if err := store.SavePreference(ctx, preference("pref-3", alice, domain.ChannelInApp)); err != nil {
		t.Fatal(err)
	}
	preferences, err = store.PreferencesFor(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	if len(preferences) != 2 {
		t.Errorf("the second channel did not survive as its own subscription: %#v", preferences)
	}
}

// The ledger is the claim that this schema was applied. A fully migrated
// database has nothing pending, which is what the restore drill asserts and
// what a second start relies on.
func TestPostgresNotificationMigrationsAreRecordedOnce(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	if err := pgmigrate.Validate(Component(), Migrations()); err != nil {
		t.Fatal(err)
	}
	pending, err := pgmigrate.Pending(ctx, store.database, Component(), Migrations())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("a started store still reports %d pending migrations: %#v", len(pending), pending)
	}
	// Migrations returns a copy, so appending to what a caller received cannot
	// change what the next start runs.
	appended := append(Migrations(), pgmigrate.Migration{Name: "0002-not-real", Statements: "SELECT 1"})
	if len(Migrations()) == len(appended) {
		t.Error("a caller's append reached this store's migration list")
	}
}

func preference(id string, owner domain.Owner, channel domain.Channel) domain.Preference {
	return domain.Preference{
		ID: id, Owner: owner, ResourceRID: resource, Channel: channel,
		Types: []domain.EventType{domain.EventPlanFailures}, CreatedAt: at(), UpdatedAt: at(),
	}
}

func planFailed(id string) domain.Event {
	return domain.Event{
		ID: id, Type: domain.EventPlanFailures, ResourceRID: resource, Environment: "tenant-a",
		Summary:    "Reconciliation of installation mi-catalog in Environment tenant-a gave up",
		OccurredAt: at(),
	}
}

func newStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is not set")
	}
	store, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func resetStore(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()
	if _, err := store.database.ExecContext(ctx, `
TRUNCATE conduit_notification_inbox, conduit_notification_events,
    conduit_notification_preferences`); err != nil {
		t.Fatal(err)
	}
}

// The editing grain against the real UNIQUE constraint. The HTTP surface edits
// one (resource, owner, channel) tuple, and the identifier it mints is derived
// from that tuple rather than chosen -- so saving the same page twice has to
// leave one row here, in the database that actually enforces it, and not two.
func TestPostgresAPreferenceEditedTwiceLeavesOneRow(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	preferences, err := application.NewPreferences(store, at)
	if err != nil {
		t.Fatal(err)
	}
	alice := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	if _, err := preferences.Get(ctx, resource, alice, domain.ChannelEmail); !errors.Is(err, application.ErrNoPreference) {
		t.Fatalf("an unset preference read back as %v", err)
	}
	first, err := preferences.Save(ctx, domain.Preference{
		Owner: alice, ResourceRID: resource, Channel: domain.ChannelEmail,
		Types: []domain.EventType{domain.EventPlanFailures},
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := preferences.Save(ctx, domain.Preference{
		Owner: alice, ResourceRID: resource, Channel: domain.ChannelEmail, AllFutureTypes: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Revision != first.Revision+1 || second.CreatedAt != first.CreatedAt {
		t.Fatalf("a second edit of one tuple did not replace the first: %#v then %#v", first, second)
	}
	stored, err := store.PreferencesFor(ctx, resource)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || !stored[0].AllFutureTypes {
		t.Fatalf("two edits of one tuple left %#v", stored)
	}
	// The other channel is a different tuple, so it is a second row rather than
	// an overwrite -- the property the settings page depends on.
	if _, err := preferences.Save(ctx, domain.Preference{
		Owner: alice, ResourceRID: resource, Channel: domain.ChannelInApp, AllFutureTypes: true,
	}); err != nil {
		t.Fatal(err)
	}
	inApp, err := preferences.Get(ctx, resource, alice, domain.ChannelInApp)
	if err != nil {
		t.Fatal(err)
	}
	email, err := preferences.Get(ctx, resource, alice, domain.ChannelEmail)
	if err != nil {
		t.Fatal(err)
	}
	if inApp.ID == email.ID {
		t.Fatalf("two channels share one row: %#v", inApp)
	}
}
