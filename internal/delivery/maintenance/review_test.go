package maintenance

import (
	"testing"
	"time"
)

func TestTemporaryOverrideDurationBoundariesAndIntent(t *testing.T) {
	for _, duration := range []string{"0", "-1h", "3600h1ns", "151d", "junk", "999999999999999999h"} {
		draft := OverrideDraft{Entities: []EntityTarget{{"prod", "api"}}, Duration: duration, Reason: "CAB-364"}
		if _, err := ValidateDraft(draft); err == nil {
			t.Fatal("invalid duration accepted", duration)
		}
	}
	for _, duration := range []string{"1ns", "30m", "3600h"} {
		draft := OverrideDraft{Entities: []EntityTarget{{"prod", "api"}}, Duration: duration, Reason: "CAB-364"}
		if _, err := ValidateDraft(draft); err != nil {
			t.Fatal(duration, err)
		}
	}
	draft := OverrideDraft{Entities: []EntityTarget{{"prod", "api"}}, Duration: "1h", Reason: " "}
	if _, err := ValidateDraft(draft); err == nil {
		t.Fatal("missing justification accepted")
	}
	draft.Reason = "CAB-364"
	draft.Entities[0].Environment = "other:environment"
	if _, err := ValidateDraft(draft); err == nil {
		t.Fatal("ambiguous target accepted")
	}
}

func TestScheduledOverrideRequiresRepresentableInterval(t *testing.T) {
	draft := OverrideDraft{Entities: []EntityTarget{{"prod", "api"}}, Duration: "2h", Reason: "reviewed schedule"}
	zero := time.Time{}
	draft.StartsAt = &zero
	if _, err := ValidateDraft(draft); err == nil {
		t.Fatal("zero scheduled start accepted")
	}
	late := time.Date(9999, 12, 31, 23, 0, 0, 0, time.UTC)
	draft.StartsAt = &late
	if _, err := ValidateDraft(draft); err == nil {
		t.Fatal("unrepresentable scheduled end accepted")
	}
}
