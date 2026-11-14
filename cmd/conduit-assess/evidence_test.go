package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidenceCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM example.invalid/app@sha256:placeholder\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := evidence([]string{dir, "Dockerfile"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "source-evidence-not-assessed") {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := evidence([]string{dir, "Dockerfile", "missing"}, &out); err == nil {
		t.Fatal("missing path accepted")
	}
	if out.Len() != 0 {
		t.Fatal("partial output on failure")
	}
	if err := evidence([]string{dir, "Dockerfile", "Dockerfile"}, &out); err == nil {
		t.Fatal("duplicate accepted")
	}
}
