package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func at() time.Time { return time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC) }

// fixedAt is the ports.Clock the preference service now takes, pinned to at()
// plus an offset a test can choose, so a saved preference's timestamps stay
// assertable and a second save can be made to land later. The service reads a
// ports.Clock rather than a func() time.Time because the delivery dispatcher in
// this same context already does, and one spelling of "now" per bounded context
// is the point of having declared the port.
type fixedAt struct{ offset time.Duration }

func (clock fixedAt) Now() time.Time { return at().Add(clock.offset) }

func planFailed() domain.Event {
	return domain.Event{
		ID: "nev-1", Type: domain.EventPlanFailures,
		ResourceRID: "ri.apollo.main.environment.abc", Environment: "tenant-a",
		Summary:    "Reconciliation of catalog in tenant-a exhausted its retry budget",
		OccurredAt: at(),
	}
}

// The property this whole package exists for. An emitter that stored only what
// it delivered would answer "nothing" to both "did anybody subscribe" and "did
// the Hub notice", and those have opposite fixes.
func TestAnEventNobodySubscribedToIsStillRecorded(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	emitter, err := NewEmitter(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(context.Background(), planFailed()); err != nil {
		t.Fatalf("emitting to nobody failed: %v", err)
	}
	if len(store.events) != 1 {
		t.Fatalf("an event reaching nobody was not recorded: %#v", store.events)
	}
	if len(store.entries) != 0 {
		t.Errorf("entries were produced with no preferences stored: %#v", store.entries)
	}
}

func TestEmitFansOutToTheSubscribersOfThatResourceOnly(t *testing.T) {
	t.Parallel()
	store := &fakeStore{preferences: map[string][]domain.Preference{
		"ri.apollo.main.environment.abc": {{
			ID: "pref-1", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"},
			ResourceRID: "ri.apollo.main.environment.abc", Channel: domain.ChannelEmail,
			Types: []domain.EventType{domain.EventPlanFailures},
		}},
		"ri.apollo.main.environment.other": {{
			ID: "pref-2", Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "bob"},
			ResourceRID: "ri.apollo.main.environment.other", Channel: domain.ChannelEmail,
			AllFutureTypes: true,
		}},
	}}
	emitter, err := NewEmitter(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(context.Background(), planFailed()); err != nil {
		t.Fatal(err)
	}
	if len(store.entries) != 1 {
		t.Fatalf("expected exactly the one subscriber of this resource, got %#v", store.entries)
	}
	if store.entries[0].Recipient.Owner.ID != "alice" || store.entries[0].PreferenceID != "pref-1" {
		t.Errorf("the entry did not come from this resource's preference: %#v", store.entries[0])
	}
}

// An event the domain refuses must not reach the store at all: a stored event
// with no summary or no resource is an inbox row nobody can act on, and it
// arrives having already been counted as told.
func TestAnInvalidEventIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	emitter, err := NewEmitter(store)
	if err != nil {
		t.Fatal(err)
	}
	broken := planFailed()
	broken.Summary = ""
	if err := emitter.Emit(context.Background(), broken); err == nil {
		t.Fatal("an event with no summary was accepted")
	}
	if len(store.events) != 0 || store.preferenceReads != 0 {
		t.Errorf("a refused event touched the store: events=%#v reads=%d", store.events, store.preferenceReads)
	}
}

func TestAStoreFailureIsReportedRatherThanSwallowed(t *testing.T) {
	t.Parallel()
	failure := errors.New("the database is down")
	store := &fakeStore{recordErr: failure}
	emitter, err := NewEmitter(store)
	if err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(context.Background(), planFailed()); !errors.Is(err, failure) {
		t.Fatalf("a store failure did not reach the caller: %v", err)
	}
}

// The read path is bounded whatever the caller asks for. An unbounded read is a
// way to ask a control plane to load its whole history into memory.
func TestTheReadPathBoundsWhatItReturns(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	reader, err := NewReader(store)
	if err != nil {
		t.Fatal(err)
	}
	recipient := domain.Recipient{
		Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: domain.ChannelEmail,
	}
	for _, asked := range []int{0, -1, 10_000} {
		if _, err := reader.Inbox(context.Background(), recipient, asked); err != nil {
			t.Fatal(err)
		}
		if store.lastLimit != MaximumLimit {
			t.Errorf("a request for %d rows reached the store as %d", asked, store.lastLimit)
		}
	}
	if _, err := reader.Inbox(context.Background(), recipient, 5); err != nil {
		t.Fatal(err)
	}
	if store.lastLimit != 5 {
		t.Errorf("a bounded request was not passed through: %d", store.lastLimit)
	}
	if _, err := reader.Inbox(context.Background(), domain.Recipient{
		Owner: domain.Owner{Kind: "SERVICE", ID: "robot"}, Channel: domain.ChannelEmail,
	}, 5); err == nil {
		t.Error("an inbox was read for a principal kind that cannot own one")
	}
}

type fakeStore struct {
	events          []domain.Event
	entries         []domain.InboxEntry
	preferences     map[string][]domain.Preference
	preferenceReads int
	lastLimit       int
	recordErr       error
	preferencesErr  error
	saved           []domain.Preference
}

func (store *fakeStore) RecordEvent(_ context.Context, event domain.Event, entries []domain.InboxEntry) error {
	if store.recordErr != nil {
		return store.recordErr
	}
	store.events = append(store.events, event)
	store.entries = append(store.entries, entries...)
	return nil
}

func (store *fakeStore) PreferencesFor(_ context.Context, resourceRID string) ([]domain.Preference, error) {
	store.preferenceReads++
	if store.preferencesErr != nil {
		return nil, store.preferencesErr
	}
	return store.preferences[resourceRID], nil
}

// SavePreference upserts on (owner, resource, channel), which is the tuple the
// PostgreSQL store's UNIQUE constraint holds one row for. A fake that appended
// would let a test pass because the fake forgot the grain.
func (store *fakeStore) SavePreference(_ context.Context, preference domain.Preference) error {
	if store.preferences == nil {
		store.preferences = map[string][]domain.Preference{}
	}
	store.saved = append(store.saved, preference)
	existing := store.preferences[preference.ResourceRID]
	for index, candidate := range existing {
		if candidate.Owner == preference.Owner && candidate.Channel == preference.Channel {
			existing[index] = preference
			return nil
		}
	}
	store.preferences[preference.ResourceRID] = append(existing, preference)
	return nil
}

func (store *fakeStore) Inbox(
	_ context.Context, recipient domain.Recipient, limit int,
) ([]domain.InboxEntry, error) {
	store.lastLimit = limit
	matched := []domain.InboxEntry{}
	for _, entry := range store.entries {
		if entry.Recipient == recipient {
			matched = append(matched, entry)
		}
	}
	return matched, nil
}

func (store *fakeStore) EventsForResource(
	_ context.Context, resourceRID string, limit int,
) ([]domain.Event, error) {
	store.lastLimit = limit
	matched := []domain.Event{}
	for _, event := range store.events {
		if event.ResourceRID == resourceRID {
			matched = append(matched, event)
		}
	}
	return matched, nil
}
