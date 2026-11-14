package packagingskill

import (
	"strings"
	"testing"
)

func TestSourceIsTheSkillFile(t *testing.T) {
	if !strings.HasPrefix(Source, "---\nname: packaging\n") {
		t.Fatalf("source starts %q", Source[:min(80, len(Source))])
	}
	if !strings.Contains(Source, "memory limit must not exceed 1Gi") {
		t.Fatal("source omits the memory gate rule")
	}
	if !strings.Contains(Source, "application requirement is unsupported") {
		t.Fatal("source omits a gate reason")
	}
	if !strings.Contains(Source, "dockerfile: base image must be pinned by a sha256 digest") {
		t.Fatal("source omits a dockerfile gate reason")
	}
	if !strings.Contains(Source, "Prefer a multi-stage build") {
		t.Fatal("source omits the containerization section")
	}
	if got := SHA256(); len(got) != 64 {
		t.Fatalf("sha256 = %q", got)
	}
}
