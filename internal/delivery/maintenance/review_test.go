package maintenance

import "testing"

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
