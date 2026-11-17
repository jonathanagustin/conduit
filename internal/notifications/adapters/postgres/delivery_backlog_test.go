package postgres

import (
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// The backlog a scrape reads: per channel, what is waiting, what is dead, and
// how long the oldest waiting entry has waited since it was raised.
func TestTheDeliveryBacklogIsPerChannelAndAgesFromWhenTheEventWasRaised(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	emitter, err := application.NewEmitter(store, allowRecipients{})
	if err != nil {
		t.Fatal(err)
	}
	owner := domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}
	for _, saved := range []domain.Preference{
		preference("pref-email", owner, domain.ChannelEmail),
		preference("pref-slack", owner, domain.ChannelSlack),
		preference("pref-inbox", owner, domain.ChannelInApp),
	} {
		if err := store.SavePreference(ctx, saved); err != nil {
			t.Fatal(err)
		}
	}
	// Two events, each fanned out to all three channels, both raised at at().
	for _, id := range []string{"nev-1", "nev-2"} {
		if err := emitter.Emit(ctx, planFailed(id)); err != nil {
			t.Fatal(err)
		}
	}

	read := func(now time.Time) map[string]ChannelBacklog {
		t.Helper()
		rows, err := store.DeliveryBacklog(ctx, now)
		if err != nil {
			t.Fatal(err)
		}
		byChannel := map[string]ChannelBacklog{}
		for _, row := range rows {
			byChannel[row.Channel] = row
		}
		return byChannel
	}

	backlog := read(at().Add(10 * time.Minute))
	// An IN_APP entry is never queued for a transport, so it is not backlog.
	if _, queued := backlog[string(domain.ChannelInApp)]; queued || len(backlog) != 2 {
		t.Fatalf("backlog channels = %#v, want EMAIL and SLACK only", backlog)
	}
	for _, channel := range []domain.Channel{domain.ChannelEmail, domain.ChannelSlack} {
		row := backlog[string(channel)]
		if row.Pending != 2 || row.Exhausted != 0 || row.OldestPendingAge != 10*time.Minute {
			t.Errorf("%s backlog = %#v, want two pending aged ten minutes", channel, row)
		}
	}

	// Fail one: it backs off for an hour. It is still pending, and its age is
	// still measured from when it was raised, not from when it is next due.
	first, found, err := store.LeaseDelivery(ctx, at(), time.Minute)
	if err != nil || !found {
		t.Fatalf("nothing to lease: found=%v err=%v", found, err)
	}
	failed := string(first.Entry.Recipient.Channel)
	if err := store.FailDelivery(ctx, first, "endpoint refused", at().Add(time.Hour), false); err != nil {
		t.Fatal(err)
	}
	if row := read(at().Add(20 * time.Minute))[failed]; row.Pending != 2 || row.OldestPendingAge != 20*time.Minute {
		t.Errorf("after a retryable failure %s backlog = %#v, want two pending aged twenty minutes", failed, row)
	}

	// Exhaust it: no longer pending, counted as dead.
	again, found, err := store.LeaseDelivery(ctx, at().Add(2*time.Hour), time.Minute)
	if err != nil || !found {
		t.Fatalf("the retried entry never came back: found=%v err=%v", found, err)
	}
	exhausted := string(again.Entry.Recipient.Channel)
	if err := store.FailDelivery(ctx, again, "gave up", at().Add(2*time.Hour), true); err != nil {
		t.Fatal(err)
	}
	if row := read(at().Add(3 * time.Hour))[exhausted]; row.Pending != 1 || row.Exhausted != 1 {
		t.Errorf("after exhaustion %s backlog = %#v, want one pending and one exhausted", exhausted, row)
	}

	// Deliver everything that is left: nothing waiting means no age, not a
	// stale high-water mark that would alert forever.
	for {
		lease, found, err := store.LeaseDelivery(ctx, at().Add(4*time.Hour), time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if !found {
			break
		}
		if err := store.CompleteDelivery(ctx, lease, at().Add(4*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	for channel, row := range read(at().Add(5 * time.Hour)) {
		if row.Pending != 0 || row.OldestPendingAge != 0 {
			t.Errorf("a drained %s queue reports %#v", channel, row)
		}
	}
}
