// Package domain models what Conduit tells people about, and who asked to be
// told.
//
// Apollo does not publish its notification Hub API, but it does publish the
// settings pages that drive it, and the screenshots on those pages state the
// vocabulary the prose leaves out. Everything structural here is read off that
// capture, recorded in docs/evidence/apollo-notification-types.json: the term
// "notification event type", the observed type labels, the two per-resource
// delivery channels, the three team-contact kinds, the per-(resource, channel)
// preference grain, and the two opt-into-future toggles.
//
// One property of that model is worth stating up front, because getting it
// wrong is what an earlier draft of this package did. Apollo's type vocabulary
// is open -- the documentation says the supported types "will grow over time",
// and the settings page carries a per-channel toggle whose tooltip is "If
// enabled, you will opt into future notification types added for this
// resource". A subscriber declares in advance whether growth reaches them. A
// closed set cannot express that, and choosing one silently converts every
// future type into something nobody is subscribed to.
//
// What remains Conduit's own is marked CONDUIT-INVENTED: the inbox record and
// the fan-out rule, neither of which any capture shows. Emission and delivery
// stay separate -- this package decides that something happened and who wanted
// to know; it does not send, because delivery needs a transport, a credential
// and a retry policy, none of which belong in a domain that must stay a pure
// function of its inputs.
package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// EventType is Apollo's own term, taken from the team-contact dialog: "Select
// the notification event types you'd like to receive across all resources."
//
// The vocabulary is open. Valid reports whether this build knows a type, which
// is a different and weaker question than whether Apollo has one, and callers
// that need to admit types this build has never heard of use AllFutureTypes on
// a Preference rather than an escape hatch here.
type EventType string

// The types legible in the capture. Two lists were published at two scopes and
// they are carried separately: nothing establishes that Product In Open Ended
// Recall and Product recalls, or Role Change and RBAC Change, are the same
// event seen at two granularities. Merging them would be a guess in the shape
// of a reading.
const (
	// Observed on a Product's Settings > Notifications page.
	EventLatestReleaseVulnerability    EventType = "LATEST_PRODUCT_RELEASE_VULNERABILITY_UPDATE"
	EventInstalledReleaseVulnerability EventType = "INSTALLED_PRODUCT_RELEASE_VULNERABILITY_UPDATE"
	EventPromotionFailed               EventType = "PRODUCT_RELEASE_PROMOTION_FAILED"
	EventProductOpenEndedRecall        EventType = "PRODUCT_IN_OPEN_ENDED_RECALL"
	EventBlueGreenUpgradeFailed        EventType = "BLUE_GREEN_UPGRADE_FAILED"
	EventRoleChange                    EventType = "ROLE_CHANGE"

	// Observed in the team-contact dialog's cross-resource list.
	EventProductRecalls               EventType = "PRODUCT_RECALLS"
	EventRBACChange                   EventType = "RBAC_CHANGE"
	EventChangeRequestPendingApproval EventType = "CHANGE_REQUEST_PENDING_APPROVAL"
)

// EventPlanFailures is named by Apollo's prose and by no screenshot, which is
// why it is declared apart from the nine above rather than among them.
//
// The overview page lists "Plan failures" among the types it describes, and
// docs/evidence/apollo-notification-types.json records it under proseTypeNames
// beside the reason the prose list cannot simply be folded into the observed
// one: the two do not correspond entry for entry, and "neither list is a
// superset of the other, so neither can be treated as the vocabulary."
//
// It is declared anyway, because this is the type Conduit has an event for. A
// reconciliation proposal that exhausts its retry budget is a Plan that will
// never be minted, and there is nothing else it is. The alternative was to emit
// PRODUCT_RELEASE_PROMOTION_FAILED, which the same capture ties to the prose
// name "Promotion failures" -- a different entry in the same list. Reusing it to
// avoid declaring a type would assert an identity Apollo's own documentation
// keeps apart, which is the mistake the Product-recall pair above exists to
// avoid.
//
// The value is the published name under the same mechanical rule every constant
// above follows -- uppercase, non-alphanumeric runs to underscores -- so it is
// the plural "Plan failures" rather than a tidier singular. Choosing the tidier
// one would make this the only identifier in the file that is not derived from
// what Apollo wrote, and scripts/verify-notification-types.mjs would have
// nothing to bind it to.
//
// What is not claimed: that Apollo offers this type on any settings page, or
// that its wire value is this string. ObservedScope answers false for it, which
// is what that method's second return value is for.
const (
	EventPlanFailures EventType = "PLAN_FAILURES"
)

