package application

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

type intentQueueProbe struct {
	lease     ports.IntentLease
	taken     bool
	completed int
	failed    string
	terminal  bool
	empty     func()
	following []ports.IntentLease
	failErr   error
}

func (queue *intentQueueProbe) LeaseIntent(context.Context, time.Time, time.Duration) (ports.IntentLease, bool, error) {
	if queue.taken {
		if len(queue.following) > 0 {
			lease := queue.following[0]
			queue.following = queue.following[1:]
			return lease, true, nil
		}
		if queue.empty != nil {
			queue.empty()
		}
		return ports.IntentLease{}, false, nil
	}
	queue.taken = true
	return queue.lease, true, nil
}
func (queue *intentQueueProbe) CompleteIntent(context.Context, ports.IntentLease, time.Time) error {
	queue.completed++
	return nil
}
func (queue *intentQueueProbe) FailIntent(_ context.Context, _ ports.IntentLease, reason string, _ time.Time, terminal bool) error {
	if queue.failErr != nil {
		return queue.failErr
	}
	queue.failed, queue.terminal = reason, terminal
	return nil
}
func intentWorkerConfig() IntentDrainerConfig {
	return IntentDrainerConfig{LeaseDuration: time.Minute, MaximumAttempts: 3, BaseRetryDelay: time.Second, PollInterval: time.Second}
}
func intentWorkerLease() ports.IntentLease {
	return ports.IntentLease{Intent: domain.Intent{Event: planFailed(), Candidates: []domain.ConsentCandidate{{Recipient: domain.Recipient{Owner: alice(), Channel: domain.ChannelInApp}, PreferenceID: "source-consent"}}}, Token: "lease", Attempt: 1}
}

func TestIntentDrainerRequiresAllBoundariesAndPositiveBounds(t *testing.T) {
	queue, store, clock := &intentQueueProbe{}, &fakeStore{}, fixedAt{}
	config := intentWorkerConfig()
	for _, makeWorker := range []func() error{
		func() error { _, err := NewIntentDrainer(nil, store, allowRecipients{}, clock, config); return err },
		func() error { _, err := NewIntentDrainer(queue, nil, allowRecipients{}, clock, config); return err },
		func() error { _, err := NewIntentDrainer(queue, store, nil, clock, config); return err },
		func() error { _, err := NewIntentDrainer(queue, store, allowRecipients{}, nil, config); return err },
	} {
		if err := makeWorker(); err == nil {
			t.Fatal("intent drainer accepted a missing boundary")
		}
	}
	for _, change := range []func(*IntentDrainerConfig){
		func(c *IntentDrainerConfig) { c.LeaseDuration = 0 }, func(c *IntentDrainerConfig) { c.MaximumAttempts = 0 }, func(c *IntentDrainerConfig) { c.BaseRetryDelay = 0 }, func(c *IntentDrainerConfig) { c.PollInterval = 0 },
	} {
		altered := config
		change(&altered)
		if _, err := NewIntentDrainer(queue, store, allowRecipients{}, clock, altered); err == nil {
			t.Fatal("unbounded intent drainer accepted")
		}
	}
}

