package domain

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func exampleSource(t *testing.T, index int) Citation {
	t.Helper()
	source, err := NewCitation(Citation{Kind: SourceOntology, Title: "Environment", Source: fmt.Sprintf("Environment:visible-%d", index), Quote: fmt.Sprintf(`{"id":"visible-%d","objectType":"Environment"}`, index)})
	if err != nil {
		t.Fatal(err)
	}
	source.RetrievedAt = time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	source.AuditEventID = "aud_fixture"
	return source
}

func TestGroundingRejectsInventedModifiedAndUnreceiptedSources(t *testing.T) {
	valid := exampleSource(t, 1)
	grounding := NewGrounding()
	grounding.Add([]Citation{valid, valid}, false)
	if !grounding.Valid() || len(grounding.Sources) != 1 || grounding.Truncated {
		t.Fatalf("bad canonical source set: %+v", grounding)
	}
	for _, mutate := range []func(*Citation){func(s *Citation) { s.Quote = "invented" }, func(s *Citation) { s.ID = "csrc_invented" }, func(s *Citation) { s.ValueTier = "APOLLO" }, func(s *Citation) { s.AuditEventID = "" }, func(s *Citation) { s.RetrievedAt = time.Time{} }} {
		bad := valid
		mutate(&bad)
		set := NewGrounding()
		set.Add([]Citation{bad}, false)
		if len(set.Sources) != 0 || !set.Truncated {
			t.Fatalf("bad source retained: %+v", bad)
		}
	}
	clone := grounding.Clone()
	clone.Sources[0].Quote = "changed"
	if !grounding.Valid() || clone.Valid() {
		t.Fatal("clone aliases source evidence or accepts changed content")
	}
	if _, err := NewCitation(Citation{Kind: SourceOntology, Title: "x", Source: "Environment:x", Quote: strings.Repeat("x", MaxCitationQuoteBytes+1)}); err == nil {
		t.Fatal("oversized quote accepted")
	}
}

func TestGroundingBoundsSourcesAndAdmitsLegacyMessages(t *testing.T) {
	grounding := NewGrounding()
	for i := 0; i < MaxCitations+2; i++ {
		grounding.Add([]Citation{exampleSource(t, i)}, false)
	}
	if !grounding.Valid() || len(grounding.Sources) != MaxCitations || !grounding.Truncated {
		t.Fatalf("unbounded source set: %+v", grounding)
	}
	if err := (Message{Role: RoleAssistant, Text: "older stored answer"}).Validate(); err != nil {
		t.Fatalf("legacy document refused: %v", err)
	}
	bad := grounding.Clone()
	bad.Sources[0].Quote = "forged"
	if err := (Message{Role: RoleAssistant, Text: "answer", Grounding: bad}).Validate(); err == nil {
		t.Fatal("unverified source document accepted")
	}
	for _, args := range []string{`{"query":""}`, `{"query":"TLS","path":"/run/secrets/private"}`, `{"query":"TLS","url":"https://external.invalid"}`} {
		if _, err := ParseToolCall(ToolDocumentation, []byte(args)); err == nil {
			t.Fatalf("invalid documentation args accepted: %s", args)
		}
	}
	for _, mode := range []string{ModeExplore, ModeReleasePlan, ModeConfigDiff, ModeOntology} {
		if !ModeAllows(mode, ToolDocumentation) {
			t.Fatalf("read missing from mode %s", mode)
		}
	}
	if ModeAllows("unknown", ToolDocumentation) || ModeAllows(ModeExplore, ToolWrite) {
		t.Fatal("documentation read broadened authority")
	}
}
