// Package application admits authenticated evidence, not deployment permission.
package application

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagevalidation/domain"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/ports"
)

var ErrCorrupt = errors.New("validation admission authority returned inconsistent facts")

// Service admits only current authenticated validation evidence.
// CONDUIT-INVENTED: packagevalidation.EvidenceAdmission
// Displaced-by: a reviewed recipient-side validation evidence admission and atomic challenge/audit contract.
type Service struct {
	authority ports.Authority
	now       func() time.Time
}

func New(authority ports.Authority) (*Service, error) {
	if authority == nil {
		return nil, domain.ErrInvalid
	}
	return &Service{authority, time.Now}, nil
}

// Admit consumes one currently authorized attempt and records authenticated
// validation evidence. It has no executor or governance-approval port.
func (s *Service) Admit(ctx context.Context, locator ports.Locator, raw []byte) (ports.Admission, error) {
	if err := ctx.Err(); err != nil {
		return ports.Admission{}, err
	}
	claim, err := domain.Decode(raw)
	if err != nil {
		return ports.Admission{}, err
	}
	if claim.Manifest.RunID != locator.RunID || claim.Manifest.ProposalRevision != locator.ProposalRevision {
		return ports.Admission{}, domain.ErrUntrusted
	}
	calls := 0
	var expected ports.Admission
	var callbackErr error
	result, authorityErr := s.authority.UseAttempt(ctx, locator, func(attempt ports.Attempt) (record ports.Admission, err error) {
		calls++
		defer func() {
			if err != nil {
				callbackErr = err
			}
		}()
		if calls != 1 {
			return record, ErrCorrupt
		}
		if err := ctx.Err(); err != nil {
			return record, err
		}
		if attempt.Cancelled || attempt.Consumed || attempt.Manifest.RunID != locator.RunID || attempt.Manifest.ProposalRevision != locator.ProposalRevision {
			return record, domain.ErrUntrusted
		}
		now := s.now().UTC()
		evidence, err := domain.Verify(claim, attempt.Manifest, attempt.Policy, now)
		if err != nil {
			return record, err
		}
		document, err := domain.Encode(claim)
		if err != nil {
			return record, err
		}
		expected = ports.Admission{Attestation: document, ID: evidence.ID(), ValueTier: "CONDUIT_NATIVE", State: "VERIFIED_EVIDENCE", Manifest: evidence.Manifest(), ProducerID: evidence.Producer(), VerifiedAt: now, ExpiresAt: evidence.ExpiresAt()}
		if err := ctx.Err(); err != nil {
			return record, err
		}
		if !evidence.ValidAt(s.now().UTC()) {
			return record, domain.ErrUntrusted
		}
		return expected, nil
	})
	if callbackErr != nil {
		return ports.Admission{}, callbackErr
	}
	if authorityErr != nil {
		return ports.Admission{}, authorityErr
	}
	if calls != 1 || !bytes.Equal(result.Attestation, expected.Attestation) || result.ID != expected.ID || result.ID == "" || result.ValueTier != expected.ValueTier || result.State != expected.State || result.Manifest != expected.Manifest || result.ProducerID != expected.ProducerID || !result.VerifiedAt.Equal(expected.VerifiedAt) || !result.ExpiresAt.Equal(expected.ExpiresAt) {
		return ports.Admission{}, ErrCorrupt
	}
	if now := s.now().UTC(); now.Before(result.VerifiedAt) || !now.Before(result.ExpiresAt) {
		return ports.Admission{}, domain.ErrUntrusted
	}
	return result, nil
}
