// Package notificationsadapter translates a recall coming into force into a
// notification event. It is the only place where the catalogue's vocabulary and
// the notification vocabulary meet, which is what keeps the surface that accepts
// a recall free of the event type, the resource identifier and the sentence
// below.
package notificationsadapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	apolloapplication "github.com/jonathanagustin/conduit/internal/apollo/application"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	catalogports "github.com/jonathanagustin/conduit/internal/catalog/ports"
	notifications "github.com/jonathanagustin/conduit/internal/notifications/domain"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
)

// RecallOpenings raises a notification for each recall in force that nobody has
// been told about yet.
type RecallOpenings struct {
	emitter notificationports.Emitter
	now     func() time.Time
}

var _ catalogports.RecallOpenings = (*RecallOpenings)(nil)

// NewRecallOpenings refuses an announcer that could not announce. The clock is
// injected rather than read from the package, so a test can state the moment a
// recall opened instead of asserting around it.
func NewRecallOpenings(emitter notificationports.Emitter, now func() time.Time) (*RecallOpenings, error) {
	if emitter == nil || now == nil {
		return nil, errors.New("a notification emitter and a clock are required to announce a recall")
	}
	return &RecallOpenings{emitter: emitter, now: now}, nil
}

// RecallsInForce announces each live recall once.
//
// Once is the store's guarantee rather than this adapter's. The event identity
// is derived from the Environment and the recall's own ID, and
// ports.Store.RecordEvent is idempotent on it, so a recall restated by every
// subsequent Plan proposal -- which is every recall, because recalls are a field
// of a DesiredState and are submitted whole each time -- produces one inbox
// entry and not one per submission. Keeping that memory here instead would be
// wrong in the deployment this is for: every Hub replica sees the same
// submissions, and a per-process set would announce the same recall once per
// replica and again after every restart.
//
// Every recall is attempted even when one fails, and the failures are returned
// joined. One malformed recall in a submitted set must not silence the rest;
// that is the failure mode the whole capability exists to remove.
func (openings *RecallOpenings) RecallsInForce(
	ctx context.Context, environment string, recalls []catalog.Recall,
) error {
	if strings.TrimSpace(environment) == "" {
		return errors.New("announce recalls: the Environment they are in force for is not named")
	}
	at := openings.now().UTC()
	var failures []error
	for _, recall := range catalog.LiveRecalls(recalls) {
		if strings.TrimSpace(recall.ID) == "" {
			// Without an identity there is nothing to deduplicate on, so
			// announcing this would put the same recall in an inbox again on
			// every submission. Validation rejects it upstream; this is what
			// happens if that ever stops being true.
			failures = append(failures, fmt.Errorf(
				"announce recall of %s in Environment %s: the recall has no id to announce it under",
				recall.Product, environment))
			continue
		}
		if err := openings.emitter.Emit(ctx, event(environment, recall, at)); err != nil {
			failures = append(failures, fmt.Errorf("announce recall %s in Environment %s: %w", recall.ID, environment, err))
		}
	}
	return errors.Join(failures...)
}

// event describes the recall in the terms a subscriber filtered on.
//
// The event is about the Environment, and that is a limitation stated rather
// than hidden. Apollo offers both recall types on a Product -- "Product In Open
// Ended Recall" sits on a Product's Settings > Notifications page -- but a
// preference is joined to an event by resource identifier, and the Environment
// is the identifier Conduit publishes for the thing a recall is submitted
// against. #126 records why: "Recalls is a field of DesiredState, not
// Environment", so a recall enters the Hub inside one Environment's submission
// and is scoped to it. The Product it withdraws is named in the summary, where
// a reader can act on it, rather than in the identifier, where it would join to
// a preference nobody could have set.
func event(environment string, recall catalog.Recall, at time.Time) notifications.Event {
	return notifications.Event{
		ID:          eventID(environment, recall.ID),
		Type:        eventType(recall),
		ResourceRID: apolloapplication.EnvironmentRID(environment),
		Environment: environment,
		Summary:     summary(environment, recall),
		// No actor. A DesiredState carries no author for an individual recall --
		// the person who proposed the Plan is recorded on its Change Request, and
		// attributing the recall to them would claim they wrote it when they may
		// have resubmitted somebody else's. An automatic security recall has no
		// person behind it at all, which is exactly what an empty Actor means.
		OccurredAt: at,
	}
}

// eventType picks between the two recall types the capture records.
//
// Both are observed, neither is invented, and the two carry different published
// descriptions: "Product In Open Ended Recall" is described as "Future releases
// of this Product will be recalled", and "Product recalls" as "Anytime a bug or
// critical issue has been discovered in my product". A Conduit recall with no
// ending version and no single-version bound covers every version the Product
// will ever publish above its starting version, which is the first description
// exactly; a bounded one is a discovered problem in a stated range, which is
// the second.
//
// What is Apollo's here is the two type labels and their published
// descriptions. What is Conduit's is routing between them on the recall's
// range. docs/evidence/apollo-notification-types.json records that nothing
// captured says whether these are two types or one type rendered at two
// granularities, so nothing establishes that Apollo splits them this way. If
// they turn out to be one, the label on an open-ended recall changes and
// nothing else does.
// CONDUIT-INVENTED: catalog.RecallEventTypeSelection
// Displaced-by: a capture showing which notification event type Apollo raises
// for a recall with an ending version, against one without
func eventType(recall catalog.Recall) notifications.EventType {
	if catalog.OpenEnded(recall) {
		return notifications.EventProductOpenEndedRecall
	}
	return notifications.EventProductRecalls
}

// summary says what was withdrawn, from where, and why.
//
// The range is spelled out rather than named, because "recalled" without a
// range leaves a reader to open the console to find out whether the version
// they are running is in it -- which is the thing this notification exists to
// save them.
func summary(environment string, recall catalog.Recall) string {
	var scope string
	switch {
	case recall.SingleVersion:
		scope = "version " + recall.StartingVersion + " is recalled"
	case recall.EndingVersionExclusive != "":
		scope = "versions " + recall.StartingVersion + " up to (but not including) " +
			recall.EndingVersionExclusive + " are recalled"
	default:
		scope = "every version from " + recall.StartingVersion +
			" onward is recalled, including Releases not yet published"
	}
	sentence := fmt.Sprintf("Product %s: %s in Environment %s. Reason: %s",
		recall.Product, scope, environment, bounded(recall.Reason))
	if len(recall.ExcludedVersions) > 0 {
		sentence += " Excluded from the recall: " + strings.Join(recall.ExcludedVersions, ", ") + "."
	}
	if recall.Source == catalog.RecallSourceSecurity {
		sentence += " Opened automatically from vulnerability assessment " + recall.AssessmentRunID + "."
	}
	return sentence
}

// eventID is stable in the Environment and the recall, so the same recall
// restated by a later submission is one event.
//
// The recall's contents are deliberately not part of it. An operator who
// corrects the wording of a reason has not opened a second recall, and giving
// the corrected one a fresh identity would announce it again. The consequence
// is stated rather than avoided: a recall reverted and then reopened under the
// same ID is announced once, because to this identity it never stopped.
func eventID(environment, recallID string) string {
	digest := sha256.Sum256([]byte("catalog-recall-opened\x00" + environment + "\x00" + recallID))
	return "nev_" + hex.EncodeToString(digest[:16])
}

// bounded keeps an operator's reason to a sentence an inbox row can hold.
//
// Truncation is on a rune boundary because a reason is free text an operator
// wrote, and half a rune renders as a replacement character in the one place
// this has to be legible.
func bounded(reason string) string {
	const maximum = 256
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "none was given"
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
