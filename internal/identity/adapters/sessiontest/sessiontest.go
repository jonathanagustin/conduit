// Package sessiontest is the conformance suite every SessionStore runs.
//
// The in-memory adapter runs it on every `go test`; the PostgreSQL adapter runs
// it wherever a database is available. One suite, so the two cannot come to
// disagree about what "expired" or "already used" means.
package sessiontest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

func secret(t *testing.T) string {
	t.Helper()
	value, err := domain.NewOpaqueSecret()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

// Run exercises one store. Open must return an empty store each time.
func Run(t *testing.T, open func(*testing.T) ports.SessionStore) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	t.Run("a sign-in attempt is redeemed exactly once", func(t *testing.T) {
		store := open(t)
		attempt := domain.LoginAttempt{
			StateHash: domain.HashSecret(secret(t)), BindingHash: domain.HashSecret(secret(t)),
			Nonce: secret(t), CodeVerifier: secret(t), ReturnPath: "/console/#/plans",
			ExpiresAt: now.Add(domain.LoginAttemptLifetime),
		}
		if err := store.SaveLoginAttempt(ctx, attempt, now); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveLoginAttempt(ctx, attempt, now); err == nil {
			t.Fatal("a second attempt under the same state was accepted")
		}
		got, err := store.TakeLoginAttempt(ctx, attempt.StateHash, now.Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if got.BindingHash != attempt.BindingHash || got.Nonce != attempt.Nonce ||
			got.CodeVerifier != attempt.CodeVerifier || got.ReturnPath != attempt.ReturnPath ||
			!got.ExpiresAt.Equal(attempt.ExpiresAt) {
			t.Fatalf("attempt = %#v, want %#v", got, attempt)
		}
		if _, err := store.TakeLoginAttempt(ctx, attempt.StateHash, now.Add(time.Minute)); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("a replayed state answered %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("an expired sign-in attempt is absent and is still consumed", func(t *testing.T) {
		store := open(t)
		attempt := domain.LoginAttempt{
			StateHash: domain.HashSecret(secret(t)), BindingHash: domain.HashSecret(secret(t)),
			Nonce: secret(t), CodeVerifier: secret(t), ReturnPath: "/console/",
			ExpiresAt: now.Add(time.Minute),
		}
		if err := store.SaveLoginAttempt(ctx, attempt, now); err != nil {
			t.Fatal(err)
		}
		if _, err := store.TakeLoginAttempt(ctx, attempt.StateHash, now.Add(time.Minute)); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("an attempt at its expiry answered %v, want ErrSessionNotFound", err)
		}
		if _, err := store.TakeLoginAttempt(ctx, attempt.StateHash, now); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("an expired attempt survived being asked for: %v", err)
		}
	})

	t.Run("an unknown state is absent", func(t *testing.T) {
		store := open(t)
		if _, err := store.TakeLoginAttempt(ctx, domain.HashSecret(secret(t)), now); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("got %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("a session is read until it expires and not after", func(t *testing.T) {
		store := open(t)
		session := domain.Session{
			IDHash:    domain.HashSecret(secret(t)),
			Claims:    domain.Claims{Username: "ada", Groups: []string{"operators", "reviewers"}},
			CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		}
		if err := store.SaveSession(ctx, session); err != nil {
			t.Fatal(err)
		}
		if err := store.SaveSession(ctx, session); err == nil {
			t.Fatal("a second session under the same identifier was accepted")
		}
		got, err := store.Session(ctx, session.IDHash, now.Add(59*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if got.Claims.Username != "ada" || len(got.Claims.Groups) != 2 || got.Claims.Groups[0] != "operators" ||
			got.Claims.ServiceAccount || got.Claims.OnBehalfOf != "" || !got.ExpiresAt.Equal(session.ExpiresAt) {
			t.Fatalf("session = %#v", got)
		}
		if _, err := store.Session(ctx, session.IDHash, now.Add(time.Hour)); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("a session at its expiry answered %v, want ErrSessionNotFound", err)
		}
		if _, err := store.Session(ctx, domain.HashSecret(secret(t)), now); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("an unknown session answered %v", err)
		}
	})

	t.Run("deleting a session removes it and is idempotent", func(t *testing.T) {
		store := open(t)
		session := domain.Session{
			IDHash: domain.HashSecret(secret(t)), Claims: domain.Claims{Username: "ada"},
			CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		}
		if err := store.SaveSession(ctx, session); err != nil {
			t.Fatal(err)
		}
		if err := store.DeleteSession(ctx, session.IDHash); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Session(ctx, session.IDHash, now); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("a deleted session answered %v", err)
		}
		if err := store.DeleteSession(ctx, session.IDHash); err != nil {
			t.Fatalf("deleting twice: %v", err)
		}
	})

	t.Run("pruning removes only what has expired", func(t *testing.T) {
		store := open(t)
		live := domain.Session{IDHash: domain.HashSecret(secret(t)), Claims: domain.Claims{Username: "ada"},
			CreatedAt: now, ExpiresAt: now.Add(2 * time.Hour)}
		dead := domain.Session{IDHash: domain.HashSecret(secret(t)), Claims: domain.Claims{Username: "grace"},
			CreatedAt: now, ExpiresAt: now.Add(time.Minute)}
		for _, session := range []domain.Session{live, dead} {
			if err := store.SaveSession(ctx, session); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Prune(ctx, now.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		// Asked with a clock before the dead session's expiry: only a row that
		// is really gone answers not-found here.
		if _, err := store.Session(ctx, dead.IDHash, now); !errors.Is(err, domain.ErrSessionNotFound) {
			t.Fatalf("an expired session survived pruning: %v", err)
		}
		if _, err := store.Session(ctx, live.IDHash, now.Add(time.Hour)); err != nil {
			t.Fatalf("a live session was pruned: %v", err)
		}
	})
}
