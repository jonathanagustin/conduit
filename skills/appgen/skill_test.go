package appgenskill

import (
	"strings"
	"testing"
)

func TestSourceIsTheSkillFile(t *testing.T) {
	if !strings.HasPrefix(Source, "---\nname: appgen\n") {
		t.Fatalf("source starts %q", Source[:min(80, len(Source))])
	}
	if !strings.Contains(Source, "none (no source repository — problem-statement intake)") {
		t.Fatal("source omits the no-repository resolvedCommit sentinel")
	}
	if !strings.Contains(Source, "main.go") || !strings.Contains(Source, "zarf.yaml") {
		t.Fatal("source omits generated or packaged file conventions")
	}
	if got := SHA256(); len(got) != 64 {
		t.Fatalf("sha256 = %q", got)
	}
	if SHA256From(Source+"# modified\n") == SHA256() {
		t.Fatal("modified skill source produced identical sha256")
	}
}
