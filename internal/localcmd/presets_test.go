package localcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnlyThePresetWithARecordedRunIsVerified(t *testing.T) {
	want := map[string]bool{"deterministic": true}
	presets := Presets()
	if len(presets) != len(want) {
		t.Fatalf("presets = %d, want %d", len(presets), len(want))
	}
	for _, preset := range presets {
		verified, known := want[preset.Name]
		if !known {
			t.Fatalf("unexpected preset %q", preset.Name)
		}
		// Marking a preset verified needs a recorded run and a change here.
		if preset.Verified != verified {
			t.Fatalf("%s: verified = %t, want %t", preset.Name, preset.Verified, verified)
		}
		if preset.Evidence == "" {
			t.Fatalf("%s: no evidence text", preset.Name)
		}
		if wantStatus := map[bool]string{true: "VERIFIED", false: "UNVERIFIED"}[verified]; preset.Status() != wantStatus {
			t.Fatalf("%s: status = %q", preset.Name, preset.Status())
		}
		if (len(preset.Argv) == 0) != (preset.Name == DeterministicPreset) {
			t.Fatalf("%s: argv = %v", preset.Name, preset.Argv)
		}
	}
}

func TestPresetFromArgv(t *testing.T) {
	cases := []struct {
		argv     []string
		env      string
		wantName string
		wantOK   bool
	}{
		{[]string{"packaging-workflow", "agent-exec"}, "", "deterministic", true},
		{[]string{"packaging-workflow", "agent-exec"}, "claude", "", false},
		{[]string{"packaging-workflow", "agent-exec", "--provider", "grok"}, "claude", "", false},
		{[]string{"/opt/bin/packaging-workflow", "agent-exec", "--provider=gemini"}, "", "", false},
		{[]string{"packaging-workflow", "agent-exec", "--provider", "bogus"}, "", "", false},
		{[]string{"codex", "exec", "-"}, "claude", "", false},
		{nil, "", "", false},
	}
	for _, c := range cases {
		preset, ok := PresetFromArgv(c.argv, c.env)
		if ok != c.wantOK || preset.Name != c.wantName {
			t.Errorf("PresetFromArgv(%v, %q) = %q %t, want %q %t", c.argv, c.env, preset.Name, ok, c.wantName, c.wantOK)
		}
	}
}

// The operator document carries the same table. A status changed in one
// place and not the other fails here.
func TestPresetDocumentMatchesTheTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "operations", "local-model-cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, preset := range Presets() {
		row := ""
		for _, line := range strings.Split(doc, "\n") {
			if strings.HasPrefix(line, "| `"+preset.Name+"`") && strings.Contains(line, "VERIFIED") {
				row = line
				break
			}
		}
		if row == "" {
			t.Fatalf("no status row for %s in local-model-cli.md", preset.Name)
		}
		cells := strings.Split(row, "|")
		status := ""
		for _, cell := range cells {
			if cell = strings.TrimSpace(cell); cell == "VERIFIED" || cell == "UNVERIFIED" {
				status = cell
			}
		}
		if status != preset.Status() {
			t.Fatalf("%s: document says %q, table says %q", preset.Name, status, preset.Status())
		}
	}
}
