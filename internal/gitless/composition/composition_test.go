package composition_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/gitless/application"
	"github.com/jonathanagustin/conduit/internal/gitless/composition"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

type nowhereStore struct{}

func (nowhereStore) Put(context.Context, string, []byte) error             { return nil }
func (nowhereStore) CurrentDigest(context.Context, string) (string, error) { return "", nil }

type durableLog struct{}

func (durableLog) Record(context.Context, domain.Resolution) error { return nil }

type silent struct{}

func (silent) Deployed(string, application.Outcome)  {}
func (silent) Unchanged(string, application.Outcome) {}
func (silent) Refused(string, []domain.Refusal)      {}
func (silent) Failed(string, error)                  {}

func policyFile(t *testing.T, environments ...string) string {
	t.Helper()
	declared := map[string]any{}
	for _, environment := range environments {
		declared[environment] = map[string]string{
			"certificateIssuer":   `^https://token\.actions\.githubusercontent\.com$`,
			"certificateIdentity": `^https://github\.com/example/x/\.github/workflows/y\.yml@refs/tags/v[0-9]+$`,
		}
	}
	body, err := json.Marshal(map[string]any{"environments": declared})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "policies.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func base(t *testing.T, bindings ...string) composition.Config {
	t.Helper()
	return composition.Config{
		Bindings:          bindings,
		PolicyFile:        policyFile(t, "prod"),
		ResolutionLogFile: filepath.Join(t.TempDir(), "resolutions.jsonl"),
		Interval:          time.Minute,
		Convergence:       "flux",
		Flux: composition.FluxConfig{
			ServiceAccountName: "platform-runtime-reconciler",
			PullSecretName:     "conduit-runtime-pull",
			ManifestStore:      nowhereStore{},
		},
	}
}

// A Hub that says nothing about Gitless GitOps must start exactly as before.
// Defaulting to some source would mean an upgrade could begin deploying things
// nobody asked for.
func TestNoBindingsMeansOffRatherThanAnError(t *testing.T) {
	scheduler, err := composition.Configure(composition.Config{}, composition.ConduitConfig{}, silent{})
	if err != nil {
		t.Fatalf("an unconfigured Hub failed to start: %v", err)
	}
	if scheduler != nil {
		t.Fatal("an unconfigured Hub built a scheduler")
	}
}

func TestAConfiguredHubBuildsAScheduler(t *testing.T) {
	scheduler, err := composition.Configure(
		base(t, "prod=ghcr.io/example/runtime:latest-stable"), composition.ConduitConfig{}, silent{})
	if err != nil {
		t.Fatalf("configure: %v", err)
	}
	if scheduler == nil {
		t.Fatal("a configured Hub built no scheduler")
	}
	if bound := scheduler.Bindings(); len(bound) != 1 || bound[0].Environment != "prod" {
		t.Fatalf("bindings: %+v", bound)
	}
}

func TestASharedResolutionLogReplacesTheSingleReplicaFile(t *testing.T) {
	config := base(t, "prod=ghcr.io/example/runtime:latest-stable")
	config.ResolutionLogFile = ""
	config.ResolutionLog = durableLog{}
	if scheduler, err := composition.Configure(config, composition.ConduitConfig{}, silent{}); err != nil || scheduler == nil {
		t.Fatalf("shared resolution log did not configure: scheduler=%v err=%v", scheduler, err)
	}
	config.ResolutionLogFile = filepath.Join(t.TempDir(), "also.jsonl")
	if _, err := composition.Configure(config, composition.ConduitConfig{}, silent{}); err == nil {
		t.Fatal("two resolution-log authorities were accepted")
	}
}

// Which convergence path is in use decides whether a source change passes
// through Conduit's approval semantics. Falling back into either would be
// choosing somebody's governance model for them.
func TestConvergenceHasNoDefault(t *testing.T) {
	config := base(t, "prod=ghcr.io/example/runtime:latest-stable")
	config.Convergence = ""
	_, err := composition.Configure(config, composition.ConduitConfig{}, silent{})
	if err == nil {
		t.Fatal("a Hub with no convergence choice was configured")
	}
	if !strings.Contains(err.Error(), "no default") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

// There is no safe default for who may publish, or for where deployments are
// recorded.
func TestTheSecurityRelevantFilesHaveNoDefaults(t *testing.T) {
	for name, mutate := range map[string]func(*composition.Config){
		"no policy file": func(config *composition.Config) { config.PolicyFile = "" },
		"no log file":    func(config *composition.Config) { config.ResolutionLogFile = "" },
	} {
		config := base(t, "prod=ghcr.io/example/runtime:latest-stable")
		mutate(&config)
		if _, err := composition.Configure(config, composition.ConduitConfig{}, silent{}); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

// A bound environment with no policy would be a Hub that started cleanly and
// then silently never deployed that environment.
func TestABoundEnvironmentWithNoPolicyFailsAtStartup(t *testing.T) {
	config := base(t, "prod=ghcr.io/example/runtime:latest-stable", "staging=ghcr.io/example/runtime:latest")
	_, err := composition.Configure(config, composition.ConduitConfig{}, silent{})
	if err == nil {
		t.Fatal("an environment with no publisher policy was configured")
	}
	if !strings.Contains(err.Error(), "staging") {
		t.Errorf("the refusal does not name the environment: %v", err)
	}
}

// --- selector parsing -------------------------------------------------------

func TestSelectorParsing(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	for reference, want := range map[string]domain.Selector{
		"ghcr.io/example/runtime:latest-stable": {
			Kind: domain.ByChannel, Repository: "ghcr.io/example/runtime", Channel: "latest-stable",
		},
		// A registry may carry a port, so the channel separator is the last
		// colon after the last slash rather than the first colon in the string.
		"registry.example:5000/team/runtime:v2": {
			Kind: domain.ByChannel, Repository: "registry.example:5000/team/runtime", Channel: "v2",
		},
		"ghcr.io/example/runtime@" + digest: {
			Kind: domain.ByDigest, Repository: "ghcr.io/example/runtime", Digest: digest,
		},
		// A digest reference is never a channel, however it is punctuated.
		"registry.example:5000/team/runtime@" + digest: {
			Kind: domain.ByDigest, Repository: "registry.example:5000/team/runtime", Digest: digest,
		},
	} {
		got, err := composition.ParseSelector(reference)
		if err != nil {
			t.Errorf("%s: %v", reference, err)
			continue
		}
		if got != want {
			t.Errorf("%s parsed as %+v, want %+v", reference, got, want)
		}
	}
}

func TestAReferenceNamingNeitherChannelNorDigestIsRefused(t *testing.T) {
	if _, err := composition.ParseSelector("ghcr.io/example/runtime"); err == nil {
		t.Fatal("a reference with no channel or digest was accepted")
	}
}

func TestABindingWithNoEnvironmentIsRefused(t *testing.T) {
	if _, err := composition.ParseBindings([]string{"ghcr.io/example/runtime:latest"}); err == nil {
		t.Fatal("a binding with no environment was accepted")
	}
}

func TestBlankBindingEntriesAreIgnored(t *testing.T) {
	bindings, err := composition.ParseBindings([]string{"", "  ", "prod=ghcr.io/example/runtime:latest"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected one binding, got %d", len(bindings))
	}
}
