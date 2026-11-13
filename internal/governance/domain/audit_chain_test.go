package domain

import (
	"bytes"
	"testing"
	"time"
)

func chainEvent() AuthorizationAuditEvent {
	return NewAuthorizationCheckEvent(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), "operator", "read", "resource", DecisionAllow, "permitted", nil)
}

func TestAuditChainBindsPayloadIdentityOriginAndPredecessor(t *testing.T) {
	genesis := EmptyAuditCheckpoint()
	first, err := NewAuditChainRecord(genesis, chainEvent(), AuditOriginLegacy)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := VerifyAuditChainRecord(genesis, first)
	if err != nil || checkpoint.Sequence != 1 || checkpoint.Digest != first.Digest {
		t.Fatalf("first record: %#v, %v", checkpoint, err)
	}
	older := chainEvent()
	older.ID = "second-event"
	older.CreatedAt = older.CreatedAt.Add(-365 * 24 * time.Hour)
	second, err := NewAuditChainRecord(checkpoint, older, AuditOriginLive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAuditChainRecord(checkpoint, second); err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 {
		t.Fatal("event time replaced commit ordering")
	}
	for name, mutate := range map[string]func(*AuditChainRecord){
		"payload": func(record *AuditChainRecord) {
			record.Payload = bytes.ReplaceAll(record.Payload, []byte("permitted"), []byte("forged"))
		},
		"id":          func(record *AuditChainRecord) { record.EventID = "other" },
		"resource":    func(record *AuditChainRecord) { record.ResourceRID = "other" },
		"origin":      func(record *AuditChainRecord) { record.Origin = AuditOriginLegacy },
		"sequence":    func(record *AuditChainRecord) { record.Sequence++ },
		"predecessor": func(record *AuditChainRecord) { record.PreviousDigest = genesis.Digest },
		"digest":      func(record *AuditChainRecord) { record.Digest = genesis.Digest },
	} {
		t.Run(name, func(t *testing.T) {
			changed := second
			changed.Payload = bytes.Clone(second.Payload)
			mutate(&changed)
			if _, err := VerifyAuditChainRecord(checkpoint, changed); err == nil {
				t.Fatal("altered record verified")
			}
		})
	}
	if _, err := VerifyAuditChainRecord(genesis, second); err == nil {
		t.Fatal("removed prefix verified")
	}
}

func TestAuditChainRefusesInvalidCheckpointAndUnknownOrigin(t *testing.T) {
	for _, previous := range []AuditCheckpoint{{}, {Version: AuditChainVersion, Digest: "00"}, {Version: AuditChainVersion, Digest: string(bytes.Repeat([]byte("0"), 64))}} {
		if _, err := NewAuditChainRecord(previous, chainEvent(), AuditOriginLive); err == nil {
			t.Fatal("invalid checkpoint accepted")
		}
	}
	if _, err := NewAuditChainRecord(EmptyAuditCheckpoint(), chainEvent(), "UNKNOWN"); err == nil {
		t.Fatal("unknown origin accepted")
	}
	event := chainEvent()
	event.ID = ""
	if _, err := NewAuditChainRecord(EmptyAuditCheckpoint(), event, AuditOriginLive); err == nil {
		t.Fatal("unnamed event accepted")
	}
}

// Fixed vector independently computed with Python hashlib and big-endian
// length framing; this constrains the interchange bytes, not just round trips.
func TestAuditChainKnownDigestVector(t *testing.T) {
	record, err := NewAuditChainRecord(EmptyAuditCheckpoint(), chainEvent(), AuditOriginLive)
	if err != nil {
		t.Fatal(err)
	}
	if record.Digest != "e551ef52d0dc5c92749a730002b53e9b0ad589220ac3e65761249bbfd8f4b3f6" {
		t.Fatal("audit chain framing changed")
	}
}
