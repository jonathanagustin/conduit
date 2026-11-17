package application

import (
	"context"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/observability/domain"
)

// Transport kinds: the closed set of values the `transport` label may take.
//
// A kind of destination, never a destination. "webhook" and not the endpoint,
// "smtp" and not the relay or the mailbox, "slack" and not the workspace. The
// recorder maps anything outside the set to TransportUnknown, so a transport
// added later is visible as "unknown" until it is named here, rather than
// minting a label value from whatever its Name() returns.
const (
	TransportWebhook = "webhook"
	TransportLog     = "log"
	TransportSMTP    = "smtp"
	TransportSlack   = "slack"
	TransportInApp   = "in_app"
	TransportUnknown = "unknown"
)

var transportKinds = map[string]struct{}{
	TransportWebhook: {}, TransportLog: {}, TransportSMTP: {}, TransportSlack: {},
	TransportInApp: {}, TransportUnknown: {},
}

func boundedTransport(kind string) string {
	if _, known := transportKinds[kind]; known {
		return kind
	}
	return TransportUnknown
}

// DeliveryOutcome is how one delivery attempt ended.
type DeliveryOutcome int

const (
	// DeliveryAttempted: an entry was leased. Every attempt starts here.
	DeliveryAttempted DeliveryOutcome = iota
	// DeliverySucceeded: the transport took it and the store recorded that.
	DeliverySucceeded
	// DeliveryFailed: the attempt failed and the entry will be retried.
	DeliveryFailed
	// DeliveryExhausted: the attempt failed and no further one will be made.
	// It is also a failure; Record counts it as both.
	DeliveryExhausted
)

type deliveryCounts struct{ attempts, successes, failures, exhausted uint64 }

// DeliveryRecorder counts what this replica's dispatcher did.
//
// Counters, in the process, summed across replicas by the scraper -- the same
// division as HTTPRecorder. What is shared between replicas (how much is
// waiting, how long, how much is dead) is read from the database by
// WithNotificationDeliveries instead, because a counter in one process cannot
// know what the other leased.
type DeliveryRecorder struct {
	mutex  sync.Mutex
	counts map[string]*deliveryCounts
}

// NewDeliveryRecorder builds an empty recorder.
func NewDeliveryRecorder() *DeliveryRecorder {
	return &DeliveryRecorder{counts: map[string]*deliveryCounts{}}
}

// Record counts one event for one kind of transport.
func (recorder *DeliveryRecorder) Record(transport string, outcome DeliveryOutcome) {
	if recorder == nil {
		return
	}
	transport = boundedTransport(transport)
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	counts := recorder.counts[transport]
	if counts == nil {
		counts = &deliveryCounts{}
		recorder.counts[transport] = counts
	}
	switch outcome {
	case DeliveryAttempted:
		counts.attempts++
	case DeliverySucceeded:
		counts.successes++
	case DeliveryFailed:
		counts.failures++
	case DeliveryExhausted:
		counts.failures++
		counts.exhausted++
	}
}

// DeliveryBacklog is the state of the durable delivery queue for one kind of
// transport, as the database has it.
type DeliveryBacklog struct {
	Transport        string
	Pending          int
	Exhausted        int
	OldestPendingAge time.Duration
}

// ReadDeliveryBacklog is the store call, narrowed.
type ReadDeliveryBacklog func(context.Context) ([]DeliveryBacklog, error)

// WithNotificationDeliveries adds notification delivery to a gathered registry.
//
// A failed backlog read fails the scrape, for the reason Gather gives: a
// registry without these series would read as "nothing is waiting".
func WithNotificationDeliveries(
	gather func(context.Context) (*domain.Registry, error), recorder *DeliveryRecorder, read ReadDeliveryBacklog,
) func(context.Context) (*domain.Registry, error) {
	if recorder == nil && read == nil {
		return gather
	}
	return func(ctx context.Context) (*domain.Registry, error) {
		registry, err := gather(ctx)
		if err != nil {
			return nil, err
		}
		if read != nil {
			backlog, err := read(ctx)
			if err != nil {
				return nil, err
			}
			// Summed by bounded kind, so two rows that both map to "unknown"
			// are one series and not a duplicate.
			merged := map[string]DeliveryBacklog{}
			for _, row := range backlog {
				kind := boundedTransport(row.Transport)
				sum := merged[kind]
				sum.Pending += row.Pending
				sum.Exhausted += row.Exhausted
				if row.OldestPendingAge > sum.OldestPendingAge {
					sum.OldestPendingAge = row.OldestPendingAge
				}
				merged[kind] = sum
			}
			for kind, row := range merged {
				labels := map[string]string{"transport": kind}
				registry.Add(domain.Metric{
					Name: "conduit_notification_delivery_pending", Kind: domain.KindGauge, Value: float64(row.Pending),
					Help:   "notifications waiting to be delivered, including those backing off before a retry",
					Labels: labels,
				})
				registry.Add(domain.Metric{
					Name: "conduit_notification_delivery_exhausted", Kind: domain.KindGauge, Value: float64(row.Exhausted),
					Help:   "notifications that will never be delivered: retries ran out or the failure was permanent",
					Labels: labels,
				})
				registry.Add(domain.Metric{
					Name: "conduit_notification_delivery_oldest_pending_seconds", Kind: domain.KindGauge,
					Value:  row.OldestPendingAge.Seconds(),
					Help:   "how long the longest-waiting undelivered notification has been waiting since it was raised",
					Labels: labels,
				})
			}
		}
		if recorder != nil {
			recorder.mutex.Lock()
			defer recorder.mutex.Unlock()
			for kind, counts := range recorder.counts {
				labels := map[string]string{"transport": kind}
				for name, entry := range map[string]struct {
					help  string
					value uint64
				}{
					"conduit_notification_delivery_attempts_total":  {"delivery attempts this replica started", counts.attempts},
					"conduit_notification_delivery_successes_total": {"attempts the transport accepted", counts.successes},
					"conduit_notification_delivery_failures_total":  {"attempts that failed, whether or not a retry follows", counts.failures},
					"conduit_notification_delivery_exhausted_total": {"attempts after which this replica gave up on a notification", counts.exhausted},
				} {
					registry.Add(domain.Metric{
						Name: name, Kind: domain.KindCounter, Value: float64(entry.value), Help: entry.help, Labels: labels,
					})
				}
			}
		}
		return registry, nil
	}
}
