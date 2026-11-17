package localcmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestParseArgvDoesNotUseAShell(t *testing.T) {
	argv, err := ParseArgv(`tool exec --flag "quoted value" -`)
	if err != nil || len(argv) != 5 || argv[3] != "quoted value" {
		t.Fatalf("argv = %#v %v", argv, err)
	}
	if _, err := ParseArgv(`tool $(echo hi)`); err == nil {
		t.Fatal("accepted shell syntax")
	}
	if _, err := ParseArgv(`tool "unterminated`); err == nil {
		t.Fatal("accepted an unclosed quote")
	}
}

func TestExecuteTimeoutAndMissingCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "sleep")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), []string{script}, 200*time.Millisecond, []byte("prompt"))
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout = %v", err)
	}
	_, err = Execute(context.Background(), []string{filepath.Join(dir, "missing")}, time.Second, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	if _, err := Execute(context.Background(), nil, time.Second, nil); !errors.Is(err, ErrEmpty) {
		t.Fatalf("empty = %v", err)
	}
}

// A command that exists and cannot be started is told apart from one that
// ran and failed: only the first is safe to run again.
func TestAStartFailureIsToldApartFromANonZeroExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("script fixture is a shell")
	}
	dir := t.TempDir()
	notAProgram := filepath.Join(dir, "bad-format")
	if err := os.WriteFile(notAProgram, []byte("\x7fnot a program\x00\x01"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(context.Background(), []string{notAProgram}, time.Second, nil); !errors.Is(err, ErrFailed) || !errors.Is(err, ErrStartFailed) {
		t.Fatalf("a file that is not a program returned %v", err)
	}
	failing := filepath.Join(dir, "failing")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\nexit 3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := Execute(context.Background(), []string{failing}, time.Second, nil)
	if !errors.Is(err, ErrFailed) || errors.Is(err, ErrStartFailed) {
		t.Fatalf("a non-zero exit returned %v", err)
	}
	var wrapped ExecFunc = func(context.Context, []string, time.Duration, []byte) ([]byte, error) { return []byte("wrapped"), nil }
	if out, _ := wrapped.Run(context.Background(), []string{failing}, time.Second, nil); string(out) != "wrapped" {
		t.Fatalf("a wrapper was not used: %q", out)
	}
	if _, err := ExecFunc(nil).Run(context.Background(), []string{failing}, time.Second, nil); !errors.Is(err, ErrFailed) {
		t.Fatalf("a nil wrapper did not run the command: %v", err)
	}
}
