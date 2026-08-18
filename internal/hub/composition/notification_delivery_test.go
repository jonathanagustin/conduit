package composition_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

func environment(pairs map[string]string) func(string) string {
	return func(name string) string { return pairs[name] }
}

// The whole opt-in, stated as a test: an existing deployment that sets nothing
// gains no transport, so no dispatcher is built, so no loop runs and nothing
// leaves the process.
func TestDeliveryIsOffUntilAnOperatorAsksForIt(t *testing.T) {
	t.Parallel()
	transport, err := composition.NewNotificationTransport(environment(nil), nil)
	if err != nil {
		t.Fatal(err)
	}
	if transport != nil {
		t.Fatalf("an unconfigured deployment built a %s transport", transport.Name())
	}
	dispatcher, err := composition.NewNotificationDispatcher(
		nilQueue{}, transport, stoppedClock{}, composition.DefaultDispatcherConfig())
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher != nil {
		t.Fatal("a dispatcher was started with nowhere to deliver")
	}
}

func TestAConfiguredWebhookBuildsTheWebhookTransport(t *testing.T) {
	t.Parallel()
	transport, err := composition.NewNotificationTransport(environment(map[string]string{
		composition.NotificationWebhookURLVariable:     "https://hooks.example.com/conduit",
		composition.NotificationWebhookTimeoutVariable: "3s",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if transport == nil || transport.Name() != "webhook" {
		t.Fatalf("transport = %v", transport)
	}
	dispatcher, err := composition.NewNotificationDispatcher(
		nilQueue{}, transport, stoppedClock{}, composition.DefaultDispatcherConfig())
	if err != nil {
		t.Fatal(err)
	}
	if dispatcher == nil {
		t.Fatal("a configured transport built no dispatcher, so nothing would drain the queue")
	}
}

func TestTheLogTransportIsAvailableWithoutAnEndpoint(t *testing.T) {
	t.Parallel()
	transport, err := composition.NewNotificationTransport(environment(map[string]string{
		composition.NotificationDeliveryLogVariable: "true",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if transport == nil || transport.Name() != "log" {
		t.Fatalf("transport = %v", transport)
	}
}

// A misconfiguration is a startup failure rather than a silent choice: a Hub
// that came up cleanly and told nobody anything is worse than one that refused.
func TestAMisconfiguredTransportRefusesToStart(t *testing.T) {
	t.Parallel()
	for name, variables := range map[string]map[string]string{
		"two destinations at once": {
			composition.NotificationWebhookURLVariable:  "https://hooks.example.com/conduit",
			composition.NotificationDeliveryLogVariable: "true",
		},
		"a timeout with no endpoint": {
			composition.NotificationWebhookTimeoutVariable: "3s",
		},
		"a timeout that is not a duration": {
			composition.NotificationWebhookURLVariable:     "https://hooks.example.com/conduit",
			composition.NotificationWebhookTimeoutVariable: "soon",
		},
		"a timeout that never expires": {
			composition.NotificationWebhookURLVariable:     "https://hooks.example.com/conduit",
			composition.NotificationWebhookTimeoutVariable: "0s",
		},
		"an endpoint nothing can POST to": {
			composition.NotificationWebhookURLVariable: "ftp://files.example.com/conduit",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := composition.NewNotificationTransport(environment(variables), nil); err == nil {
				t.Errorf("%s was accepted", name)
			}
		})
	}
	if _, err := composition.NewNotificationTransport(nil, nil); err == nil {
		t.Error("a transport was configured with no environment to read")
	}
}

// A configured transport with no durable queue is a configuration that cannot
// work, and answering nil would leave the operator believing delivery is on.
func TestAConfiguredTransportWithNoQueueIsRefused(t *testing.T) {
	t.Parallel()
	transport, err := composition.NewNotificationTransport(environment(map[string]string{
		composition.NotificationDeliveryLogVariable: "true",
	}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := composition.NewNotificationDispatcher(
		nil, transport, stoppedClock{}, composition.DefaultDispatcherConfig()); err == nil {
		t.Error("notification delivery was enabled with no queue behind it")
	}
}

func TestTheDefaultRetryBudgetMatchesReconciliations(t *testing.T) {
	t.Parallel()
	config := composition.DefaultDispatcherConfig()
	if config.MaximumAttempts != 10 || config.BaseRetryDelay != 2*time.Second ||
		config.LeaseDuration != 30*time.Second || config.PollInterval != time.Second {
		t.Errorf("the delivery budget diverged from the reconciliation worker's: %#v", config)
	}
}

func TestTheWebhookVariableNamesStaySpelledTheWayOperatorsSetThem(t *testing.T) {
	t.Parallel()
	for _, variable := range []string{
		composition.NotificationWebhookURLVariable,
		composition.NotificationWebhookTimeoutVariable,
		composition.NotificationDeliveryLogVariable,
	} {
		if !strings.HasPrefix(variable, "NOTIFICATION_") {
			t.Errorf("%q is not in the NOTIFICATION_ namespace an operator would look under", variable)
		}
	}
}

type stoppedClock struct{}

func (stoppedClock) Now() time.Time { return time.Unix(0, 0).UTC() }

type nilQueue struct{}

func (nilQueue) LeaseDelivery(context.Context, time.Time, time.Duration) (ports.DeliveryLease, bool, error) {
	return ports.DeliveryLease{}, false, nil
}

func (nilQueue) CompleteDelivery(context.Context, ports.DeliveryLease, time.Time) error { return nil }

func (nilQueue) FailDelivery(context.Context, ports.DeliveryLease, string, time.Time, bool) error {
	return nil
}
