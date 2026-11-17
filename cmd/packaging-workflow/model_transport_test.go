package main

import (
	"bytes"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	packagingmodelhttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

func envMap(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}
func clearModelConfiguration(t *testing.T) {
	t.Helper()
	for _, key := range []string{"CONDUIT_MODEL_CMD", "CONDUIT_ASSISTANT_MODEL_CMD", "CONDUIT_PLANNING_MODEL_CMD", "CONDUIT_PACKAGING_MODEL_CMD", "CONDUIT_APPGEN_MODEL_CMD", "CONDUIT_MODEL_BASE_URL", "CONDUIT_MODEL_NAME", "CONDUIT_MODEL_TIMEOUT", "CONDUIT_MODEL_API_KEY_FILE", "CONDUIT_MODEL_CA_FILE", "CONDUIT_AGENT_PROVIDER"} {
		t.Setenv(key, "")
	}
}
func TestWorkerRefusesEveryLocalModelCommand(t *testing.T) {
	clearModelConfiguration(t)
	for _, key := range []string{"CONDUIT_MODEL_CMD", "CONDUIT_ASSISTANT_MODEL_CMD", "CONDUIT_PLANNING_MODEL_CMD", "CONDUIT_PACKAGING_MODEL_CMD", "CONDUIT_APPGEN_MODEL_CMD"} {
		for _, base := range []string{"", "https://models.example/v1"} {
			get := envMap(map[string]string{key: "private-command --key sensitive-value", "CONDUIT_MODEL_BASE_URL": base, "CONDUIT_MODEL_NAME": "fixture"})
			if _, err := resolveModelTransport(get); !errors.Is(err, localcmd.ErrLocalCommandUnsupported) || strings.Contains(err.Error(), "sensitive-value") {
				t.Fatalf("%s: %v", key, err)
			}
		}
	}
	t.Setenv("CONDUIT_PLANNING_MODEL_CMD", "ignored-command")
	if _, err := planningActivities(t.TempDir(), nil, true); !errors.Is(err, localcmd.ErrLocalCommandUnsupported) {
		t.Fatal(err)
	}
	root := newRoot()
	root.SetArgs([]string{"agent-exec"})
	root.SetIn(refuseRead{})
	root.SetOut(&bytes.Buffer{})
	if err := root.Execute(); !errors.Is(err, localcmd.ErrLocalCommandUnsupported) {
		t.Fatal(err)
	}
}
func TestWorkerPreservesHTTPAndOfflineSelection(t *testing.T) {
	for _, get := range []func(string) string{envMap(nil), envMap(map[string]string{"CONDUIT_MODEL_NAME": "m"})} {
		transport, err := resolveModelTransport(get)
		if err != nil || transport.configured() {
			t.Fatalf("offline=%+v err=%v", transport, err)
		}
	}
	transport, err := resolveModelTransport(envMap(map[string]string{"CONDUIT_MODEL_BASE_URL": "https://model.example/v1", "CONDUIT_MODEL_NAME": "asked", "CONDUIT_MODEL_TIMEOUT": "90s"}))
	if err != nil || !transport.configured() {
		t.Fatalf("HTTP=%+v err=%v", transport, err)
	}
	if model, ok := transport.planner(nil).Model.(planner.HTTPModel); !ok || model.Identity().ConfiguredModel != "asked" {
		t.Fatalf("planner=%+v", model)
	}
	if _, ok := transport.packageWriter().(packagingmodelhttp.Adapter); !ok {
		t.Fatalf("writer=%T", transport.packageWriter())
	}
}

type refuseRead struct{}

func (refuseRead) Read([]byte) (int, error) { panic("refused provider read stdin") }
func TestAgentExecRefusesEveryDelegatedProviderBeforeReading(t *testing.T) {
	clearModelConfiguration(t)
	for _, provider := range []string{"claude", "codex", "gemini", "grok", "custom"} {
		t.Setenv("CONDUIT_PROVIDER_"+strings.ToUpper(provider)+"_CMD", "/bin/sh -c exit")
		root := newRoot()
		root.SetArgs([]string{"agent-exec", "--provider", provider})
		root.SetIn(refuseRead{})
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		if err := root.Execute(); !errors.Is(err, localcmd.ErrLocalCommandUnsupported) {
			t.Fatalf("%s: %v", provider, err)
		}
	}
}
func TestAgentExecDeterministicRemainsAnOfflineUtility(t *testing.T) {
	clearModelConfiguration(t)
	root := newRoot()
	root.SetArgs([]string{"agent-exec", "--provider", "deterministic"})
	root.SetIn(strings.NewReader("Inspect the bound source repository for this planning session."))
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "inspect_repo") {
		t.Fatalf("reply=%q", out.String())
	}
}

// The script self-test supplies a fake docker executable and starts no engine.
func TestGovernedPackagingScriptRequiresExplicitHTTPBeforeTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Bash entrypoint")
	}
	if out, err := exec.Command("bash", "../../scripts/governed-packaging-run.test.sh").CombinedOutput(); err != nil {
		t.Fatalf("guard regression: %v\n%s", err, out)
	}
}
