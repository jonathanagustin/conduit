package domain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseProposalRejectsAnythingButOneObject(t *testing.T) {
	ok, err := ParseProposal([]byte(`{"field":"memory","value":"512Mi","rationale":"headroom"}`))
	if err != nil || ok.Value != "512Mi" {
		t.Fatalf("valid = %#v %v", ok, err)
	}
	for _, raw := range []string{
		`{"field":"cpu","value":"512Mi","rationale":"no"}`,
		`{"field":"memory","value":"512Mi","rationale":"headroom","extra":1}`,
		`{"field":"memory","value":"512Mi","rationale":"headroom"}{"field":"memory","value":"1Gi","rationale":"again"}`,
		`not json`,
		`{"field":"memory","value":"lots","rationale":"no"}`,
	} {
		if _, err := ParseProposal([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestAssessMemoryUsesTheCeiling(t *testing.T) {
	if _, _, blocked := AssessMemory("512Mi"); blocked {
		t.Fatal("512Mi blocked")
	}
	if _, _, blocked := AssessMemory("1Gi"); blocked {
		t.Fatal("1Gi blocked")
	}
	_, reason, blocked := AssessMemory("8Gi")
	if !blocked || reason != "memory limit 8Gi exceeds the 1Gi ceiling" {
		t.Fatalf("8Gi = %q %v", reason, blocked)
	}
	_, reason, blocked = AssessMemory("2Gi")
	if !blocked || !strings.Contains(reason, "exceeds the 1Gi ceiling") {
		t.Fatalf("2Gi = %q %v", reason, blocked)
	}
}

func TestDomainDoesNotImportProcesses(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		for _, banned := range []string{`"os/exec"`, `"os"`, `"net/http"`} {
			if strings.Contains(text, banned) {
				t.Fatalf("%s imports %s", name, banned)
			}
		}
	}
}

func TestPromptIncludesTheSkillAndItsDigest(t *testing.T) {
	input := PackagingContext{
		RunID: "sample-app-1", GateRules: MemoryGateRule,
		Skill: "packaging skill body", SkillSHA256: DigestText([]byte("packaging skill body")),
	}
	prompt := Prompt(input)
	marker := "--- packaging skill sha256 " + input.SkillSHA256 + " ---"
	if !strings.Contains(prompt, marker) || !strings.Contains(prompt, "packaging skill body") || !strings.Contains(prompt, "--- end packaging skill ---") {
		t.Fatalf("prompt missing skill\n%s", prompt)
	}
	if !strings.Contains(prompt, "Gate rules: "+MemoryGateRule) {
		t.Fatalf("prompt dropped gate rules\n%s", prompt)
	}
	if Prompt(PackagingContext{RunID: "sample-app-1"}) == prompt {
		t.Fatal("prompt with no skill matched the skill prompt")
	}
}
