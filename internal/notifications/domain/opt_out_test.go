package domain

import "testing"

func TestAnExplicitOptOutIsValidAndMatchesNoEvent(t *testing.T) {
	for _, owner := range []Owner{{Kind: PrincipalUser, ID: "alice"}, {Kind: PrincipalTeam, ID: "platform"}} {
		for _, contact := range []string{"", "selected-or-deleted-contact"} {
			value := preference("pref-off", owner, func(value *Preference) {
				value.Types = nil
				value.AllFutureTypes = false
				value.ContactID = contact
			})
			if err := value.Validate(); err != nil {
				t.Fatalf("explicit off consent for %s with contact %q was refused: %v", owner.Kind, contact, err)
			}
			if err := value.ValidateSelection(); err != nil {
				t.Fatal(err)
			}
			for _, eventType := range append(EventTypes(), EventType("FUTURE_TYPE")) {
				event := promotionFailed()
				event.Type = eventType
				if value.Matches(event) || len(Fan(event, []Preference{value})) != 0 {
					t.Fatalf("off consent matched event %s", eventType)
				}
			}
		}
	}
}
