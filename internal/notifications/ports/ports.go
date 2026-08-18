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
// It carries no address. Nothing in this repository stores a person's email
// address or a team's Slack channel, so a port that demanded one would demand a
// value Conduit cannot produce; a transport is configured with where it sends
// the way the metrics listener is configured with where it listens. Name is for
// operator-facing logs, so "which of the configured transports refused this"
// is answerable without reading the wiring.
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
