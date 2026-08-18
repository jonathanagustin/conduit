package domain

import (
	"fmt"
	"strings"
	"time"
)

// NeedsTransport reports whether an inbox entry has to leave the process before
// the recipient learns anything.
//
// In-app entries do not. The inbox row *is* the in-app delivery: Hub Settings
// carries a "My notifications" page, the tenant viewer role names it, and
// application.Reader.Inbox already answers it, so handing an IN_APP entry to a
// mail relay or a webhook would deliver it somewhere its owner never chose. An
// EMAIL entry is the opposite case and is the whole of Tier 1 gap 1: it is
// recorded, nothing carries it, and the operator learns only by reading.
//
// This is a rule about channels rather than about transports on purpose. A
// deployment with no transport configured still records which entries would
// have needed one, so enabling a transport later is a configuration change
// rather than a schema change.
func NeedsTransport(entry InboxEntry) bool {
	return entry.Recipient.Channel != ChannelInApp
}

// Message is one inbox entry rendered for a transport.
//
// Rendered here, in the domain, rather than in each adapter, because two
// transports that phrase the same failure differently give an operator two
// facts to reconcile during an incident. The transport chooses the envelope;
// the wording is one decision made once.
//
// Destination is where this one goes, and it is the field that used to be
// absent. The comment here said a Message could not carry an address because
// nothing in this repository stored one, which was true and was the whole of
// the readiness document's Tier 1 gap 1. A store holds one now, so a delivery
// names the recipient's own destination whenever they have a contact for it.
//
// The zero value still means what the absence meant: this delivery names no
// destination of its own, and the transport sends where it was configured to
// send. That is the ordinary case for a deployment with an endpoint and no
// contacts, so upgrading changes where nothing goes.
//
// Render does not fill it. Rendering is a pure function of one entry, and a
// destination is a fact about the recipient's address book read at delivery
// time; putting that read here would make the wording of a message depend on a
// store.
// CONDUIT-INVENTED: notifications.Message
// Displaced-by: a capture of what Apollo puts in a notification email or Slack
// post, in particular whether the subject names the event type or the resource
type Message struct {
	// EventID and Recipient together are the delivery's identity, so a
	// transport that deduplicates has something stable to deduplicate on.
	EventID     string      `json:"eventId"`
	Recipient   Recipient   `json:"recipient"`
	Destination Destination `json:"destination,omitzero"`
	Type        EventType   `json:"type"`
	ResourceRID string      `json:"resourceRid"`
	Environment string      `json:"environment,omitempty"`
	Subject     string      `json:"subject"`
	Body        string      `json:"body"`
	OccurredAt  time.Time   `json:"occurredAt"`
}

// Render turns one inbox entry into the message a transport sends.
//
// A pure function of the entry, and it fails rather than guessing: an entry
// whose event does not validate cannot be phrased into anything a person should
// act on, and the caller's correct response is to stop retrying it rather than
// to send a half-rendered one every few seconds forever.
func Render(entry InboxEntry) (Message, error) {
	if err := entry.Event.Validate(); err != nil {
		return Message{}, fmt.Errorf("render notification: %w", err)
	}
	if !entry.Recipient.Channel.Valid() {
		return Message{}, fmt.Errorf(
			"render notification: channel %q is not one an inbox is kept on", entry.Recipient.Channel)
	}
	if strings.TrimSpace(entry.Recipient.Owner.ID) == "" {
		return Message{}, fmt.Errorf("render notification: the entry names no recipient")
	}
	return Message{
		EventID:     entry.Event.ID,
		Recipient:   entry.Recipient,
		Type:        entry.Event.Type,
		ResourceRID: entry.Event.ResourceRID,
		Environment: entry.Event.Environment,
		Subject:     subject(entry.Event),
		Body:        body(entry.Event),
		OccurredAt:  entry.Event.OccurredAt.UTC(),
	}, nil
}

// subject leads with the event type because that is what a preference filtered
// on: a reader who receives this already chose to receive this type, and the
// type is the word they chose it by.
func subject(event Event) string {
	if strings.TrimSpace(event.Environment) != "" {
		return fmt.Sprintf("[Conduit] %s in %s", event.Type, event.Environment)
	}
	return fmt.Sprintf("[Conduit] %s", event.Type)
}

// body states the sentence the emitter wrote, then the facts needed to act on
// it without opening the console.
//
// The actor line is present only when a person caused the event, because the
// domain reserves an empty Actor for events the system raised on its own and
// "actor: (none)" reads as a missing value rather than as a fact.
func body(event Event) string {
	lines := []string{
		event.Summary,
		"",
		"Event: " + event.ID,
		"Type: " + string(event.Type),
		"Resource: " + event.ResourceRID,
	}
	if strings.TrimSpace(event.Environment) != "" {
		lines = append(lines, "Environment: "+event.Environment)
	}
	if strings.TrimSpace(event.Actor) != "" {
		lines = append(lines, "Actor: "+event.Actor)
	}
	lines = append(lines, "Occurred: "+event.OccurredAt.UTC().Format(time.RFC3339))
	return strings.Join(lines, "\n")
}
