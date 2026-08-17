package domain

import (
	"testing"
	"time"
)

func at() time.Time { return time.Date(2026, time.August, 17, 9, 0, 0, 0, time.UTC) }

func planFailed() Event {
	return Event{
		ID: "evt-1", Kind: EventPlanFailed, Severity: SeverityCritical,
		ResourceRID: "environment/prod", Environment: "prod",
		Summary: "Plan pln-9 failed applying checkout-api 1.4.0", OccurredAt: at(),
	}
}

func subscription(id string, owner Owner, mutate func(*Subscription)) Subscription {
	value := Subscription{ID: id, Owner: owner, MinimumSeverity: SeverityInfo, CreatedAt: at(), UpdatedAt: at()}
	if mutate != nil {
		mutate(&value)
	}
	return value
}

// An event nobody could act on is worse than no event: it matches no
// subscription, so the emitter believes it told someone and nobody was told.
// Every field here is one whose absence produces that silence.
func TestAnEventThatCouldNotReachAnyoneIsRefused(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*Event){
		"no id":            func(event *Event) { event.ID = "" },
		"unknown kind":     func(event *Event) { event.Kind = "SOMETHING_ELSE" },
		"unknown severity": func(event *Event) { event.Severity = "SEV1" },
		"no resource":      func(event *Event) { event.ResourceRID = "" },
		"no summary":       func(event *Event) { event.Summary = "" },
		"no moment":        func(event *Event) { event.OccurredAt = time.Time{} },
	} {
		event := planFailed()
		mutate(&event)
		if err := event.Validate(); err == nil {
			t.Errorf("%s: an unusable event was accepted", name)
		}
	}
	if err := planFailed().Validate(); err != nil {
		t.Fatalf("a usable event was refused: %v", err)
	}
}

// The severity floor is a floor, not a set. An operator who asks for CRITICAL
// almost never means "and please stop telling me about WARNING" -- they mean
// "this and worse".
func TestASeverityFloorAdmitsWorseAndRefusesLess(t *testing.T) {
	t.Parallel()
	if !SeverityCritical.AtLeast(SeverityWarning) {
		t.Error("a critical event did not clear a warning floor")
	}
	if SeverityInfo.AtLeast(SeverityAction) {
		t.Error("an informational event cleared an action-required floor")
	}
	if SeverityCritical.AtLeast(SeverityCritical) == false {
		t.Error("a floor excluded its own level")
	}
	// A severity this build does not understand must not clear any floor. It
	// fails quiet rather than paging somebody about a value nobody can read.
	if Severity("SEV0").AtLeast(SeverityInfo) {
		t.Error("an uninterpretable severity cleared the lowest floor")
	}
	// The case that makes the guard load-bearing, and the one a naive
	// implementation gets wrong: an unknown FLOOR ranks zero, so a bare
	// comparison admits everything -- including another unknown severity. That
	// is a fail-open, and it is reachable because Matches is callable on a
	// subscription that never went through Validate.
	if Severity("SEV0").AtLeast(Severity("ALSO_UNKNOWN")) {
		t.Error("two uninterpretable severities compared as a match; an unreadable floor admits everything")
	}
	if SeverityInfo.AtLeast(Severity("ALSO_UNKNOWN")) == false {
		t.Error("a known severity was refused by an unknown floor, which would silence a working subscription")
	}
}

// Muting is the operator's answer to a planned outage, and the distinction that
// makes it safe is that the event is still recorded -- only this owner is not
// told. A mute that dropped the event would lose the record for everyone.
func TestAMutedSubscriptionMatchesNothingWhileTheEventStillExists(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalTeam, ID: "platform"}
	muted := subscription("sub-1", owner, func(value *Subscription) { value.Muted = true })
	if muted.Matches(planFailed()) {
		t.Fatal("a muted subscription matched")
	}
	if entries := Fan(planFailed(), []Subscription{muted}); len(entries) != 0 {
		t.Fatalf("a muted subscription produced inbox entries: %#v", entries)
	}
	if err := planFailed().Validate(); err != nil {
		t.Fatalf("muting made the event itself invalid: %v", err)
	}
}

// Scope is the join. Without it a subscription on one Environment would page on
// every Environment, which is the failure that makes people turn notifications
// off entirely.
func TestAScopedSubscriptionOnlyMatchesItsOwnResource(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalUser, ID: "alice"}
	scoped := subscription("sub-1", owner, func(value *Subscription) { value.ResourceRID = "environment/staging" })
	if scoped.Matches(planFailed()) {
		t.Error("a staging subscription matched a prod event")
	}
	unscoped := subscription("sub-2", owner, nil)
	if !unscoped.Matches(planFailed()) {
		t.Error("an unscoped subscription did not match")
	}
}

