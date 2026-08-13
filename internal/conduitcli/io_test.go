package conduitcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestDecodeDocumentUsesJSONFieldNamesForYAMLAndRejectsUnknownFields(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	valid := filepath.Join(directory, "team.yaml")
	if err := os.WriteFile(valid, []byte("displayName: Release reviewers\nadminGroups:\n  - platform-admins\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var team controlplane.Team
	if err := decodeDocument(valid, &team); err != nil {
		t.Fatal(err)
	}
	if team.DisplayName != "Release reviewers" || len(team.AdminGroups) != 1 {
		t.Fatalf("team=%#v", team)
	}
	invalid := filepath.Join(directory, "invalid.yaml")
	if err := os.WriteFile(invalid, []byte("displayName: Reviewers\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := decodeDocument(invalid, &team); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteOutputSupportsStableAutomationFormats(t *testing.T) {
	t.Parallel()
	value := map[string]any{"status": "PENDING", "revision": 2}
	var jsonOutput bytes.Buffer
	if err := writeOutput(&jsonOutput, "json", value); err != nil {
		t.Fatal(err)
	}
	if jsonOutput.String() != "{\n  \"revision\": 2,\n  \"status\": \"PENDING\"\n}\n" {
		t.Fatalf("json=%q", jsonOutput.String())
	}
	var yamlOutput bytes.Buffer
	if err := writeOutput(&yamlOutput, "yaml", value); err != nil {
		t.Fatal(err)
	}
	if yamlOutput.String() != "revision: 2\nstatus: PENDING\n" {
		t.Fatalf("yaml=%q", yamlOutput.String())
	}
}

// decodeDocument bounds what it will read before it parses. The reader is
// given maxDocumentBytes+1 so the check can tell "exactly the limit" from
// "past it"; if the reader took the limit exactly, an oversized document would
// arrive truncated and parse as a valid, smaller document.
func TestDecodeDocumentBoundsWhatItReads(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()

	// YAML whose size is controlled by a padded comment, so the document stays
	// parseable at any length.
	document := func(size int) []byte {
		body := []byte("displayName: Reviewers\n#")
		return append(body, append(bytes.Repeat([]byte("x"), size-len(body)-1), '\n')...)
	}

	for name, test := range map[string]struct {
		size      int
		wantError string
	}{
		"exactly the bound": {size: maxDocumentBytes},
		"one byte over":     {size: maxDocumentBytes + 1, wantError: "exceeds"},
	} {
		path := filepath.Join(directory, name+".yaml")
		if err := os.WriteFile(path, document(test.size), 0o600); err != nil {
			t.Fatal(err)
		}
		var team controlplane.Team
		err := decodeDocument(path, &team)
		if test.wantError == "" {
			if err != nil {
				t.Errorf("%s (%d bytes) was refused: %v", name, test.size, err)
			} else if team.DisplayName != "Reviewers" {
				t.Errorf("%s decoded to %#v", name, team)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s (%d bytes) was accepted", name, test.size)
		} else if !strings.Contains(err.Error(), test.wantError) {
			t.Errorf("%s: error %q does not mention %q", name, err, test.wantError)
		}
	}
}
