package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestJournalCodecBindsExactStorageKeyAndCanonicalizesTime(t *testing.T) {
	e := request(t)
	e.CreatedAt = e.CreatedAt.In(time.FixedZone("offset", 3600))
	e.ExpiresAt = e.ExpiresAt.In(time.FixedZone("offset", 3600))
	e.RecordedAt = e.RecordedAt.In(time.FixedZone("offset", 3600))
	b, err := EncodeEvent(e)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEvent(e.SessionID, e.Revision, b)
	if err != nil {
		t.Fatal(err)
	}
	if got != canonicalEvent(e) {
		t.Fatal(got, e)
	}
	if _, err := DecodeEvent("dsr_bcdefghijklmnopqrstuvwxyz2", e.Revision, b); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	if _, err := DecodeEvent(e.SessionID, e.Revision+1, b); !errors.Is(err, ErrCorrupt) {
		t.Fatal(err)
	}
	b2, err := EncodeEvent(got)
	if err != nil || !bytes.Equal(b, b2) {
		t.Fatal("unstable storage representation", err)
	}
}
func TestJournalCodecRejectsAmbiguousAndCorruptDocuments(t *testing.T) {
	e := request(t)
	b, _ := EncodeEvent(e)
	cases := [][]byte{
		nil, []byte("null"), []byte("[]"), append(append([]byte{}, b...), b...), append([]byte(" "), bytes.Repeat([]byte(" "), MaximumDocumentBytes)...),
		[]byte(strings.Replace(string(b), `"owner":`, `"extra":true,"owner":`, 1)),
		[]byte(strings.Replace(string(b), `"owner":`, `"owner":"attacker","owner":`, 1)),
		[]byte(strings.Replace(string(b), `"owner":`, `"Owner":"attacker","owner":`, 1)),
		[]byte(strings.Replace(string(b), `"owner":`, `"Owner":`, 1)),
		[]byte(strings.Replace(string(b), `"namespace":`, `"namespace":"other","namespace":`, 1)),
		[]byte(strings.Replace(string(b), `"namespace":`, `"Namespace":`, 1)),
		[]byte(strings.Replace(string(b), `"revision":1`, `"revision":1.0`, 1)),
		[]byte(strings.Replace(string(b), `"state":"REQUESTED"`, `"state":"READY"`, 1)),
		[]byte(strings.Replace(string(b), `"target":{`, `"target":{"unknown":true,`, 1)),
	}
	for i, doc := range cases {
		if _, err := DecodeEvent(e.SessionID, 1, doc); !errors.Is(err, ErrCorrupt) {
			t.Fatalf("case %d accepted: %s (%v)", i, doc, err)
		}
	}
	e.ValueTier = "OBSERVED"
	if _, err := EncodeEvent(e); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}
func TestJournalCodecAcceptsWhitespaceAndObjectOrderWithoutAliasing(t *testing.T) {
	e := request(t)
	b, _ := EncodeEvent(e)
	var record map[string]any
	if err := json.Unmarshal(b, &record); err != nil {
		t.Fatal(err)
	}
	pretty, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeEvent(e.SessionID, 1, pretty)
	if err != nil || got != e {
		t.Fatal(got, err)
	}
}
