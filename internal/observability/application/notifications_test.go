package application_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/application"
)

func renderDeliveries(
	t *testing.T, recorder *application.DeliveryRecorder, backlog []application.DeliveryBacklog,
) string {
	t.Helper()
	var read application.ReadDeliveryBacklog
	if backlog != nil {
		read = func(context.Context) ([]application.DeliveryBacklog, error) { return backlog, nil }
	}
	registry, err := application.WithNotificationDeliveries(empty, recorder, read)(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Validate(); err != nil {
		t.Fatalf("the delivery metrics would be refused at publish time: %v", err)
	}
	return registry.Render()
}

func TestDeliveryAttemptsAndOutcomesAreCountedPerTransportKind(t *testing.T) {
	t.Parallel()
	recorder := application.NewDeliveryRecorder()
	for range 5 {
		recorder.Record(application.TransportSMTP, application.DeliveryAttempted)
	}
	recorder.Record(application.TransportSMTP, application.DeliverySucceeded)
	recorder.Record(application.TransportSMTP, application.DeliverySucceeded)
	recorder.Record(application.TransportSMTP, application.DeliveryFailed)
	recorder.Record(application.TransportSMTP, application.DeliveryFailed)
	recorder.Record(application.TransportSMTP, application.DeliveryExhausted)
	recorder.Record(application.TransportSlack, application.DeliveryAttempted)
	rendered := renderDeliveries(t, recorder, nil)
	for _, want := range []string{
		`conduit_notification_delivery_attempts_total{transport="smtp"} 5`,
		`conduit_notification_delivery_successes_total{transport="smtp"} 2`,
		// An exhausted attempt is a failed attempt too.
		`conduit_notification_delivery_failures_total{transport="smtp"} 3`,
		`conduit_notification_delivery_exhausted_total{transport="smtp"} 1`,
		`conduit_notification_delivery_attempts_total{transport="slack"} 1`,
		`conduit_notification_delivery_failures_total{transport="slack"} 0`,
		"# TYPE conduit_notification_delivery_attempts_total counter",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %q in:\n%s", want, rendered)
		}
	}
}

// The label is a kind of transport from a closed set. An endpoint, an address
// or a transport nobody has named here is "unknown".
func TestTheTransportLabelIsAClosedSet(t *testing.T) {
	t.Parallel()
	recorder := application.NewDeliveryRecorder()
	for _, kind := range []string{
		"https://hooks.example.test/T000/B000/secret", "alice@example.test", "pagerduty", "", "SMTP",
	} {
		recorder.Record(kind, application.DeliveryAttempted)
	}
	rendered := renderDeliveries(t, recorder, []application.DeliveryBacklog{
		{Transport: "alice@example.test", Pending: 2, OldestPendingAge: time.Minute},
		{Transport: "#ops-channel", Pending: 3, Exhausted: 1, OldestPendingAge: time.Hour},
	})
	for _, want := range []string{
		`conduit_notification_delivery_attempts_total{transport="unknown"} 5`,
		// Two rows that both fold into "unknown" are one series: summed, with
		// the older of the two ages.
		`conduit_notification_delivery_pending{transport="unknown"} 5`,
		`conduit_notification_delivery_exhausted{transport="unknown"} 1`,
		`conduit_notification_delivery_oldest_pending_seconds{transport="unknown"} 3600`,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %q in:\n%s", want, rendered)
		}
	}
	for _, leaked := range []string{"example.test", "hooks", "pagerduty", "ops-channel", "SMTP"} {
		if strings.Contains(rendered, leaked) {
			t.Errorf("%q reached a label:\n%s", leaked, rendered)
		}
	}
	if got := strings.Count(rendered, "conduit_notification_delivery_pending{"); got != 1 {
		t.Errorf("two unknown rows rendered as %d series, which a scraper rejects as a duplicate", got)
	}
}

func TestTheBacklogIsAGaugePerTransportFromTheStore(t *testing.T) {
	t.Parallel()
	rendered := renderDeliveries(t, nil, []application.DeliveryBacklog{
		{Transport: application.TransportWebhook, Pending: 4, Exhausted: 2, OldestPendingAge: 90 * time.Second},
		{Transport: application.TransportInApp},
	})
	for _, want := range []string{
		`conduit_notification_delivery_pending{transport="webhook"} 4`,
		`conduit_notification_delivery_exhausted{transport="webhook"} 2`,
		`conduit_notification_delivery_oldest_pending_seconds{transport="webhook"} 90`,
		`conduit_notification_delivery_oldest_pending_seconds{transport="in_app"} 0`,
		"# TYPE conduit_notification_delivery_exhausted gauge",
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("missing %q in:\n%s", want, rendered)
		}
	}
}

// A backlog that cannot be read fails the scrape. A registry without those
// series would say nothing is waiting.
func TestAFailedBacklogReadFailsTheScrape(t *testing.T) {
	t.Parallel()
	read := func(context.Context) ([]application.DeliveryBacklog, error) {
		return nil, errors.New("database is down")
	}
	registry, err := application.WithNotificationDeliveries(empty, application.NewDeliveryRecorder(), read)(t.Context())
	if err == nil || registry != nil {
		t.Fatal("a failed backlog read produced a registry")
	}
	if untouched, err := application.WithNotificationDeliveries(empty, nil, nil)(t.Context()); err != nil || untouched.Render() != "" {
		t.Fatal("no recorder and no reader still changed the registry")
	}
}
