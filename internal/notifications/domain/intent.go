package domain

import (
	"fmt"
	"strings"
)

// ConsentCandidate freezes subscription consent read within the source
// transaction. It is not an authorization grant or a contact destination. Current
// authority is checked when admitting the inbox entry and again before sending.
type ConsentCandidate struct {
	Recipient    Recipient `json:"recipient"`
	PreferenceID string    `json:"preferenceId"`
}

// Intent is the immutable handoff from a source transition to notification
// admission. Even an empty candidate set is a snapshot, never a request to read
// whatever preferences happen to exist when a delayed worker runs.
// CONDUIT-INVENTED: notifications.ProducerIntent
// Displaced-by: An authoritative source-to-notification durability contract, including source and subscription ordering, consent snapshots, current recipient authority, recovery and retention semantics. No captured Apollo notification API establishes these internals.
type Intent struct {
	Event      Event              `json:"event"`
	Candidates []ConsentCandidate `json:"candidates"`
}

func (intent Intent) Validate() error {
	if err := intent.Event.Validate(); err != nil {
		return fmt.Errorf("notification intent: %w", err)
	}
	seen := map[Recipient]bool{}
	for _, candidate := range intent.Candidates {
		recipient := candidate.Recipient
		if (recipient.Owner.Kind != PrincipalUser && recipient.Owner.Kind != PrincipalTeam) ||
			strings.TrimSpace(recipient.Owner.ID) == "" || !recipient.Channel.Valid() ||
			strings.TrimSpace(candidate.PreferenceID) == "" {
			return fmt.Errorf("notification intent has an invalid consent candidate")
		}
		if seen[recipient] {
			return fmt.Errorf("notification intent repeats a consent candidate")
		}
		seen[recipient] = true
	}
	return nil
}
