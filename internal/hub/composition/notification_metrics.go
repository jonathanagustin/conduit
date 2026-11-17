package composition

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
)

// NotificationTransportKind names the kind of transport a delivery on this
// channel goes through, given the transport the Hub was configured with.
//
// The answer is one of a closed set and is a function of configuration and
// channel only -- never of the message, its recipient or its destination. A
// relay transport (webhook, log) carries every channel, so the kind is the
// relay. Native delivery picks an adapter per channel, so the kind is the
// adapter: email goes by SMTP, Slack by Slack, and an in-app entry is
// acknowledged without leaving the process.
//
// Both replicas run the same configuration, so both give the same answer, and
// the database gauges and the process counters share one label.
func NotificationTransportKind(transport ports.Transport, channel domain.Channel) string {
	if transport == nil {
		return metricsapplication.TransportUnknown
	}
	switch transport.Name() {
	case "webhook":
		return metricsapplication.TransportWebhook
	case "log":
		return metricsapplication.TransportLog
	case "native":
		switch channel {
		case domain.ChannelEmail:
			return metricsapplication.TransportSMTP
		case domain.ChannelSlack:
			return metricsapplication.TransportSlack
		case domain.ChannelInApp:
			return metricsapplication.TransportInApp
		}
	}
	return metricsapplication.TransportUnknown
}

// ObserveNotificationDeliveries counts what a dispatcher does with a queue.
//
// It decorates the queue rather than the dispatcher or the transport because
// the queue is where every outcome is committed: an attempt begins with a
// lease, and ends with CompleteDelivery or FailDelivery whichever of render,
// address-book lookup or transport decided it. An outcome is counted only
// after the store accepted it, so a write that failed -- which the dispatcher
// will see again when the lease expires -- is not counted as an outcome that
// did not durably happen.
//
// A nil recorder returns the queue unchanged.
func ObserveNotificationDeliveries(
	queue ports.DeliveryQueue, transport ports.Transport, recorder *metricsapplication.DeliveryRecorder,
) ports.DeliveryQueue {
	if queue == nil || recorder == nil {
		return queue
	}
	return observedDeliveryQueue{queue: queue, transport: transport, recorder: recorder}
}

type observedDeliveryQueue struct {
	queue     ports.DeliveryQueue
	transport ports.Transport
	recorder  *metricsapplication.DeliveryRecorder
}

func (observed observedDeliveryQueue) kind(lease ports.DeliveryLease) string {
	return NotificationTransportKind(observed.transport, lease.Entry.Recipient.Channel)
}

func (observed observedDeliveryQueue) LeaseDelivery(
	ctx context.Context, now time.Time, duration time.Duration,
) (ports.DeliveryLease, bool, error) {
	lease, found, err := observed.queue.LeaseDelivery(ctx, now, duration)
	if err == nil && found {
		observed.recorder.Record(observed.kind(lease), metricsapplication.DeliveryAttempted)
	}
	return lease, found, err
}

func (observed observedDeliveryQueue) CompleteDelivery(
	ctx context.Context, lease ports.DeliveryLease, deliveredAt time.Time,
) error {
	err := observed.queue.CompleteDelivery(ctx, lease, deliveredAt)
	if err == nil {
		observed.recorder.Record(observed.kind(lease), metricsapplication.DeliverySucceeded)
	}
	return err
}

func (observed observedDeliveryQueue) FailDelivery(
	ctx context.Context, lease ports.DeliveryLease, reason string, retryAt time.Time, exhausted bool,
) error {
	err := observed.queue.FailDelivery(ctx, lease, reason, retryAt, exhausted)
	if err == nil {
		outcome := metricsapplication.DeliveryFailed
		if exhausted {
			outcome = metricsapplication.DeliveryExhausted
		}
		observed.recorder.Record(observed.kind(lease), outcome)
	}
	return err
}
