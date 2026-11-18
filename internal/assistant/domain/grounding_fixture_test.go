package domain

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// This synthetic wire fixture is shared with console tests, not a claim about
// a real operator, deployment, document or object.
func TestGroundingWireFixture(t *testing.T) {
	quote := "# Fixture operator reference\nUse TLS. <script>untrusted source text</script>\n"
	document := quote + "Additional fixture context outside this excerpt.\n"
	doc, err := NewCitation(Citation{Kind: SourceDocumentation, Title: "Fixture operator reference", Source: "fixture/operator-reference.md", Quote: quote, DocumentSHA256: Digest([]byte(document)), FirstLine: 1, LastLine: 2, EndByte: len(quote)})
	if err != nil {
		t.Fatal(err)
	}
	objectBytes, err := json.Marshal(struct {
		ID    string `json:"id"`
		Type  string `json:"objectType"`
		Label string `json:"label"`
	}{"visible-fixture", "Environment", "Development <img src=x onerror=alert(1)>"})
	if err != nil {
		t.Fatal(err)
	}
	object, err := NewCitation(Citation{Kind: SourceOntology, Title: "Development <img src=x onerror=alert(1)>", Source: "Environment:visible-fixture", Quote: string(objectBytes)})
	if err != nil {
		t.Fatal(err)
	}
	doc.RetrievedAt = time.Date(2026, 11, 18, 0, 0, 0, 123456789, time.UTC)
	object.RetrievedAt = doc.RetrievedAt
	doc.AuditEventID = "aud_fixture_doc"
	object.AuditEventID = "aud_fixture_object"
	grounding := NewGrounding()
	grounding.Add([]Citation{doc, object}, false)
	if !grounding.Valid() {
		t.Fatal("fixture is not valid source evidence")
	}
	want, err := json.MarshalIndent(grounding, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	path := filepath.Join("testdata", "grounding-v1.json")
	if os.Getenv("CONDUIT_RECORD_GROUNDING_FIXTURE") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, want, 0644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("grounding wire fixture differs from the native DTO; review the contract before recording it")
	}
}
