package main

import (
	"bytes"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/localcmd"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	packagingmodelhttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

func envMap(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestWorkerRefusesUnsupportedModelPolicies(t *testing.T) {
	policies := []struct {
		name   string
		values map[string]string
	}{
		{"call budget", map[string]string{modelgovernor.EnvDailyCallBudget: "123457"}},
		{"token budget", map[string]string{modelgovernor.EnvDailyTokenBudget: "987653"}},
		{"transcripts", map[string]string{modelgovernor.EnvTranscripts: "on", modelgovernor.EnvTranscriptRetention: "37h"}},
		{"combined", map[string]string{modelgovernor.EnvDailyCallBudget: "123457", modelgovernor.EnvDailyTokenBudget: "987653", modelgovernor.EnvTranscripts: "on", modelgovernor.EnvTranscriptRetention: "37h"}},
	}
	for _, policy := range policies {
		for _, endpoint := range []string{"", "https://model.example/v1"} {
			t.Run(policy.name+"/"+endpoint, func(t *testing.T) {
				values := map[string]string{"CONDUIT_MODEL_BASE_URL": endpoint, "CONDUIT_MODEL_NAME": "fixture"}
				for key, value := range policy.values {
					values[key] = value
				}
				transport, err := resolveModelTransport(envMap(values))
				if !errors.Is(err, modelgovernor.ErrInvalidConfig) || transport.configured() || transport.governor != nil {
					t.Fatalf("unsupported policy returned usable transport: configured=%v governor=%v err=%v", transport.configured(), transport.governor != nil, err)
				}
				if !strings.Contains(err.Error(), "standalone packaging worker cannot enforce") {
					t.Fatalf("missing actionable worker refusal: %v", err)
				}
				for _, privateValue := range []string{"123457", "987653", "37h"} {
					if strings.Contains(err.Error(), privateValue) {
						t.Fatalf("refusal exposed configuration value: %v", err)
					}
				}
			})
		}
	}
}

func TestWorkerAcceptsSupportedModelPolicies(t *testing.T) {
	for _, values := range []map[string]string{
		nil,
		{modelgovernor.EnvGlobalConcurrency: "2", modelgovernor.EnvOwnerConcurrency: "1", modelgovernor.EnvMaxAttempts: "3", modelgovernor.EnvQueueWait: "25ms", modelgovernor.EnvTranscripts: "off", modelgovernor.EnvTranscriptRetention: "37h"},
	} {
		governor, err := workerGovernor(envMap(values))
		if err != nil || governor == nil {
			t.Fatalf("supported worker configuration refused: %v", err)
		}
	}
	if governor, err := workerGovernor(envMap(map[string]string{modelgovernor.EnvGlobalConcurrency: "invalid-sensitive-value"})); !errors.Is(err, modelgovernor.ErrInvalidConfig) || governor != nil {
		t.Fatalf("invalid supported configuration accepted: %v", err)
	}
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
		t.Setenv("CONDUIT_AGENT_PROVIDER", provider)
		for _, args := range [][]string{{"agent-exec"}, {"agent-exec", "--provider", provider}} {
			root := newRoot()
			root.SetArgs(args)
			root.SetIn(refuseRead{})
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&out)
			if err := root.Execute(); !errors.Is(err, localcmd.ErrLocalCommandUnsupported) {
				t.Fatalf("%s: %v", provider, err)
			}
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
