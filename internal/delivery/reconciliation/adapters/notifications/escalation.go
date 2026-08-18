// Package notificationsadapter translates a reconciliation escalation into a
// notification event. It is the only place where reconciliation's vocabulary
// and the notification vocabulary meet, which is what keeps the worker free of
// both the event type and the resource identifier below.
package notificationsadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// Escalations raises a notification for a proposal reconciliation has given up
// on.
type Escalations struct {
	emitter notificationports.Emitter
}

var _ ports.Escalations = (*Escalations)(nil)

func NewEscalations(emitter notificationports.Emitter) (*Escalations, error) {
	if emitter == nil {
		return nil, errors.New("a notification emitter is required to escalate a reconciliation proposal")
	}
	return &Escalations{emitter: emitter}, nil
}

// ProposalExhausted describes the failure in the terms a subscriber filtered on.
//
// The event is about the Environment, not the installation, and that is a
// limitation stated rather than hidden. A preference is joined to an event by
// resource identifier, and the Environment is the only thing in a
// reconciliation key that Conduit already publishes one for -- the Apollo
// surface mints it from the same environment name this key carries, so a
// preference set through that surface matches this event exactly. An
// installation has an identifier on both sides and they are not the same one:
// reconciliation keys on the ModuleInstallation ID, while the Apollo projection
// mints ri.apollo.main.installation from the environment plus the entity name,
// so an installation-scoped event raised here would join to nothing anybody
// could have subscribed to. Until one identity spans both, the installation is
// named in the summary and the subscription grain is the Environment.
func (escalations *Escalations) ProposalExhausted(
	ctx context.Context, proposal domain.Proposal, reason string, attempt uint32, at time.Time,
) error {
	if err := proposal.Key.Validate(); err != nil {
		return fmt.Errorf("escalate reconciliation proposal: %w", err)
	}
	return escalations.emitter.Emit(ctx, notifications.Event{
		// Derived from the proposal, so a redelivery after a crash carries the
		// identity the store deduplicates on. The attempt is deliberately not
		// part of it: a second escalation for the same proposal is the same
		// fact, and giving it a fresh identity would put it in the inbox twice.
		ID:          eventID(proposal.ID),
		Type:        notifications.EventPlanFailures,
		ResourceRID: apolloapplication.EnvironmentRID(proposal.Key.Environment),
		Environment: proposal.Key.Environment,
		Summary: fmt.Sprintf(
			"Reconciliation of installation %s in Environment %s gave up after %d attempts and will not retry: %s",
			proposal.Key.Installation, proposal.Key.Environment, attempt, summarize(reason),
		),
		// No actor. The domain reserves an empty Actor for events the system
		// raised on its own, and nobody asked for this one: it is the end of a
		// retry loop, not somebody's action.
		OccurredAt: at.UTC(),
	})
}

// eventID is stable in the proposal, so the same escalation raised twice is one
// event.
func eventID(proposalID string) string {
	digest := sha256.Sum256([]byte("reconciliation-proposal-exhausted\x00" + proposalID))
	return "nev_" + hex.EncodeToString(digest[:16])
}

// summarize bounds the planner's error text.
//
// The summary is a sentence a person reads in an inbox, and a planner error can
// carry a whole rendered refusal. Truncation is on a rune boundary because the
// text can contain anything a Plan validation wrote, and half a rune renders as
// a replacement character in the one place the message has to be legible.
func summarize(reason string) string {
	const maximum = 512
	if reason == "" {
		return "the planner reported no reason"
	}
	if len(reason) <= maximum {
		return reason
	}
	runes := []rune(reason)
	for len(string(runes)) > maximum {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}
