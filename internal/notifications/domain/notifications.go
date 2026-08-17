// Package domain models what Conduit tells people about, and who asked to be
// told.
//
// This context is Conduit's own. Apollo ships a notifications product and does
// not publish its Hub API: what was recovered is a three-role authorization
// hierarchy from a live tenant, and a Foundry-side seam showing a consumer
// subscribing to an Apollo event receiver at a severity. Both are recorded in
// docs/apollo/reference/notifications.md. Neither establishes how Apollo
// creates a receiver, evaluates a subscription, or delivers anything.
//
// So the shapes here are invented against that evidence rather than recovered
// from it, and they are marked CONDUIT_NATIVE wherever they surface. That
// distinction is not bookkeeping. A reader who later gets an authorized Apollo
// Hub session must be able to tell, without archaeology, which of these
// decisions were Apollo's and which were ours -- and the answer is that all of
// them were ours except the three role names and the receiver's id/name pair.
//
// Emission and delivery are deliberately separate. This package decides that
// something happened and who wanted to know; it does not send. Delivery needs a
// transport, a credential and a retry policy, none of which belong in a domain
// that must stay a pure function of its inputs. What Conduit owns is the part
// nobody else can supply: an ordered, attributable record of what happened,
// filtered to the people entitled to see it.
package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Severity orders how much an event demands attention.
//
// Four levels rather than Apollo's unrecovered set. The Foundry seam passes a
// `MonitoringSeverity` whose members were never captured, so borrowing the name
// and guessing the members would produce a vocabulary that looks recovered and
// is not. These are named for what an operator does about them.
// CONDUIT-INVENTED: notifications.Severity
// Displaced-by: a capture establishing Apollo's MonitoringSeverity enum members
type Severity string

const (
	// SeverityInfo records something worth having in the record and worth
	// nobody's night. A promotion passed; a bundle built.
	SeverityInfo Severity = "INFO"
	// SeverityAction means a person must do something before the system can
	// continue. A Change Request is waiting on a reviewer.
	SeverityAction Severity = "ACTION_REQUIRED"
	// SeverityWarning means something is degrading but still working.
	SeverityWarning Severity = "WARNING"
	// SeverityCritical means something is broken or unsafe now. A Plan failed;
	// a Release was recalled for a vulnerability.
	SeverityCritical Severity = "CRITICAL"
)

// severityRank orders severities for filtering. Unknown severities rank below
// everything, so a subscription with a minimum severity never matches an event
// whose severity this build does not understand -- failing quiet rather than
// paging someone about a value nobody can interpret.
var severityRank = map[Severity]int{
	SeverityInfo: 1, SeverityWarning: 2, SeverityAction: 3, SeverityCritical: 4,
}

// AtLeast reports whether this severity meets a subscription's floor.
func (severity Severity) AtLeast(minimum Severity) bool {
	return severityRank[severity] != 0 && severityRank[severity] >= severityRank[minimum]
}

// Valid reports whether this is a severity this build understands.
func (severity Severity) Valid() bool { return severityRank[severity] != 0 }

// EventKind is what happened. The set is closed and validated, for the reason
// the Change Type taxonomy is closed: an open vocabulary lets a caller invent a
// kind that no subscription can ever match, and the subscriber sees silence
// rather than an error.
// CONDUIT-INVENTED: notifications.EventKind
// Displaced-by: a capture of Apollo's own notification event vocabulary
type EventKind string

const (
	EventPlanFailed         EventKind = "PLAN_FAILED"
	EventPlanSucceeded      EventKind = "PLAN_SUCCEEDED"
	EventChangeAwaitingYou  EventKind = "CHANGE_REQUEST_AWAITING_REVIEW"
	EventChangeDecided      EventKind = "CHANGE_REQUEST_DECIDED"
	EventReleaseRecalled    EventKind = "RELEASE_RECALLED"
	EventPromotionBlocked   EventKind = "PROMOTION_BLOCKED"
	EventGrantChanged       EventKind = "RESOURCE_GRANT_CHANGED"
	EventEnvironmentDegrade EventKind = "ENVIRONMENT_DEGRADED"
)