// Scope records where a type was observed being offered. It is provenance, not
// a rule: nothing captured says a Product-scoped type cannot appear elsewhere,
// so this is never used to reject an event.
type Scope string

const (
	ScopeProduct       Scope = "PRODUCT"
	ScopeCrossResource Scope = "CROSS_RESOURCE"
)

var eventTypes = map[EventType]Scope{
	EventLatestReleaseVulnerability:    ScopeProduct,
	EventInstalledReleaseVulnerability: ScopeProduct,
	EventPromotionFailed:               ScopeProduct,
	EventProductOpenEndedRecall:        ScopeProduct,
	EventBlueGreenUpgradeFailed:        ScopeProduct,
	EventRoleChange:                    ScopeProduct,
	EventProductRecalls:                ScopeCrossResource,
	EventRBACChange:                    ScopeCrossResource,
	EventChangeRequestPendingApproval:  ScopeCrossResource,
}

// documentedTypes are named by Apollo's documentation and by no capture of a
// page offering them. They are subscribable, because a settings API that could
// not offer one would leave it reaching only the AllFutureTypes subscribers --
// and they carry no scope, because nothing was observed to have a scope.
var documentedTypes = map[EventType]struct{}{
	EventPlanFailures: {},
}

// Valid reports whether this build knows the type.
func (eventType EventType) Valid() bool {
	if _, observed := eventTypes[eventType]; observed {
		return true
	}
	_, documented := documentedTypes[eventType]
	return documented
}

// ObservedScope reports where this type was seen offered, and whether it was
// seen offered at all.
//
// False is two different answers and the caller cannot tell them apart from
// here: a type this build has never heard of, and a type it knows from prose
// with no page behind it. That is deliberate -- both mean "no capture puts this
// on a settings page at a scope", which is the only question this answers. Valid
// is where the two separate.
func (eventType EventType) ObservedScope() (Scope, bool) {
	scope, known := eventTypes[eventType]
	return scope, known
}

// EventTypes is every type this build knows, sorted, so a settings API can
// offer the list the way Apollo's page does.
//
// This is deliberately not called "all types". Two rows of Apollo's own list
// are redacted in the published screenshot and the documentation says the set
// grows, so a name promising completeness would be a claim the capture does
// not support.
func EventTypes() []EventType {
	types := make([]EventType, 0, len(eventTypes)+len(documentedTypes))
	for eventType := range eventTypes {
		types = append(types, eventType)
	}
	for eventType := range documentedTypes {
		types = append(types, eventType)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })
	return types
}

// Channel is where a notification is delivered.
//
// Email and in-app are the two the resource settings page configures, each with
// its own independent preference list over the same types. Slack and PagerDuty
// exist only as team-contact kinds, so they are ContactKind values below and
// not Channels: the capture shows no per-resource Slack preference list.
type Channel string

const (
	ChannelEmail Channel = "EMAIL"
	ChannelInApp Channel = "IN_APP"
)

// Valid reports whether this is a channel a resource can express preferences on.
func (channel Channel) Valid() bool {
	return channel == ChannelEmail || channel == ChannelInApp
}

// Channels is both delivery channels, sorted.
func Channels() []Channel { return []Channel{ChannelEmail, ChannelInApp} }

// ContactKind is a destination a team can own.
//
// PagerDuty is here because the New Team Contact dialog offers it, and it is
// separately recorded as undeliverable: the team-notifications page says
// "While you can add a PagerDuty contact for a team, PagerDuty is not supported
// for notifications." Both facts are Apollo's, and the contradiction is theirs
// rather than something to resolve by picking the more sensible one.
type ContactKind string

