package modelcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
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
	if err != nil || pass.Value != "512Mi" || provenance.PromptSHA256 == "" || provenance.ResponseSHA256 == "" {
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