var eventKinds = map[EventKind]struct{}{
	EventPlanFailed: {}, EventPlanSucceeded: {}, EventChangeAwaitingYou: {},
	EventChangeDecided: {}, EventReleaseRecalled: {}, EventPromotionBlocked: {},
	EventGrantChanged: {}, EventEnvironmentDegrade: {},
}

// Valid reports whether this build knows the kind.
func (kind EventKind) Valid() bool { _, known := eventKinds[kind]; return known }

// EventKinds is every kind this build emits, sorted. Exposed so a subscription
// API can offer the closed set rather than inviting free text.
func EventKinds() []EventKind {
	kinds := make([]EventKind, 0, len(eventKinds))
	for kind := range eventKinds {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	return kinds
}

// PrincipalKind says whether a subscription belongs to one person or a team.
// Both exist because the recovered role descriptions distinguish them: the
// editor role manages "the user's own subscriptions or subscriptions of teams
// on which the user has the Edit permission".
type PrincipalKind string

const (
	PrincipalUser PrincipalKind = "USER"
	PrincipalTeam PrincipalKind = "TEAM"
)

// Owner is who a subscription belongs to.
type Owner struct {
	Kind PrincipalKind `json:"kind"`
	ID   string        `json:"id"`
}

// Event is something that happened, recorded once, in terms a subscriber can
// filter on.
//
// ResourceRID is what the event is about and is the join every subscription
// filters on. It is a RID rather than a name because a name is ambiguous across
// resource types and a subscription that matched "prod" would match an
// Environment and a Team alike.
//
// Summary is a sentence for a human. It is deliberately not a template with
// substitutions: a template needs a renderer, a renderer needs a locale, and
// none of that is established. A caller writes the sentence it means.
type Event struct {
	ID          string    `json:"id"`
	Kind        EventKind `json:"kind"`
	Severity    Severity  `json:"severity"`
	ResourceRID string    `json:"resourceRid"`
	Environment string    `json:"environment,omitempty"`
	Summary     string    `json:"summary"`
	// Actor is who caused it, when a person did. Empty for events the system
	// raised on its own, which is a distinction a reader needs: "alice recalled
	// this" and "a scanner recalled this" are different facts.
	Actor      string    `json:"actor,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

// Validate refuses an event that could not be delivered meaningfully.
//
// Refused rather than defaulted, and this is the one place the old
// MAPPED-or-REFUSED instinct still earns its keep: an event with an unknown
// kind matches no subscription, so accepting it would drop it silently and the
// emitter would believe it had told someone.
func (event Event) Validate() error {
	switch {
	case strings.TrimSpace(event.ID) == "":
		return fmt.Errorf("notification event requires an id")
	case !event.Kind.Valid():
		return fmt.Errorf("notification event kind %q is not one this build emits", event.Kind)
	case !event.Severity.Valid():
		return fmt.Errorf("notification event severity %q is not one this build understands", event.Severity)
	case strings.TrimSpace(event.ResourceRID) == "":
		return fmt.Errorf("notification event requires the resource it is about")
	case strings.TrimSpace(event.Summary) == "":
		return fmt.Errorf("notification event requires a summary a person can read")
	case event.OccurredAt.IsZero():
		return fmt.Errorf("notification event requires the moment it happened")
	}
	return nil
}

// Subscription is a standing request to be told about a class of events.
//
// MinimumSeverity is a floor rather than a set, because an operator who wants
// to know about CRITICAL almost never wants to be told they may skip WARNING.
// Kinds empty means every kind, which is the useful default for a team that
// owns an Environment.
// CONDUIT-INVENTED: notifications.Subscription
// Displaced-by: a capture of Apollo's Hub-side subscription CRUD, which the
// recovered Foundry seam shows only from the consumer side
type Subscription struct {
	ID    string `json:"id"`
	Owner Owner  `json:"owner"`
	// ResourceRID scopes the subscription. Empty means every resource the owner
	// may see, which is only useful for a small deployment and is why the
	// authorization filter below is not optional.
	ResourceRID     string      `json:"resourceRid,omitempty"`
	Kinds           []EventKind `json:"kinds,omitempty"`
	MinimumSeverity Severity    `json:"minimumSeverity"`
	// Muted suspends a subscription without deleting it, which is what an
	// operator wants during a planned outage. Apollo has entity- and
	// environment-scoped mute operations in the recovered role vocabulary
	// (entity:mute-monitors, environment:mute-monitors); this is Conduit's own
	// shape for the same need and claims no correspondence to theirs.
	Muted     bool      `json:"muted,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Revision  uint64    `json:"revision"`
}

// Validate refuses a subscription that cannot match anything.
func (subscription Subscription) Validate() error {
	switch {
	case strings.TrimSpace(subscription.ID) == "":
		return fmt.Errorf("subscription requires an id")
	case subscription.Owner.Kind != PrincipalUser && subscription.Owner.Kind != PrincipalTeam:
		return fmt.Errorf("subscription owner must be a USER or a TEAM")
	case strings.TrimSpace(subscription.Owner.ID) == "":
		return fmt.Errorf("subscription requires an owner id")
	case !subscription.MinimumSeverity.Valid():
		return fmt.Errorf("subscription severity %q is not one this build understands", subscription.MinimumSeverity)
	}
	for _, kind := range subscription.Kinds {
		if !kind.Valid() {
			return fmt.Errorf("subscription names event kind %q, which this build never emits", kind)
		}
	}
	return nil
}

// Matches reports whether an event satisfies this subscription.
//
// Scope, then kind, then severity -- narrowest first, so the cheap rejections
// happen before the ordered comparison. A muted subscription matches nothing,
// which is the whole of what muting means here: the event is still recorded,
// and this owner is not told.
func (subscription Subscription) Matches(event Event) bool {
	if subscription.Muted {
		return false
	}
	if subscription.ResourceRID != "" && subscription.ResourceRID != event.ResourceRID {
		return false
	}
	if len(subscription.Kinds) > 0 {
		matched := false
		for _, kind := range subscription.Kinds {
			if kind == event.Kind {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return event.Severity.AtLeast(subscription.MinimumSeverity)
}

// InboxEntry is one event as it reaches one owner.
//
// A separate record from the Event because read state is per-owner: two people
// subscribed to the same Plan failure read it independently, and an inbox that
// stored readAt on the event would let one of them mark it read for both.
// CONDUIT-INVENTED: notifications.InboxEntry
// Displaced-by: a capture of Apollo's notification inbox read model, including
// whether read state is per-owner as assumed here
type InboxEntry struct {
	Owner          Owner     `json:"owner"`
	Event          Event     `json:"event"`
	SubscriptionID string    `json:"subscriptionId"`
	ReadAt         time.Time `json:"readAt,omitempty"`
}

// Fan reports which owners a single event reaches, deduplicated.
//
// Deduplicated because a person may be reached by their own subscription and by
// a team's, and being told twice about one failure teaches people to ignore the
// inbox. The subscription that produced the entry is recorded so an operator
// asking "why am I getting this" has an answer.
//
// A pure function of its inputs: the caller supplies the subscriptions it has
// already authorized this reader to see, exactly as Reporting's ports require,
// because filtering after fanning out would let an inbox hold an entry its
// owner cannot account for.
// CONDUIT-INVENTED: notifications.Fan
// Displaced-by: a capture of how Apollo resolves one event to its recipients,
// in particular whether it deduplicates across user and team subscriptions
func Fan(event Event, subscriptions []Subscription) []InboxEntry {
	seen := map[Owner]struct{}{}
	entries := []InboxEntry{}
	for _, subscription := range subscriptions {
		if !subscription.Matches(event) {
			continue
		}
		if _, already := seen[subscription.Owner]; already {
			continue
		}
		seen[subscription.Owner] = struct{}{}
		entries = append(entries, InboxEntry{
			Owner: subscription.Owner, Event: event, SubscriptionID: subscription.ID,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Owner.Kind != entries[j].Owner.Kind {
			return entries[i].Owner.Kind < entries[j].Owner.Kind
		}
		return entries[i].Owner.ID < entries[j].Owner.ID
	})
	return entries
}
