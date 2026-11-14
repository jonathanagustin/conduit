// Package memory supplies a process-local reference authority. It is not durable
// across restart and is not composed into the Hub or a deployment path.
package memory

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagevalidation/domain"
	"github.com/jonathanagustin/conduit/internal/packagevalidation/ports"
)

var ErrMissing = errors.New("validation attempt not found")
var ErrUsed = errors.New("validation challenge already consumed")

// Store runs its authorizer under the local serialization boundary and must not
// re-enter Store. Production adapters need the actual governance/target store's
// transaction boundary; this reference proves local admission semantics only.
type Store struct {
	mu           sync.Mutex
	authorize    func(context.Context, ports.Locator) error
	now          func() time.Time
	attempts     map[ports.Locator]ports.Attempt
	records      map[ports.Locator]ports.Admission
	audits       []ports.Admission
	auditFailure error
}

func New(authorize func(context.Context, ports.Locator) error) (*Store, error) {
	if authorize == nil {
		return nil, domain.ErrInvalid
	}
	return &Store{authorize: authorize, now: time.Now, attempts: map[ports.Locator]ports.Attempt{}, records: map[ports.Locator]ports.Admission{}}, nil
}

// SetAttempt is trusted operator/test setup, not a generated-input API. An
// existing locator cannot replace its challenge or clear consumed state.
func (s *Store) SetAttempt(locator ports.Locator, attempt ports.Attempt) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateAttempt(locator, attempt); err != nil {
		return err
	}
	if _, exists := s.attempts[locator]; exists {
		return domain.ErrUntrusted
	}
	s.attempts[locator] = cloneAttempt(attempt)
	return nil
}

// Update is trusted owner control for current target/policy facts. It cannot
// rotate an existing challenge, unconsume it, or undo cancellation. It is not
// exposed as a generated-input or operator HTTP API.
func (s *Store) Update(locator ports.Locator, change func(*ports.Attempt) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.attempts[locator]
	if !ok {
		return ErrMissing
	}
	next := cloneAttempt(old)
	if err := change(&next); err != nil {
		return err
	}
	if next.Manifest.Challenge != old.Manifest.Challenge || next.Consumed != old.Consumed || (old.Cancelled && !next.Cancelled) {
		return domain.ErrUntrusted
	}
	if err := validateAttempt(locator, next); err != nil {
		return err
	}
	s.attempts[locator] = cloneAttempt(next)
	return nil
}
func validateAttempt(locator ports.Locator, attempt ports.Attempt) error {
	digest, err := attempt.Policy.Digest()
	if err != nil || attempt.Manifest.RunID != locator.RunID || attempt.Manifest.ProposalRevision != locator.ProposalRevision || attempt.Manifest.Validate() != nil || attempt.Manifest.PolicyDigest != digest || attempt.Manifest.ProfileDigest != attempt.Policy.ProfileDigest {
		return domain.ErrInvalid
	}
	return nil
}

func (s *Store) UseAttempt(ctx context.Context, locator ports.Locator, verify func(ports.Attempt) (ports.Admission, error)) (ports.Admission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ports.Admission{}, err
	}
	if err := s.authorize(ctx, locator); err != nil {
		return ports.Admission{}, err
	}
	attempt, ok := s.attempts[locator]
	if !ok {
		return ports.Admission{}, ErrMissing
	}
	if attempt.Consumed {
		return ports.Admission{}, ErrUsed
	}
	record, err := verify(cloneAttempt(attempt))
	if err != nil {
		return ports.Admission{}, err
	}
	if err := ctx.Err(); err != nil {
		return ports.Admission{}, err
	}
	if attempt.Cancelled || record.Manifest != attempt.Manifest || record.State != "VERIFIED_EVIDENCE" || record.ValueTier != "CONDUIT_NATIVE" || record.ID == "" || s.now().UTC().Before(record.VerifiedAt) || !s.now().UTC().Before(record.ExpiresAt) {
		return ports.Admission{}, domain.ErrUntrusted
	}
	claim, err := domain.Decode(record.Attestation)
	if err != nil {
		return ports.Admission{}, err
	}
	evidence, err := domain.Verify(claim, attempt.Manifest, attempt.Policy, s.now().UTC())
	if err != nil {
		return ports.Admission{}, err
	}
	if evidence.ID() != record.ID || evidence.Producer() != record.ProducerID || !evidence.ExpiresAt().Equal(record.ExpiresAt) {
		return ports.Admission{}, domain.ErrUntrusted
	}
	if s.auditFailure != nil {
		return ports.Admission{}, s.auditFailure
	}
	if err := ctx.Err(); err != nil {
		return ports.Admission{}, err
	}
	if !evidence.ValidAt(s.now().UTC()) {
		return ports.Admission{}, domain.ErrUntrusted
	}
	// All three writes happen after the final check while one lock is held.
	attempt.Consumed = true
	s.attempts[locator] = attempt
	s.records[locator] = cloneAdmission(record)
	s.audits = append(s.audits, cloneAdmission(record))
	return cloneAdmission(record), nil
}

// GetAdmission reads historical evidence metadata after the same authorization
// check. A historical record is not a fresh verification or execution permit.
func (s *Store) GetAdmission(ctx context.Context, locator ports.Locator) (ports.Admission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return ports.Admission{}, err
	}
	if err := s.authorize(ctx, locator); err != nil {
		return ports.Admission{}, err
	}
	record, ok := s.records[locator]
	if !ok {
		return ports.Admission{}, ErrMissing
	}
	return cloneAdmission(record), nil
}

// Cancel is trusted owner setup/control, not a generated-input API.
func (s *Store) Cancel(locator ports.Locator) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempt, ok := s.attempts[locator]
	if !ok {
		return ErrMissing
	}
	attempt.Cancelled = true
	s.attempts[locator] = attempt
	return nil
}

func cloneAttempt(attempt ports.Attempt) ports.Attempt {
	attempt.Policy.Checks = cloneMap(attempt.Policy.Checks)
	attempt.Policy.Producers = cloneMap(attempt.Policy.Producers)
	for id, producer := range attempt.Policy.Producers {
		producer.PublicKey = slices.Clone(producer.PublicKey)
		producer.Profiles = slices.Clone(producer.Profiles)
		attempt.Policy.Producers[id] = producer
	}
	return attempt
}
func cloneMap[K comparable, V any](m map[K]V) map[K]V {
	copy := make(map[K]V, len(m))
	for k, v := range m {
		copy[k] = v
	}
	return copy
}

var _ ports.Authority = (*Store)(nil)

func cloneAdmission(record ports.Admission) ports.Admission {
	record.Attestation = slices.Clone(record.Attestation)
	return record
}
