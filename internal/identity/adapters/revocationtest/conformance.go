// Package revocationtest is the one behaviour both Agent credential stores
// must have. The in-memory adapter runs it on every `go test`; the PostgreSQL
// adapter runs it wherever a database is supplied.
package revocationtest

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// Run drives a fresh store from open through the whole port.
func Run(t *testing.T, open func(*testing.T) ports.AgentCredentialStore) {
	ctx := context.Background()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	revocation := func(t *testing.T, draft domain.AgentRevocation, at time.Time) domain.AgentRevocation {
		t.Helper()
		created, err := domain.NewAgentRevocation(draft, "alice", at)
		if err != nil {
			t.Fatal(err)
		}
		return created
	}

	t.Run("a revocation is read back exactly, per Environment, and replaced under its identifier", func(t *testing.T) {
		store := open(t)
		agent := revocation(t, domain.AgentRevocation{Scope: domain.RevocationAgent, Environment: "prod", AgentID: "primary", Reason: "key leaked"}, now)
		serial := revocation(t, domain.AgentRevocation{Scope: domain.RevocationSerial, Environment: "prod", Serial: "0A:1B", Reason: "one certificate"}, now)
		other := revocation(t, domain.AgentRevocation{Scope: domain.RevocationEnvironment, Environment: "staging", Reason: "decommissioned"}, now)
		for _, entry := range []domain.AgentRevocation{agent, serial, other} {
			if previous, err := store.PutRevocation(ctx, entry); err != nil || previous != nil {
				t.Fatalf("first write of %s = %v, %v", entry.ID, previous, err)
			}
		}
		listed, err := store.Revocations(ctx, "prod")
		if err != nil {
			t.Fatal(err)
		}
		want := []domain.AgentRevocation{agent, serial}
		if want[0].ID > want[1].ID {
			want[0], want[1] = want[1], want[0]
		}
		if !reflect.DeepEqual(listed, want) {
			t.Fatalf("prod revocations\n got %#v\nwant %#v", listed, want)
		}
		later := revocation(t, domain.AgentRevocation{Scope: domain.RevocationAgent, Environment: "prod", AgentID: "primary", Reason: "leaked again"}, now.Add(time.Hour))
		previous, err := store.PutRevocation(ctx, later)
		if err != nil || previous == nil || !reflect.DeepEqual(*previous, agent) {
			t.Fatalf("replacement returned %v, %v; want the record it replaced", previous, err)
		}
		listed, _ = store.Revocations(ctx, "prod")
		if len(listed) != 2 {
			t.Fatalf("revoking the same identity twice left %d records, want 2", len(listed))
		}
	})

	t.Run("removal returns the record, is scoped to its Environment, and is not repeatable", func(t *testing.T) {
		store := open(t)
		entry := revocation(t, domain.AgentRevocation{Scope: domain.RevocationEnvironment, Environment: "prod", Reason: "incident"}, now)
		if _, err := store.PutRevocation(ctx, entry); err != nil {
			t.Fatal(err)
		}
		if _, err := store.DeleteRevocation(ctx, "staging", entry.ID); !errors.Is(err, domain.ErrRevocationNotFound) {
			t.Fatalf("removal through another Environment = %v, want not found", err)
		}
		removed, err := store.DeleteRevocation(ctx, "prod", entry.ID)
		if err != nil || !reflect.DeepEqual(removed, entry) {
			t.Fatalf("removal = %#v, %v", removed, err)
		}
		if _, err := store.DeleteRevocation(ctx, "prod", entry.ID); !errors.Is(err, domain.ErrRevocationNotFound) {
			t.Fatalf("second removal = %v, want not found", err)
		}
		if listed, _ := store.Revocations(ctx, "prod"); len(listed) != 0 {
			t.Fatalf("revocations after removal = %#v", listed)
		}
	})

	t.Run("an Environment's deny-list is bounded", func(t *testing.T) {
		store := open(t)
		for index := 1; index <= domain.MaximumRevocationsPerEnvironment; index++ {
			entry := revocation(t, domain.AgentRevocation{
				Scope: domain.RevocationSerial, Environment: "prod", Serial: strconv.FormatInt(int64(index), 16), Reason: "bulk",
			}, now)
			if _, err := store.PutRevocation(ctx, entry); err != nil {
				t.Fatalf("write %d: %v", index, err)
			}
		}
		overflow := revocation(t, domain.AgentRevocation{Scope: domain.RevocationEnvironment, Environment: "prod", Reason: "one too many"}, now)
		if _, err := store.PutRevocation(ctx, overflow); !errors.Is(err, domain.ErrRevocationLimit) {
			t.Fatalf("write past the bound = %v, want the limit", err)
		}
		// Replacing a record already held is not growth.
		held := revocation(t, domain.AgentRevocation{Scope: domain.RevocationSerial, Environment: "prod", Serial: "1", Reason: "restated"}, now)
		if _, err := store.PutRevocation(ctx, held); err != nil {
			t.Fatalf("replacement at the bound = %v", err)
		}
	})

	t.Run("the latest certificate per Agent is kept and the earliest expiry is over recently seen Agents", func(t *testing.T) {
		store := open(t)
		if _, agents, err := store.EarliestExpiry(ctx, now.Add(-time.Hour)); err != nil || agents != 0 {
			t.Fatalf("empty store reports %d agents, %v", agents, err)
		}
		sighting := func(environment, agent, serial string, notAfter, seen time.Time) domain.AgentCredentialSighting {
			return domain.AgentCredentialSighting{
				Environment: environment, AgentID: agent, Serial: serial,
				NotBefore: now.Add(-24 * time.Hour), NotAfter: notAfter, SeenAt: seen,
			}
		}
		for _, entry := range []domain.AgentCredentialSighting{
			sighting("prod", "primary", "a1", now.Add(48*time.Hour), now.Add(-time.Minute)),
			sighting("prod", "primary", "a2", now.Add(72*time.Hour), now),
			// Arrives late from another replica; must not replace the newer one.
			sighting("prod", "primary", "a0", now.Add(time.Minute), now.Add(-2*time.Minute)),
			sighting("staging", "primary", "b1", now.Add(30*time.Hour), now),
			sighting("retired", "primary", "c1", now.Add(time.Hour), now.Add(-48*time.Hour)),
		} {
			if err := store.RecordSighting(ctx, entry); err != nil {
				t.Fatal(err)
			}
		}
		listed, err := store.Sightings(ctx, "prod")
		if err != nil || len(listed) != 1 || listed[0].Serial != "a2" || !listed[0].NotAfter.Equal(now.Add(72*time.Hour)) {
			t.Fatalf("prod sightings = %#v, %v; want only the renewed certificate", listed, err)
		}
		earliest, agents, err := store.EarliestExpiry(ctx, now.Add(-time.Hour))
		if err != nil || agents != 2 || !earliest.Equal(now.Add(30*time.Hour)) {
			t.Fatalf("earliest expiry = %s over %d agents, %v; want staging's over 2", earliest, agents, err)
		}
	})
}