const (
	ContactEmail     ContactKind = "EMAIL"
	ContactSlack     ContactKind = "SLACK"
	ContactPagerDuty ContactKind = "PAGERDUTY"
)

var contactKinds = map[ContactKind]struct{}{
	ContactEmail: {}, ContactSlack: {}, ContactPagerDuty: {},
}

// Valid reports whether this is a kind a team contact can be created with.
func (kind ContactKind) Valid() bool { _, known := contactKinds[kind]; return known }

// Deliverable reports whether Apollo delivers to this kind of contact. A
// PagerDuty contact can be created and receives nothing.
func (kind ContactKind) Deliverable() bool { return kind == ContactEmail || kind == ContactSlack }

// PrincipalKind says whether a preference belongs to one person or a team,
// which is the For User / For Team split on every resource's settings page and
// is corroborated by the tenant role descriptions: the editor role manages "the
// user's own subscriptions or subscriptions of teams on which the user has the
// Edit permission".
type PrincipalKind string

const (
	PrincipalUser PrincipalKind = "USER"
	PrincipalTeam PrincipalKind = "TEAM"
)

// Owner is who a preference belongs to.
type Owner struct {
	Kind PrincipalKind `json:"kind"`
	ID   string        `json:"id"`
}

// Contact is a destination created in Hub Settings rather than on a resource --
// a resource can only select one that already exists, which is why an owner
// with no contact receives nothing however many resources name it.
//
// Apollo's is a *team* contact and nothing captured shows a per-user one; a
// tenant's account email would do the same job there. Conduit keys one on an
// Owner instead, so a person can hold one too, because the alternative is a
// USER preference on the EMAIL channel that names no address anywhere and
// therefore reaches nobody -- which is the readiness document's Tier 1 gap 1
// stated exactly. One per (owner, kind): the dialog creates a contact of a
// kind, and a second of the same kind for the same owner is an edit of the
// first rather than a new destination.
//
// Address is deliberately the only field this package will not restate. It is
// personal data -- an email address, or the Slack channel a team talks in -- so
// it never appears in an error here, in a log line, or on a metric label, and
// the only place it is allowed to travel is to the transport that delivers to
// it. Validate's messages below are written to that rule; check any you add.
// CONDUIT-INVENTED: notifications.ContactAddressBook
// Displaced-by: a capture of Apollo's contact API -- what a contact is keyed
// on, whether a user holds one at all, and what it is called
type Contact struct {
	ID    string      `json:"id"`
	Owner Owner       `json:"owner"`
	Kind  ContactKind `json:"kind"`
	// Address is the email address or Slack channel. Opaque here: this package
	// does not deliver, so it does not validate what a transport will accept.
	Address string `json:"address"`
	// OptIntoFutureResources is the dialog's "Opt-in to future team resources"
	// toggle: "If you're the contact team on these 12 products, you'll
	// auto-subscribe to all of these and future resources added to this team."
	OptIntoFutureResources bool      `json:"optIntoFutureResources"`
	CreatedAt              time.Time `json:"createdAt"`
	UpdatedAt              time.Time `json:"updatedAt,omitzero"`
}

// Validate refuses a contact that could not receive anything.
//
// Note what the last case does not say. An absent address is reported as
// absent; a present one is never quoted back, here or anywhere else, because an
// error is the value most likely to reach a log an operator does not own.
func (contact Contact) Validate() error {
	switch {
	case strings.TrimSpace(contact.ID) == "":
		return fmt.Errorf("notification contact requires an id")
	case contact.Owner.Kind != PrincipalUser && contact.Owner.Kind != PrincipalTeam:
		return fmt.Errorf("a notification contact belongs to a USER or a TEAM")
	case strings.TrimSpace(contact.Owner.ID) == "":
		return fmt.Errorf("notification contact requires the owner that holds it")
	case !contact.Kind.Valid():
		return fmt.Errorf("notification contact kind %q is not one Apollo offers", contact.Kind)
	case strings.TrimSpace(contact.Address) == "":
		return fmt.Errorf("notification contact requires a destination address")
	}
	return nil
}

