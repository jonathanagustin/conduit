package application_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/application"
	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

func moment() time.Time { return time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC) }

type fixedClock struct{ now time.Time }

func (clock *fixedClock) Now() time.Time { return clock.now }

// queuedEntry is one row of the fake queue, carrying the same state the
// PostgreSQL columns carry.
type queuedEntry struct {
	entry       domain.InboxEntry
	attempt     uint32
	token       string
	availableAt time.Time
	deliveredAt time.Time
	exhausted   bool
	lastError   string
}

// fakeQueue is an in-memory DeliveryQueue with the same contract as the
// PostgreSQL one: due, undelivered and non-terminal entries only, one lease at
// a time, and a stale token is an error rather than a silent success.
type fakeQueue struct {
	mutex    sync.Mutex
	rows     []*queuedEntry
	leaseErr error
	failErr  error
}

func (queue *fakeQueue) LeaseDelivery(
	_ context.Context, now time.Time, duration time.Duration,
) (ports.DeliveryLease, bool, error) {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	if queue.leaseErr != nil {
		return ports.DeliveryLease{}, false, queue.leaseErr
	}
	for _, row := range queue.rows {
		if row.exhausted || !row.deliveredAt.IsZero() || row.availableAt.After(now) || row.token != "" {
			continue
		}
		row.attempt++
		row.token = fmt.Sprintf("token-%d", row.attempt)
		return ports.DeliveryLease{
			Entry: row.entry, Token: row.token, Attempt: row.attempt,
			ExpiresAt: now.Add(duration),
		}, true, nil
	}
	return ports.DeliveryLease{}, false, nil
}

func (queue *fakeQueue) find(lease ports.DeliveryLease) *queuedEntry {
	for _, row := range queue.rows {
		if row.entry.Event.ID == lease.Entry.Event.ID &&
			row.entry.Recipient == lease.Entry.Recipient && row.token == lease.Token {
			return row
		}
	}
	return nil
}

func (queue *fakeQueue) CompleteDelivery(
	_ context.Context, lease ports.DeliveryLease, deliveredAt time.Time,
) error {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	row := queue.find(lease)
	if row == nil {
		return errors.New("stale notification delivery lease")
	}
	row.deliveredAt = deliveredAt
	row.token = ""
	row.lastError = ""
	return nil
}

func (queue *fakeQueue) FailDelivery(
	_ context.Context, lease ports.DeliveryLease, reason string, retryAt time.Time, exhausted bool,
) error {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	if queue.failErr != nil {
		return queue.failErr
	}
	row := queue.find(lease)
	if row == nil {
		return errors.New("stale notification delivery lease")
	}
	row.token = ""
	row.availableAt = retryAt
	row.exhausted = exhausted
	row.lastError = reason
	return nil
}

type recordingTransport struct {
	mutex     sync.Mutex
	delivered []domain.Message
	err       error
}

func (transport *recordingTransport) Name() string { return "recording" }

func (transport *recordingTransport) Deliver(_ context.Context, message domain.Message) error {
	transport.mutex.Lock()
	defer transport.mutex.Unlock()
	if transport.err != nil {
		return transport.err
	}
	transport.delivered = append(transport.delivered, message)
	return nil
}

func deliverableEntry(id string, channel domain.Channel) domain.InboxEntry {
	return domain.InboxEntry{
		Recipient: domain.Recipient{
			Owner: domain.Owner{Kind: domain.PrincipalUser, ID: "alice"}, Channel: channel,
		},
		Event: domain.Event{
			ID: id, Type: domain.EventPlanFailures,
			ResourceRID: "ri.apollo.main.environment.d41d8cd98f00", Environment: "tenant-a",
			Summary:    "Reconciliation of installation mi-catalog in Environment tenant-a gave up",
			OccurredAt: moment(),
		},
		PreferenceID: "pref-1",
	}
}

