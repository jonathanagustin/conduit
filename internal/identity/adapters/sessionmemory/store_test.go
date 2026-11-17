package sessionmemory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/sessiontest"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

func TestMemoryStoreConformsToTheSessionStorePort(t *testing.T) {
	sessiontest.Run(t, func(*testing.T) ports.SessionStore { return New() })
}

// An anonymous caller can start sign-in attempts at will. The store refuses to
// grow past its bound, and frees the room again once attempts expire.
func TestMemoryStoreRefusesToGrowPastItsBoundsAndRecoversWhenAttemptsExpire(t *testing.T) {
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	store := NewBounded(2, 1)
	attempt := func(expires time.Time) domain.LoginAttempt {
		state, _ := domain.NewOpaqueSecret()
		binding, _ := domain.NewOpaqueSecret()
		return domain.LoginAttempt{
			StateHash: domain.HashSecret(state), BindingHash: domain.HashSecret(binding),
			Nonce: state, CodeVerifier: binding, ReturnPath: "/console/", ExpiresAt: expires,
		}
	}
	for range 2 {
		if err := store.SaveLoginAttempt(ctx, attempt(now.Add(time.Minute)), now); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveLoginAttempt(ctx, attempt(now.Add(time.Minute)), now); !errors.Is(err, domain.ErrSessionStoreFull) {
		t.Fatalf("a third attempt answered %v, want ErrSessionStoreFull", err)
	}
	if err := store.SaveLoginAttempt(ctx, attempt(now.Add(10*time.Minute)), now.Add(2*time.Minute)); err != nil {
		t.Fatalf("expired attempts were not reclaimed: %v", err)
	}
	session := func(name string) domain.Session {
		identifier, _ := domain.NewOpaqueSecret()
		return domain.Session{IDHash: domain.HashSecret(identifier), Claims: domain.Claims{Username: name},
			CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	}
	first := session("ada")
	if err := store.SaveSession(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(ctx, session("grace")); !errors.Is(err, domain.ErrSessionStoreFull) {
		t.Fatalf("a session past the bound answered %v", err)
	}
	// Refused, not evicted: the session that was there still is.
	if _, err := store.Session(ctx, first.IDHash, now); err != nil {
		t.Fatalf("a full store evicted a live session: %v", err)
	}
}