// Event is something that happened, recorded once, in terms a subscriber can
// filter on.
//
// ResourceRID is what the event is about and is the join every preference
// filters on. It is a RID rather than a name because a name is ambiguous across
// resource types, and a preference matching "prod" would match an Environment
// and a Team alike.
//
// There is no severity field. Nothing in the notification pages or their
// screenshots carries one: filtering is by type and channel. Apollo does have a
// severity, on the Environment Events surface, where the observed facet is
// Success/Info/Warning/Error -- a different surface, and not this one.
type Event struct {
	ID          string    `json:"id"`
	Type        EventType `json:"type"`
	ResourceRID string    `json:"resourceRid"`
	Environment string    `json:"environment,omitempty"`
	// Summary is a sentence for a human, deliberately not a template with
	// substitutions: a template needs a renderer, a renderer needs a locale, and
	// none of that is established. A caller writes the sentence it means.
	Summary string `json:"summary"`
	// Actor is who caused it, when a person did. Empty for events the system
	// raised on its own, which is a distinction a reader needs: "alice recalled
	// this" and "a scanner recalled this" are different facts.
	Actor      string    `json:"actor,omitempty"`
	OccurredAt time.Time `json:"occurredAt"`
}

// Validate refuses an event that could not be delivered meaningfully.
//
// An unknown type is refused at emission even though the vocabulary is open,
// and the asymmetry is deliberate. Openness is about what a subscriber may
// consent to in advance, not about what an emitter may invent: a type this
// build never registered has no label, no description and no settings row, so
// nobody could have chosen it and only AllFutureTypes preferences would ever
// see it.
func (event Event) Validate() error {
	switch {
	case strings.TrimSpace(event.ID) == "":
		return fmt.Errorf("notification event requires an id")
	case !event.Type.Valid():
		return fmt.Errorf("notification event type %q is not one this build emits", event.Type)
	case strings.TrimSpace(event.ResourceRID) == "":
		return fmt.Errorf("notification event requires the resource it is about")
	case strings.TrimSpace(event.Summary) == "":
		return fmt.Errorf("notification event requires a summary a person can read")
	case event.OccurredAt.IsZero():
		return fmt.Errorf("notification event requires the moment it happened")
	}
	return nil
}

