package application

import (
	"time"

	"github.com/jonathanagustin/conduit/internal/packagevalidation/domain"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/ports"
)

// ValidateAttempt binds trusted owner setup to its locator and policy pins.
// It does not authenticate a caller or grant execution authority.
func ValidateAttempt(locator ports.Locator, attempt ports.Attempt) error {
	digest, err := attempt.Policy.Digest()
	if err != nil || attempt.Manifest.RunID != locator.RunID || attempt.Manifest.ProposalRevision != locator.ProposalRevision || attempt.Manifest.Validate() != nil || attempt.Manifest.PolicyDigest != digest || attempt.Manifest.ProfileDigest != attempt.Policy.ProfileDigest {
		return domain.ErrInvalid
	}
	return nil
}

// VerifyAdmission independently checks a callback's proposed record against
// current trusted facts. Authorities call it again at their persistence boundary.
func VerifyAdmission(attempt ports.Attempt, record ports.Admission, now time.Time) error {
	if attempt.Cancelled || attempt.Consumed || record.Manifest != attempt.Manifest || record.State != "VERIFIED_EVIDENCE" || record.ValueTier != "CONDUIT_NATIVE" || record.ID == "" || now.Before(record.VerifiedAt) || !now.Before(record.ExpiresAt) {
		return domain.ErrUntrusted
	}
	claim, err := domain.Decode(record.Attestation)
	if err != nil {
		return err
	}
	evidence, err := domain.Verify(claim, attempt.Manifest, attempt.Policy, now)
	if err != nil {
		return err
	}
	if evidence.ID() != record.ID || evidence.Producer() != record.ProducerID || !evidence.ExpiresAt().Equal(record.ExpiresAt) {
		return domain.ErrUntrusted
	}
	return nil
}
