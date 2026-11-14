package modelcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	packagingskill "github.com/jonathanagustin/conduit/skills/packaging"
)

func TestParseArgvDoesNotUseAShell(t *testing.T) {
	argv, err := ParseArgv(`tool exec --flag "quoted value" -`)
	if err != nil || len(argv) != 5 || argv[3] != "quoted value" {
		t.Fatalf("argv = %#v %v", argv, err)
	}
	if _, err := ParseArgv(`tool $(echo hi)`); err == nil {
		t.Fatal("accepted shell syntax")
	}
}

func TestRunnerPassBlockedTimeoutMalformedAndExit(t *testing.T) {
	script := writeScript(t)
	input := domain.PackagingContext{RunID: "sample-app-1", GateRules: "memory limit must not exceed 1Gi"}
	pass, provenance, err := (Runner{Argv: []string{script, "pass"}, Timeout: 5 * time.Second}).Propose(context.Background(), input)
	if err != nil || pass.Value != "512Mi" || provenance.PromptSHA256 == "" || provenance.ResponseSHA256 == "" || provenance.SkillSHA256 != packagingskill.SHA256() {
		t.Fatalf("pass = %#v %#v %v", pass, provenance, err)
	}
	blocked, _, err := (Runner{Argv: []string{script, "blocked"}, Timeout: 5 * time.Second}).Propose(context.Background(), input)
	if err != nil || blocked.Value != "8Gi" {
		t.Fatalf("blocked = %#v %v", blocked, err)
	}
	_, _, err = (Runner{Argv: []string{script, "sleep"}, Timeout: 200 * time.Millisecond}).Propose(context.Background(), input)
	if !errors.Is(err, domain.ErrModelTimeout) {
		t.Fatalf("timeout = %v", err)
	}
	_, _, err = (Runner{Argv: []string{script, "malformed"}, Timeout: 5 * time.Second}).Propose(context.Background(), input)
	if !errors.Is(err, domain.ErrProposalInvalid) {
		t.Fatalf("malformed = %v", err)
	}
	_, _, err = (Runner{Argv: []string{script, "fail"}, Timeout: 5 * time.Second}).Propose(context.Background(), input)
	if !errors.Is(err, domain.ErrModelFailed) {
		t.Fatalf("exit = %v", err)
	}
}

func TestRunnerPromptCarriesPackagingFacts(t *testing.T) {
	script := writeCaptureScript(t)
	digest := "sha256:" + strings.Repeat("ab", 32)
	input := domain.PackagingContext{
		RunID: "sample-app-1", ChartValues: "memory: 512Mi",
		Resources:     "limits.memory=512Mi requests.memory=256Mi",
		MemoryRequest: "256Mi", MemoryLimit: "512Mi",
		ImageRef: "127.0.0.1:5000/sample-app:0.2.0", ImageDigest: digest,
		GateRules: domain.MemoryGateRule,
	}
	if _, _, err := (Runner{Argv: []string{script}, Timeout: 5 * time.Second}).Propose(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(script), "prompt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Chart values: memory: 512Mi",
		"Resources: limits.memory=512Mi requests.memory=256Mi",
		"Memory request: 256Mi",
		"Memory limit: 512Mi",
		"Image ref: 127.0.0.1:5000/sample-app:0.2.0",
		"Image digest: " + digest,
		"Gate rules: " + domain.MemoryGateRule,
		"--- packaging skill sha256 " + packagingskill.SHA256() + " ---",
		packagingskill.Source,
		"--- end packaging skill ---",
	} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("prompt missing %q\n%s", want, body)
		}
	}
}

func writeCaptureScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "model")
	body := "#!/bin/sh\ncat > \"$(dirname \"$0\")/prompt.txt\"\nprintf '%s\\n' '{\"field\":\"memory\",\"value\":\"512Mi\",\"rationale\":\"chart memory is 512Mi\"}'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeScript(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "model")
	body := `#!/bin/sh
cat >/dev/null
case "$1" in
  pass) printf '%s\n' '{"field":"memory","value":"512Mi","rationale":"headroom"}' ;;
  blocked) printf '%s\n' '{"field":"memory","value":"8Gi","rationale":"unbounded"}' ;;
  malformed) printf '%s\n' 'not-json' ;;
  sleep) sleep 5 ;;
  fail) exit 3 ;;
esac
`
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

type stubTools struct {
	ref string
}

func (stub *stubTools) InspectImage(_ context.Context, ref string) (domain.ImageInspect, error) {
	stub.ref = ref
	return domain.ImageInspect{Ref: ref, User: "65532", ExposedPorts: []string{"8080/tcp"}, Env: []string{"PORT"}}, nil
}
func (stub *stubTools) ValidateManifests(context.Context, string) (domain.ManifestReport, error) {
	return domain.ManifestReport{OK: true}, nil
}
func (stub *stubTools) DryRunGates(context.Context, domain.GateInput) (domain.Validation, error) {
	return domain.Validation{}, nil
}
func (stub *stubTools) InspectRepo(context.Context) (domain.RepoInspect, error) {
	return domain.RepoInspect{}, nil
}
func (stub *stubTools) ReadFile(context.Context, string) (domain.RepoFile, error) {
	return domain.RepoFile{}, nil
}
func (stub *stubTools) ValidateDockerfile(context.Context, string, string) (domain.ManifestReport, error) {
	return domain.ManifestReport{OK: true}, nil
}

func TestRunnerRecordsInspectImageToolFacts(t *testing.T) {
	script := writeCaptureScript(t)
	tools := &stubTools{}
	input := domain.PackagingContext{
		RunID: "sample-app-1", ImageRef: "127.0.0.1:15000/web@sha256:" + strings.Repeat("ab", 32),
		GateRules: domain.MemoryGateRule,
	}
	_, provenance, err := (Runner{Argv: []string{script}, Timeout: 5 * time.Second, Tools: tools}).Propose(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if tools.ref != input.ImageRef {
		t.Fatalf("inspected %q", tools.ref)
	}
	if len(provenance.ToolCalls) != 1 || provenance.ToolCalls[0].Name != "inspect_image" {
		t.Fatalf("tool calls = %#v", provenance.ToolCalls)
	}
	body, err := os.ReadFile(filepath.Join(filepath.Dir(script), "prompt.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Tool results:") || !strings.Contains(string(body), `"user":"65532"`) || strings.Contains(string(body), "PORT=") {
		t.Fatalf("prompt tool facts = %s", body)
	}
}
