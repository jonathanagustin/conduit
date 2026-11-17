package revocationpostgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/revocationtest"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
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

func TestPostgresStoreConformsToTheAgentCredentialPort(t *testing.T) {
	if os.Getenv(pgtest.DataSourceNameEnvironment) == "" {
		t.Skip(pgtest.DataSourceNameEnvironment + " is not set")
	}
	revocationtest.Run(t, func(t *testing.T) ports.AgentCredentialStore {
		store, err := New(context.Background(), freshDSN(t))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	})
}

// Two pools on one database are two Hub replicas. A revocation written by one
// must be what the other reads on its very next request, and a removal
// likewise: there is no cache whose expiry either would have to wait for.
func TestPostgresRevocationsConvergeBetweenReplicasImmediately(t *testing.T) {
	dsn := freshDSN(t)
	ctx := context.Background()
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
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	credential := domain.AgentCredential{
		Identity: domain.AgentIdentity{Environment: "prod", AgentID: "primary"}, Serial: "a1", NotBefore: now.Add(-time.Hour),
	}
	refused := func(store *Store) bool {
		t.Helper()
		revocations, err := store.Revocations(ctx, "prod")
		if err != nil {
			t.Fatal(err)
		}
		_, found := domain.RefusingRevocation(revocations, credential)
		return found
	}
	if refused(second) {
		t.Fatal("credential refused before any revocation")
	}
	revocation, err := domain.NewAgentRevocation(domain.AgentRevocation{
		Scope: domain.RevocationAgent, Environment: "prod", AgentID: "primary", Reason: "key leaked",
	}, "alice", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.PutRevocation(ctx, revocation); err != nil {
		t.Fatal(err)
	}
	if !refused(second) {
		t.Fatal("a revocation written through one replica was not read by the other")
	}
	if _, err := second.DeleteRevocation(ctx, "prod", revocation.ID); err != nil {
		t.Fatal(err)
	}
	if refused(first) {
		t.Fatal("a removal through one replica was not read by the other")
	}
}
