// Package sessionmemory keeps operator sessions in this process.
//
// It is the store for a single-replica Hub on the file repository. Sessions do
// not survive a restart and are not shared, which for that deployment is the
// honest behaviour: a second replica would need the PostgreSQL store.
package sessionmemory

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// The bounds are what keep an anonymous caller, who can start sign-in attempts
// at will, from growing this without limit.
const (
	DefaultMaximumLoginAttempts = 10_000
	DefaultMaximumSessions      = 100_000
)

// Store is safe for concurrent use.
type Store struct {
	mutex           sync.Mutex
	attempts        map[string]domain.LoginAttempt
	sessions        map[string]domain.Session
	maximumAttempts int
	maximumSessions int
}

// New returns an empty store with the default bounds.
func New() *Store { return NewBounded(DefaultMaximumLoginAttempts, DefaultMaximumSessions) }

// NewBounded is New with the bounds stated, for tests that reach them.
func NewBounded(maximumAttempts, maximumSessions int) *Store {
	return &Store{
		attempts: map[string]domain.LoginAttempt{}, sessions: map[string]domain.Session{},
		maximumAttempts: maximumAttempts, maximumSessions: maximumSessions,
	}
}

func (store *Store) SaveLoginAttempt(_ context.Context, attempt domain.LoginAttempt, now time.Time) error {
	if attempt.StateHash == "" || attempt.BindingHash == "" || attempt.Nonce == "" || attempt.CodeVerifier == "" {
		return errors.New("a sign-in attempt requires its state, binding, nonce and verifier")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if len(store.attempts) >= store.maximumAttempts {
		store.pruneLocked(now)
	}
	if len(store.attempts) >= store.maximumAttempts {
		return domain.ErrSessionStoreFull
	}
	if _, exists := store.attempts[attempt.StateHash]; exists {
		return errors.New("a sign-in attempt with this state already exists")
	}
	store.attempts[attempt.StateHash] = attempt
	return nil
}

func (store *Store) TakeLoginAttempt(_ context.Context, stateHash string, now time.Time) (domain.LoginAttempt, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	attempt, exists := store.attempts[stateHash]
	if !exists {
		return domain.LoginAttempt{}, domain.ErrSessionNotFound
	}
	delete(store.attempts, stateHash)
	if !attempt.ExpiresAt.After(now) {
		return domain.LoginAttempt{}, domain.ErrSessionNotFound
	}
	return attempt, nil
}

func (store *Store) SaveSession(_ context.Context, session domain.Session) error {
	if session.IDHash == "" || session.Claims.Username == "" || !session.ExpiresAt.After(session.CreatedAt) {
		return errors.New("a session requires its identifier hash, a username and an expiry after its creation")
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if len(store.sessions) >= store.maximumSessions {
		store.pruneLocked(session.CreatedAt)
	}
	if len(store.sessions) >= store.maximumSessions {
		return domain.ErrSessionStoreFull
	}
	if _, exists := store.sessions[session.IDHash]; exists {
		return errors.New("a session with this identifier already exists")
	}
	session.Claims.Groups = append([]string(nil), session.Claims.Groups...)
	store.sessions[session.IDHash] = session
	return nil
}

func (store *Store) Session(_ context.Context, idHash string, now time.Time) (domain.Session, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	session, exists := store.sessions[idHash]
	if !exists || !session.ExpiresAt.After(now) {
		return domain.Session{}, domain.ErrSessionNotFound
	}
	session.Claims.Groups = append([]string(nil), session.Claims.Groups...)
	return session, nil
}

func (store *Store) DeleteSession(_ context.Context, idHash string) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	delete(store.sessions, idHash)
	return nil
}

func (store *Store) Prune(_ context.Context, now time.Time) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.pruneLocked(now)
	return nil
}

func (store *Store) pruneLocked(now time.Time) {
	for key, attempt := range store.attempts {
		if !attempt.ExpiresAt.After(now) {
			delete(store.attempts, key)
		}
	}
	for key, session := range store.sessions {
		if !session.ExpiresAt.After(now) {
			delete(store.sessions, key)
		}
	}
}
