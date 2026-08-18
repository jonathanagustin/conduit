package application

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// DispatcherConfig bounds one dispatcher's retry behaviour.
//
// Every field is required and positive. There is no zero-value default,
// because the two defaults that would matter -- an unbounded attempt budget and
// a zero retry delay -- are both ways of hammering a broken endpoint forever,
// and a config that silently chose one would do it in a deployment nobody
// inspected.
//
// Destinations is the one field that may be absent, and its absence is a real
// deployment rather than a degraded one: a Hub with no address book delivers
// every entry to the endpoint its operator configured, which is what every Hub
// did before contacts existed. Supplying one narrows each delivery to the
// recipient's own destination wherever they have one.
type DispatcherConfig struct {
	LeaseDuration   time.Duration
	MaximumAttempts uint32
	BaseRetryDelay  time.Duration
	PollInterval    time.Duration
	OnError         func(error)
	// Destinations resolves the recipient's own contact at delivery time. Nil
	// means every delivery goes wherever the transport was configured to send.
	Destinations ports.DestinationResolver
}

// Dispatcher drains undelivered inbox entries through one transport.
//
// It is the missing half of Tier 1 gap 1: the emitter records who should be
// told, and this tells them. Every Hub replica may run one, exactly as every
// replica may run the reconciliation supervisor -- the database lease picks the
// winner for each entry, so two replicas racing produce one delivery rather
// than two.
type Dispatcher struct {
	queue     ports.DeliveryQueue
	transport ports.Transport
	clock     ports.Clock
	config    DispatcherConfig
}

// NewDispatcher refuses a dispatcher that could not deliver.
//
// The transport is required rather than optional, and the difference matters:
// "no transport configured" is answered by not constructing a dispatcher at
// all, in the composition root, so a deployment that has not asked for outbound
// delivery runs no loop and opens no socket. A nil transport here would instead
// be a dispatcher that leases entries, sends them nowhere and marks them
// delivered -- silence that looks like success, which is worse than the gap.
func NewDispatcher(
	queue ports.DeliveryQueue, transport ports.Transport, clock ports.Clock, config DispatcherConfig,
) (*Dispatcher, error) {
	if queue == nil || transport == nil || clock == nil {
		return nil, errors.New("a notification delivery queue, transport, and clock are required")
	}
	if config.LeaseDuration <= 0 || config.MaximumAttempts == 0 ||
		config.BaseRetryDelay <= 0 || config.PollInterval <= 0 {
		return nil, errors.New("notification delivery lease, attempts, retry delay, and poll interval must be positive")
	}
	return &Dispatcher{queue: queue, transport: transport, clock: clock, config: config}, nil
}

// DeliverOne claims at most one entry and reports whether there was one.
//
// The order is lease, render, send, record -- and recording the outcome is the
// last step on every path, so a crash anywhere before it leaves the entry
// leased, the lease expiring, and the entry delivered again later. That is
// at-least-once and it is the deliberate choice: a duplicate notification is a
// nuisance, and a lost one is the failure this closes.
func (dispatcher *Dispatcher) DeliverOne(ctx context.Context) (bool, error) {
	now := dispatcher.clock.Now().UTC()
	lease, found, err := dispatcher.queue.LeaseDelivery(ctx, now, dispatcher.config.LeaseDuration)
	if err != nil || !found {
		return found, err
	}
	message, err := domain.Render(lease.Entry)
	if err != nil {
		// Terminal on the first attempt, with no backoff spent. An entry that
		// does not render will not render differently in four seconds, and the
		// budget exists to outlast an outage rather than a malformed row.
		return true, dispatcher.queue.FailDelivery(
			ctx, lease, fmt.Sprintf("render: %v", err), now, true)
	}
	message.Destination, err = dispatcher.destination(ctx, lease.Entry.Recipient)
	if err != nil {
		// Retried rather than sent, and this is the one place where falling back
		// would be the wrong answer. A store that could not be read is not a
		// recipient with no contact: delivering to the operator's endpoint here
		// would send a person's notification somewhere they did not choose,
		// because a query failed. The attempt is spent and the entry comes back.
		return true, dispatcher.retry(ctx, lease, fmt.Sprintf("resolve destination: %v", err), now)
	}
	if err := dispatcher.transport.Deliver(ctx, message); err != nil {
		return true, dispatcher.fail(ctx, lease, err, now)
	}
	return true, dispatcher.queue.CompleteDelivery(ctx, lease, dispatcher.clock.Now().UTC())
}

