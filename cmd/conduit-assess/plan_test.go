package main

import (
	"bytes"
	"encoding/json"
	"github.com/jonathanagustin/conduit/internal/packaging/domain"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanCLIFromGitThroughReviewedScaffold(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		env := []string{}
		for _, v := range os.Environ() {
			if !strings.HasPrefix(strings.ToUpper(v), "GIT_") {
				env = append(env, v)
			}
		}
		c.Env = append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "--quiet")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM registry.example.invalid/base\nEXPOSE 8080\nUSER 1000\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "Dockerfile")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+t.TempDir(), "commit", "--quiet", "-m", "fixture")
	revision := git("rev-parse", "HEAD")
	args := []string{dir, revision, "Dockerfile"}
	var out bytes.Buffer
	if err := plan(args, strings.NewReader("{}"), &out); err != nil {
		t.Fatal(err)
	}
	var packet domain.PackagingPlan
	if err := json.Unmarshal(out.Bytes(), &packet); err != nil {
		t.Fatal(err)
	}
	if packet.Artifacts != nil || packet.Status != "needs-review" {
		t.Fatal("unreviewed source generated artifacts")
	}
	input := domain.PlanInput{Scaffold: domain.ScaffoldInput{Profile: domain.ScaffoldProfile, Name: "sample-app", Version: "0.1.0", SourceRevision: revision, SourceSHA256: packet.Sources[0].SHA256, Image: "registry.example.invalid/app@sha256:" + strings.Repeat("a", 64), Port: 8080, UID: 1000, CPUMilli: 100, MemoryMiB: 128, ZarfVersion: domain.ZarfVersion, UDSCommit: domain.UDSCommit}, Review: domain.EvidenceReview{EvidenceSHA256: packet.EvidenceSHA256}}
	for _, topic := range []string{"persistence", "secrets", "startup-downloads", "identity"} {
		input.Review.Decisions = append(input.Review.Decisions, domain.RequirementDecision{Topic: topic, Disposition: "none", Rationale: "Synthetic test fixture, not user approval."})
	}
	encoded, _ := json.Marshal(input)
	out.Reset()
	if err := plan(args, bytes.NewReader(encoded), &out); err != nil {
		t.Fatal(err)
	}
	var result domain.PackagingPlan
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Artifacts == nil || len(result.Artifacts.Files) != 9 || len(result.PlanSHA256) != 64 {
		t.Fatal("reviewed pipeline failed")
	}
	// The same real source pipeline must fail closed on an explicitly required secret.
	input.Review.Decisions[1].Disposition = "required"
	encoded, _ = json.Marshal(input)
	out.Reset()
	if err := plan(args, bytes.NewReader(encoded), &out); err != nil {
		t.Fatal(err)
	}
	result = domain.PackagingPlan{}
	json.Unmarshal(out.Bytes(), &result)
	if result.Artifacts != nil {
		t.Fatal("unsupported secret requirement ignored")
	}
}
