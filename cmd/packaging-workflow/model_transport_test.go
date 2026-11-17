package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/modelhttp"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelcli"
	packagingmodelhttp "github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters/modelhttp"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
)

func envMap(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// captureStderr returns what run wrote to os.Stderr.
func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = writer
	defer func() { os.Stderr = saved }()
	run()
	_ = writer.Close()
	raw, _ := io.ReadAll(reader)
	return string(raw)
}

func TestWorkerRefusesBothModelTransports(t *testing.T) {
	both := map[string]string{
		"CONDUIT_MODEL_CMD": "codex exec -", "CONDUIT_MODEL_BASE_URL": "https://model.example/v1", "CONDUIT_MODEL_NAME": "m",
	}
	if _, err := resolveModelTransport(envMap(both)); !errors.Is(err, modelhttp.ErrBothConfigured) {
		t.Fatalf("resolve: %v", err)
	}
	for name, value := range both {
		t.Setenv(name, value)
	}
	if _, err := planningActivities(t.TempDir(), nil, true); !errors.Is(err, modelhttp.ErrBothConfigured) {
		t.Fatalf("planning activities: %v", err)
	}
	// Every subcommand refuses, agent-exec included, before it reads stdin.
	for _, args := range [][]string{{"agent-exec"}, {"agent-exec", "--provider", "claude"}, {"worker"}} {
		root := newRoot()
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetIn(strings.NewReader("Inspect the bound source repository for this planning session."))
		root.SetArgs(args)
		if err := root.Execute(); !errors.Is(err, modelhttp.ErrBothConfigured) {
			t.Fatalf("%v: err = %v, output %q", args, err, out.String())
		}
	}
}

func TestWorkerSelectsOneModelTransport(t *testing.T) {
	none, err := resolveModelTransport(envMap(nil))
	if err != nil || none.configured() {
		t.Fatalf("no model: %+v %v", none, err)
	}
	// The HTTP variables select nothing without a base URL.
	nameOnly, err := resolveModelTransport(envMap(map[string]string{"CONDUIT_MODEL_NAME": "m"}))
	if err != nil || nameOnly.configured() {
		t.Fatalf("name only: %+v %v", nameOnly, err)
	}

	overHTTP, err := resolveModelTransport(envMap(map[string]string{
		"CONDUIT_MODEL_BASE_URL": "https://model.example/v1", "CONDUIT_MODEL_NAME": "asked",
	}))
	if err != nil || !overHTTP.configured() {
		t.Fatalf("http: %+v %v", overHTTP, err)
	}
	runner := overHTTP.planner(nil)
	if model, ok := runner.Model.(planner.HTTPModel); !ok || model.Identity().ConfiguredModel != "asked" || len(runner.Argv) != 0 {
		t.Fatalf("http planner = %+v", runner)
	}
	if _, ok := overHTTP.packageWriter().(packagingmodelhttp.Adapter); !ok {
		t.Fatalf("http writer = %T", overHTTP.packageWriter())
	}

	overCLI, err := resolveModelTransport(envMap(map[string]string{
		"CONDUIT_MODEL_CMD": "codex exec -", "CONDUIT_MODEL_NAME": " operator-chosen ",
	}))
	if err != nil || !overCLI.configured() {
		t.Fatalf("cli: %+v %v", overCLI, err)
	}
	runner = overCLI.planner(nil)
	if runner.Model != nil || runner.Argv[0] != "codex" || runner.ModelName != "operator-chosen" {
		t.Fatalf("cli planner = %+v", runner)
	}
	if writer, ok := overCLI.packageWriter().(modelcli.Runner); !ok || writer.ModelName != "operator-chosen" {
		t.Fatalf("cli writer = %+v", overCLI.packageWriter())
	}

	if _, err := resolveModelTransport(envMap(map[string]string{
		"CONDUIT_MODEL_BASE_URL": "http://vllm.models.svc:8000/v1", "CONDUIT_MODEL_NAME": "m",
	})); !errors.Is(err, modelhttp.ErrInsecureEndpoint) {
		t.Fatalf("plaintext to a non-loopback host: %v", err)
	}
}

func TestUnverifiedPresetWarnsOnStderr(t *testing.T) {
	for _, provider := range []string{providerClaude, providerCodex, providerGemini, providerGrok} {
		got := captureStderr(t, func() {
			warnUnverifiedPreset([]string{"packaging-workflow", "agent-exec", "--provider", provider}, "")
		})
		if !strings.Contains(got, "UNVERIFIED") || !strings.Contains(got, "provider="+provider) {
			t.Fatalf("%s: stderr = %q", provider, got)
		}
	}
	quiet := captureStderr(t, func() {
		warnUnverifiedPreset([]string{"packaging-workflow", "agent-exec"}, "")
		warnUnverifiedPreset([]string{"packaging-workflow", "agent-exec", "--provider=deterministic"}, "claude")
		warnUnverifiedPreset([]string{"my-own-model-cli", "--provider", "claude"}, "")
	})
	if quiet != "" {
		t.Fatalf("the deterministic agent and a non-preset command must not warn: %q", quiet)
	}
	fromEnv := captureStderr(t, func() {
		warnUnverifiedPreset([]string{"packaging-workflow", "agent-exec"}, "gemini")
	})
	if !strings.Contains(fromEnv, "provider=gemini") {
		t.Fatalf("CONDUIT_AGENT_PROVIDER was not honoured: %q", fromEnv)
	}
}

func TestAgentExecWarnsWhenAnUnverifiedPresetRuns(t *testing.T) {
	script := t.TempDir() + "/fake-llm"
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat >/dev/null\nprintf 'ok\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONDUIT_PROVIDER_GROK_CMD", script)
	run := func(args ...string) (string, string) {
		var out bytes.Buffer
		stderr := captureStderr(t, func() {
			root := newRoot()
			root.SetOut(&out)
			root.SetIn(strings.NewReader("Inspect the bound source repository for this planning session."))
			root.SetArgs(args)
			if err := root.Execute(); err != nil {
				t.Fatalf("%v: %v", args, err)
			}
		})
		return out.String(), stderr
	}
	stdout, stderr := run("agent-exec", "--provider", "grok")
	if strings.TrimSpace(stdout) != "ok" {
		t.Fatalf("the warning must not reach stdout, which is the reply: %q", stdout)
	}
	if !strings.Contains(stderr, "UNVERIFIED") || !strings.Contains(stderr, "provider=grok") {
		t.Fatalf("stderr = %q", stderr)
	}
	if _, stderr = run("agent-exec"); strings.Contains(stderr, "UNVERIFIED") {
		t.Fatalf("the deterministic agent warned: %q", stderr)
	}
}
