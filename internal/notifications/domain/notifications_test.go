package domain

import (
	"testing"
	"time"
)

func at() time.Time { return time.Date(2026, time.August, 17, 9, 0, 0, 0, time.UTC) }

func promotionFailed() Event {
	return Event{
		ID: "evt-1", Type: EventPromotionFailed,
		ResourceRID: "product/checkout-api", Environment: "prod",
		Summary: "Promotion of checkout-api 1.4.0 to STABLE failed", OccurredAt: at(),
	}
}

func preference(id string, owner Owner, mutate func(*Preference)) Preference {
	value := Preference{
		ID: id, Owner: owner, ResourceRID: "product/checkout-api",
		Channel: ChannelEmail, Types: []EventType{EventPromotionFailed},
		CreatedAt: at(), UpdatedAt: at(),
	}
	if owner.Kind == PrincipalTeam {
		value.ContactID = "contact-1"
	}
	if mutate != nil {
		mutate(&value)
	}
	return value
}

// An event nobody could act on is worse than no event: it matches no
// preference, so the emitter believes it told someone and nobody was told.
// Every field here is one whose absence produces that silence.
func TestAnEventThatCouldNotReachAnyoneIsRefused(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Event){
		"no id":        func(event *Event) { event.ID = "" },
		"unknown type": func(event *Event) { event.Type = "SOMETHING_ELSE" },
		"no resource":  func(event *Event) { event.ResourceRID = "" },
		"no summary":   func(event *Event) { event.Summary = "" },
		"no moment":    func(event *Event) { event.OccurredAt = time.Time{} },
	} {
		event := promotionFailed()
		mutate(&event)
		if err := event.Validate(); err == nil {
			t.Errorf("%s: an unusable event was accepted", name)
		}
	}
	if err := promotionFailed().Validate(); err != nil {
		t.Fatalf("a usable event was refused: %v", err)
	}
}

// The property an earlier draft of this package got wrong, and the reason the
// capture mattered. Apollo's documentation says the supported types "will grow
// over time" and its settings page carries a toggle reading "If enabled, you
// will opt into future notification types added for this resource". A build
// that models the vocabulary as closed turns every type Apollo adds later into
// something nobody is subscribed to -- silently, because the subscriber
// explicitly asked for the opposite.
func TestOptingIntoFutureTypesAdmitsATypeThisBuildHasNeverHeardOf(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalUser, ID: "alice"}
	future := Event{
		ID: "evt-9", Type: "SOME_TYPE_APOLLO_ADDS_IN_2027",
		ResourceRID: "product/checkout-api", Summary: "something new happened",
		OccurredAt: at(),
	}
	if future.Type.Valid() {
		t.Fatal("the fixture type is already known, so this test proves nothing")
	}
	opted := preference("pref-1", owner, func(value *Preference) {
		value.AllFutureTypes = true
		value.Types = nil
	})
	if !opted.Matches(future) {
		t.Error("a preference opted into future types refused an unknown type, which is the whole of what the toggle promises")
	}
	// And the negative half: enumerating every type this build knows is NOT the
	// same thing, which is what makes the toggle more than sugar.
	enumerated := preference("pref-2", owner, func(value *Preference) { value.Types = EventTypes() })
	if enumerated.Matches(future) {
		t.Error("enumerating the known types admitted an unknown one, so AllFutureTypes carries no distinct meaning")
	}
}

// Channels are configured independently over the same type list -- the settings
// page shows two separate preference blocks with their own master toggles -- so
// a person with both enabled is meant to be reached on both. Collapsing them
// would silently drop a channel the operator turned on.
func TestOneOwnerOnTwoChannelsIsReachedTwice(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalUser, ID: "alice"}
	entries := Fan(promotionFailed(), []Preference{
		preference("pref-1", owner, nil),
		preference("pref-2", owner, func(value *Preference) { value.Channel = ChannelInApp }),
	})
	if len(entries) != 2 {
		t.Fatalf("an owner enabled on two channels received %d entries", len(entries))
	}
	if entries[0].Recipient.Channel == entries[1].Recipient.Channel {
		t.Errorf("both entries landed on the same channel: %#v", entries)
	}
}

