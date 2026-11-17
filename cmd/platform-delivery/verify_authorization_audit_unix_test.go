//go:build unix

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestAuditArchiveRefusesFIFOWithoutWaitingForAWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sensitive-fifo-path")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		var output bytes.Buffer
		done <- verifyAuthorizationAudit(t.Context(), func(string) string { return "" }, []string{"--bootstrap-checkpoint", "--archive-file", path, "--timeout=20ms"}, &output)
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "regular file") || strings.Contains(err.Error(), path) {
			t.Fatalf("FIFO rejection=%v", err)
		}
	case <-time.After(time.Second):
		// Release a regressed blocking Open before failing the assertion.
		file, _ := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if file != nil {
			_ = file.Close()
		}
		t.Fatal("archive verification waited for a FIFO writer")
	}
}