// Preference is one owner's settings for one resource on one channel, which is
// the grain Apollo's settings page uses: email and in-app are configured
// independently over the same list of types, each with its own master toggle.
type Preference struct {
	ID          string  `json:"id"`
	Owner       Owner   `json:"owner"`
	ResourceRID string  `json:"resourceRid"`
	Channel     Channel `json:"channel"`
	// Types are the individually enabled types. Ignored while AllFutureTypes is
	// set, matching the page, which renders every checkbox checked and disabled
	// when the master toggle is on.
	Types []EventType `json:"types,omitempty"`
	// AllFutureTypes is the master toggle: "If enabled, you will opt into future
	// notification types added for this resource." It is what makes an open
	// vocabulary safe, and it must admit types this build does not know, or it
	// would be an expensive way to write a closed set.
	AllFutureTypes bool `json:"allFutureTypes,omitempty"`
	// ContactID names the team contact to deliver to, for a team preference. A
	// team preference without one is refused rather than stored, because Apollo
	// requires a contact before a team receives anything and a stored preference
	// that delivers nowhere is indistinguishable from a working one.
	ContactID string    `json:"contactId,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Revision  uint64    `json:"revision"`
}

// Validate refuses a preference that could never deliver anything.
func (preference Preference) Validate() error {
	switch {
	case strings.TrimSpace(preference.ID) == "":
		return fmt.Errorf("notification preference requires an id")
	case preference.Owner.Kind != PrincipalUser && preference.Owner.Kind != PrincipalTeam:
		return fmt.Errorf("notification preference owner must be a USER or a TEAM")
	case strings.TrimSpace(preference.Owner.ID) == "":
		return fmt.Errorf("notification preference requires an owner id")
	case strings.TrimSpace(preference.ResourceRID) == "":
		return fmt.Errorf("notification preference requires the resource it is about")
	case !preference.Channel.Valid():
		return fmt.Errorf("notification channel %q is not one a resource can be configured on", preference.Channel)
	case preference.Owner.Kind == PrincipalTeam && strings.TrimSpace(preference.ContactID) == "":
		return fmt.Errorf("a team notification preference requires the contact it delivers to")
	case !preference.AllFutureTypes && len(preference.Types) == 0:
		return fmt.Errorf("notification preference enables no types and has not opted into future ones")
	}
	for _, eventType := range preference.Types {
		if !eventType.Valid() {
			return fmt.Errorf("notification preference names event type %q, which this build never emits", eventType)
		}
	}
	return nil
}

// Matches reports whether an event reaches this preference.
//
// Resource first, then type. AllFutureTypes short-circuits the type check
// entirely rather than being expanded into the known set, which is the whole
// point of it: expansion would freeze the answer at the vocabulary this build
// happens to ship.
func (preference Preference) Matches(event Event) bool {
	if preference.ResourceRID != event.ResourceRID {
		return false
	}
	if preference.AllFutureTypes {
		return true
	}
	for _, eventType := range preference.Types {
		if eventType == event.Type {
			return true
		}
	}
	return false
}

// Recipient is one owner reached on one channel.
type Recipient struct {
	Owner   Owner   `json:"owner"`
	Channel Channel `json:"channel"`
}

// InboxEntry is one event as it reaches one recipient.
//
// A separate record from the Event because read state is per-recipient: two
// people subscribed to the same promotion failure read it independently, and an
// inbox storing readAt on the event would let one mark it read for both. Apollo
// has an inbox -- Hub Settings carries a "My notifications" page and the tenant
// viewer role names it -- but no capture shows an entry or its read state.
// CONDUIT-INVENTED: notifications.InboxEntry
// Displaced-by: a capture of Apollo's notification inbox entry and its read
// state, in particular whether read state is per-recipient as assumed here
type InboxEntry struct {
	Recipient    Recipient `json:"recipient"`
	Event        Event     `json:"event"`
	PreferenceID string    `json:"preferenceId"`
	ReadAt       time.Time `json:"readAt,omitempty"`
}

// Fan reports which recipients a single event reaches, deduplicated per owner
// and channel.
//
// Per owner and channel, not per owner: a person who has both email and in-app
// enabled is meant to be reached twice, once on each, and collapsing those
// would silently drop a channel the operator turned on. What is collapsed is
// one owner reached twice on the same channel, because being told the same
// thing twice in the same place teaches people to ignore it.
//
// A pure function of its inputs: the caller supplies the preferences it has
// already authorized this reader to see, exactly as Reporting's ports require,
// because filtering after fanning out would let an inbox hold an entry its
// owner cannot account for.
// CONDUIT-INVENTED: notifications.Fan
// Displaced-by: a capture of how Apollo resolves one event to its recipients,
// in particular whether it deduplicates a recipient reached by two preferences
func Fan(event Event, preferences []Preference) []InboxEntry {
	seen := map[Recipient]struct{}{}
	entries := []InboxEntry{}
	for _, preference := range preferences {
		if !preference.Matches(event) {
			continue
		}
		recipient := Recipient{Owner: preference.Owner, Channel: preference.Channel}
		if _, already := seen[recipient]; already {
			continue
		}
		seen[recipient] = struct{}{}
		entries = append(entries, InboxEntry{
			Recipient: recipient, Event: event, PreferenceID: preference.ID,
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i].Recipient, entries[j].Recipient
		if left.Owner.Kind != right.Owner.Kind {
			return left.Owner.Kind < right.Owner.Kind
		}
		if left.Owner.ID != right.Owner.ID {
			return left.Owner.ID < right.Owner.ID
		}
		return left.Channel < right.Channel
	})
	return entries
}