// What is collapsed: the same owner reached twice on the same channel. Being
// told the same thing twice in the same place teaches people to ignore it.
func TestOneOwnerReachedTwiceOnOneChannelReceivesOneEntry(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalUser, ID: "alice"}
	entries := Fan(promotionFailed(), []Preference{
		preference("pref-1", owner, nil),
		preference("pref-2", owner, func(value *Preference) { value.AllFutureTypes = true }),
	})
	if len(entries) != 1 {
		t.Fatalf("one owner on one channel received %d entries", len(entries))
	}
	// The preference that produced it is recorded, so "why am I getting this"
	// has an answer that does not require re-running the match by hand.
	if entries[0].PreferenceID != "pref-1" {
		t.Errorf("the entry does not name the preference that produced it: %q", entries[0].PreferenceID)
	}
}

// Scope is the join. Without it a preference on one Product would fire on every
// Product, which is the failure that makes people turn notifications off.
// Apollo's preferences live on a resource's own settings page, so there is no
// unscoped form to fall back to.
func TestAPreferenceOnlyMatchesItsOwnResource(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalUser, ID: "alice"}
	other := preference("pref-1", owner, func(value *Preference) { value.ResourceRID = "product/billing" })
	if other.Matches(promotionFailed()) {
		t.Error("a preference on one Product matched an event about another")
	}
	// Including when it has opted into everything: the toggle is per resource,
	// so "all future types" never means "all resources".
	everything := preference("pref-2", owner, func(value *Preference) {
		value.ResourceRID = "product/billing"
		value.AllFutureTypes = true
	})
	if everything.Matches(promotionFailed()) {
		t.Error("opting into future types on one resource matched an event about another")
	}
}

// A user and a team are different owners even when the same name is behind
// both, because preferences are per-principal and a team outlives its members.
func TestAUserAndATeamAreDistinctOwners(t *testing.T) {
	t.Parallel()
	entries := Fan(promotionFailed(), []Preference{
		preference("pref-1", Owner{Kind: PrincipalUser, ID: "platform"}, nil),
		preference("pref-2", Owner{Kind: PrincipalTeam, ID: "platform"}, nil),
	})
	if len(entries) != 2 {
		t.Fatalf("a user and a team sharing a name collapsed into %d entry/entries", len(entries))
	}
}

// Fan is ordered so two runs over the same inputs produce the same record. Map
// iteration is the usual way this rots, and an inbox that reshuffles makes every
// diff of stored state unreadable.
func TestFanIsOrderedSoTwoRunsAgree(t *testing.T) {
	t.Parallel()
	preferences := []Preference{
		preference("p1", Owner{Kind: PrincipalUser, ID: "carol"}, nil),
		preference("p2", Owner{Kind: PrincipalTeam, ID: "platform"}, nil),
		preference("p3", Owner{Kind: PrincipalUser, ID: "alice"}, nil),
		preference("p4", Owner{Kind: PrincipalUser, ID: "alice"}, func(value *Preference) {
			value.Channel = ChannelInApp
		}),
	}
	first, second := Fan(promotionFailed(), preferences), Fan(promotionFailed(), preferences)
	if len(first) != 4 {
		t.Fatalf("expected four recipients, got %d", len(first))
	}
	for index := range first {
		if first[index].Recipient != second[index].Recipient {
			t.Fatalf("two runs disagreed at %d: %#v vs %#v", index, first[index], second[index])
		}
	}
	// Kind, then owner id, then channel -- what Fan documents. The particular
	// order carries no meaning; what is pinned is that it is stated and stable.
	if first[0].Recipient.Owner.Kind != PrincipalTeam {
		t.Errorf("teams do not sort first as documented: %#v", first[0].Recipient)
	}
	if first[1].Recipient.Owner.ID != "alice" || first[2].Recipient.Owner.ID != "alice" {
		t.Errorf("owners are not grouped in id order: %#v", first)
	}
	if first[1].Recipient.Channel != ChannelEmail || first[2].Recipient.Channel != ChannelInApp {
		t.Errorf("one owner's channels are not in channel order: %#v", first)
	}
}

