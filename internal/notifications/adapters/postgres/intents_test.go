package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

func newIntentStore(t *testing.T) (*Store, string) {
	t.Helper()
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required")
	}
	shared, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	schema, err := randomToken("conduit_intents_")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := shared.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := shared.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.RuntimeParams["search_path"] = schema
	isolated := stdlib.RegisterConnConfig(config)
	t.Cleanup(func() { stdlib.UnregisterConnConfig(isolated) })
	store, err := New(t.Context(), isolated)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, isolated
}

func commitIntent(t *testing.T, store *Store, event domain.Event) {
	t.Helper()
	transaction, err := store.database.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	recorder, err := NewIntentRecorder(transaction)
	if err != nil {
		t.Fatal(err)
	}
	if err := recorder.Emit(t.Context(), event); err != nil {
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresIntentCommitsWithItsSourceAndSurvivesCancellation(t *testing.T) {
	for _, outcome := range []string{"rollback", "savepoint rollback", "commit then cancel"} {
		t.Run(outcome, func(t *testing.T) {
			store, dsn := newIntentStore(t)
			if _, err := store.database.ExecContext(t.Context(), "CREATE TABLE source_transition (id text PRIMARY KEY)"); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			transaction, err := store.database.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer transaction.Rollback()
			if _, err := transaction.ExecContext(ctx, "SAVEPOINT source_work; INSERT INTO source_transition VALUES ('source')"); err != nil {
				t.Fatal(err)
			}
			// The recorder must read this same transaction's consent, which no
			// other pool can see yet. A pool-backed preference lookup loses it.
			consent := preference("consent", alice(), domain.ChannelInApp)
			document, err := json.Marshal(consent)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := transaction.ExecContext(ctx, `
INSERT INTO conduit_notification_preferences
    (id, owner_kind, owner_id, resource_rid, channel, updated_at, document)
VALUES ($1, $2, $3, $4, $5, $6, $7)`, consent.ID, consent.Owner.Kind,
				consent.Owner.ID, consent.ResourceRID, consent.Channel, consent.UpdatedAt, document); err != nil {
				t.Fatal(err)
			}
			recorder, err := NewIntentRecorder(transaction)
			if err != nil {
				t.Fatal(err)
			}
			if err := recorder.Emit(ctx, planFailed("source-event")); err != nil {
				t.Fatal(err)
			}
			// An independent pool sees neither source nor intent before commit.
			second, err := New(t.Context(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()
			if stats, err := second.Intents(t.Context()); err != nil || stats.Pending != 0 {
				t.Fatalf("uncommitted intent escaped: %#v, %v", stats, err)
			}
			if events, err := second.EventsForResource(t.Context(), resource, 10); err != nil || len(events) != 0 {
				t.Fatalf("source recorder admitted an event before handoff: %#v %v", events, err)
			}
			switch outcome {
			case "rollback":
				err = transaction.Rollback()
			case "savepoint rollback":
				if _, err = transaction.ExecContext(ctx, "ROLLBACK TO SAVEPOINT source_work; RELEASE SAVEPOINT source_work"); err == nil {
					err = transaction.Commit()
				}
			default:
				err = transaction.Commit()
				cancel()
			}
			if err != nil {
				t.Fatal(err)
			}
			_ = store.Close()
			restarted, err := New(t.Context(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer restarted.Close()
			var sourceRows int
			if err := restarted.database.QueryRowContext(t.Context(), "SELECT count(*) FROM source_transition").Scan(&sourceRows); err != nil {
				t.Fatal(err)
			}
			lease, found, err := restarted.LeaseIntent(t.Context(), at(), time.Minute)
			want := outcome == "commit then cancel"
			if err != nil || found != want || (sourceRows == 1) != want {
				t.Fatalf("source and intent split across %s: source=%d found=%v err=%v", outcome, sourceRows, found, err)
			}
			if want && (lease.Intent.Event.ID != "source-event" || len(lease.Intent.Candidates) != 1 || lease.Intent.Candidates[0].PreferenceID != "consent") {
				t.Fatalf("restart lost source consent: %#v", lease)
			}
			if events, err := restarted.EventsForResource(t.Context(), resource, 10); err != nil || len(events) != 0 {
				t.Fatalf("source recorder admitted an event without a worker: %#v %v", events, err)
			}
		})
	}
}

func TestPostgresIntentDuplicateNeverChangesFirstConsentIncludingEmpty(t *testing.T) {
	for _, subscribed := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "subscribed"}[subscribed], func(t *testing.T) {
			store, _ := newIntentStore(t)
			if subscribed {
				selected := preference("first-consent", alice(), domain.ChannelEmail)
				selected.ContactID = "private-contact-selection"
				if err := store.SavePreference(t.Context(), selected); err != nil {
					t.Fatal(err)
				}
			}
			original := planFailed("stable-intent")
			commitIntent(t, store, original)
			if err := store.SaveContact(t.Context(), domain.Contact{ID: "private-contact-selection", Owner: alice(), Kind: domain.ContactEmail, Address: "private-address@example.test", CreatedAt: at()}); err != nil {
				t.Fatal(err)
			}
			if err := store.SavePreference(t.Context(), preference("later-consent", domain.Owner{Kind: domain.PrincipalUser, ID: "bob"}, domain.ChannelEmail)); err != nil {
				t.Fatal(err)
			}
			replay := original
			replay.Summary = "changed source content must not replace the first intent"
			commitIntent(t, store, replay)
			lease, found, err := store.LeaseIntent(t.Context(), at(), time.Minute)
			want := 0
			if subscribed {
				want = 1
			}
			if err != nil || !found || !reflect.DeepEqual(lease.Intent.Event, original) || len(lease.Intent.Candidates) != want {
				t.Fatalf("duplicate changed source snapshot: %#v %v", lease, err)
			}
			if subscribed && lease.Intent.Candidates[0].Recipient.Owner.ID != "alice" {
				t.Fatalf("later subscriber entered frozen consent: %#v", lease.Intent.Candidates)
			}
			var document []byte
			if err := store.database.QueryRowContext(t.Context(), "SELECT document FROM conduit_notification_intents").Scan(&document); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(document), "private-address") || strings.Contains(string(document), "private-contact-selection") || strings.Contains(string(document), "contactId") {
				t.Fatalf("source intent copied a contact destination: %s", document)
			}
			if err := store.CompleteIntent(t.Context(), lease, at()); err != nil {
				t.Fatal(err)
			}
			commitIntent(t, store, replay)
			if _, found, err := store.LeaseIntent(t.Context(), at().Add(time.Hour), time.Minute); err != nil || found {
				t.Fatalf("duplicate restarted completed intent: %v %v", found, err)
			}
		})
	}
}

func TestPostgresIntentLeasesFenceReplicasExpiryAndExhaustion(t *testing.T) {
	first, dsn := newIntentStore(t)
	second, err := New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	commitIntent(t, first, planFailed("one-intent"))
	type result struct {
		lease ports.IntentLease
		found bool
		err   error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	var wait sync.WaitGroup
	for i := range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			store := first
			if i%2 != 0 {
				store = second
			}
			lease, found, err := store.LeaseIntent(t.Context(), at(), time.Minute)
			results <- result{lease, found, err}
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var initial ports.IntentLease
	claimed := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.found {
			claimed++
			initial = result.lease
		}
	}
	if claimed != 1 {
		t.Fatalf("replicas claimed one intent %d times", claimed)
	}
	if _, found, err := second.LeaseIntent(t.Context(), at().Add(time.Minute-time.Nanosecond), time.Minute); err != nil || found {
		t.Fatalf("unexpired lease was stolen: %v %v", found, err)
	}
	successor, found, err := second.LeaseIntent(t.Context(), at().Add(time.Minute), time.Minute)
	if err != nil || !found || successor.Attempt != 2 || successor.Token == initial.Token {
		t.Fatalf("expiry did not create a new fenced attempt: %#v %v %v", successor, found, err)
	}
	if err := first.CompleteIntent(t.Context(), initial, at()); err == nil {
		t.Fatal("stale completion was accepted")
	}
	if err := first.FailIntent(t.Context(), initial, "stale", at(), true); err == nil {
		t.Fatal("stale failure was accepted")
	}
	if err := second.CompleteIntent(t.Context(), successor, at().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	commitIntent(t, first, planFailed("exhausted-intent"))
	lease, found, err := first.LeaseIntent(t.Context(), at(), time.Minute)
	if err != nil || !found {
		t.Fatalf("missing exhaustion fixture: %v %v", found, err)
	}
	if err := first.FailIntent(t.Context(), lease, strings.Repeat("x", 5000), at(), true); err != nil {
		t.Fatal(err)
	}
	if _, found, err := second.LeaseIntent(t.Context(), at().AddDate(1, 0, 0), time.Minute); err != nil || found {
		t.Fatalf("terminal intent was silently restarted: %v %v", found, err)
	}
	stats, err := second.Intents(t.Context())
	if err != nil || stats.Pending != 0 || stats.Completed != 1 || stats.Exhausted != 1 {
		t.Fatalf("intent outcomes not visible: %#v %v", stats, err)
	}
	var diagnostic string
	if err := second.database.QueryRowContext(t.Context(), "SELECT last_error FROM conduit_notification_intents WHERE event_id='exhausted-intent'").Scan(&diagnostic); err != nil || len(diagnostic) != 4096 {
		t.Fatalf("terminal diagnostic missing or unbounded: %d %v", len(diagnostic), err)
	}
}

type intentTestClock struct{ now time.Time }

func (clock *intentTestClock) Now() time.Time { return clock.now }

type intentEligibility func(context.Context, domain.Owner, string) (bool, error)

func (check intentEligibility) Check(ctx context.Context, owner domain.Owner, resource string) (bool, error) {
	return check(ctx, owner, resource)
}

type intentSink struct {
	store *Store
	fail  bool
}

func (sink *intentSink) RecordEvent(ctx context.Context, event domain.Event, entries []domain.InboxEntry) error {
	if sink.fail {
		return errors.New("sink temporarily unavailable")
	}
	return sink.store.RecordEvent(ctx, event, entries)
}

type intentLostAck struct {
	ports.IntentQueue
	fail bool
}

func (queue *intentLostAck) CompleteIntent(ctx context.Context, lease ports.IntentLease, at time.Time) error {
	if queue.fail {
		queue.fail = false
		return errors.New("ack lost after event commit")
	}
	return queue.IntentQueue.CompleteIntent(ctx, lease, at)
}

func TestPostgresIntentDrainerRecoversWithoutDiscoveringNewSubscribers(t *testing.T) {
	for _, scenario := range []string{"sink recovers", "ack lost", "authority recovers", "revoked", "authority exhausted", "empty snapshot"} {
		t.Run(scenario, func(t *testing.T) {
			store, _ := newIntentStore(t)
			if scenario != "empty snapshot" {
				if err := store.SavePreference(t.Context(), preference("original", alice(), domain.ChannelInApp)); err != nil {
					t.Fatal(err)
				}
			}
			commitIntent(t, store, planFailed("handoff-event"))
			bob := domain.Owner{Kind: domain.PrincipalUser, ID: "bob"}
			if err := store.SavePreference(t.Context(), preference("too-late", bob, domain.ChannelEmail)); err != nil {
				t.Fatal(err)
			}
			clock := &intentTestClock{now: at()}
			sink := &intentSink{store: store, fail: scenario == "sink recovers"}
			queue := &intentLostAck{IntentQueue: store, fail: scenario == "ack lost"}
			lookupFails := strings.HasPrefix(scenario, "authority")
			eligibility := intentEligibility(func(_ context.Context, owner domain.Owner, _ string) (bool, error) {
				if owner.ID != "alice" || scenario == "empty snapshot" {
					t.Fatalf("worker discovered a new subscriber: %#v", owner)
				}
				if lookupFails {
					return false, errors.New("current authority unavailable")
				}
				return scenario != "revoked", nil
			})
			worker, err := application.NewIntentDrainer(queue, sink, eligibility, clock, application.IntentDrainerConfig{LeaseDuration: time.Minute, MaximumAttempts: 2, BaseRetryDelay: time.Second, PollInterval: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			found, err := worker.TransferOne(t.Context())
			if !found || (scenario == "ack lost") != (err != nil) {
				t.Fatalf("first transfer=%v %v", found, err)
			}
			if scenario != "revoked" && scenario != "empty snapshot" {
				stats, err := store.Intents(t.Context())
				if err != nil || stats.Pending != 1 {
					t.Fatalf("interrupted intent disappeared: %#v %v", stats, err)
				}
				events, err := store.EventsForResource(t.Context(), resource, 10)
				wantFirstEvents := 0
				if scenario == "ack lost" {
					wantFirstEvents = 1
				}
				if err != nil || len(events) != wantFirstEvents {
					t.Fatalf("failed handoff partially admitted an event: %#v %v", events, err)
				}
				if scenario == "ack lost" {
					clock.now = clock.now.Add(time.Minute)
				} else {
					clock.now = clock.now.Add(time.Second)
				}
				sink.fail = false
				lookupFails = scenario == "authority exhausted"
				if found, err := worker.TransferOne(t.Context()); err != nil || !found {
					t.Fatalf("retry=%v %v", found, err)
				}
			}
			stats, err := store.Intents(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			wantCompleted := int64(1)
			if scenario == "authority exhausted" {
				wantCompleted = 0
			}
			if stats.Pending != 0 || stats.Completed != wantCompleted || stats.Exhausted != 1-wantCompleted {
				t.Fatalf("outcomes=%#v", stats)
			}
			entries, err := store.Inbox(t.Context(), domain.Recipient{Owner: alice(), Channel: domain.ChannelInApp}, 10)
			wantEntries := 1
			if scenario == "revoked" || scenario == "authority exhausted" || scenario == "empty snapshot" {
				wantEntries = 0
			}
			if err != nil || len(entries) != wantEntries {
				t.Fatalf("admitted audience=%#v %v", entries, err)
			}
			assertReplayInbox(t, store, bob, 0)
			events, err := store.EventsForResource(t.Context(), resource, 10)
			if err != nil || len(events) != int(wantCompleted) {
				t.Fatalf("durable events=%#v %v", events, err)
			}
		})
	}
}
