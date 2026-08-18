// Package ports declares what notifications needs from the world and what the
// world may ask of notifications, without choosing a database, a transport or a
// clock.
//
// Emission and delivery are separate concerns, and the separation is now
// expressed by two interfaces rather than by the absence of one. Store covers
// everything up to the inbox: something happened, these people wanted to know,
// here is the durable record that they were told. DeliveryQueue and Transport
// cover what happens after, and they are declared apart from Store on purpose --
// a Store that also sent email would make recording an event fail when a mail
// server did, which is the failure mode the split exists to prevent.
package ports

import (
	"context"
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
)

// Store holds events, who they reached, and the preferences that decided.
//
// RecordEvent takes the event and its fan-out together rather than exposing an
// entry writer, because an event stored without its entries is a notification
// nobody was told about that reads exactly like one everybody was: the two
// writes have to be one transaction, and a port that could not express that
// would push the requirement onto every caller.
type Store interface {
	// RecordEvent commits an event and the entries it reached in one
	// transaction. It is idempotent on the event's ID: an emitter that crashes
	// between writing and acknowledging will emit the same event again, and the
	// second call must not duplicate an inbox entry.
	RecordEvent(ctx context.Context, event domain.Event, entries []domain.InboxEntry) error
	// PreferencesFor returns every stored preference for one resource, on every
	// channel and for every owner. Filtering happens in the domain, which needs
	// the whole set to fan out over.
	PreferencesFor(ctx context.Context, resourceRID string) ([]domain.Preference, error)
	// SavePreference stores or replaces one owner's settings for one resource on
	// one channel, which is the grain Apollo's settings page edits.
	SavePreference(ctx context.Context, preference domain.Preference) error
	// Inbox reads what reached one recipient, most recent first.
	Inbox(ctx context.Context, recipient domain.Recipient, limit int) ([]domain.InboxEntry, error)
	// EventsForResource reads what was recorded about a resource, most recent
	// first, whether or not anybody was subscribed to it.
	//
	// This is the read that makes "nobody was told" answerable. Without it an
	// operator asking why they heard nothing cannot distinguish a Hub that
	// raised no event from one that raised it and found no subscriber, and those
	// call for opposite responses.
	EventsForResource(ctx context.Context, resourceRID string, limit int) ([]domain.Event, error)
}

// Emitter is the inbound port every emit site depends on, so a site that raises
// an event needs neither a store nor the fan-out rule.
type Emitter interface {
	Emit(ctx context.Context, event domain.Event) error
}

// Contacts is the address book: one destination per (owner, kind).
//
// Declared apart from Store rather than folded into it, for the same reason
// Transport is kept out of Store. An address is personal data with its own
// lifetime -- created by the person it belongs to, changed when they move team,
// deleted when they ask -- while an event is immutable and an inbox entry is
// written by a fan-out. A component that only needs to read a contact should
// not be able to record an event, and a narrow port is what makes that true
// rather than promised.
//
// Keyed on (owner, kind) because that is what the New Team Contact dialog
// creates: a contact of a kind, for an owner. A second contact of the same kind
// for the same owner is an edit of the first rather than a second destination,
// so SaveContact replaces.
type Contacts interface {
	// SaveContact stores or replaces one owner's destination of one kind.
	SaveContact(ctx context.Context, contact domain.Contact) error
	// ContactsFor returns every contact one owner holds, in a stable order, so
	// resolving the same recipient twice chooses the same destination.
	ContactsFor(ctx context.Context, owner domain.Owner) ([]domain.Contact, error)
	// DeleteContact removes one, reporting whether there was one to remove.
	//
	// Deletion is declared here rather than left to a later change, because the
	// value being stored is somebody's address. "Conduit may record it and you
	// may not withdraw it" is not a defensible position for personal data, and a
	// port that could only write would have taken it by omission.
	DeleteContact(ctx context.Context, owner domain.Owner, kind domain.ContactKind) (bool, error)
}

// DestinationResolver answers where one recipient's own contacts point.
//
// The delivery path depends on this rather than on Contacts, because it has no
// business reading a contact it is not delivering to and none at all writing
// one. A false second return means "this recipient names no destination of
// their own", which the dispatcher reads as "send where the transport was
// configured to send" rather than as a failure.
type DestinationResolver interface {
	DestinationFor(ctx context.Context, recipient domain.Recipient) (domain.Destination, bool, error)
}

// Clock is the only source of now in this context, so a dispatcher's backoff
// and lease arithmetic can be tested without sleeping.
type Clock interface {
	Now() time.Time
}

// ErrPermanentDelivery marks a failure that retrying cannot fix.
//
// A transport returns it wrapped -- an endpoint that answers 404 or 401 will
// answer the same way in an hour, and spending a ten-attempt budget discovering
// that turns one misconfiguration into ten identical log lines and an hour of
// queue latency for everything behind it. Everything else is assumed
// retryable, which is the safe default in the other direction: a signal
// retried once too often is noise, a signal dropped once too early is the gap
// this whole path exists to close.
var ErrPermanentDelivery = errors.New("notification delivery cannot succeed by retrying")

// Transport hands a rendered message to the world and reports whether the
// world took it.
//
// The address, when there is one, arrives on the message rather than in this
// signature. A transport is still configured with where it sends, the way the
// metrics listener is configured with where it listens -- what changed when
// contacts landed is that a message may now also name the recipient's own
// destination, which a transport that can honour one should prefer and a
// transport that cannot may ignore. Name is for operator-facing logs, so "which
// of the configured transports refused this" is answerable without reading the
// wiring.
//
// A transport is the one place an address is allowed to travel to. It must not
// be logged, put in an error, or attached to a metric on the way.
type Transport interface {
	Name() string
	Deliver(ctx context.Context, message domain.Message) error
}

// DeliveryLease is one undelivered inbox entry, claimed for one attempt.
//
// Attempt is the count including this claim, so the first lease reports 1. It
// is the same shape reconciliation's ProposalLease carries, for the same
// reason: the budget has to be visible to the code deciding whether this
// failure is the terminal one.
type DeliveryLease struct {
	Entry     domain.InboxEntry
	Token     string
	Attempt   uint32
	ExpiresAt time.Time
}

// DeliveryQueue is at-least-once delivery over the inbox.
//
// Deliberately the same shape as the reconciliation proposal outbox --
// lease with an expiry and an attempt counter, complete, or fail with a retry
// time and a durable terminal flag. That outbox already solved this problem
// against real PostgreSQL with SKIP LOCKED and a partial index, and a second
// solution to a solved problem is a second set of bugs.
//
// What differs is only that a delivered entry is retained rather than deleted:
// a proposal's completion produces a plan receipt that records it happened,
// while an inbox entry is the record, and deleting it on delivery would erase
// what the recipient is meant to read.
type DeliveryQueue interface {
	// LeaseDelivery claims one entry that is due, not yet delivered and not
	// terminal, or reports that there is none.
	LeaseDelivery(ctx context.Context, now time.Time, duration time.Duration) (DeliveryLease, bool, error)
	// CompleteDelivery marks the leased entry delivered. A stale lease is an
	// error rather than a silent no-op, because the entry has been claimed by
	// somebody else and reporting success would hide a double send.
	CompleteDelivery(ctx context.Context, lease DeliveryLease, deliveredAt time.Time) error
	// FailDelivery records why, when to try again, and whether there is any
	// point. An exhausted entry is never leased again and keeps its reason.
	FailDelivery(ctx context.Context, lease DeliveryLease, reason string, retryAt time.Time, exhausted bool) error
}
