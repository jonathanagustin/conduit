package revocationmemory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

func TestAuditedRemovalKeepsTheRecordUntilAuditSucceeds(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "audit error", true: "client canceled"}[canceled], func(t *testing.T) {
			store := New()
			held, err := domain.NewAgentRevocation(domain.AgentRevocation{
				Scope: domain.RevocationEnvironment, Environment: "prod", Reason: "incident",
			}, "alice", time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.PutRevocation(t.Context(), held); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			auditFailure := errors.New("audit unavailable")
			_, err = store.DeleteRevocationAudited(ctx, "prod", held.ID,
				func(ctx context.Context, removed domain.AgentRevocation) error {
					// The callback runs with the store lock: inspecting the map here
					// proves it was never deleted before the audit attempt.
					if got, exists := store.revocations["prod"][held.ID]; !exists || got != held || removed != held {
						t.Errorf("revocation disappeared before audit: %#v, present=%v, removed=%#v", got, exists, removed)
					}
					if canceled {
						cancel()
						return ctx.Err()
					}
					return auditFailure
				})
			if canceled && !errors.Is(err, context.Canceled) || !canceled && !errors.Is(err, auditFailure) {
				t.Fatalf("removal error = %v", err)
			}
			if got, err := store.Revocations(t.Context(), "prod"); err != nil || len(got) != 1 || got[0] != held {
				t.Fatalf("failed removal changed deny-list: %#v, %v", got, err)
			}

			audited := false
			if removed, err := store.DeleteRevocationAudited(t.Context(), "prod", held.ID,
				func(context.Context, domain.AgentRevocation) error { audited = true; return nil }); err != nil || removed != held || !audited {
				t.Fatalf("audited removal: %#v, %v, audited=%v", removed, err, audited)
			}
			if got, err := store.Revocations(t.Context(), "prod"); err != nil || len(got) != 0 {
				t.Fatalf("successful removal did not lift revocation: %#v, %v", got, err)
			}
		})
	}
}

func TestAuditedRemovalRefusesAnAbsentAuditSink(t *testing.T) {
	store := New()
	if _, err := store.DeleteRevocationAudited(t.Context(), "prod", "id", nil); err == nil {
		t.Fatal("removal with no audit sink was accepted")
	}
}