// Kind filtering is opt-in: an empty list means every kind, because a team that
// owns an Environment wants everything about it and should not have to
// enumerate a closed set that grows.
func TestAnEmptyKindListMeansEveryKind(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalTeam, ID: "platform"}
	if !subscription("sub-1", owner, nil).Matches(planFailed()) {
		t.Error("an empty kind list did not admit an event")
	}
	narrow := subscription("sub-2", owner, func(value *Subscription) {
		value.Kinds = []EventKind{EventPromotionBlocked}
	})
	if narrow.Matches(planFailed()) {
		t.Error("a subscription narrowed to promotions matched a plan failure")
	}
}

// The deduplication rule, which is a judgement rather than a recovery and is
// registered as an invention. Being told twice about one failure teaches people
// to ignore the inbox, so one owner reached by two subscriptions gets one entry.
func TestOneOwnerReachedTwiceReceivesOneEntry(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalUser, ID: "alice"}
	entries := Fan(planFailed(), []Subscription{
		subscription("sub-1", owner, nil),
		subscription("sub-2", owner, func(value *Subscription) { value.ResourceRID = "environment/prod" }),
	})
	if len(entries) != 1 {
		t.Fatalf("one owner received %d entries", len(entries))
	}
	// The subscription that produced it is recorded, so "why am I getting this"
	// has an answer. Without it the only way to find out is to read every
	// subscription the owner has and re-run the match by hand.
	if entries[0].SubscriptionID != "sub-1" {
		t.Errorf("the entry does not name the subscription that produced it: %q", entries[0].SubscriptionID)
	}
}

// A user and a team are different owners even when the same person is behind
// both, because the inbox is per-owner and a team inbox outlives its members.
func TestAUserAndATeamAreDistinctOwners(t *testing.T) {
	t.Parallel()
	entries := Fan(planFailed(), []Subscription{
		subscription("sub-1", Owner{Kind: PrincipalUser, ID: "platform"}, nil),
		subscription("sub-2", Owner{Kind: PrincipalTeam, ID: "platform"}, nil),
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
	subscriptions := []Subscription{
		subscription("s1", Owner{Kind: PrincipalUser, ID: "carol"}, nil),
		subscription("s2", Owner{Kind: PrincipalTeam, ID: "platform"}, nil),
		subscription("s3", Owner{Kind: PrincipalUser, ID: "alice"}, nil),
	}
	first, second := Fan(planFailed(), subscriptions), Fan(planFailed(), subscriptions)
	if len(first) != 3 {
		t.Fatalf("expected three owners, got %d", len(first))
	}
	for index := range first {
		if first[index].Owner != second[index].Owner {
			t.Fatalf("two runs disagreed at %d: %#v vs %#v", index, first[index].Owner, second[index].Owner)
		}
	}
	// Kind then id, which is what Fan documents. The particular order carries no
	// meaning -- there is no reason a team should precede a user -- so what is
	// pinned here is that it is stated and stable, not that it is right.
	if first[0].Owner.Kind != PrincipalTeam || first[1].Owner.ID != "alice" || first[2].Owner.ID != "carol" {
		t.Errorf("fan-out is not in the order Fan documents (kind, then id): %#v", first)
	}
}

// A subscription that cannot match anything is refused at the door rather than
// stored, because a stored one is indistinguishable from a working one that
// nothing has happened to yet -- and the owner concludes the system is quiet.
func TestASubscriptionThatCouldNeverMatchIsRefused(t *testing.T) {
	t.Parallel()
	owner := Owner{Kind: PrincipalUser, ID: "alice"}
	for name, mutate := range map[string]func(*Subscription){
		"no id":            func(value *Subscription) { value.ID = "" },
		"no owner id":      func(value *Subscription) { value.Owner.ID = "" },
		"unknown owner":    func(value *Subscription) { value.Owner.Kind = "SERVICE" },
		"unknown severity": func(value *Subscription) { value.MinimumSeverity = "SEV1" },
		"unemitted kind":   func(value *Subscription) { value.Kinds = []EventKind{"NEVER_EMITTED"} },
	} {
		value := subscription("sub-1", owner, mutate)
		if err := value.Validate(); err == nil {
			t.Errorf("%s: an unmatchable subscription was accepted", name)
		}
	}
	if err := subscription("sub-1", owner, nil).Validate(); err != nil {
		t.Fatalf("a usable subscription was refused: %v", err)
	}
}

// The kind vocabulary is closed and enumerable, so a subscription API can offer
// the set rather than inviting free text. A caller who cannot see the set will
// guess, and a guessed kind matches nothing.
func TestTheEmittedKindsAreEnumerableAndClosed(t *testing.T) {
	t.Parallel()
	kinds := EventKinds()
	if len(kinds) == 0 {
		t.Fatal("no event kinds are advertised")
	}
	for _, kind := range kinds {
		if !kind.Valid() {
			t.Errorf("advertised kind %q is not valid", kind)
		}
	}
	for index := 1; index < len(kinds); index++ {
		if kinds[index-1] >= kinds[index] {
			t.Fatalf("advertised kinds are not sorted: %v", kinds)
		}
	}
	if EventKind("PLAN_EXPLODED").Valid() {
		t.Error("an unknown kind validated")
	}
}
