// Package application turns something that happened into a durable record of
// who was told. It depends only on the notifications domain and its ports.
package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/notifications/domain"
	"github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// Emitter records an event and the inbox entries it produced.
type Emitter struct {
	store ports.Store
}

var _ ports.Emitter = (*Emitter)(nil)

func NewEmitter(store ports.Store) (*Emitter, error) {
	if store == nil {
		return nil, errors.New("a notification store is required; an emitter with nowhere to write is the gap it exists to close")
	}
	return &Emitter{store: store}, nil
}

// Emit validates the event, resolves who wanted to know, and commits both.
//
// The event is recorded even when it reaches nobody, and that is the whole
// shape of this method rather than an edge case. An emitter that stored only
// what it delivered would make "no operator is subscribed to this Environment"
// indistinguishable from "the Hub never raised the event" -- and those two
// have opposite fixes, one a settings change and one a bug. The recorded event
// with no entries says which it was.
//
// Fan-out is read-then-decide against the preferences that exist now. A
// preference created a second later does not reach back for events it missed,
// which matches what a subscription means: you are told about what happens
// after you ask, not about what happened before.
func (emitter *Emitter) Emit(ctx context.Context, event domain.Event) error {
	if err := event.Validate(); err != nil {
		return fmt.Errorf("refuse notification event: %w", err)
	}
	preferences, err := emitter.store.PreferencesFor(ctx, event.ResourceRID)
	if err != nil {
		return fmt.Errorf("read notification preferences for %s: %w", event.ResourceRID, err)
	}
	if err := emitter.store.RecordEvent(ctx, event, domain.Fan(event, preferences)); err != nil {
		return fmt.Errorf("record notification event %s: %w", event.ID, err)
	}
	return nil
}

// Reader answers the two questions an operator asks after the fact: what
// reached me, and what did the Hub raise about this resource at all.
type Reader struct {
	store ports.Store
}

func NewReader(store ports.Store) (*Reader, error) {
	if store == nil {
		return nil, errors.New("a notification store is required to read notifications")
	}
	return &Reader{store: store}, nil
}

// MaximumLimit bounds an unbounded request. A read path with no ceiling is a
// way to ask a control plane to load its whole history into memory, and every
// caller that forgets to pass one gets the same answer size.
//
// Exported, with Bound beside it, because a transport that pages has to be able
// to tell a caller what it actually applied. A ceiling a client cannot observe
// looks exactly like an empty next page.
const MaximumLimit = 100

// Inbox reads what reached one recipient, most recent first.
func (reader *Reader) Inbox(
	ctx context.Context, recipient domain.Recipient, limit int,
) ([]domain.InboxEntry, error) {
	if recipient.Owner.Kind != domain.PrincipalUser && recipient.Owner.Kind != domain.PrincipalTeam {
		return nil, errors.New("an inbox belongs to a USER or a TEAM")
	}
	if !recipient.Channel.Valid() {
		return nil, fmt.Errorf("notification channel %q is not one an inbox is kept on", recipient.Channel)
	}
	return reader.store.Inbox(ctx, recipient, Bound(limit))
}

// EventsForResource reads what the Hub raised about a resource, subscribed to
// or not.
func (reader *Reader) EventsForResource(
	ctx context.Context, resourceRID string, limit int,
) ([]domain.Event, error) {
	if resourceRID == "" {
		return nil, errors.New("reading events requires the resource they are about")
	}
	return reader.store.EventsForResource(ctx, resourceRID, Bound(limit))
}

// Bound reports the limit a read will actually use.
func Bound(limit int) int {
	if limit <= 0 || limit > MaximumLimit {
		return MaximumLimit
	}
	return limit
}
