package conduitcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"go.yaml.in/yaml/v4"
)

const minimalDesiredState = `apiVersion: platform.delivery/v1alpha1
kind: EnvironmentDeployment
environment:
  id: prod
  generation: 0123456789abcdef
releases: []
entities: []
`

// The contract test proves these commands EXIST. That is a weak claim on its
// own: a command that exists and does nothing satisfies it. These prove the two
// things that actually matter -- that the answered ones answer, and that the
// refused ones refuse for the stated reason rather than by accident.

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs(args)
	root.SilenceUsage, root.SilenceErrors = true, true
	err := root.Execute()
	return output.String(), err
}

// --- answered ---------------------------------------------------------------

// The settings document is the vocabulary CEL diff paths are written against,
// so a projection that dropped a field would make an upstream policy match
// nothing. This drives the real converter on a populated state rather than
// asserting a shape, and it is a unit test rather than a command test because
// the desired-state validator is not what is under examination here.
func TestSettingsProjectionCarriesTheEntityAndItsPublishedKeys(t *testing.T) {
	t.Parallel()
	settings := controlplane.ApolloEnvironmentSettingsFromDesiredState(controlplane.DesiredState{
		Environment: controlplane.Environment{ID: "prod"},
		Entities: []controlplane.Entity{
			{Name: "checkout", Product: "com.example:checkout", Version: "1.4.2", Namespace: "shop"},
		},
	})
	encoded, err := controlplane.EncodeApolloEnvironmentSettings(settings)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// `k8s-namespace` is the name the settings document uses and the one a CEL
	// path references. Emitting Conduit's internal spelling would look correct
	// here and match nothing upstream, which is the whole failure this
	// conversion exists to prevent.
	for _, want := range []string{"checkout", "k8s-namespace", "shop"} {
		if !strings.Contains(encoded, want) {
			t.Errorf("projected settings omit %q:\n%s", want, encoded)
		}
	}
}

// And the command is wired to that projection rather than to something else.
func TestEnvironmentSettingGetEmitsTheProjection(t *testing.T) {
	t.Parallel()
	desired := filepath.Join(t.TempDir(), "desired.yaml")
	if err := os.WriteFile(desired, []byte(minimalDesiredState), 0o600); err != nil {
		t.Fatal(err)
	}
	output, err := runCLI(t, "environment", "setting", "get", "--file", desired)
	if err != nil {
		t.Fatalf("setting get: %v\n%s", err, output)
	}
	// Compared against the projection itself rather than against a literal, so
	// this stays a wiring test: it fails if the command stops calling the
	// converter, and does not fail every time the converter's output changes.
	var state controlplane.DesiredState
	if err := yaml.Unmarshal([]byte(minimalDesiredState), &state); err != nil {
		t.Fatal(err)
	}
	want, err := controlplane.EncodeApolloEnvironmentSettings(
		controlplane.ApolloEnvironmentSettingsFromDesiredState(state),
	)
	if err != nil {
		t.Fatal(err)
	}
	if output != want {
		t.Errorf("command output is not the projection:\n got %q\nwant %q", output, want)
	}
}

func TestEnvironmentSettingEditRequiresExactlyOneSourceOfSettings(t *testing.T) {
	t.Parallel()
	desired := filepath.Join(t.TempDir(), "desired.yaml")
	if err := os.WriteFile(desired, []byte(minimalDesiredState), 0o600); err != nil {
		t.Fatal(err)
	}
	// Neither. Defaulting to an editor is what apollo-cli does and what this
	// deliberately does not, so "neither" has to be an error rather than a mode.
	if _, err := runCLI(t, "environment", "setting", "edit", "--file", desired); err == nil {
		t.Error("edit accepted no settings document at all")
	}
	// Both. Silently preferring one would make the other argument a no-op that
	// an operator believes took effect.
	_, err := runCLI(t, "environment", "setting", "edit", "--file", desired,
		"--content", "managed-helm-charts: []", "--settings-file", desired)
	if err == nil {
		t.Error("edit accepted two conflicting settings documents")
	}
}

// An Apollo entity ID names one Entity in one Environment. Resolving it by
// scanning for a matching name would answer for the wrong Entity as soon as two
// Environments run one with the same name, which is the ordinary case.
func TestApolloEntityIDIsParsedRatherThanSearchedFor(t *testing.T) {
	t.Parallel()
	environment, name, err := parseApolloEntityID("aeid:prod:helm-chart:checkout")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if environment != "prod" || name != "checkout" {
		t.Fatalf("parsed environment=%q name=%q", environment, name)
	}
	for _, malformed := range []string{
		"checkout",                   // bare name
		"aeid:prod:checkout",         // missing the type segment
		"aeid:prod:service:checkout", // a type this form does not carry
		"aeid::helm-chart:checkout",  // empty Environment
		"aeid:prod:helm-chart:",      // empty Entity
		"aeid:prod:helm-chart:a:b",   // an extra segment
	} {
		if _, _, err := parseApolloEntityID(malformed); err == nil {
			t.Errorf("%q was accepted as an Apollo entity ID", malformed)
		}
	}
}

// --- refused ----------------------------------------------------------------

// Each refusal must name what is missing. A generic "not supported" would leave
// an operator unable to tell a deliberate boundary from an unimplemented
// feature, which is the distinction these messages exist to carry.
func TestUnbackedCommandsRefuseAndSayWhy(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		args []string
		want string
	}{
		{[]string{"cve", "list"}, "due-date model"},
		{[]string{"target-hub", "list"}, "no set to enumerate"},
		{[]string{"target-hub", "get", "edge"}, "no set to enumerate"},
		{[]string{"target-hub", "pipeline", "list", "edge"}, "no set to enumerate"},
		{[]string{"terminal", "grant", "list"}, "break-glass"},
		{[]string{"terminal", "policy", "list"}, "break-glass"},
	} {
		name := strings.Join(testCase.args, " ")
		output, err := runCLI(t, testCase.args...)
		if err == nil {
			t.Errorf("%s did not refuse:\n%s", name, output)
			continue
		}
		if !strings.Contains(err.Error(), testCase.want) {
			t.Errorf("%s refused without naming %q: %v", name, testCase.want, err)
		}
	}
}

// The refusals must not need a configured Hub. An operator discovering what a
// command does should not have to authenticate first to be told Conduit will
// never answer it.
func TestRefusalsDoNotRequireHubConfiguration(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"cve", "list"}, {"target-hub", "list"}, {"terminal", "grant", "list"}} {
		_, err := runCLI(t, args...)
		if err == nil {
			t.Fatalf("%v did not refuse", args)
		}
		for _, leaked := range []string{"hub", "token", "profile", "config"} {
			if strings.Contains(strings.ToLower(err.Error()), leaked+" is required") {
				t.Errorf("%v refused for a configuration reason instead of its own: %v", args, err)
			}
		}
	}
}
