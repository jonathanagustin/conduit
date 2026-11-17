package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

type recipientCheck func(context.Context, domain.Owner, string) (bool, error)

func (check recipientCheck) Check(ctx context.Context, owner domain.Owner, resource string) (bool, error) {
	return check(ctx, owner, resource)
}

type beforeSubmission func()

func (resolve beforeSubmission) DestinationFor(context.Context, domain.Recipient) (domain.Destination, bool, error) {
	resolve()
	return domain.Destination{}, false, nil
}

func TestRecipientAuthorizationIsRequiredForExternalDelivery(t *testing.T) {
	_, err := application.NewDispatcher(&fakeQueue{}, &recordingTransport{}, &fixedClock{}, application.DispatcherConfig{
		LeaseDuration: time.Minute, MaximumAttempts: 3, BaseRetryDelay: time.Second, PollInterval: time.Second,
	})
	if err == nil {
		t.Fatal("a dispatcher without current recipient authorization was accepted")
	}
}

func TestARevokedRecipientIsRefusedImmediatelyBeforeExternalSubmission(t *testing.T) {
	for _, channel := range []domain.Channel{domain.ChannelEmail, domain.ChannelSlack} {
		t.Run(string(channel), func(t *testing.T) {
			entry := deliverableEntry("nev-revoked", channel)
			queue := &fakeQueue{rows: []*queuedEntry{{entry: entry, availableAt: moment()}}}
			transport := &recordingTransport{}
			allowed := true
			worker, err := application.NewDispatcher(queue, transport, &fixedClock{now: moment()}, application.DispatcherConfig{
				LeaseDuration: time.Minute, MaximumAttempts: 3, BaseRetryDelay: time.Second, PollInterval: time.Second,
				Destinations: beforeSubmission(func() { allowed = false }),
				Eligibility: recipientCheck(func(_ context.Context, owner domain.Owner, resource string) (bool, error) {
					if owner != entry.Recipient.Owner || resource != entry.Event.ResourceRID {
						t.Fatalf("checked somebody else's authority: %#v %s", owner, resource)
					}
					return allowed, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			if found, err := worker.DeliverOne(t.Context()); err != nil || !found {
				t.Fatalf("refusal was not recorded: found=%v %v", found, err)
			}
			row := queue.rows[0]
			if len(transport.delivered) != 0 || !row.deliveredAt.IsZero() || !row.exhausted || !strings.Contains(row.lastError, "recipient authorization refused") {
				t.Fatalf("revoked recipient was sent or counted as delivered: sent=%#v queue=%#v", transport.delivered, row)
			}
			if found, err := worker.DeliverOne(t.Context()); err != nil || found {
				t.Fatalf("an explicit refusal was retried: found=%v %v", found, err)
			}
		})
	}
}

func TestAnUnknownRecipientAuthorityRetriesWithoutSendingOrMarkingDelivered(t *testing.T) {
	for _, recovers := range []bool{true, false} {
		t.Run(map[bool]string{true: "lookup recovers", false: "lookup exhausts budget"}[recovers], func(t *testing.T) {
			entry := deliverableEntry("nev-unknown", domain.ChannelEmail)
			queue := &fakeQueue{rows: []*queuedEntry{{entry: entry, availableAt: moment()}}}
			transport := &recordingTransport{}
			clock := &fixedClock{now: moment()}
			lookupFails := true
			worker, err := application.NewDispatcher(queue, transport, clock, application.DispatcherConfig{
				LeaseDuration: time.Minute, MaximumAttempts: 2, BaseRetryDelay: 2 * time.Second, PollInterval: time.Second,
				Eligibility: recipientCheck(func(context.Context, domain.Owner, string) (bool, error) {
					if lookupFails {
						return false, errors.New("current authority lookup unavailable")
					}
					return true, nil
				}),
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := worker.DeliverOne(t.Context()); err != nil {
				t.Fatal(err)
			}
			row := queue.rows[0]
			if row.exhausted || !row.deliveredAt.IsZero() || len(transport.delivered) != 0 || !strings.Contains(row.lastError, "authorize recipient: current authority lookup unavailable") || !row.availableAt.Equal(moment().Add(2*time.Second)) {
				t.Fatalf("lookup uncertainty became denial or delivery: %#v sent=%#v", row, transport.delivered)
			}
			clock.now = row.availableAt
			lookupFails = !recovers
			if _, err := worker.DeliverOne(t.Context()); err != nil {
				t.Fatal(err)
			}
			if recovers {
				if row.deliveredAt.IsZero() || row.exhausted || len(transport.delivered) != 1 {
					t.Fatalf("recovered authority was not delivered: %#v sent=%#v", row, transport.delivered)
				}
			} else if !row.exhausted || !row.deliveredAt.IsZero() || len(transport.delivered) != 0 {
				t.Fatalf("exhausted uncertainty was counted as sent: %#v sent=%#v", row, transport.delivered)
			}
		})
	}
}

func TestDeliveryCancelledDuringEligibilityCheckNeverReachesTheTransport(t *testing.T) {
	entry := deliverableEntry("nev-cancelled", domain.ChannelEmail)
	queue := &fakeQueue{rows: []*queuedEntry{{entry: entry, availableAt: moment()}}}
	transport := &recordingTransport{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	worker, err := application.NewDispatcher(queue, transport, &fixedClock{now: moment()}, application.DispatcherConfig{
		LeaseDuration: time.Minute, MaximumAttempts: 3, BaseRetryDelay: time.Second, PollInterval: time.Second,
		Eligibility: recipientCheck(func(context.Context, domain.Owner, string) (bool, error) {
			cancel()
			return true, nil
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.DeliverOne(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation was not reported: %v", err)
	}
	if len(transport.delivered) != 0 || !queue.rows[0].deliveredAt.IsZero() || queue.rows[0].exhausted {
		t.Fatalf("cancelled work was sent or made terminal: %#v %#v", transport.delivered, queue.rows[0])
	}
}
