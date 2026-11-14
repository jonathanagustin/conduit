// Package domain defines debug-session requests, never execution credentials.
package domain

import (
	"errors"
	"regexp"
	"time"
)

const (
	ValueTier       = "CONDUIT_NATIVE"
	MaximumLifetime = 4 * time.Hour
	Requested       = "REQUESTED"
	Expired         = "EXPIRED"
	Revoked         = "REVOKED"
	Deleted         = "DELETED"
)

var (
	ErrInvalid   = errors.New("invalid debug-session request")
	ErrConflict  = errors.New("debug-session request revision conflict")
	ErrNotFound  = errors.New("debug-session request not found")
	ErrForbidden = errors.New("debug-session request forbidden")
	coordinate   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$`)
	sessionID    = regexp.MustCompile(`^dsr_[a-z2-7]{26}$`)
	namespace    = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
)

// Identity comes from authenticated authority, not the request body. Actor is
// the authenticated original; Owner is the authorized effective principal.
type Identity struct {
	Actor string `json:"actor"`
	Owner string `json:"owner"`
}
type Target struct {
	Environment string `json:"environment"`
	Namespace   string `json:"namespace"`
	Profile     string `json:"profile"`
}

// Event is both a lifecycle transition and its audit fact. There is no READY
// transition: a resource recorded here has no broker or execution authority.
// CONDUIT-INVENTED: debugsessions.RequestLifecycle
// Displaced-by: A published lifecycle/event schema or authorized sanitized capture that establishes the corresponding Apollo record, identity, identifier and transition semantics.
type Event struct {
	SessionID  string    `json:"sessionId"`
	Revision   uint64    `json:"revision"`
	State      string    `json:"state"`
	Target     Target    `json:"target"`
	Owner      string    `json:"owner"`
	Actor      string    `json:"actor"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	RecordedAt time.Time `json:"recordedAt"`
	ValueTier  string    `json:"valueTier"`
}

func (i Identity) Validate() error {
	if !coordinate.MatchString(i.Actor) || !coordinate.MatchString(i.Owner) {
		return ErrInvalid
	}
	return nil
}
func (t Target) Validate() error {
	if !coordinate.MatchString(t.Environment) || !namespace.MatchString(t.Namespace) || !coordinate.MatchString(t.Profile) {
		return ErrInvalid
	}
	return nil
}
func ValidID(id string) bool { return sessionID.MatchString(id) }
func New(id string, target Target, identity Identity, now time.Time, lifetime time.Duration) (Event, error) {
	if !ValidID(id) || target.Validate() != nil || identity.Validate() != nil || now.IsZero() || lifetime <= 0 || lifetime > MaximumLifetime {
		return Event{}, ErrInvalid
	}
	e := Event{SessionID: id, Revision: 1, State: Requested, Target: target, Owner: identity.Owner, Actor: identity.Actor, CreatedAt: now.UTC(), ExpiresAt: now.Add(lifetime).UTC(), RecordedAt: now.UTC(), ValueTier: ValueTier}
	return e, e.Validate()
}
func (e Event) Validate() error {
	if !ValidID(e.SessionID) || e.Target.Validate() != nil || !coordinate.MatchString(e.Owner) || !coordinate.MatchString(e.Actor) || e.ValueTier != ValueTier || e.Revision == 0 || e.Revision > 4 || e.CreatedAt.IsZero() || !e.ExpiresAt.After(e.CreatedAt) || e.ExpiresAt.Sub(e.CreatedAt) > MaximumLifetime || e.RecordedAt.Before(e.CreatedAt) {
		return ErrInvalid
	}
	switch e.State {
	case Requested:
		if e.Revision != 1 || !e.RecordedAt.Equal(e.CreatedAt) {
			return ErrInvalid
		}
	case Expired:
		if e.Revision != 2 || e.RecordedAt.Before(e.ExpiresAt) {
			return ErrInvalid
		}
	case Revoked:
		if e.Revision != 2 || !e.RecordedAt.Before(e.ExpiresAt) {
			return ErrInvalid
		}
	case Deleted:
		if e.Revision < 2 || e.Revision > 3 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// Transition never extends expiry or changes scope/owner. The caller must
// persist the returned event atomically with its audit before acknowledging it.
func (e Event) Transition(state, actor string, now time.Time) (Event, error) {
	if e.Validate() != nil || !coordinate.MatchString(actor) || now.Before(e.RecordedAt) {
		return Event{}, ErrInvalid
	}
	switch state {
	case Expired:
		if e.State != Requested || now.Before(e.ExpiresAt) {
			return Event{}, ErrConflict
		}
	case Revoked:
		if e.State != Requested || !now.Before(e.ExpiresAt) {
			return Event{}, ErrConflict
		}
	case Deleted:
		if e.State == Deleted {
			return Event{}, ErrConflict
		}
	default:
		return Event{}, ErrInvalid
	}
	next := e
	next.Revision++
	next.State = state
	next.Actor = actor
	next.RecordedAt = now.UTC()
	return next, next.Validate()
}

// ValidateHistory rejects missing, reordered, duplicated and scope-changing
// events rather than projecting an untrusted last row as a session.
func ValidateHistory(events []Event) (Event, error) {
	if len(events) == 0 {
		return Event{}, ErrNotFound
	}
	if len(events) > 3 {
		return Event{}, ErrInvalid
	}
	first := events[0]
	if first.State != Requested || first.Validate() != nil {
		return Event{}, ErrInvalid
	}
	previous := first
	for _, next := range events[1:] {
		expected, err := previous.Transition(next.State, next.Actor, next.RecordedAt)
		if err != nil || next.Validate() != nil || expected != next {
			return Event{}, ErrInvalid
		}
		previous = next
	}
	return previous, nil
}
