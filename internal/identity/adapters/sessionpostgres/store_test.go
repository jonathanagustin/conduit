package sessionpostgres

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/sessiontest"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
	"github.com/jonathanagustin/conduit/internal/pgmigrate"
	"github.com/jonathanagustin/conduit/internal/pgtest"
)

// freshDSN returns a throwaway database, or skips without a server.
func freshDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv(pgtest.DataSourceNameEnvironment)
	if dsn == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
	}
	shared, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shared.Close() })
	fresh, drop, err := pgtest.Fresh(context.Background(), shared, dsn, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(drop)
	return fresh
}

func secret(t *testing.T) string {
	t.Helper()
	value, err := domain.NewOpaqueSecret()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestPostgresStoreConformsToTheSessionStorePort(t *testing.T) {
	if os.Getenv(pgtest.DataSourceNameEnvironment) == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
	}
	sessiontest.Run(t, func(t *testing.T) ports.SessionStore {
		store, err := New(context.Background(), freshDSN(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	})
}

// Two pools on one database are two Hub replicas. A person who signed in at one
// is signed in at the other, a sign-out at one ends it at both, and a state
// offered to both at once is redeemed by exactly one.
func TestPostgresSessionsAreSharedBetweenReplicas(t *testing.T) {
	dsn := freshDSN(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	first, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if pending, err := pgmigrate.Pending(ctx, second.db, Component, Migrations()); err != nil || len(pending) != 0 {
		t.Fatalf("pending after apply = %v %v", pending, err)
	}

	identifier := secret(t)
	session := domain.Session{
		IDHash: domain.HashSecret(identifier), Claims: domain.Claims{Username: "ada", Groups: []string{"operators"}},
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	if err := first.SaveSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	got, err := second.Session(ctx, session.IDHash, now)
	if err != nil || got.Claims.Username != "ada" || len(got.Claims.Groups) != 1 || !got.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatalf("the second replica read %#v %v", got, err)
	}
	if err := second.DeleteSession(ctx, session.IDHash); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Session(ctx, session.IDHash, now); !errors.Is(err, domain.ErrSessionNotFound) {
		t.Fatalf("a session signed out at one replica still resolves at the other: %v", err)
	}

	// The cookie's value is nowhere in the database: only its hash is.
	var holdsValue int
	if err := first.SaveSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := first.db.QueryRowContext(ctx, `SELECT count(*) FROM conduit_operator_sessions
		WHERE id_hash = $1 OR position($1 in convert_from(claims, 'UTF8')) > 0`, identifier).Scan(&holdsValue); err != nil {
		t.Fatal(err)
	}
	if holdsValue != 0 {
		t.Fatal("the session table holds the cookie's value")
	}

	state := secret(t)
	attempt := domain.LoginAttempt{
		StateHash: domain.HashSecret(state), BindingHash: domain.HashSecret(secret(t)),
		Nonce: secret(t), CodeVerifier: secret(t), ReturnPath: "/console/", ExpiresAt: now.Add(10 * time.Minute),
	}
	if err := first.SaveLoginAttempt(ctx, attempt, now); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	results := make([]error, 8)
	for index := range results {
		group.Add(1)
		go func() {
			defer group.Done()
			replica := first
			if index%2 == 1 {
				replica = second
			}
			_, results[index] = replica.TakeLoginAttempt(ctx, attempt.StateHash, now)
		}()
	}
	group.Wait()
	redeemed := 0
	for _, result := range results {
		switch {
		case result == nil:
			redeemed++
		case !errors.Is(result, domain.ErrSessionNotFound):
			t.Fatalf("a racing redemption failed with %v", result)
		}
	}
	if redeemed != 1 {
		t.Fatalf("one state was redeemed %d times across two replicas", redeemed)
	}
}

// The database refuses what the adapter would never write, so a second writer
// that skipped the adapter cannot plant a session keyed by something that is
// not a hash, or one that outlives the bound.
func TestPostgresSchemaEnforcesTheShape(t *testing.T) {
	ctx := context.Background()
	store, err := New(ctx, freshDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hash := strings.Repeat("a", 64)
	for name, statement := range map[string]string{
		"a session keyed by a raw identifier": `INSERT INTO conduit_operator_sessions VALUES ('4tZ8Yw0Q1gq3mXl2c9s7r5v1b6n8m0k2j4h6g8f0d2s', '\x7b7d', now(), now() + interval '1 hour')`,
		"a session with upper-case hex":       `INSERT INTO conduit_operator_sessions VALUES ('` + strings.Repeat("A", 64) + `', '\x7b7d', now(), now() + interval '1 hour')`,
		"a session past 24 hours":             `INSERT INTO conduit_operator_sessions VALUES ('` + hash + `', '\x7b7d', now(), now() + interval '25 hours')`,
		"a session expiring before it began":  `INSERT INTO conduit_operator_sessions VALUES ('` + hash + `', '\x7b7d', now(), now() - interval '1 second')`,
		"a session with empty claims":         `INSERT INTO conduit_operator_sessions VALUES ('` + hash + `', '', now(), now() + interval '1 hour')`,
		"an attempt with a short verifier":    `INSERT INTO conduit_operator_login_attempts VALUES ('` + hash + `', '` + hash + `', repeat('n', 43), 'short', '/console/', now())`,
		"an attempt with a raw state":         `INSERT INTO conduit_operator_login_attempts VALUES ('state', '` + hash + `', repeat('n', 43), repeat('v', 43), '/console/', now())`,
		"an attempt with no return path":      `INSERT INTO conduit_operator_login_attempts VALUES ('` + hash + `', '` + hash + `', repeat('n', 43), repeat('v', 43), '', now())`,
	} {
		if _, err := store.db.ExecContext(ctx, statement); err == nil {
			t.Errorf("%s: the schema accepted it", name)
		}
	}
}
