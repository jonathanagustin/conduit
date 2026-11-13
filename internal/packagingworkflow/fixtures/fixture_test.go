package fixtures

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func TestPinnedFixturePassesLintAndOmitsSourceBody(t *testing.T) {
	dir := t.TempDir()
	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	id, err := Identity("sample-app-1")
	if err != nil {
		t.Fatal(err)
	}
	assess, err := Suite{}.Assess(context.Background(), id, dir)
	if err != nil {
		t.Fatal(err)
	}
	if assess.SourceDigest != SourceDigest() || assess.Outcome != domain.OutcomePass {
		t.Fatalf("assessment = %#v", assess)
	}
	proposal, err := Suite{}.Propose(context.Background(), id, dir)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(dir, "out", "manifest.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(manifest, []byte("kind: fixture")) {
		t.Fatal("rendered manifest copied the source body")
	}
	validation, err := Suite{}.Validate(context.Background(), id, dir, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if validation.Outcome != domain.OutcomePass || validation.ArtifactDigest != proposal.Artifact.Digest {
		t.Fatalf("validation = %#v", validation)
	}
}

func TestGatesBlockInsteadOfPassing(t *testing.T) {
	source := []byte("name: sample-app\n")
	manifest := Render(domain.DigestBytes(source), TemplateVersion, ToolVersions)
	cases := []struct {
		marker string
		want   domain.Outcome
	}{
		{"requirement: unsupported\n", domain.OutcomeUnsupported},
		{"security-evidence: unknown\n", domain.OutcomeUnknownSecurity},
		{"lint: fail\n", domain.OutcomeFailRequired},
	}
	for _, tc := range cases {
		got := Gate(append(source, tc.marker...), manifest, domain.DigestBytes(append(source, tc.marker...)), TemplateVersion, ToolVersions)
		if got.Outcome != tc.want {
			t.Fatalf("%s outcome = %s", tc.marker, got.Outcome)
		}
	}
}

func TestCredentialInSourceIsRefusedWithoutCopyingIt(t *testing.T) {
	dir := t.TempDir()
	body := []byte("-----BEGIN PRIVATE KEY-----\nMIISECRET\n")
	if err := os.WriteFile(filepath.Join(dir, "source.txt"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Suite{}.Assess(context.Background(), domain.RunIdentity{RunID: "sample-app-1"}, dir)
	if err == nil || bytes.Contains([]byte(err.Error()), []byte("MIISECRET")) {
		t.Fatalf("error = %v", err)
	}
}
