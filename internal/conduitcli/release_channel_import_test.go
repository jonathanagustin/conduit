package conduitcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// runChannelCommand drives the real root command, so a command that exists in
// the tree but is not wired into `release-channel` fails here rather than
// passing against a hand-built parent.
func runChannelCommand(t *testing.T, stdout *bytes.Buffer, args ...string) error {
	t.Helper()
	root := NewRoot(testRuntime(stdout, &fakeService{}))
	root.SetArgs(append([]string{"--server", "http://127.0.0.1:8081", "--development-subject", "developer"}, args...))
	return root.Execute()
}

// export had no inverse. A Release Channel definition could be lifted out of
// one desired state and there was no supported way to put it into another,
// which cuts against the air-gap story the rest of the export/import surface
// supports: an operator moving a channel between Hubs hand-edited YAML.

func writeJSONFile(t *testing.T, dir, name string, value any) string {
	t.Helper()
	path := filepath.Join(dir, name)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func importFixture(t *testing.T, existing []controlplane.ReleaseChannelDefinition) (string, string) {
	t.Helper()
	dir := t.TempDir()
	desired := controlplane.DesiredState{
		APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment",
		Environment:     controlplane.Environment{ID: "prod", Generation: "1", DeclaredStateSource: controlplane.SourceEditOnThisHub},
		ReleaseChannels: existing,
	}
	definition := controlplane.ReleaseChannelDefinition{Name: "CANARY", Description: "imported from another Hub"}
	return writeJSONFile(t, dir, "state.json", desired), writeJSONFile(t, dir, "channel.json", definition)
}

func TestReleaseChannelImportProposesADefinitionFromAnotherDesiredState(t *testing.T) {
	t.Parallel()
	stateFile, definitionFile := importFixture(t, nil)
	stdout := new(bytes.Buffer)
	err := runChannelCommand(t, stdout, "release-channel", "import",
		"--state-file", stateFile, "--definition-file", definitionFile, "--dry-run")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(stdout.String(), "CANARY") {
		t.Fatalf("the imported channel is not in the proposed desired state: %s", stdout.String())
	}
}

// Refused rather than merged. An import that quietly replaced a channel would
// let a definition carried between Hubs drop label requirements the receiving
// Hub had added, and the operator would see a successful import either way.
func TestReleaseChannelImportRefusesToClobberWithoutOverwrite(t *testing.T) {
	t.Parallel()
	stateFile, definitionFile := importFixture(t, []controlplane.ReleaseChannelDefinition{
		{Name: "CANARY", Description: "the receiving Hub's own definition"},
	})
	err := runChannelCommand(t, new(bytes.Buffer), "release-channel", "import",
		"--state-file", stateFile, "--definition-file", definitionFile, "--dry-run")
	if err == nil {
		t.Fatal("an existing Release Channel was replaced without --overwrite")
	}
	if !strings.Contains(err.Error(), "--overwrite") {
		t.Errorf("the refusal does not say how to proceed deliberately: %v", err)
	}
}

func TestReleaseChannelImportReplacesWhenOverwriteIsStated(t *testing.T) {
	t.Parallel()
	stateFile, definitionFile := importFixture(t, []controlplane.ReleaseChannelDefinition{
		{Name: "CANARY", Description: "the receiving Hub's own definition"},
	})
	stdout := new(bytes.Buffer)
	err := runChannelCommand(t, stdout, "release-channel", "import",
		"--state-file", stateFile, "--definition-file", definitionFile, "--overwrite", "--dry-run")
	if err != nil {
		t.Fatalf("import --overwrite: %v", err)
	}
	if strings.Contains(stdout.String(), "the receiving Hub's own definition") {
		t.Fatalf("--overwrite did not replace the existing definition: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "imported from another Hub") {
		t.Fatalf("the imported definition did not land: %s", stdout.String())
	}
}

// A definition with no name cannot be placed, and appending it would produce a
// desired state whose validation failure names something the operator never
// wrote. Refused where the cause is still visible.
func TestReleaseChannelImportRefusesAnUnnamedDefinition(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stateFile := writeJSONFile(t, dir, "state.json", controlplane.DesiredState{
		APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment",
		Environment: controlplane.Environment{ID: "prod", Generation: "1", DeclaredStateSource: controlplane.SourceEditOnThisHub},
	})
	definitionFile := writeJSONFile(t, dir, "channel.json", controlplane.ReleaseChannelDefinition{Description: "no name"})
	err := runChannelCommand(t, new(bytes.Buffer), "release-channel", "import",
		"--state-file", stateFile, "--definition-file", definitionFile, "--dry-run")
	if err == nil || !strings.Contains(err.Error(), "no Release Channel name") {
		t.Fatalf("an unnamed definition was imported: %v", err)
	}
}

// The published 105-command contract carries no `import` and no `populate`;
// both are named only in Apollo's changelog. `import` is here because its
// meaning follows from `export`. `populate` is not, because it does not: the
// changelog line alone does not say whether it fills a channel with releases
// (which `add` already does), seeds one channel from another, or something
// else. This asserts the refusal so nobody adds a guess later without deciding
// to.
func TestReleaseChannelPopulateStaysUnimplementedWhileItsMeaningIsUnestablished(t *testing.T) {
	t.Parallel()
	// Asserted against the command tree rather than by running it: a parent
	// with no RunE prints help and returns nil for an unknown subcommand, so
	// executing "release-channel populate" cannot tell absence from success.
	root := NewRoot(testRuntime(new(bytes.Buffer), &fakeService{}))
	var channel *cobra.Command
	for _, candidate := range root.Commands() {
		if candidate.Name() == "release-channel" {
			channel = candidate
		}
	}
	if channel == nil {
		t.Fatal("the release-channel command is gone")
	}
	names := []string{}
	for _, sub := range channel.Commands() {
		names = append(names, sub.Name())
	}
	for _, name := range names {
		if name == "populate" {
			t.Fatalf("release-channel populate exists; if its contract is now established, "+
				"record the evidence and delete this test. Subcommands: %v", names)
		}
	}
	// import is the counterpart claim: it is here precisely because its meaning
	// follows from export, so the pair states the rule rather than one absence.
	if !slices.Contains(names, "import") {
		t.Fatalf("release-channel import is missing: %v", names)
	}
}

func TestReleaseChannelReleaseDiffFindsThePromotionWorklist(t *testing.T) {
	t.Parallel()
	desired := controlplane.DesiredState{Releases: []controlplane.Release{
		{Product: "app", Version: "1.0.0", Channels: []string{"STAGING", "PRODUCTION"}},
		{Product: "app", Version: "1.1.0", Channels: []string{"STAGING"}},
		{Product: "worker", Version: "2.0.0", Channels: []string{"STAGING"}},
		{Product: "legacy", Version: "0.9.0", Channels: []string{"PRODUCTION"}},
		{Product: "unrelated", Version: "0.1.0", Channels: []string{"DEV"}},
	}}
	onlySource, onlyTarget, both := releaseChannelReleaseDiff(&desired, "STAGING", "PRODUCTION")
	// The promotion worklist: on STAGING, not yet on PRODUCTION. Sorted by
	// product@version, and the DEV-only Release is on neither channel so it is
	// not part of the comparison at all.
	if !slices.Equal(onlySource, []string{"app@1.1.0", "worker@2.0.0"}) {
		t.Errorf("promotion worklist = %v", onlySource)
	}
	if !slices.Equal(onlyTarget, []string{"legacy@0.9.0"}) {
		t.Errorf("only-on-target = %v", onlyTarget)
	}
	if !slices.Equal(both, []string{"app@1.0.0"}) {
		t.Errorf("shared = %v", both)
	}
}