func dispatcher(t *testing.T, queue *fakeQueue, transport ports.Transport, clock ports.Clock) *application.Dispatcher {
	t.Helper()
	built, err := application.NewDispatcher(queue, transport, clock, application.DispatcherConfig{
		LeaseDuration: 30 * time.Second, MaximumAttempts: 3,
		BaseRetryDelay: 2 * time.Second, PollInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func TestTheDispatcherDrainsWhatNobodyWouldOtherwiseSee(t *testing.T) {
	t.Parallel()
	queue := &fakeQueue{rows: []*queuedEntry{
		{entry: deliverableEntry("nev-1", domain.ChannelEmail), availableAt: moment()},
		{entry: deliverableEntry("nev-2", domain.ChannelEmail), availableAt: moment()},
	}}
	transport := &recordingTransport{}
	if err := dispatcher(t, queue, transport, &fixedClock{now: moment()}).Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(transport.delivered) != 2 {
		t.Fatalf("the transport was handed %d messages, want 2", len(transport.delivered))
	}
	for _, row := range queue.rows {
		if row.deliveredAt.IsZero() {
			t.Errorf("entry %s was sent and not marked delivered, so it will be sent again",
				row.entry.Event.ID)
		}
	}
}

// The heart of at-least-once: a transport that fails leaves the entry pending
// with a later due time, not delivered and not terminal.
func TestAFailedDeliveryIsRetriedWithBackoffAndNotMarkedDelivered(t *testing.T) {
	t.Parallel()
	queue := &fakeQueue{rows: []*queuedEntry{
		{entry: deliverableEntry("nev-1", domain.ChannelEmail), availableAt: moment()},
	}}
	transport := &recordingTransport{err: errors.New("connection refused")}
	if err := dispatcher(t, queue, transport, &fixedClock{now: moment()}).Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	row := queue.rows[0]
	if !row.deliveredAt.IsZero() {
		t.Fatal("a delivery nobody received was recorded as delivered")
	}
	if row.exhausted {
		t.Fatal("one failed attempt out of three exhausted the budget")
	}
	if want := moment().Add(2 * time.Second); !row.availableAt.Equal(want) {
		t.Errorf("the retry is due at %s, want the base delay at %s", row.availableAt, want)
	}
	if !strings.Contains(row.lastError, "connection refused") ||
		!strings.Contains(row.lastError, "recording") {
		t.Errorf("the recorded reason names neither the failure nor the transport: %q", row.lastError)
	}
}

// Backoff has to actually double, or a broken endpoint is retried at a fixed
// rate for the whole budget.
func TestRetriesBackOffExponentiallyUntilTheBudgetIsSpent(t *testing.T) {
	t.Parallel()
	queue := &fakeQueue{rows: []*queuedEntry{
		{entry: deliverableEntry("nev-1", domain.ChannelEmail), availableAt: moment()},
	}}
	transport := &recordingTransport{err: errors.New("connection refused")}
	clock := &fixedClock{now: moment()}
	dispatch := dispatcher(t, queue, transport, clock)
	for _, want := range []time.Duration{2 * time.Second, 4 * time.Second, 0} {
		queue.rows[0].availableAt = clock.now
		delivered, err := dispatch.DeliverOne(t.Context())
		if err != nil || !delivered {
			t.Fatalf("attempt did not run: delivered=%v err=%v", delivered, err)
		}
		if want == 0 {
			break
		}
		if got := queue.rows[0].availableAt.Sub(clock.now); got != want {
			t.Errorf("attempt %d backs off %s, want %s", queue.rows[0].attempt, got, want)
		}
	}
	row := queue.rows[0]
	if !row.exhausted {
		t.Fatal("the third of three attempts did not exhaust the budget, so nothing is ever terminal")
	}
	if !row.deliveredAt.IsZero() {
		t.Error("an exhausted entry was also marked delivered")
	}
}

// A permanent failure must not spend the budget. Nine more identical answers
// from an endpoint that already understood and refused the request are nine
// more minutes of latency for everything queued behind it.
func TestAPermanentTransportFailureIsTerminalOnTheFirstAttempt(t *testing.T) {
	t.Parallel()
	queue := &fakeQueue{rows: []*queuedEntry{
		{entry: deliverableEntry("nev-1", domain.ChannelEmail), availableAt: moment()},
	}}
	transport := &recordingTransport{
		err: fmt.Errorf("%w: endpoint answered 404", ports.ErrPermanentDelivery),
	}
	if err := dispatcher(t, queue, transport, &fixedClock{now: moment()}).Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	row := queue.rows[0]
	if !row.exhausted {
		t.Fatal("a permanent failure was queued for retry, spending the budget on a settled answer")
	}
	if row.attempt != 1 {
		t.Errorf("a permanent failure cost %d attempts, want 1", row.attempt)
	}
	if !strings.Contains(row.lastError, "404") {
		t.Errorf("the terminal reason does not say what the endpoint answered: %q", row.lastError)
	}
}

// An entry that cannot be rendered will never render. It becomes terminal
// without spending a budget or sending anything.
func TestAnUnrenderableEntryIsTerminalAndNeverReachesTheTransport(t *testing.T) {
	t.Parallel()
	broken := deliverableEntry("nev-1", domain.ChannelEmail)
	broken.Event.Summary = ""
	queue := &fakeQueue{rows: []*queuedEntry{{entry: broken, availableAt: moment()}}}
	transport := &recordingTransport{}
	if err := dispatcher(t, queue, transport, &fixedClock{now: moment()}).Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(transport.delivered) != 0 {
		t.Fatal("a message that could not be rendered was sent anyway")
	}
	if !queue.rows[0].exhausted {
		t.Fatal("an entry that can never render was queued for retry forever")
	}
	if !strings.Contains(queue.rows[0].lastError, "render") {
		t.Errorf("the reason does not say the entry could not be rendered: %q", queue.rows[0].lastError)
	}
}

// A store that cannot be read is not a condition more attempts improve, and a
// Drain that swallowed it would loop forever reporting success.
func TestDrainStopsWhenTheQueueItselfFails(t *testing.T) {
	t.Parallel()
	queue := &fakeQueue{leaseErr: errors.New("connection reset by peer")}
	err := dispatcher(t, queue, &recordingTransport{}, &fixedClock{now: moment()}).Drain(t.Context())
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("Drain hid a queue failure: %v", err)
	}
}

func TestADispatcherWithNothingToDeliverWithIsRefused(t *testing.T) {
	t.Parallel()
	valid := application.DispatcherConfig{
		LeaseDuration: time.Second, MaximumAttempts: 1,
		BaseRetryDelay: time.Second, PollInterval: time.Second,
	}
	for name, build := range map[string]func() (*application.Dispatcher, error){
		"no transport": func() (*application.Dispatcher, error) {
			return application.NewDispatcher(&fakeQueue{}, nil, &fixedClock{}, valid)
		},
		"no queue": func() (*application.Dispatcher, error) {
			return application.NewDispatcher(nil, &recordingTransport{}, &fixedClock{}, valid)
		},
		"no clock": func() (*application.Dispatcher, error) {
			return application.NewDispatcher(&fakeQueue{}, &recordingTransport{}, nil, valid)
		},
		"an unbounded attempt budget": func() (*application.Dispatcher, error) {
			unbounded := valid
			unbounded.MaximumAttempts = 0
			return application.NewDispatcher(&fakeQueue{}, &recordingTransport{}, &fixedClock{}, unbounded)
		},
		"no retry delay": func() (*application.Dispatcher, error) {
			hammering := valid
			hammering.BaseRetryDelay = 0
			return application.NewDispatcher(&fakeQueue{}, &recordingTransport{}, &fixedClock{}, hammering)
		},
		"no poll interval": func() (*application.Dispatcher, error) {
			spinning := valid
			spinning.PollInterval = 0
			return application.NewDispatcher(&fakeQueue{}, &recordingTransport{}, &fixedClock{}, spinning)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := build(); err == nil {
				t.Errorf("a dispatcher with %s was constructed", name)
			}
		})
	}
}

// Run has to stop with its context, or a Hub shutting down waits on a ticker.
func TestRunStopsWithItsContext(t *testing.T) {
	t.Parallel()
	queue := &fakeQueue{rows: []*queuedEntry{
		{entry: deliverableEntry("nev-1", domain.ChannelEmail), availableAt: moment()},
	}}
	transport := &recordingTransport{}
	dispatch := dispatcher(t, queue, transport, &fixedClock{now: moment()})
	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan struct{})
	go func() {
		dispatch.Run(ctx)
		close(stopped)
	}()
	deadline := time.After(5 * time.Second)
	for {
		queue.mutex.Lock()
		done := !queue.rows[0].deliveredAt.IsZero()
		queue.mutex.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Run never delivered the queued entry")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run ignored its cancelled context")
	}
}

