package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

// The default. A Hub that says nothing about Gitless GitOps must start exactly
// as it did before this existed.
func TestGitlessIsOffUnlessConfigured(t *testing.T) {
	scheduler, err := configureGitless(env(nil))
	if err != nil {
		t.Fatalf("an unconfigured Hub failed to start: %v", err)
	}
	if scheduler != nil {
		t.Fatal("an unconfigured Hub built a scheduler")
	}
}

// A misconfiguration must stop the Hub rather than surface on the first tick.
// A Hub that started cleanly and silently never deployed an environment is
// worse than one that refused to start.
func TestAMisconfiguredHubRefusesToStart(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"no policy file": {
			"GITLESS_BINDINGS":    "prod=ghcr.io/example/runtime:latest",
			"GITLESS_CONVERGENCE": "flux",
		},
		"no convergence choice": {
			"GITLESS_BINDINGS":              "prod=ghcr.io/example/runtime:latest",
			"GITLESS_PUBLISHER_POLICY_FILE": policyFile(t),
			"GITLESS_RESOLUTION_LOG_FILE":   filepath.Join(t.TempDir(), "log.jsonl"),
		},
		"unparseable interval": {
			"GITLESS_BINDINGS": "prod=ghcr.io/example/runtime:latest",
			"GITLESS_INTERVAL": "every so often",
		},
		"binding with no channel": {
			"GITLESS_BINDINGS": "prod=ghcr.io/example/runtime",
		},
	} {
		if _, err := configureGitless(env(values)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// Flux convergence needs somewhere to publish manifests. Naming no repository
// must refuse rather than start a scheduler that fails every pass.
func TestFluxConvergenceWithoutAManifestRepositoryIsRefused(t *testing.T) {
	_, err := configureGitless(env(fluxEnvironment(t, "")))
	if err == nil {
		t.Fatal("Flux convergence was configured with nowhere to publish")
	}
	if !strings.Contains(err.Error(), "publish") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
}

// The check that the previous version of this file could not make. Every
// configuration of the Flux path failed at startup because the binary supplied
// no manifest store, so Gitless GitOps could not be turned on at all -- while
// the commit message said it ran. It starts now.
func TestAFullyConfiguredFluxHubStartsItsScheduler(t *testing.T) {
	scheduler, err := configureGitless(env(fluxEnvironment(t, "registry.example/team/manifests")))
	if err != nil {
		t.Fatalf("a fully configured Hub refused to start Gitless GitOps: %v", err)
	}
	if scheduler == nil {
		t.Fatal("a fully configured Hub built no scheduler")
	}
	bound := scheduler.Bindings()
	if len(bound) != 1 || bound[0].Environment != "prod" {
		t.Fatalf("bindings: %+v", bound)
	}
}

// The Conduit-owned path is still not wired in this binary. It must refuse
// rather than start something that cannot work.
func TestConduitConvergenceIsRefusedUntilItIsWired(t *testing.T) {
	values := fluxEnvironment(t, "registry.example/team/manifests")
	values["GITLESS_CONVERGENCE"] = "conduit"
	if _, err := configureGitless(env(values)); err == nil {
		t.Fatal("the unwired Conduit convergence path started")
	}
}

func fluxEnvironment(t *testing.T, repository string) map[string]string {
	t.Helper()
	return map[string]string{
		"GITLESS_BINDINGS":                 "prod=ghcr.io/example/runtime:latest",
		"GITLESS_PUBLISHER_POLICY_FILE":    policyFile(t),
		"GITLESS_RESOLUTION_LOG_FILE":      filepath.Join(t.TempDir(), "log.jsonl"),
		"GITLESS_CONVERGENCE":              "flux",
		"GITLESS_FLUX_SERVICE_ACCOUNT":     "platform-runtime-reconciler",
		"GITLESS_FLUX_PULL_SECRET":         "conduit-runtime-pull",
		"GITLESS_FLUX_MANIFEST_REPOSITORY": repository,
	}
}

func policyFile(t *testing.T) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{"environments": map[string]any{
		"prod": map[string]string{
			"certificateIssuer":   `^https://token\.actions\.githubusercontent\.com$`,
			"certificateIdentity": `^https://github\.com/example/x/\.github/workflows/y\.yml@refs/tags/v[0-9]+$`,
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policies.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