// A preference that cannot deliver is refused at the door rather than stored,
// because a stored one is indistinguishable from a working one that nothing has
// happened to yet -- and the owner concludes the system is quiet.
func TestAPreferenceThatCouldNeverDeliverIsRefused(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalUser, ID: "alice"}
	for name, mutate := range map[string]func(*Preference){
		"no id":         func(value *Preference) { value.ID = "" },
		"no owner id":   func(value *Preference) { value.Owner.ID = "" },
		"unknown owner": func(value *Preference) { value.Owner.Kind = "SERVICE" },
		"no resource":   func(value *Preference) { value.ResourceRID = "" },
		"unknown type":  func(value *Preference) { value.Types = []EventType{"NEVER_EMITTED"} },
		"no types at all": func(value *Preference) {
			value.Types = nil
			value.AllFutureTypes = false
		},
		// Slack is a team-contact kind, not something a resource's settings page
		// can express a preference on. Accepting it would promise delivery on a
		// channel that has no preference list behind it.
		"contact kind as channel": func(value *Preference) { value.Channel = Channel(ContactSlack) },
	} {
		value := preference("pref-1", owner, mutate)
		if err := value.Validate(); err == nil {
			t.Errorf("%s: an undeliverable preference was accepted", name)
		}
	}
	if err := preference("pref-1", owner, nil).Validate(); err != nil {
		t.Fatalf("a usable preference was refused: %v", err)
	}
}

// A team receives nothing until a contact exists for it -- contacts are created
// in Hub Settings and a resource can only select one that already exists. A team
// preference without one is the exact shape of "configured and silent".
func TestATeamPreferenceWithoutAContactIsRefused(t *testing.T) {
	t.Parallel()
	team := Owner{Kind: PrincipalTeam, ID: "platform"}
	orphan := preference("pref-1", team, func(value *Preference) { value.ContactID = "" })
	if err := orphan.Validate(); err == nil {
		t.Error("a team preference with no contact was accepted, so a team could be configured and never told")
	}
	// A user preference needs no contact: delivery is to the linked account.
	user := preference("pref-2", Owner{Kind: PrincipalUser, ID: "alice"}, func(value *Preference) {
		value.ContactID = ""
	})
	if err := user.Validate(); err != nil {
		t.Errorf("a user preference was made to require a team contact: %v", err)
	}
}

// PagerDuty is the case where Apollo contradicts itself and this package must
// not pick a side: the New Team Contact dialog offers it, and the documentation
// says it is not supported for notifications. So it is creatable and it does
// not deliver, which is what both statements together say.
func TestAPagerDutyContactIsCreatableAndDeliversNothing(t *testing.T) {
	t.Parallel()
	contact := Contact{
		ID: "contact-1", Team: "platform", Kind: ContactPagerDuty,
		Address: "platform-oncall", CreatedAt: at(),
	}
	if err := contact.Validate(); err != nil {
		t.Errorf("a PagerDuty contact was refused, though Apollo's dialog offers one: %v", err)
	}
	if contact.Kind.Deliverable() {
		t.Error("a PagerDuty contact reported itself deliverable, though Apollo says it is not supported")
	}
	for _, kind := range []ContactKind{ContactEmail, ContactSlack} {
		if !kind.Deliverable() {
			t.Errorf("%s was reported undeliverable", kind)
		}
	}
}

func TestAContactThatCouldNotReceiveAnythingIsRefused(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Contact){
		"no id":        func(value *Contact) { value.ID = "" },
		"no team":      func(value *Contact) { value.Team = "" },
		"unknown kind": func(value *Contact) { value.Kind = "CARRIER_PIGEON" },
		"no address":   func(value *Contact) { value.Address = "" },
	} {
		value := Contact{ID: "c-1", Team: "platform", Kind: ContactEmail, Address: "ops@example.test", CreatedAt: at()}
		mutate(&value)
		if err := value.Validate(); err == nil {
			t.Errorf("%s: an unusable contact was accepted", name)
		}
	}
}

