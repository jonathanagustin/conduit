package composition

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
	observability "github.com/jonathanagustin/conduit/internal/observability/domain"
)

type namedTransport struct {
	name string
	fail error
}

func (transport namedTransport) Name() string { return transport.name }

func (transport namedTransport) Deliver(context.Context, domain.Message) error { return transport.fail }

// scriptedQueue offers its entries once each and can be told to refuse writes.
type scriptedQueue struct {
	entries   []domain.Channel
	attempt   uint32
	writeFail error
}

func (queue *scriptedQueue) LeaseDelivery(context.Context, time.Time, time.Duration) (ports.DeliveryLease, bool, error) {
	if len(queue.entries) == 0 {
		return ports.DeliveryLease{}, false, nil
	}
	channel := queue.entries[0]
	queue.entries = queue.entries[1:]
	attempt := queue.attempt
	if attempt == 0 {
		attempt = 1
	}
	return ports.DeliveryLease{Token: "ndl_test", Attempt: attempt, Entry: domain.InboxEntry{
		Recipient: domain.Recipient{Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice@example.test"}, Channel: channel},
		Event: domain.Event{
			ID: "nev-1", Type: domain.EventPlanFailures, ResourceRID: "ri.conduit.main.environment.tenant-a",
			Environment: "tenant-a", Summary: "it failed", OccurredAt: time.Unix(0, 0).UTC(),
		},
	}}, true, nil
}

func (queue *scriptedQueue) CompleteDelivery(context.Context, ports.DeliveryLease, time.Time) error {
	return queue.writeFail
}

func (queue *scriptedQueue) FailDelivery(context.Context, ports.DeliveryLease, string, time.Time, bool) error {
	return queue.writeFail
}

func drain(t *testing.T, queue *scriptedQueue, transport ports.Transport, recorder *metricsapplication.DeliveryRecorder) string {
	t.Helper()
	dispatcher, err := NewNotificationDispatcher(
		ObserveNotificationDeliveries(queue, transport, recorder), transport, application.SystemClock{}, DefaultDispatcherConfig())
	if err != nil {
		t.Fatal(err)
	}
	_ = dispatcher.Drain(t.Context())
	registry, err := metricsapplication.WithNotificationDeliveries(func(context.Context) (*observability.Registry, error) {
		return observability.NewRegistry(), nil
	}, recorder, nil)(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatalf("the delivery metrics would be refused at publish time: %v", err)
	}
	return registry.Render()
}

func TestADispatchersOutcomesAreCountedThroughTheRealDispatcher(t *testing.T) {
	t.Parallel()
	recorder := metricsapplication.NewDeliveryRecorder()
	delivered := drain(t, &scriptedQueue{entries: []domain.Channel{domain.ChannelEmail, domain.ChannelEmail}},
		namedTransport{name: "webhook"}, recorder)
	for _, want := range []string{
		`conduit_notification_delivery_attempts_total{transport="webhook"} 2`,
		`conduit_notification_delivery_successes_total{transport="webhook"} 2`,
		`conduit_notification_delivery_failures_total{transport="webhook"} 0`,
	} {
		if !strings.Contains(delivered, want) {
			t.Errorf("missing %q in:\n%s", want, delivered)
		}
	}

	// A refused send is a failure that will be retried; a permanent one, or
	// the last of the budget, is also an exhaustion.
	retried := drain(t, &scriptedQueue{entries: []domain.Channel{domain.ChannelEmail}},
		namedTransport{name: "log", fail: errors.New("refused")}, recorder)
	permanent := drain(t, &scriptedQueue{entries: []domain.Channel{domain.ChannelEmail}},
		namedTransport{name: "log", fail: ports.ErrPermanentDelivery}, recorder)
	if !strings.Contains(retried, `conduit_notification_delivery_failures_total{transport="log"} 1`) ||
		!strings.Contains(retried, `conduit_notification_delivery_exhausted_total{transport="log"} 0`) {
		t.Errorf("a retryable failure was miscounted:\n%s", retried)
	}
	if !strings.Contains(permanent, `conduit_notification_delivery_failures_total{transport="log"} 2`) ||
		!strings.Contains(permanent, `conduit_notification_delivery_exhausted_total{transport="log"} 1`) {
		t.Errorf("a permanent failure was miscounted:\n%s", permanent)
	}
	// The recipient is in the lease; it must not be in the exposition.
	if strings.Contains(permanent, "alice") || strings.Contains(permanent, "tenant-a") {
		t.Fatalf("a recipient or environment reached a label:\n%s", permanent)
	}
}

// An outcome the store did not accept did not durably happen: the lease will
// expire and the entry will be attempted again. Counting it would report a
// delivery nobody can find in the database.
func TestAnOutcomeTheStoreRefusedIsNotCounted(t *testing.T) {
	t.Parallel()
	recorder := metricsapplication.NewDeliveryRecorder()
	rendered := drain(t, &scriptedQueue{entries: []domain.Channel{domain.ChannelEmail}, writeFail: errors.New("stale lease")},
		namedTransport{name: "webhook"}, recorder)
	if !strings.Contains(rendered, `conduit_notification_delivery_attempts_total{transport="webhook"} 1`) ||
		!strings.Contains(rendered, `conduit_notification_delivery_successes_total{transport="webhook"} 0`) {
		t.Fatalf("an unrecorded outcome was counted:\n%s", rendered)
	}
}

func TestTheTransportKindIsAFunctionOfConfigurationAndChannelOnly(t *testing.T) {
	t.Parallel()
	smtp, slack := namedTransport{name: "smtp"}, namedTransport{name: "slack"}
	native, err := application.NewChannelTransport(map[domain.Channel]ports.Transport{
		domain.ChannelEmail: smtp, domain.ChannelSlack: slack,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct {
		transport ports.Transport
		channel   domain.Channel
		want      string
	}{
		{namedTransport{name: "webhook"}, domain.ChannelEmail, "webhook"},
		{namedTransport{name: "webhook"}, domain.ChannelSlack, "webhook"},
		{namedTransport{name: "log"}, domain.ChannelInApp, "log"},
		{native, domain.ChannelEmail, "smtp"},
		{native, domain.ChannelSlack, "slack"},
		{native, domain.ChannelInApp, "in_app"},
		{native, domain.Channel("PAGER"), "unknown"},
		{namedTransport{name: "https://hooks.example.test/secret"}, domain.ChannelEmail, "unknown"},
		{nil, domain.ChannelEmail, "unknown"},
	} {
		if got := NotificationTransportKind(check.transport, check.channel); got != check.want {
			t.Errorf("kind(%v, %s) = %q, want %q", check.transport, check.channel, got, check.want)
		}
	}
	if ObserveNotificationDeliveries(&scriptedQueue{}, smtp, nil) == nil {
		t.Error("a nil recorder discarded the queue")
	}
}