func TestIntentDrainerCancellationLeavesLeaseWithoutRecordingOrAcknowledging(t *testing.T) {
	queue, store := &intentQueueProbe{lease: intentWorkerLease()}, &fakeStore{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	worker, err := NewIntentDrainer(queue, store, recipientCheck(func(context.Context, domain.Owner, string) (bool, error) { cancel(); return true, nil }), fixedAt{}, intentWorkerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if found, err := worker.TransferOne(ctx); !found || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled transfer=%v %v", found, err)
	}
	if len(store.events) != 0 || queue.completed != 0 || queue.failed != "" {
		t.Fatalf("cancellation became admission or acknowledgement: %#v %#v", store, queue)
	}
}

func TestIntentDrainerUncertainAuthorityCannotPartiallyAdmitKnownRecipients(t *testing.T) {
	queue, store := &intentQueueProbe{lease: intentWorkerLease()}, &fakeStore{}
	queue.lease.Intent.Candidates = append(queue.lease.Intent.Candidates,
		domain.ConsentCandidate{Recipient: domain.Recipient{Owner: alice(), Channel: domain.ChannelEmail}, PreferenceID: "alice-email"},
		domain.ConsentCandidate{Recipient: domain.Recipient{Owner: domain.Owner{Kind: domain.PrincipalTeam, ID: "operators"}, Channel: domain.ChannelInApp}, PreferenceID: "team-consent"},
	)
	checks := map[domain.Owner]int{}
	worker, err := NewIntentDrainer(queue, store, recipientCheck(func(_ context.Context, owner domain.Owner, _ string) (bool, error) {
		checks[owner]++
		if owner.Kind == domain.PrincipalTeam {
			return false, errors.New("current team authority unavailable")
		}
		return true, nil
	}), fixedAt{}, intentWorkerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if found, err := worker.TransferOne(t.Context()); err != nil || !found {
		t.Fatalf("uncertain handoff=%v %v", found, err)
	}
	if len(store.events) != 0 || len(store.entries) != 0 || queue.completed != 0 || queue.failed == "" || queue.terminal || checks[alice()] != 1 {
		t.Fatalf("uncertain authority became partial admission or terminal denial: %#v %#v checks=%#v", store, queue, checks)
	}
}

func TestIntentDrainerRunsInAppWithoutReadingLivePreferences(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queue := &intentQueueProbe{lease: intentWorkerLease(), empty: cancel}
	store := &fakeStore{preferencesErr: errors.New("live preferences must never be read")}
	worker, err := NewIntentDrainer(queue, store, allowRecipients{}, fixedAt{}, intentWorkerConfig())
	if err != nil {
		t.Fatal(err)
	}
	worker.Run(ctx)
	if len(store.events) != 1 || len(store.entries) != 1 || store.entries[0].Recipient.Channel != domain.ChannelInApp || store.preferenceReads != 0 || queue.completed != 1 {
		t.Fatalf("in-app frozen handoff failed: %#v %#v", store, queue)
	}
}

func TestInvalidIntentRetainsExplicitTerminalFailure(t *testing.T) {
	queue, store := &intentQueueProbe{lease: intentWorkerLease()}, &fakeStore{}
	queue.lease.Intent.Candidates[0].PreferenceID = ""
	worker, err := NewIntentDrainer(queue, store, allowRecipients{}, fixedAt{}, intentWorkerConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.TransferOne(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !queue.terminal || queue.failed == "" || queue.completed != 0 || len(store.events) != 0 {
		t.Fatalf("invalid intent was lost or admitted: %#v %#v", queue, store)
	}
}

func TestIntentDrainerReportsDurableRetriesExhaustionAndInvalidIntent(t *testing.T) {
	for _, scenario := range []string{"retry", "exhausted", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			queue := &intentQueueProbe{lease: intentWorkerLease()}
			queue.lease.Intent.Event.Summary = "private-contact@example.test"
			queue.lease.Intent.Candidates[0].Recipient.Owner.ID = "private-recipient"
			queue.lease.Intent.Candidates[0].PreferenceID = "private-preference"
			if scenario == "exhausted" {
				queue.lease.Attempt = 3
			}
			if scenario == "invalid" {
				queue.lease.Intent.Candidates[0].PreferenceID = ""
			}
			var diagnostics []string
			config := intentWorkerConfig()
			config.OnError = func(err error) {
				if queue.failed == "" {
					t.Fatal("diagnostic claimed an outcome before its persistence")
				}
				diagnostics = append(diagnostics, err.Error())
			}
			worker, err := NewIntentDrainer(queue, &fakeStore{}, recipientCheck(func(context.Context, domain.Owner, string) (bool, error) {
				return false, errors.New("directory unavailable")
			}), fixedAt{}, config)
			if err != nil {
				t.Fatal(err)
			}
			if found, err := worker.TransferOne(t.Context()); err != nil || !found {
				t.Fatalf("retained failure stopped the worker: %v %v", found, err)
			}
			if len(diagnostics) != 1 {
				t.Fatalf("durable failure was silent or reported twice: %#v", diagnostics)
			}
			message := diagnostics[0]
			for _, field := range []string{`event_id="nev-1"`, fmt.Sprintf("attempt=%d/3", queue.lease.Attempt), fmt.Sprintf("exhausted=%t", scenario != "retry")} {
				if !strings.Contains(message, field) {
					t.Errorf("diagnostic omitted %q: %s", field, message)
				}
			}
			wantReason := "directory unavailable"
			if scenario == "invalid" {
				wantReason = "invalid intent"
			}
			if !strings.Contains(message, wantReason) || strings.Contains(message, "private-") {
				t.Fatalf("diagnostic lost its reason or copied recipient/contact payload: %s", message)
			}
		})
	}
}

func TestIntentDrainerBoundsQuotedDiagnosticAndContinuesOtherDueWork(t *testing.T) {
	first, next := intentWorkerLease(), intentWorkerLease()
	first.Intent.Event.ID = "event-prefix-" + strings.Repeat("\x00", 10_000)
	first.Intent.Event.ResourceRID = "environment/uncertain"
	next.Intent.Event.ID = "next-event"
	queue := &intentQueueProbe{lease: first, following: []ports.IntentLease{next}}
	store := &fakeStore{}
	var diagnostics []string
	config := intentWorkerConfig()
	config.OnError = func(err error) { diagnostics = append(diagnostics, err.Error()) }
	worker, err := NewIntentDrainer(queue, store, recipientCheck(func(_ context.Context, _ domain.Owner, resource string) (bool, error) {
		if resource == "environment/uncertain" {
			return false, errors.New("lookup unavailable: " + strings.Repeat("\x00", 10_000))
		}
		return true, nil
	}), fixedAt{}, config)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(diagnostics) != 1 || len(diagnostics[0]) > 4096 || strings.ContainsRune(diagnostics[0], '\x00') || !strings.Contains(diagnostics[0], "event-prefix-") || !strings.Contains(diagnostics[0], "lookup unavailable") {
		t.Fatalf("diagnostic was absent, unbounded or unquoted: %#v", diagnostics)
	}
	if queue.completed != 1 || len(store.events) != 1 || store.events[0].ID != "next-event" {
		t.Fatalf("a retained retry starved other due work: completed=%d events=%#v", queue.completed, store.events)
	}
}

func TestIntentDrainerReportsPersistenceFailureWithoutClaimingRetainedOutcome(t *testing.T) {
	queue := &intentQueueProbe{lease: intentWorkerLease(), failErr: errors.New("failure outcome could not be persisted")}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var diagnostics []error
	config := intentWorkerConfig()
	config.OnError = func(err error) { diagnostics = append(diagnostics, err); cancel() }
	worker, err := NewIntentDrainer(queue, &fakeStore{recordErr: errors.New("event recorder unavailable")}, allowRecipients{}, fixedAt{}, config)
	if err != nil {
		t.Fatal(err)
	}
	worker.Run(ctx)
	if len(diagnostics) != 1 || !errors.Is(diagnostics[0], queue.failErr) || strings.Contains(diagnostics[0].Error(), "notification intent retained") || queue.failed != "" || queue.completed != 0 {
		t.Fatalf("failed persistence was lost or falsely reported as a retained outcome: %#v %#v", diagnostics, queue)
	}
}
