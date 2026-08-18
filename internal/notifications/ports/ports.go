// Package ports declares what notifications needs from the world and what the
// world may ask of notifications, without choosing a database, a transport or a
// clock.
//
// There is no delivery port here yet, and its absence is deliberate rather than
// pending. Emission and delivery are separate concerns -- the domain package
// says so and the reason holds at this layer too: delivery needs a transport, a
// credential and a retry policy, and a Store that also sent email would make
// recording an event fail when a mail server did. What this package covers is
// everything up to the inbox: something happened, these people wanted to know,
// and here is the durable record that they were told.
package ports

import (
	"context"

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
