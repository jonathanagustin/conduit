package postgres

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

func TestPostgresReplayKeepsTheOriginalAudience(t *testing.T) {
	for _, initiallySubscribed := range []bool{true, false} {
		t.Run(fmt.Sprintf("initially subscribed=%v", initiallySubscribed), func(t *testing.T) {
			ctx := t.Context()
			store := newStore(t, ctx)
			resetStore(t, ctx, store)
			emitter, err := application.NewEmitter(store)
			if err != nil {
				t.Fatal(err)
			}
			alice := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
			bob := domain.Owner{Kind: domain.PrincipalUser, ID: "bob"}
			if initiallySubscribed {
				if err := store.SavePreference(ctx, preference("pref-alice", alice, domain.ChannelEmail)); err != nil {
					t.Fatal(err)
				}
			}
			original := planFailed("nev-replayed")
			if err := emitter.Emit(ctx, original); err != nil {
				t.Fatal(err)
			}
			if err := store.SavePreference(ctx, preference("pref-bob", bob, domain.ChannelEmail)); err != nil {
				t.Fatal(err)
			}
			// Re-resolving today's preferences must not add Bob to yesterday's
			// event, even when that event originally reached nobody.
			replayed := original
			replayed.Summary = "a replay must not replace the original document"
			replayed.OccurredAt = original.OccurredAt.Add(time.Hour)
			if err := emitter.Emit(ctx, replayed); err != nil {
				t.Fatal(err)
			}
			assertReplayInbox(t, store, bob, 0)
			want := 0
			if initiallySubscribed {
				want = 1
			}
			assertReplayInbox(t, store, alice, want)
			events, err := store.EventsForResource(ctx, resource, 10)
			if err != nil || len(events) != 1 || !reflect.DeepEqual(events[0], original) {
				t.Fatalf("replay changed the event document: %#v, %v", events, err)
			}
			assertReplayPending(t, store, want)
		})
	}
}

// Separate pools race the same identity with disjoint audience snapshots. The
// event insert chooses one winner; the loser must not union its recipients
// into the winner's committed event or queue their delivery.
func TestPostgresConcurrentDuplicatesKeepOneCompleteAudience(t *testing.T) {
	ctx := t.Context()
	first := newStore(t, ctx)
	resetStore(t, ctx, first)
	second := newStore(t, ctx)
	for trial := range 8 {
		events := [2]domain.Event{planFailed(fmt.Sprintf("nev-concurrent-%d", trial)), planFailed(fmt.Sprintf("nev-concurrent-%d", trial))}
		var entries [2][]domain.InboxEntry
		for candidate := range events {
			events[candidate].Summary = fmt.Sprintf("audience candidate %d", candidate)
			for recipient := range 2 {
				entries[candidate] = append(entries[candidate], domain.InboxEntry{
					Event: events[candidate], PreferenceID: fmt.Sprintf("pref-%d-%d-%d", trial, candidate, recipient),
					Recipient: domain.Recipient{
						Owner:   domain.Owner{Kind: domain.PrincipalUser, ID: fmt.Sprintf("recipient-%d-%d-%d", trial, candidate, recipient)},
						Channel: domain.ChannelEmail,
					},
				})
			}
		}
		start := make(chan struct{})
		var wait sync.WaitGroup
		var failures [2]error
		for candidate, store := range []*Store{first, second} {
			wait.Go(func() {
				<-start
				failures[candidate] = store.RecordEvent(ctx, events[candidate], entries[candidate])
			})
		}
		close(start)
		wait.Wait()
		if failures[0] != nil || failures[1] != nil {
			t.Fatalf("concurrent duplicate recording failed: %v", failures)
		}
		var document []byte
		if err := first.database.QueryRowContext(ctx, `SELECT document FROM conduit_notification_events WHERE id = $1`, events[0].ID).Scan(&document); err != nil {
			t.Fatal(err)
		}
		// The winner's document and audience must agree, rather than merely
		// checking that some event and two arbitrary recipients survived.
		winner := -1
		for candidate := range events {
			stored, err := first.Inbox(ctx, entries[candidate][0].Recipient, 10)
			if err != nil {
				t.Fatal(err)
			}
			if len(stored) != 0 && reflect.DeepEqual(stored[0].Event, events[candidate]) {
				winner = candidate
			}
		}
		if winner == -1 {
			t.Fatalf("neither complete candidate won the event: %s", document)
		}
		for candidate := range entries {
			want := 0
			if candidate == winner {
				want = 1
			}
			for _, entry := range entries[candidate] {
				assertReplayInbox(t, first, entry.Recipient.Owner, want)
			}
		}
		assertReplayPending(t, first, 2*(trial+1))
	}
}

func TestPostgresInvalidAudienceEntryRollsBackTheEntireEvent(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	event := planFailed("nev-rollback")
	alice := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	entry := domain.InboxEntry{
		Event: event, PreferenceID: "pref-alice",
		Recipient: domain.Recipient{Owner: alice, Channel: domain.ChannelEmail},
	}
	invalid := entry
	invalid.Event = planFailed("nev-another-event")
	if err := store.RecordEvent(ctx, event, []domain.InboxEntry{entry, invalid}); err == nil {
		t.Fatal("an entry for another event was accepted")
	}
	events, err := store.EventsForResource(ctx, resource, 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("a refused audience left its event behind: %#v, %v", events, err)
	}
	assertReplayInbox(t, store, alice, 0)
	assertReplayPending(t, store, 0)
	// A valid retry is the first committed recording, not a duplicate that
	// should be discarded because a failed attempt left a partial event.
	if err := store.RecordEvent(ctx, event, []domain.InboxEntry{entry}); err != nil {
		t.Fatal(err)
	}
	assertReplayInbox(t, store, alice, 1)
	assertReplayPending(t, store, 1)
}

func assertReplayInbox(t *testing.T, store *Store, owner domain.Owner, want int) {
	t.Helper()
	entries, err := store.Inbox(t.Context(), domain.Recipient{Owner: owner, Channel: domain.ChannelEmail}, 10)
	if err != nil || len(entries) != want {
		t.Fatalf("recipient %s has %d entries, want %d: %#v, %v", owner.ID, len(entries), want, entries, err)
	}
}

func assertReplayPending(t *testing.T, store *Store, want int) {
	t.Helper()
	stats, err := store.Deliveries(t.Context())
	if err != nil || stats.Pending != int64(want) || stats.Delivered != 0 || stats.Exhausted != 0 {
		t.Fatalf("delivery backlog = %#v, %v; want %d pending only", stats, err, want)
	}
}