// resolvingContacts is a DestinationResolver a test can point wherever it
// likes, including at a failure.
type resolvingContacts struct {
	destinations map[domain.Recipient]domain.Destination
	err          error
	calls        int
}

var _ ports.DestinationResolver = (*resolvingContacts)(nil)

func (contacts *resolvingContacts) DestinationFor(
	_ context.Context, recipient domain.Recipient,
) (domain.Destination, bool, error) {
	contacts.calls++
	if contacts.err != nil {
		return domain.Destination{}, false, contacts.err
	}
	destination, found := contacts.destinations[recipient]
	return destination, found, nil
}

func routingDispatcher(
	t *testing.T, queue *fakeQueue, transport ports.Transport,
	resolver ports.DestinationResolver,
) *application.Dispatcher {
	t.Helper()
	built, err := application.NewDispatcher(queue, transport, &fixedClock{now: moment()},
		application.DispatcherConfig{
			LeaseDuration: 30 * time.Second, MaximumAttempts: 3,
			BaseRetryDelay: 2 * time.Second, PollInterval: time.Millisecond,
			Destinations: resolver,
		})
	if err != nil {
		t.Fatal(err)
	}
	return built
}

// The point of the address book, seen from the delivery path: the message the
// transport is handed names the recipient rather than only the endpoint.
func TestADeliveryCarriesTheRecipientsOwnDestination(t *testing.T) {
	t.Parallel()
	entry := deliverableEntry("nev-1", domain.ChannelEmail)
	queue := &fakeQueue{rows: []*queuedEntry{{entry: entry, availableAt: moment()}}}
	transport := &recordingTransport{}
	resolver := &resolvingContacts{destinations: map[domain.Recipient]domain.Destination{
		entry.Recipient: {Kind: domain.ContactEmail, Address: "alice@example.test"},
	}}
	if err := routingDispatcher(t, queue, transport, resolver).Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(transport.delivered) != 1 {
		t.Fatalf("the transport was handed %d messages, want 1", len(transport.delivered))
	}
	if got := transport.delivered[0].Destination; got.Address != "alice@example.test" ||
		got.Kind != domain.ContactEmail {
		t.Fatalf("the message named destination %+v, want the recipient's own contact", got)
	}
}

