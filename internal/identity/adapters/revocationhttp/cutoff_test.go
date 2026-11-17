package revocationhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

func TestBackwardCutoffCannotReadmitACertificateWhenAuditIsUnavailable(t *testing.T) {
	for _, scope := range []string{domain.RevocationAgent, domain.RevocationEnvironment} {
		t.Run(scope, func(t *testing.T) {
			fixture := newFixture(t)
			now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
			draft := domain.AgentRevocation{Scope: scope, Reason: "compromised", NotBefore: now}
			if scope == domain.RevocationAgent {
				draft.AgentID = "primary"
			}
			body, _ := json.Marshal(draft)
			if response := fixture.do(http.MethodPost, "/v1/environments/prod/agent-revocations", string(body)); response.Code != http.StatusOK {
				t.Fatalf("setup: %d %s", response.Code, response.Body.String())
			}
			before, err := fixture.store.Revocations(t.Context(), "prod")
			if err != nil {
				t.Fatal(err)
			}
			credential := domain.AgentCredential{Identity: domain.AgentIdentity{Environment: "prod", AgentID: "primary"}, NotBefore: now.Add(-time.Hour)}
			if _, revoked := domain.RefusingRevocation(before, credential); !revoked {
				t.Fatal("precondition: certificate must be revoked")
			}
			fixture.auditErr = errors.New("audit unavailable")
			draft.NotBefore = now.Add(-2 * time.Hour)
			body, _ = json.Marshal(draft)
			response := fixture.do(http.MethodPost, "/v1/environments/prod/agent-revocations", string(body))
			if response.Code != http.StatusConflict {
				t.Errorf("backward cutoff: %d %s; want conflict", response.Code, response.Body.String())
			}
			after, err := fixture.store.Revocations(t.Context(), "prod")
			if err != nil {
				t.Fatal(err)
			}
			if len(after) != 1 || after[0] != before[0] {
				t.Errorf("failed replacement changed deny-list: %#v", after)
			}
			if _, revoked := domain.RefusingRevocation(after, credential); !revoked {
				t.Fatal("failed audit re-admitted a revoked certificate")
			}
		})
	}
}
