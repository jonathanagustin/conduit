package conduitcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const cliGrypeReport = `{"matches":[{"vulnerability":{"id":"GHSA-local","severity":"High","cvss":[{"version":"3.1","metrics":{"baseScore":7.5}}]},"relatedVulnerabilities":[{"id":"CVE-2026-1234","namespace":"nvd:cpe","cvss":[{"version":"3.1","metrics":{"baseScore":4}}]}]}],"descriptor":{"name":"grype","version":"0.116.1"}}`

func installCLIGrype(t *testing.T, report string, exitCode int) (string, string) {
	t.Helper()
	directory := t.TempDir()
	args, input, payload := filepath.Join(directory, "args"), filepath.Join(directory, "input"), filepath.Join(directory, "report")
	if err := os.WriteFile(payload, []byte(report), 0o600); err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\000' \"$@\" > %s\ncat > %s\ncat %s\nexit %d\n", quote(args), quote(input), quote(payload), exitCode)
	if err := os.WriteFile(filepath.Join(directory, "grype"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	return args, input
}

func executeLocalGrype(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdin: strings.NewReader(input), Stdout: &output, Stderr: &bytes.Buffer{}, Getenv: func(string) string { return "" }, ServiceFactory: func(context.Context, ConnectionConfig) (Service, func(), error) {
		t.Error("local scan contacted Hub")
		return nil, func() {}, errors.New("unexpected Hub request")
	}})
	root.SetArgs(append([]string{"cve", "grype", "scan"}, args...))
	err := root.ExecuteContext(context.Background())
	return output.String(), err
}

func TestCLIGrypePassesLiteralSourceAndFlagsWithoutHubAndWritesJSONForYAML(t *testing.T) {
	argsFile, inputFile := installCLIGrype(t, cliGrypeReport, 0)
	file := filepath.Join(t.TempDir(), "scan.json")
	source := "dir:literal; this-is-not-a-shell-command"
	output, err := executeLocalGrype(t, "input bytes", source, "--output", "yaml", "--output-file", file, "--", "--only-fixed", "--scope", "all-layers")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output, "conduitNativeNormalization:") || strings.HasPrefix(output, "{") {
		t.Fatalf("not YAML: %s", output)
	}
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(contents, &report); err != nil {
		t.Fatal("file did not remain JSON:", err)
	}
	if report["matches"].([]any)[0].(map[string]any)["vulnerability"].(map[string]any)["id"] != "CVE-2026-1234" {
		t.Fatal("file was not normalized")
	}
	info, err := os.Stat(file)
	if err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("report is not private")
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{source, "--output", "json", "--only-fixed", "--scope", "all-layers"}
	if actual := strings.Split(strings.TrimSuffix(string(args), "\x00"), "\x00"); !reflect.DeepEqual(actual, want) {
		t.Fatalf("arguments=%q want=%q", actual, want)
	}
	input, err := os.ReadFile(inputFile)
	if err != nil || string(input) != "input bytes" {
		t.Fatal("stdin was not forwarded")
	}
}

func TestCLIGrypePrintsValidReportAndFailsOnProviderPolicyExit(t *testing.T) {
	installCLIGrype(t, cliGrypeReport, 2)
	output, err := executeLocalGrype(t, "", "-")
	if err == nil || !strings.Contains(err.Error(), "status 2") {
		t.Fatalf("exit=%v", err)
	}
	if !json.Valid([]byte(output)) {
		t.Fatalf("valid report lost on nonzero exit: %q", output)
	}
}

func TestCLIGrypeMalformedOutputDoesNotPrintOrOverwriteReport(t *testing.T) {
	installCLIGrype(t, "not a report", 1)
	file := filepath.Join(t.TempDir(), "existing.json")
	if err := os.WriteFile(file, []byte("keep original"), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := executeLocalGrype(t, "", "image", "--output-file", file)
	if err == nil || output != "" {
		t.Fatalf("malformed scan=%q,%v", output, err)
	}
	contents, err := os.ReadFile(file)
	if err != nil || string(contents) != "keep original" {
		t.Fatal("invalid report overwrote output")
	}
}

func TestCLIGrypeRejectsOutputOverridesAndExtraSourcesBeforeExecution(t *testing.T) {
	argsFile, _ := installCLIGrype(t, cliGrypeReport, 0)
	for _, flags := range [][]string{{"--output", "yaml"}, {"--output=yaml"}, {"-oyaml"}, {"-o", "yaml"}, {"--file", "other"}, {"--file=other"}, {"-fother"}, {"-f", "other"}} {
		arguments := append([]string{"image", "--"}, flags...)
		output, err := executeLocalGrype(t, "", arguments...)
		if err == nil || output != "" {
			t.Errorf("accepted override %q", flags)
		}
	}
	for _, arguments := range [][]string{{}, {"image", "other"}, {"--", "image"}, {"image", "--output", "csv"}} {
		if _, err := executeLocalGrype(t, "", arguments...); err == nil {
			t.Errorf("accepted invalid arguments %q", arguments)
		}
	}
	if _, err := os.Stat(argsFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid arguments invoked Grype")
	}
}