// The fallback, stated as a test so nobody has to take the comment's word for
// it: a recipient with no contact still delivers, and names no destination, so
// the transport sends where its operator configured it to.
func TestARecipientWithNoContactFallsBackToTheConfiguredEndpoint(t *testing.T) {
	t.Parallel()
	entry := deliverableEntry("nev-1", domain.ChannelEmail)
	queue := &fakeQueue{rows: []*queuedEntry{{entry: entry, availableAt: moment()}}}
	transport := &recordingTransport{}
	resolver := &resolvingContacts{destinations: map[domain.Recipient]domain.Destination{}}
	if err := routingDispatcher(t, queue, transport, resolver).Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(transport.delivered) != 1 {
		t.Fatalf("a recipient with no contact was not delivered at all: %d messages", len(transport.delivered))
	}
	if transport.delivered[0].Destination.Named() {
		t.Fatalf("a recipient with no contact was given destination %+v",
			transport.delivered[0].Destination)
	}
	if queue.rows[0].deliveredAt.IsZero() {
		t.Error("the fallback delivery was not recorded as delivered")
	}
}

// A dispatcher with no address book at all is the deployment every Hub was
// before contacts existed, and it must be unchanged rather than broken.
func TestADispatcherWithNoAddressBookDeliversExactlyAsBefore(t *testing.T) {
	t.Parallel()
	queue := &fakeQueue{rows: []*queuedEntry{
		{entry: deliverableEntry("nev-1", domain.ChannelEmail), availableAt: moment()},
	}}
	transport := &recordingTransport{}
	if err := dispatcher(t, queue, transport, &fixedClock{now: moment()}).Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(transport.delivered) != 1 || transport.delivered[0].Destination.Named() {
		t.Fatalf("a dispatcher with no resolver delivered %#v", transport.delivered)
	}
}

