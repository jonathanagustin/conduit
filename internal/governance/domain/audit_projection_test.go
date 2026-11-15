package domain

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func auditProjectionFixture(t *testing.T) []AuthorizationAuditChainEntry {
	t.Helper()
	at := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	first := NewAuthorizationCheckEvent(at, "operator-雪", "packaging.model-proposal", "environment/east", DecisionAllow, "recorded fixture", map[string]any{"adapter": "recorded", "promptSha256": strings.Repeat("a", 64), "responseSha256": strings.Repeat("b", 64)})
	first.ID = "audit-fixture-1"
	second := NewAuthorizationCheckEvent(at.Add(time.Second), "reviewer", "read", "environment/west", DecisionDeny, "not permitted", nil)
	second.ID = "audit-fixture-2"
	entries, err := ChainAuthorizationAudit([]AuthorizationAuditEvent{second, first})
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestAuditProjectionReturnsExactDigestInputs(t *testing.T) {
	entries := auditProjectionFixture(t)
	checkpoint := EmptyAuditCheckpoint()
	for _, entry := range entries {
		record := AuditChainRecord{Sequence: entry.Sequence, PreviousDigest: entry.PreviousDigest, EventID: entry.ID, ResourceRID: entry.ResourceRID, Origin: entry.Origin, Payload: entry.Payload, Digest: entry.Digest}
		var err error
		checkpoint, err = VerifyAuditChainRecord(checkpoint, record)
		if err != nil {
			t.Fatal(err)
		}
	}
	expected, err := os.ReadFile("testdata/audit-projection.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture []AuthorizationAuditChainEntry
	if err := json.Unmarshal(expected, &fixture); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(entries)
	want, _ := json.Marshal(fixture)
	if !bytes.Equal(got, want) {
		t.Fatal("browser fixture does not match native projection digest inputs")
	}
}
