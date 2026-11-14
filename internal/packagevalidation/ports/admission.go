// Package ports defines trusted attempt lookup and atomic evidence admission.
package ports

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagevalidation/domain"
)

type Locator struct {
	RunID            string
	ProposalRevision string
}
type Attempt struct {
	Manifest  domain.Manifest
	Policy    domain.Policy
	Cancelled bool
	Consumed  bool
}
type Admission struct {
	Attestation json.RawMessage `json:"attestation"`
	ID          string          `json:"id"`
	ValueTier   string          `json:"valueTier"`
	State       string          `json:"state"`
	Manifest    domain.Manifest `json:"manifest"`
	ProducerID  string          `json:"producerId"`
	VerifiedAt  time.Time       `json:"verifiedAt"`
	ExpiresAt   time.Time       `json:"expiresAt"`
}

// Authority authenticates/authorizes the caller, reads current trusted facts,
// holds their serialization boundary across verify, and atomically records
// admission + audit + nonce consumption. Failure/cancellation/expiry must not
// acknowledge admission. A consumed challenge cannot be reused. Facts and
// policy never come from generated output or the supplied attestation.
//
// This records validation evidence only. Deployment still requires separate
// current governance authority and last-safe-point revalidation. A production
// adapter must persist these records and prove restart/rollback behavior.
type Authority interface {
	UseAttempt(context.Context, Locator, func(Attempt) (Admission, error)) (Admission, error)
}
