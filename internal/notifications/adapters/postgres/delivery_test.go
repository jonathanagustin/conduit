package postgres

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/adapters/webhook"
	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// recordEmailEvent stores a preference and emits an event, which is how a real
// entry gets into the queue: the fan-out writes it, not a test fixture.
func recordEmailEvent(t *testing.T, ctx context.Context, store *Store, id string, channel domain.Channel) {
	t.Helper()
	emitter, err := application.NewEmitter(store, allowRecipients{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreference(ctx, preference("pref-"+id, domain.Owner{
		Kind: domain.PrincipalUser, ID: "alice",
	}, channel)); err != nil {
		t.Fatal(err)
	}
	if err := emitter.Emit(ctx, planFailed(id)); err != nil {
		t.Fatal(err)
	}
}

// The slice this change exists for: an emitted event becomes a leasable
// delivery, is sent once, and is never leased again.
func TestAnEmittedEventBecomesADeliveryAndIsDeliveredOnce(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	recordEmailEvent(t, ctx, store, "nev-1", domain.ChannelEmail)

	lease, found, err := store.LeaseDelivery(ctx, at(), time.Minute)
	if err != nil || !found {
		t.Fatalf("the queue offered nothing to deliver: found=%v err=%v", found, err)
	}
	if lease.Entry.Event.ID != "nev-1" || lease.Entry.Event.Summary != planFailed("nev-1").Summary {
		t.Fatalf("the lease lost the event it is about: %#v", lease.Entry.Event)
	}
	if lease.Entry.Recipient.Owner.ID != "alice" || lease.Entry.Recipient.Channel != domain.ChannelEmail {
		t.Fatalf("the lease lost who it is for: %#v", lease.Entry.Recipient)
	}
	if lease.Attempt != 1 || lease.Token == "" {
		t.Fatalf("the first lease reports attempt %d with token %q", lease.Attempt, lease.Token)
	}
	// A leased entry is not offered again while the lease holds, which is what
	// keeps two replicas from sending the same notification twice.
	if _, taken, err := store.LeaseDelivery(ctx, at(), time.Minute); err != nil || taken {
		t.Fatalf("a leased delivery was offered to a second caller: taken=%v err=%v", taken, err)
	}
	if err := store.CompleteDelivery(ctx, lease, at().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, again, err := store.LeaseDelivery(ctx, at().Add(time.Hour), time.Minute); err != nil || again {
		t.Fatalf("a delivered notification was queued again: again=%v err=%v", again, err)
	}
	stats, err := store.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Delivered != 1 || stats.Pending != 0 || stats.Exhausted != 0 {
		t.Errorf("delivery statistics = %#v, want one delivered and nothing else", stats)
	}
	// The inbox entry survives its delivery: it is what the recipient reads.
	reader, err := application.NewReader(store)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := reader.Inbox(ctx, domain.Recipient{
		Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: domain.ChannelEmail,
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("delivering the notification left %d inbox entries, want 1", len(entries))
	}
}

// An in-app entry is delivered by being in the inbox. Queuing one would send it
// somewhere its owner never chose.
func TestAnInAppEntryIsNeverOfferedToATransport(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	recordEmailEvent(t, ctx, store, "nev-1", domain.ChannelInApp)
	if _, found, err := store.LeaseDelivery(ctx, at().Add(time.Hour), time.Minute); err != nil || found {
		t.Fatalf("an IN_APP entry was queued for a transport: found=%v err=%v", found, err)
	}
	stats, err := store.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Pending != 0 {
		t.Errorf("the queue holds %d entries nothing should ever send", stats.Pending)
	}
}

// A failed attempt comes back when it is due and not before, and an exhausted
// one never comes back at all but keeps the reason it stopped.
func TestAFailedDeliveryReturnsWhenDueAndAnExhaustedOneNever(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	recordEmailEvent(t, ctx, store, "nev-1", domain.ChannelEmail)

	lease, found, err := store.LeaseDelivery(ctx, at(), time.Minute)
	if err != nil || !found {
		t.Fatalf("nothing to lease: found=%v err=%v", found, err)
	}
	retryAt := at().Add(10 * time.Minute)
	if err := store.FailDelivery(ctx, lease, "webhook answered 502", retryAt, false); err != nil {
		t.Fatal(err)
	}
	if _, early, err := store.LeaseDelivery(ctx, retryAt.Add(-time.Second), time.Minute); err != nil || early {
		t.Fatalf("a delivery was retried before it was due: early=%v err=%v", early, err)
	}
	second, found, err := store.LeaseDelivery(ctx, retryAt, time.Minute)
	if err != nil || !found {
		t.Fatalf("a due retry was not offered: found=%v err=%v", found, err)
	}
	if second.Attempt != 2 {
		t.Errorf("the second attempt reports attempt %d", second.Attempt)
	}
	if err := store.FailDelivery(ctx, second, "webhook answered 404", retryAt, true); err != nil {
		t.Fatal(err)
	}
	if _, ever, err := store.LeaseDelivery(ctx, at().AddDate(1, 0, 0), time.Minute); err != nil || ever {
		t.Fatalf("an exhausted delivery was leased again: ever=%v err=%v", ever, err)
	}
	stats, err := store.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Exhausted != 1 || stats.Pending != 0 || stats.Delivered != 0 {
		t.Errorf("delivery statistics = %#v, want one exhausted and nothing else", stats)
	}
	var reason string
	if err := store.database.QueryRowContext(ctx,
		`SELECT delivery_error FROM conduit_notification_inbox WHERE event_id = $1`,
		"nev-1").Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "webhook answered 404" {
		t.Errorf("the terminal state does not say why it stopped: %q", reason)
	}
}

// An expired lease is what makes a crashed replica's work recoverable, and the
// second holder is the only one whose acknowledgement may land.
func TestAnExpiredLeaseIsRecoveredAndTheStaleHolderIsRefused(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	recordEmailEvent(t, ctx, store, "nev-1", domain.ChannelEmail)

	abandoned, found, err := store.LeaseDelivery(ctx, at(), time.Minute)
	if err != nil || !found {
		t.Fatalf("nothing to lease: found=%v err=%v", found, err)
	}
	recovered, found, err := store.LeaseDelivery(ctx, at().Add(2*time.Minute), time.Minute)
	if err != nil || !found {
		t.Fatalf("an expired lease was not recovered: found=%v err=%v", found, err)
	}
	if recovered.Token == abandoned.Token || recovered.Attempt != 2 {
		t.Fatalf("the recovered lease reuses the abandoned token or attempt: %#v", recovered)
	}
	if err := store.CompleteDelivery(ctx, abandoned, at().Add(3*time.Minute)); err == nil {
		t.Fatal("a replica whose lease expired reported success over the one holding it")
	}
	if err := store.FailDelivery(ctx, abandoned, "too late", at(), true); err == nil {
		t.Fatal("a stale lease holder marked an entry terminal")
	}
	if err := store.CompleteDelivery(ctx, recovered, at().Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
}

// Two callers polling together take two different entries rather than one
// entry twice, which is what SKIP LOCKED buys.
func TestConcurrentLeasesTakeDifferentDeliveries(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	emitter, err := application.NewEmitter(store, allowRecipients{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SavePreference(ctx, preference("pref-1", domain.Owner{
		Kind: domain.PrincipalUser, ID: "alice",
	}, domain.ChannelEmail)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"nev-1", "nev-2"} {
		if err := emitter.Emit(ctx, planFailed(id)); err != nil {
			t.Fatal(err)
		}
	}
	type outcome struct {
		lease ports.DeliveryLease
		found bool
		err   error
	}
	results := make(chan outcome, 2)
	for range 2 {
		go func() {
			lease, found, err := store.LeaseDelivery(ctx, at(), time.Minute)
			results <- outcome{lease: lease, found: found, err: err}
		}()
	}
	taken := map[string]int{}
	for range 2 {
		result := <-results
		if result.err != nil || !result.found {
			t.Fatalf("a concurrent lease found nothing: found=%v err=%v", result.found, result.err)
		}
		taken[result.lease.Entry.Event.ID]++
	}
	for id, count := range taken {
		if count != 1 {
			t.Errorf("event %s was leased %d times concurrently", id, count)
		}
	}
	if len(taken) != 2 {
		t.Errorf("two concurrent callers took %d distinct deliveries", len(taken))
	}
}

// The migration is additive over a database that already holds entries, and the
// entries it finds must not become a backlog the first start tries to send.
func TestUpgradingDoesNotQueueTheHistoryItFinds(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	recordEmailEvent(t, ctx, store, "nev-1", domain.ChannelEmail)
	// A row as an older build wrote it: no delivery state at all.
	if _, err := store.database.ExecContext(ctx,
		`UPDATE conduit_notification_inbox SET delivery_available_at = NULL`); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.LeaseDelivery(ctx, at().AddDate(1, 0, 0), time.Minute); err != nil || found {
		t.Fatalf("upgrading queued a notification recorded before delivery existed: found=%v err=%v", found, err)
	}
}

func TestTheDeliveryMigrationIsAppendedRatherThanEdited(t *testing.T) {
	t.Parallel()
	applied := Migrations()
	want := []string{"0001-notifications-core", "0002-notification-delivery", "0003-notification-contacts", "0004-notification-intents"}
	if len(applied) != len(want) {
		t.Fatalf("the notifications component has %d migrations, want %d", len(applied), len(want))
	}
	for index, name := range want {
		if applied[index].Name != name {
			t.Errorf("migration %d is named %q, want %q; names are immutable once merged",
				index, applied[index].Name, name)
		}
	}
	// A migration runs inside one transaction, so this cannot ever appear.
	for _, migration := range applied {
		if contains(migration.Statements, "CONCURRENTLY") {
			t.Errorf("%s builds an index concurrently, which cannot run in a transaction", migration.Name)
		}
	}
	// The core migration must not have grown the delivery columns: every
	// database in service has already recorded it as applied.
	if contains(applied[0].Statements, "delivery_available_at") {
		t.Error("delivery state was added to an already-merged migration, which no existing database would apply")
	}
	// Nor may either merged migration have grown the contacts table, against
	// the same risk: appending is the only way to add one.
	for _, merged := range applied[:2] {
		if contains(merged.Statements, "conduit_notification_contacts") {
			t.Errorf("the contacts table was added to %s, which every database in service has already applied",
				merged.Name)
		}
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// A closed store answers rather than panicking, because the delivery loop calls
// these on a ticker and a shutdown race should end the loop, not the process.
func TestAClosedStoreRefusesDeliveryWork(t *testing.T) {
	t.Parallel()
	var closed *Store
	if _, _, err := closed.LeaseDelivery(t.Context(), time.Now(), time.Minute); err == nil {
		t.Error("a closed store leased a delivery")
	}
	if err := closed.CompleteDelivery(t.Context(), ports.DeliveryLease{}, time.Now()); err == nil {
		t.Error("a closed store completed a delivery")
	}
	if err := closed.FailDelivery(t.Context(), ports.DeliveryLease{}, "", time.Now(), true); err == nil {
		t.Error("a closed store failed a delivery")
	}
	if _, err := closed.Deliveries(t.Context()); err == nil {
		t.Error("a closed store reported delivery statistics")
	}
}

func TestALeaseWithNoDurationIsRefused(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	if _, _, err := store.LeaseDelivery(ctx, at(), 0); err == nil {
		t.Error("a lease that expires immediately was granted")
	}
}

// The whole slice against a real database and a real transport, with no socket
// open: reconciliation's escalation is emitted, the dispatcher drains it, and
// the endpoint receives the notification an operator would otherwise have had
// to go looking for.
func TestTheDispatcherDeliversARecordedEventThroughARealTransport(t *testing.T) {
	ctx := t.Context()
	store := newStore(t, ctx)
	resetStore(t, ctx, store)
	recordEmailEvent(t, ctx, store, "nev-1", domain.ChannelEmail)

	posted := make(chan []byte, 4)
	transport, err := webhook.New("https://hooks.example.com/conduit", &http.Client{
		Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(request.Body)
			posted <- body
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("ok")),
				Header:     http.Header{},
			}, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// A clock past the event rather than the wall clock: the queue is due-time
	// ordered, so a test that ran before the fixture's own timestamp would drain
	// an empty queue and pass for the wrong reason.
	dispatcher, err := application.NewDispatcher(store, transport, fixedClock{now: at().Add(time.Hour)},
		application.DispatcherConfig{Eligibility: allowRecipients{},
			LeaseDuration: 30 * time.Second, MaximumAttempts: 3,
			BaseRetryDelay: time.Second, PollInterval: time.Second,
		})
	if err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-posted:
		if !strings.Contains(string(body), "nev-1") ||
			!strings.Contains(string(body), string(domain.EventPlanFailures)) {
			t.Errorf("the endpoint received something other than the notification: %s", body)
		}
	default:
		t.Fatal("the dispatcher drained the queue without sending anything")
	}
	// Drained means drained: a second pass sends nothing, so an operator is not
	// told the same thing on every poll.
	if err := dispatcher.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 0 {
		t.Errorf("a delivered notification was sent %d more times", len(posted))
	}
	stats, err := store.Deliveries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Delivered != 1 || stats.Pending != 0 {
		t.Errorf("delivery statistics = %#v after a successful drain", stats)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type fixedClock struct{ now time.Time }

func (clock fixedClock) Now() time.Time { return clock.now }
