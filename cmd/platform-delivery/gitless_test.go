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

func TestAConfiguredRegistryFileMustBeReadableAndUsable(t *testing.T) {
	missing := fluxEnvironment(t, "registry.example/team/manifests")
	missing["GITLESS_REGISTRY_CONFIG"] = filepath.Join(t.TempDir(), "missing.json")
	if _, err := configureGitless(env(missing)); err == nil {
		t.Fatal("a configured missing registry credential file was accepted")
	}

	empty := fluxEnvironment(t, "registry.example/team/manifests")
	emptyPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(emptyPath, []byte(`{"auths":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	empty["GITLESS_REGISTRY_CONFIG"] = emptyPath
	if _, err := configureGitless(env(empty)); err == nil {
		t.Fatal("a configured registry file with no credentials was accepted")
	}

	configured := fluxEnvironment(t, "registry.example/team/manifests")
	configuredPath := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(configuredPath,
		[]byte(`{"auths":{"registry.example":{"username":"writer","password":"token"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	configured["GITLESS_REGISTRY_CONFIG"] = configuredPath
	if scheduler, err := configureGitless(env(configured)); err != nil || scheduler == nil {
		t.Fatalf("a configured private-registry Hub did not start: scheduler=%v err=%v", scheduler, err)
	}
}

func TestAnUnconfiguredHubDoesNotReadAnUnusedRegistryFile(t *testing.T) {
	scheduler, err := configureGitless(env(map[string]string{
		"GITLESS_REGISTRY_CONFIG": filepath.Join(t.TempDir(), "missing.json"),
	}))
	if err != nil {
		t.Fatalf("an unused credential mount changed the off state: %v", err)
	}
	if scheduler != nil {
		t.Fatal("an unused credential mount enabled Gitless GitOps")
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

// Cosign is told about a private registry through DOCKER_CONFIG, which names a
// DIRECTORY it joins config.json onto. A credential file under any other name
// would leave verification authenticating anonymously while resolution and
// publication did not -- a split that first surfaces as a 401 from Cosign on
// some later reconcile, naming neither the cause nor the fix. So it is a
// startup refusal, like every other Gitless misconfiguration.
func TestACredentialFileCosignCouldNotReadRefusesToStart(t *testing.T) {
	values := fluxEnvironment(t, "registry.example/team/manifests")
	path := filepath.Join(t.TempDir(), ".dockerconfigjson")
	if err := os.WriteFile(path,
		[]byte(`{"auths":{"registry.example":{"username":"writer","password":"token"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	values["GITLESS_REGISTRY_CONFIG"] = path
	_, err := configureGitless(env(values))
	if err == nil {
		t.Fatal("a credential file Cosign would not read was accepted")
	}
	if !strings.Contains(err.Error(), "config.json") {
		t.Errorf("the refusal does not say what to rename it to: %v", err)
	}
}

// A registry directs a credential at itself unless an operator said otherwise.
// Docker Hub and GitLab genuinely issue a token realm on another host, so the
// declaration exists -- but it is a declaration, not something a 401 can make
// on the operator's behalf.
func TestAMalformedTokenRealmDeclarationRefusesToStart(t *testing.T) {
	for name, declaration := range map[string]string{
		"no separator":  "registry-1.docker.io",
		"a URL":         "registry-1.docker.io=https://auth.docker.io/token",
		"a wildcard":    "registry-1.docker.io=*",
		"nothing after": "registry-1.docker.io=",
	} {
		values := fluxEnvironment(t, "registry.example/team/manifests")
		values["GITLESS_REGISTRY_TOKEN_REALMS"] = declaration
		if _, err := configureGitless(env(values)); err == nil {
			t.Errorf("%s was accepted as a token realm declaration", name)
		}
	}
}

func TestAWellFormedTokenRealmDeclarationStarts(t *testing.T) {
	values := fluxEnvironment(t, "registry.example/team/manifests")
	values["GITLESS_REGISTRY_TOKEN_REALMS"] = "registry-1.docker.io=auth.docker.io,registry.gitlab.com=gitlab.com"
	scheduler, err := configureGitless(env(values))
	if err != nil || scheduler == nil {
		t.Fatalf("a Hub declaring cross-host token realms did not start: scheduler=%v err=%v", scheduler, err)
	}
}