// destination resolves where this recipient's own contacts point.
//
// A dispatcher with no resolver answers the zero destination and no error,
// which the transport reads as "send where you were configured to send". So
// does a recipient who holds no deliverable contact. The two are the same
// answer on purpose: the operator-configured endpoint is the fallback, and the
// recipient's own contact is what displaces it. What is *not* folded in here is
// a failed read -- that returns an error, and DeliverOne retries rather than
// delivering to the fallback.
//
// The address never reaches this function's error path, because the only error
// it can return is the store's own.
func (dispatcher *Dispatcher) destination(
	ctx context.Context, recipient domain.Recipient,
) (domain.Destination, error) {
	if dispatcher.config.Destinations == nil {
		return domain.Destination{}, nil
	}
	destination, found, err := dispatcher.config.Destinations.DestinationFor(ctx, recipient)
	if err != nil {
		return domain.Destination{}, err
	}
	if !found {
		return domain.Destination{}, nil
	}
	return destination, nil
}

// retry records one failed attempt that the transport never saw, spending the
// budget the same way a refused send does.
//
// Separate from fail because the reason must not name a transport that was
// never asked, and because a resolver failure is never permanent on its own:
// the store it read is the store this Hub needs for everything else, so if it
// stays broken the attempt budget is the right thing to run out.
func (dispatcher *Dispatcher) retry(
	ctx context.Context, lease ports.DeliveryLease, reason string, now time.Time,
) error {
	exhausted := lease.Attempt >= dispatcher.config.MaximumAttempts
	retryAt := now
	if !exhausted {
		retryAt = dispatcher.retryAt(now, lease.Attempt)
	}
	return dispatcher.queue.FailDelivery(ctx, lease, reason, retryAt, exhausted)
}

// fail records one failed attempt and decides whether it was the last.
//
// Two independent ways to become terminal, and keeping them separate is the
// point. A permanent transport failure -- the endpoint answered 404, or the URL
// names a host that does not resolve to anything a retry would reach -- is
// terminal now, because nine more attempts would produce nine more identical
// answers while everything queued behind it waits. A spent attempt budget is
// terminal because it ran out. Collapsing the two would make a misconfigured
// webhook cost a full budget of latency for every entry behind it.
func (dispatcher *Dispatcher) fail(
	ctx context.Context, lease ports.DeliveryLease, cause error, now time.Time,
) error {
	permanent := errors.Is(cause, ports.ErrPermanentDelivery)
	exhausted := permanent || lease.Attempt >= dispatcher.config.MaximumAttempts
	retryAt := now
	if !exhausted {
		retryAt = dispatcher.retryAt(now, lease.Attempt)
	}
	reason := fmt.Sprintf("%s transport: %v", dispatcher.transport.Name(), cause)
	return dispatcher.queue.FailDelivery(ctx, lease, reason, retryAt, exhausted)
}

// Drain delivers until the queue reports nothing due.
//
// It stops on the first error rather than continuing, because the errors this
// returns are the store failing rather than a transport failing -- a transport
// failure is recorded on the entry and the loop keeps going. A store that
// cannot be written is not a condition more attempts improve.
func (dispatcher *Dispatcher) Drain(ctx context.Context) error {
	for {
		delivered, err := dispatcher.DeliverOne(ctx)
		if err != nil {
			return err
		}
		if !delivered {
			return nil
		}
	}
}

// Run drains on a ticker until the context ends.
func (dispatcher *Dispatcher) Run(ctx context.Context) {
	ticker := time.NewTicker(dispatcher.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := dispatcher.Drain(ctx); err != nil && dispatcher.config.OnError != nil {
				dispatcher.config.OnError(err)
			}
		}
	}
}

// SystemClock is the clock a running Hub uses. Tests supply their own, which
// is the entire reason the port exists.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// retryAt is exponential backoff capped at an hour, the same curve
// reconciliation's worker uses. One curve rather than two so an operator
// reading two queues does not have to hold two mental models of when the next
// attempt lands.
func (dispatcher *Dispatcher) retryAt(now time.Time, attempt uint32) time.Time {
	delay := dispatcher.config.BaseRetryDelay
	for count := uint32(1); count < attempt && delay < time.Hour; count++ {
		delay *= 2
	}
	if delay > time.Hour {
		delay = time.Hour
	}
	return now.Add(delay)
}