// The known types are enumerable so a settings API can render Apollo's list. A
// caller who cannot see the set will guess, and a guessed type reaches only the
// people who opted into everything.
func TestTheKnownTypesAreEnumerableAndSorted(t *testing.T) {
	t.Parallel()
	types := EventTypes()
	if len(types) != 10 {
		t.Fatalf("expected the nine legible captured types plus the one Apollo's prose names, got %d: %v", len(types), types)
	}
	for _, eventType := range types {
		if !eventType.Valid() {
			t.Errorf("advertised type %q is not valid", eventType)
		}
	}
	for index := 1; index < len(types); index++ {
		if types[index-1] >= types[index] {
			t.Fatalf("advertised types are not sorted: %v", types)
		}
	}
	if EventType("PLAN_EXPLODED").Valid() {
		t.Error("an unknown type validated")
	}
}

// Nine types were read off screenshots of pages offering them, and each carries
// the scope it was offered at. One is named only by the overview page's prose,
// and carries no scope because no page was seen offering it.
//
// The distinction is load-bearing in one direction: a subscribable type with no
// observed scope must never start reporting one, because a settings page would
// then render it under a heading no capture supports. Enumerating it is a claim
// that Conduit emits it; scoping it would be a claim about Apollo.
func TestADocumentedTypeIsSubscribableAndCarriesNoObservedScope(t *testing.T) {
	t.Parallel()
	if !EventPlanFailures.Valid() {
		t.Fatal("the type Conduit emits for an unminted Plan is not subscribable, so only AllFutureTypes preferences would ever see it")
	}
	if scope, observed := EventPlanFailures.ObservedScope(); observed {
		t.Errorf("a type named only by prose reported the observed scope %q", scope)
	}
	// And a subscriber can name it, which is the point of registering it rather
	// than emitting an unknown type and relying on the master toggle.
	value := preference("pref-1", Owner{Kind: PrincipalUser, ID: "alice"}, func(value *Preference) {
		value.Types = []EventType{EventPlanFailures}
	})
	if err := value.Validate(); err != nil {
		t.Errorf("a preference naming the documented type was refused: %v", err)
	}
	// It is not the promotion-failure type. Apollo's own prose lists "Promotion
	// failures" and "Plan failures" separately, so merging them here would assert
	// an identity the documentation keeps apart.
	if EventPlanFailures == EventPromotionFailed {
		t.Error("a Plan failure and a promotion failure were merged into one type")
	}
}

// The two lists Apollo published were at two scopes, and nothing says whether
// their similar-sounding entries are the same event. Carrying them separately
// is the reversible choice; merging them would invent an identity that no
// capture supports and that a later capture could not easily unpick.
func TestTheTwoObservedScopesAreCarriedSeparately(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]EventType{
		{EventProductOpenEndedRecall, EventProductRecalls},
		{EventRoleChange, EventRBACChange},
	} {
		if pair[0] == pair[1] {
			t.Fatalf("%q and %q were merged into one type", pair[0], pair[1])
		}
		first, _ := pair[0].ObservedScope()
		second, _ := pair[1].ObservedScope()
		if first != ScopeProduct || second != ScopeCrossResource {
			t.Errorf("%q/%q do not carry the scopes they were observed at: %s/%s", pair[0], pair[1], first, second)
		}
	}
}

// Both channels a resource can be configured on, and only those. Slack and
// PagerDuty are team-contact kinds; no captured page offers a per-resource
// Slack preference list.
func TestOnlyTheTwoObservedChannelsAreConfigurable(t *testing.T) {
	t.Parallel()
	if channels := Channels(); len(channels) != 2 {
		t.Fatalf("expected two channels, got %v", channels)
	}
	for _, channel := range Channels() {
		if !channel.Valid() {
			t.Errorf("advertised channel %q is not valid", channel)
		}
	}
	if Channel("SLACK").Valid() {
		t.Error("Slack validated as a resource preference channel, though it is only a team-contact kind")
	}
}
