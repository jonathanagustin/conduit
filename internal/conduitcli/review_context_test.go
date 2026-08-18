package conduitcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// A Plan proposal carries a DesiredState and nothing else, so there is no field
// a compliance ticket or an uninstallation rationale can travel in. Four
// commands nevertheless refused to run without one and printed that it was
// "retained in review context" (#175); the value was then dropped on the floor.
// These pin the resolution: the flags are gone rather than accepted and
// discarded, and the commands they gated still propose their Plan.
const reviewContextDesiredState = `apiVersion: platform.delivery/v1alpha1
kind: EnvironmentDeployment
environment:
  id: prod
  generation: 0123456789abcdef
releases: []
entities: []
releaseChannels:
  - name: BETA
`

func TestReviewContextFlagsAreAbsentRatherThanDemandedAndDiscarded(t *testing.T) {
	t.Parallel()
	document := filepath.Join(t.TempDir(), "desired.yaml")
	if err := os.WriteFile(document, []byte(reviewContextDesiredState), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name string
		// args is the invocation an operator writes now that the flag is gone.
		args []string
		// retired is the flag and value the command used to demand. Passing it
		// has to fail: a flag that is still accepted is one an operator believes
		// took effect.
		retired []string
	}{
		{
			name:    "environment delete",
			args:    []string{"environment", "delete", "prod", "--file", document},
			retired: []string{"--rationale", "decommission per CAB-99"},
		},
		{
			name:    "environment artifact-registry assign",
			args:    []string{"environment", "artifact-registry", "assign", "prod", "primary", "--file", document},
			retired: []string{"--compliance-ticket", "TICKET-42"},
		},
		{
			name:    "environment artifact-registry unassign",
			args:    []string{"environment", "artifact-registry", "unassign", "prod", "primary", "--file", document},
			retired: []string{"--compliance-ticket", "TICKET-42"},
		},
		{
			name:    "environment release-channel set",
			args:    []string{"environment", "release-channel", "set", "prod", "-r", "BETA", "--file", document},
			retired: []string{"--compliance-ticket", "TICKET-42"},
		},
		{
			name:    "release-channel update --archive",
			args:    []string{"release-channel", "update", "BETA", "--state-file", document, "--archive"},
			retired: []string{"--archive-rationale", "superseded by GA"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := &fakeService{}
			output, err := runProposingCLI(t, service, testCase.args...)
			if err != nil {
				t.Fatalf("command refused a complete invocation: %v\n%s", err, output)
			}
			// Verified by output rather than by exit status: the command has to
			// have reached ProposePlan, not merely returned zero.
			if service.proposed.Environment.ID != "prod" {
				t.Fatalf("no Plan was proposed; captured state is %#v\n%s", service.proposed, output)
			}
			rejected, err := runProposingCLI(t, &fakeService{}, append(append([]string{}, testCase.args...), testCase.retired...)...)
			if err == nil {
				t.Fatalf("%s is still accepted, so its value is still discarded:\n%s", testCase.retired[0], rejected)
			}
			if !strings.Contains(err.Error(), "unknown flag") {
				t.Fatalf("%s failed for some other reason than being absent: %v", testCase.retired[0], err)
			}
		})
	}
}

// The archive case is worth reading back: --archive-rationale was required
// *with* --archive, so dropping it must not have taken the archival with it.
func TestReleaseChannelArchiveStillProposesTheArchivalWithoutARationale(t *testing.T) {
	t.Parallel()
	document := filepath.Join(t.TempDir(), "desired.yaml")
	if err := os.WriteFile(document, []byte(reviewContextDesiredState), 0o600); err != nil {
		t.Fatal(err)
	}
	service := &fakeService{}
	output, err := runProposingCLI(t, service, "release-channel", "update", "BETA", "--state-file", document, "--archive")
	if err != nil {
		t.Fatalf("archive: %v\n%s", err, output)
	}
	if len(service.proposed.ReleaseChannels) != 1 || !service.proposed.ReleaseChannels[0].Archived {
		t.Fatalf("proposed channels do not record the archival: %#v", service.proposed.ReleaseChannels)
	}
}

// A guard rather than a case: nothing in the command tree may promise that a
// flag's value is kept in review context, because no CLI call carries such a
// field. If one is ever added -- it would have to live on the Plan proposal in
// internal/controlplane -- narrow this to the flags that actually reach it
// instead of deleting it.
func TestNoFlagPromisesRetentionInReviewContext(t *testing.T) {
	t.Parallel()
	root := NewRoot(Runtime{Getenv: func(string) string { return "" }})
	var visit func(*cobra.Command)
	visit = func(command *cobra.Command) {
		usages := command.LocalFlags().FlagUsages()
		for _, promise := range []string{"review context", "retained in review", "recorded in review"} {
			if strings.Contains(strings.ToLower(usages), promise) {
				t.Errorf("%s declares a flag claiming %q, but no CLI call carries it:\n%s",
					command.CommandPath(), promise, usages)
			}
		}
		for _, child := range command.Commands() {
			visit(child)
		}
	}
	visit(root)
}

// runProposingCLI drives the real root command against a Service that records
// what it was handed, which is the only place the transmitted payload is
// visible from a command test.
func runProposingCLI(t *testing.T, service Service, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs(append([]string{"--server", "http://127.0.0.1:8081", "--development-subject", "developer"}, args...))
	root.SilenceUsage, root.SilenceErrors = true, true
	// Executed on its own line: returning `output.String(), root.Execute()`
	// evaluates the buffer before the command has written anything to it.
	err := root.Execute()
	return output.String(), err
}
