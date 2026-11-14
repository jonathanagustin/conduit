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

// `pretty` was advertised in --help as a format and implemented as a second
// name for `yaml`, so an operator who asked for a human-oriented rendering got
// the machine one and nothing said so. An unknown name is now refused by the
// branch that refuses any other one, and the refusal names the formats that do
// exist -- json, yaml, csv, and table -- so the operator can pick.
func TestWriteOutputRefusesAFormatConduitDoesNotHave(t *testing.T) {
	t.Parallel()
	value := map[string]any{"status": "PENDING"}
	for _, format := range []string{"pretty", "PRETTY", "text", ""} {
		var output bytes.Buffer
		err := writeOutput(&output, format, value)
		if err == nil {
			t.Errorf("format %q was accepted and rendered %q", format, output.String())
			continue
		}
		if !strings.Contains(err.Error(), "use json, yaml, csv, or table") {
			t.Errorf("format %q error = %v, want the formats that do exist", format, err)
		}
		// Nothing partial reaches the caller's writer: a rejected format that
		// had already written half a document would leave a pipeline parsing
		// it.
		if output.Len() != 0 {
			t.Errorf("format %q was refused after writing %q", format, output.String())
		}
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

type stubCSVMarshaler struct{ rows [][]string }

func (stub stubCSVMarshaler) MarshalCSV() ([][]string, error) { return stub.rows, nil }

// csv is offered only for a value that can name its own columns. A plain map has
// no column order, so csv must refuse it rather than serialize an invented one.
func TestWriteOutputRendersCSVOnlyForMarshalers(t *testing.T) {
	t.Parallel()

	var table bytes.Buffer
	if err := writeOutput(&table, "csv", stubCSVMarshaler{rows: [][]string{{"a", "b"}, {"1", "2"}}}); err != nil {
		t.Fatal(err)
	}
	if table.String() != "a,b\n1,2\n" {
		t.Fatalf("csv=%q", table.String())
	}

	var refused bytes.Buffer
	err := writeOutput(&refused, "csv", map[string]any{"status": "PENDING"})
	if err == nil {
		t.Fatalf("csv rendered a plain map as %q", refused.String())
	}
	if refused.Len() != 0 {
		t.Errorf("csv wrote %q before refusing", refused.String())
	}
}
