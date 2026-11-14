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
