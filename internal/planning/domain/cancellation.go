package domain

import (
	"errors"
	"strings"
)

// MaxTurnRuns bounds retained logical run identities per planning session.
const MaxTurnRuns = 512

var (
	ErrRunCancelled = errors.New("planning turn was cancelled")
	ErrRunConflict  = errors.New("planning run id has already been used")
	ErrRunCapacity  = errors.New("planning turn history is full")
)

// TurnCancellationReceipt reports the exact run's durable handler state.
// It does not attest that an external provider terminated or undo prior writes.
type TurnCancellationReceipt struct {
	RunID     string `json:"runId"`
	State     string `json:"state"`
	ValueTier string `json:"valueTier"`
}

// ValidFor refuses a mismatched or unrecognized cancellation acknowledgement.
func (r *TurnCancellationReceipt) ValidFor(id string) bool {
	if r == nil || r.RunID != id || r.ValueTier != "CONDUIT_NATIVE" {
		return false
	}
	switch r.State {
	case "cancellation_requested", "cancelled", "completed", "failed":
		return true
	default:
		return false
	}
}

// ValidTurnRunID bounds a logical run identifier without normalizing its identity.
func ValidTurnRunID(id string) bool {
	return id != "" && id == strings.TrimSpace(id) && len(id) <= 256 && !strings.ContainsAny(id, "\r\n\x00")
}
