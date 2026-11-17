package domain

import (
	"testing"
	"time"
)

func TestIntentAcceptsEmptyConsentButRefusesInvalidOrRepeatedCandidates(t *testing.T) {
	intent := Intent{Event: Event{ID: "source", Type: EventPlanFailures, ResourceRID: "environment/dev", Summary: "failed", OccurredAt: time.Now().UTC()}}
	if err := intent.Validate(); err != nil {
		t.Fatal("empty consent is a valid frozen snapshot", err)
	}
	candidate := ConsentCandidate{Recipient: Recipient{Owner: Owner{Kind: PrincipalUser, ID: "alice"}, Channel: ChannelInApp}, PreferenceID: "preference"}
	intent.Candidates = []ConsentCandidate{candidate}
	if err := intent.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Intent){
		func(value *Intent) { value.Event.ID = "" },
		func(value *Intent) { value.Candidates[0].Recipient.Owner.Kind = "SERVICE" },
		func(value *Intent) { value.Candidates[0].Recipient.Owner.ID = "" },
		func(value *Intent) { value.Candidates[0].Recipient.Channel = "PAGERDUTY" },
		func(value *Intent) { value.Candidates[0].PreferenceID = "" },
		func(value *Intent) { value.Candidates = append(value.Candidates, candidate) },
	} {
		altered := intent
		altered.Candidates = []ConsentCandidate{candidate}
		change(&altered)
		if err := altered.Validate(); err == nil {
			t.Fatal("invalid source intent accepted", altered)
		}
	}
}
