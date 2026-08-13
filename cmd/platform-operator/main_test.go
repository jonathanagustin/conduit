package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// decodeFile is how every operator input document is read. It is deliberately
// stricter than json.Unmarshal in three ways, none of which a passing decode
// would reveal.
func TestDecodeFileIsStrictAboutWhatItAccepts(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, contents string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "input.json")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	type document struct {
		Name string `json:"name"`
	}

	t.Run("decodes a single value", func(t *testing.T) {
		t.Parallel()
		var got document
		if err := decodeFile(write(t, `{"name":"platform"}`), &got); err != nil {
			t.Fatal(err)
		}
		if got.Name != "platform" {
			t.Errorf("decoded %#v", got)
		}
	})

	t.Run("refuses a field the document does not define", func(t *testing.T) {
		t.Parallel()
		// An operator sending a misspelled field would otherwise get a
		// successful call that silently did not carry the value they set.
		var got document
		if err := decodeFile(write(t, `{"name":"platform","naem":"typo"}`), &got); err == nil {
			t.Error("an unknown field was accepted")
		}
	})

	t.Run("refuses a second value in the same file", func(t *testing.T) {
		t.Parallel()
		// Two JSON values concatenated is a file where only the first is acted
		// on. Whatever is in the second is silently discarded.
		var got document
		for name, contents := range map[string]string{
			"two objects":      `{"name":"a"}{"name":"b"}`,
			"object then junk": `{"name":"a"} tail`,
		} {
			if err := decodeFile(write(t, contents), &got); err == nil {
				t.Errorf("%s was accepted", name)
			}
		}
	})

	t.Run("will not read an unbounded file", func(t *testing.T) {
		t.Parallel()
		// The bound is on the reader, so an oversized document fails as a
		// decode error rather than being read into memory in full.
		oversized := `{"name":"` + strings.Repeat("x", maxInputBytes) + `"}`
		var got document
		if err := decodeFile(write(t, oversized), &got); err == nil {
			t.Error("a document past the input bound was accepted")
		}
	})

	t.Run("names what it could not do", func(t *testing.T) {
		t.Parallel()
		var got document
		err := decodeFile(filepath.Join(t.TempDir(), "absent.json"), &got)
		if err == nil || !strings.Contains(err.Error(), "open input file") {
			t.Errorf("error = %v", err)
		}
		if err := decodeFile(write(t, `{`), &got); err == nil ||
			!strings.Contains(err.Error(), "decode input file") {
			t.Errorf("a truncated document gave %v", err)
		}
	})
}

// The usage string is the only discovery mechanism this binary has. A command
// added to the dispatch but not to usage exists and cannot be found.
func TestUsageListsEveryDispatchedCommand(t *testing.T) {
	t.Parallel()
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	dispatched := regexp.MustCompile(`(?m)^\tcase "([a-z-]+)":`).FindAllStringSubmatch(string(source), -1)
	if len(dispatched) == 0 {
		t.Fatal("found no dispatched commands; the switch has changed shape")
	}
	usage := usageError().Error()
	for _, match := range dispatched {
		if !strings.Contains(usage, match[1]) {
			t.Errorf("%q is dispatched but absent from the usage string", match[1])
		}
		if _, known := operatorCommands[match[1]]; !known {
			t.Errorf("%q is dispatched but absent from the offline command set", match[1])
		}
	}
	// And the reverse: a command removed from the dispatch but left in usage
	// is one an operator will try and be told is unknown.
	for _, listed := range regexp.MustCompile(`[a-z]+(?:-[a-z]+)+`).FindAllString(usage, -1) {
		if listed == "platform-operator" {
			continue
		}
		found := false
		for _, match := range dispatched {
			if match[1] == listed {
				found = true
			}
		}
		if !found {
			t.Errorf("usage lists %q but nothing dispatches it", listed)
		}
	}
	if len(operatorCommands) != len(dispatched) {
		t.Errorf("offline command set has %d entries, dispatch has %d", len(operatorCommands), len(dispatched))
	}
}

// Not parallel: these set environment variables, which are process-global.
func TestRunRefusesBeforeReachingTheNetwork(t *testing.T) {
	t.Setenv("CONTROL_PLANE_URL", "")
	t.Setenv("SPIFFE_ENDPOINT_SOCKET", "")
	t.Setenv("CONTROL_PLANE_SPIFFE_ID", "")

	t.Run("no arguments is a usage error", func(t *testing.T) {
		var output bytes.Buffer
		err := run(context.Background(), nil, &output)
		if err == nil || !strings.Contains(err.Error(), "usage:") {
			t.Errorf("error = %v", err)
		}
	})

	for _, command := range []string{"--help", "-h", "help"} {
		t.Run(command+" is offline", func(t *testing.T) {
			var output bytes.Buffer
			if err := run(context.Background(), []string{command}, &output); err != nil {
				t.Fatal(err)
			}
			if output.String() != usage+"\n" {
				t.Errorf("output = %q", output.String())
			}
		})
	}

	t.Run("an unknown command is rejected offline", func(t *testing.T) {
		var output bytes.Buffer
		err := run(context.Background(), []string{"unknown"}, &output)
		if err == nil || err.Error() != usage {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("a plain-http control plane is refused", func(t *testing.T) {
		// The operator's whole authentication story is the SPIFFE-verified TLS
		// session. Over http there is nothing to verify the Hub with, so this
		// has to fail before any request is made rather than fall back.
		t.Setenv("CONTROL_PLANE_URL", "http://hub.example")
		var output bytes.Buffer
		err := run(context.Background(), []string{"get-plan", "pln_1"}, &output)
		if err == nil || !strings.Contains(err.Error(), "https") {
			t.Errorf("error = %v", err)
		}
	})

	t.Run("a missing required variable names itself", func(t *testing.T) {
		t.Setenv("CONTROL_PLANE_URL", "")
		var output bytes.Buffer
		err := run(context.Background(), []string{"get-plan", "pln_1"}, &output)
		if err == nil || !strings.Contains(err.Error(), "CONTROL_PLANE_URL") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestRequiredEnvironmentTreatsBlankAsMissing(t *testing.T) {
	t.Setenv("PLATFORM_TEST_VARIABLE", "   ")
	if _, err := requiredEnvironment("PLATFORM_TEST_VARIABLE"); err == nil {
		t.Error("a whitespace-only value was accepted as set")
	}
	t.Setenv("PLATFORM_TEST_VARIABLE", "  value  ")
	value, err := requiredEnvironment("PLATFORM_TEST_VARIABLE")
	if err != nil || value != "value" {
		t.Errorf("got (%q, %v), want (\"value\", nil)", value, err)
	}
}
