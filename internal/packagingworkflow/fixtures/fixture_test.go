package fixtures

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
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
	zarf, err := os.ReadFile(filepath.Join(dir, "out", "sample-app-1", "zarf.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cr, err := os.ReadFile(filepath.Join(dir, "out", "sample-app-1", "uds-package.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(zarf, []byte("kind: fixture")) || bytes.Contains(cr, []byte("kind: fixture")) {
		t.Fatal("rendered package copied the source body")
	}
	if !bytes.Contains(zarf, []byte("apiVersion: zarf.dev/v1beta1")) || !bytes.Contains(zarf, []byte("kind: ZarfPackageConfig")) {
		t.Fatalf("zarf.yaml = %s", zarf)
	}
	for _, section := range []string{"apiVersion: uds.dev/v1alpha1", "kind: Package", "sso:", "allow:", "monitor:"} {
		if !bytes.Contains(cr, []byte(section)) {
			t.Fatalf("package missing %s\n%s", section, cr)
		}
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
	files, err := Render(source, domain.DigestBytes(source), TemplateVersion, ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		marker string
		want   domain.Outcome
	}{
		{"requirement: unsupported\n", domain.OutcomeUnsupported},
		{"security-evidence: unknown\n", domain.OutcomeUnknownSecurity},
		{"lint: fail\n", domain.OutcomeFailRequired},
		{"memory: 8Gi\n", domain.OutcomeFailRequired},
	}
	for _, tc := range cases {
		marked := append(append([]byte{}, source...), tc.marker...)
		got := Gate(marked, files.Zarf, files.CR, domain.DigestBytes(marked), TemplateVersion, ToolVersions)
		if got.Outcome != tc.want {
			t.Fatalf("%s outcome = %s", tc.marker, got.Outcome)
		}
	}
}

func TestNamedSourceBindsItsOwnDigestAndProposal(t *testing.T) {
	dir := t.TempDir()
	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	body := []byte("name: sample-app\nproposal: memory limit 8Gi\nrationale: unbounded growth\nmemory: 8Gi\n")
	if err := os.MkdirAll(filepath.Join(dir, "sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sources", "sample-app-blocked.txt"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	id, err := IdentityFromSource("sample-app-blocked", body)
	if err != nil {
		t.Fatal(err)
	}
	assess, err := Suite{}.Assess(context.Background(), id, dir)
	if err != nil || assess.SourceDigest != id.SourceDigest {
		t.Fatalf("assess = %#v err=%v", assess, err)
	}
	proposal, err := Suite{}.Propose(context.Background(), id, dir)
	if err != nil || proposal.Outcome != domain.OutcomePass || proposal.Reason != "proposed memory limit 8Gi: unbounded growth" {
		t.Fatalf("proposal = %#v err=%v", proposal, err)
	}
	validation, err := Suite{}.Validate(context.Background(), id, dir, proposal)
	if err != nil || validation.Outcome != domain.OutcomeFailRequired || validation.Reason != "memory limit 8Gi exceeds the 1Gi ceiling" {
		t.Fatalf("validation = %#v err=%v", validation, err)
	}
}

func TestConcurrentRunsValidateTheirOwnRender(t *testing.T) {
	dir := t.TempDir()
	if err := Materialize(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "sources"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(runID, memory string) domain.RunIdentity {
		t.Helper()
		body := []byte("name: sample-app\nproposal: memory limit " + memory + "\nrationale: recorded\nmemory: " + memory + "\n")
		if err := os.WriteFile(filepath.Join(dir, "sources", runID+".txt"), body, 0o644); err != nil {
			t.Fatal(err)
		}
		id, err := IdentityFromSource(runID, body)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	passing := write("sample-app-1", "512Mi")
	blocked := write("sample-app-blocked", "8Gi")
	var failures []string
	check := func(id domain.RunIdentity, want domain.Outcome, reasonPart string) string {
		proposal, err := Suite{}.Propose(context.Background(), id, dir)
		if err != nil {
			return id.RunID + " propose " + err.Error()
		}
		validation, err := Suite{}.Validate(context.Background(), id, dir, proposal)
		if err != nil || validation.Outcome != want || validation.ArtifactDigest != proposal.Artifact.Digest || !bytes.Contains([]byte(validation.Reason), []byte(reasonPart)) {
			return id.RunID + " " + validation.Reason
		}
		return ""
	}
	for range 25 {
		var wg sync.WaitGroup
		var mu sync.Mutex
		wg.Add(2)
		go func() {
			defer wg.Done()
			if failure := check(passing, domain.OutcomePass, "lint and render passed"); failure != "" {
				mu.Lock()
				failures = append(failures, failure)
				mu.Unlock()
			}
		}()
		go func() {
			defer wg.Done()
			if failure := check(blocked, domain.OutcomeFailRequired, "8Gi"); failure != "" {
				mu.Lock()
				failures = append(failures, failure)
				mu.Unlock()
			}
		}()
		wg.Wait()
	}
	if len(failures) != 0 {
		t.Fatalf("shared render leaked across runs: %v", failures)
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