// The one case where falling back would be wrong. A store that could not be
// read is not a recipient with no address, and delivering to the operator's
// endpoint on a failed query would send a person's notification somewhere they
// did not choose. The entry is retried instead, and nothing is sent.
func TestAnUnreadableAddressBookRetriesRatherThanDeliveringElsewhere(t *testing.T) {
	t.Parallel()
	entry := deliverableEntry("nev-1", domain.ChannelEmail)
	queue := &fakeQueue{rows: []*queuedEntry{{entry: entry, availableAt: moment()}}}
	transport := &recordingTransport{}
	resolver := &resolvingContacts{err: errors.New("connection reset by peer")}
	if err := routingDispatcher(t, queue, transport, resolver).Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(transport.delivered) != 0 {
		t.Fatalf("a delivery was sent with an unresolved destination: %#v", transport.delivered)
	}
	row := queue.rows[0]
	if !row.deliveredAt.IsZero() {
		t.Fatal("an entry nobody received was recorded as delivered")
	}
	if row.exhausted {
		t.Fatal("one unreadable read out of three attempts exhausted the budget")
	}
	if want := moment().Add(2 * time.Second); !row.availableAt.Equal(want) {
		t.Errorf("the retry is due at %s, want the base delay at %s", row.availableAt, want)
	}
	if !strings.Contains(row.lastError, "resolve destination") {
		t.Errorf("the recorded reason does not say what failed: %q", row.lastError)
	}
	// The reason must not name the transport, which was never asked.
	if strings.Contains(row.lastError, "recording transport") {
		t.Errorf("a resolver failure was recorded against the transport: %q", row.lastError)
	}
}

// A resolver failure that outlasts the budget becomes terminal, like every
// other repeated failure: the alternative is an entry retried forever.
func TestARepeatedlyUnreadableAddressBookExhaustsItsBudget(t *testing.T) {
	t.Parallel()
	entry := deliverableEntry("nev-1", domain.ChannelEmail)
	queue := &fakeQueue{rows: []*queuedEntry{{entry: entry, availableAt: moment()}}}
	resolver := &resolvingContacts{err: errors.New("connection reset by peer")}
	dispatch := routingDispatcher(t, queue, &recordingTransport{}, resolver)
	for attempt := 0; attempt < 3; attempt++ {
		queue.rows[0].availableAt = moment()
		if _, err := dispatch.DeliverOne(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if !queue.rows[0].exhausted {
		t.Fatal("three failed resolutions out of a budget of three left the entry pending")
	}
}
