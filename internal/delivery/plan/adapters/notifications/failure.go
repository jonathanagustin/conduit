// Package notificationsadapter translates a Plan an Agent reported FAILED into
// a notification event. It is the only place where the Plan lifecycle's
// vocabulary and the notification vocabulary meet.
package notificationsadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	"github.com/jonathanagustin/conduit/internal/delivery/plan/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// Failures raises a notification for a Plan that ran and failed.
type Failures struct {
	emitter notificationports.Emitter
}

var _ ports.Failures = (*Failures)(nil)

func NewFailures(emitter notificationports.Emitter) (*Failures, error) {
	if emitter == nil {
		return nil, errors.New("a notification emitter is required to announce a failed Plan")
	}
	return &Failures{emitter: emitter}, nil
}

// PlanFailed describes the failure in the terms a subscriber filtered on.
//
// The type is PLAN_FAILURES, the same one reconciliation raises when a
// proposal exhausts its retries, and sharing it is deliberate: Apollo's prose
// names one "Plan failures" type, and a Plan that was never minted and a Plan
// that was executed and failed are both that. The two are distinct events --
// the identity below shares nothing with reconciliation's -- and the summary
// says which this is.
//
// The event is about the Environment, for the reason the reconciliation
// adapter gives: it is the identifier the Apollo surface publishes, so a
// preference set there matches. The installation the Agent named is in the
// summary.
func (failures *Failures) PlanFailed(ctx context.Context, failure ports.PlanFailure) error {
	if strings.TrimSpace(failure.PlanID) == "" || strings.TrimSpace(failure.Environment) == "" {
		return errors.New("announce failed Plan: the Plan or its Environment is not named")
	}
	where := "Environment " + failure.Environment
	if installation := strings.TrimSpace(failure.Installation); installation != "" {
		where = "installation " + installation + " in " + where
	}
	return failures.emitter.Emit(ctx, notifications.Event{
		// Stable in the Plan. A Plan reaches FAILED once, and an Agent that
		// repeats its finish report after a lost response is restating the
		// same fact; the store deduplicates on this.
		ID:          eventID(failure.PlanID),
		Type:        notifications.EventPlanFailures,
		ResourceRID: apolloapplication.EnvironmentRID(failure.Environment),
		Environment: failure.Environment,
		Summary: fmt.Sprintf("Plan %s failed while executing on %s: %s",
			failure.PlanID, where, summarize(failure.Reason)),
		// No actor. The Agent reported it; no person caused it.
		OccurredAt: failure.At.UTC(),
	})
}

func eventID(planID string) string {
	digest := sha256.Sum256([]byte("plan-failed\x00" + planID))
	return "nev_" + hex.EncodeToString(digest[:16])
}

// summarize bounds the Agent's failure text on a rune boundary: it can carry a
// whole rendered Helm error, and an inbox row has to stay legible.
func summarize(reason string) string {
	const maximum = 512
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "the Agent reported no reason"
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
