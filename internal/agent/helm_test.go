package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo/workloadmetadata"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"go.yaml.in/yaml/v4"
)

type helmRunner struct {
	step       controlplane.PlanStep
	commands   [][]string
	major      int
	installed  bool
	upgradeErr error
	// values records the resolved values document the executor wrote for the
	// invocation, read back from the path it passed to --values.
	values string
}

type registryPreparer struct {
	accesses   []controlplane.RegistryAccess
	namespaces []string
	err        error
}

func (preparer *registryPreparer) Prepare(_ context.Context, access controlplane.RegistryAccess, namespace string) error {
	preparer.accesses = append(preparer.accesses, access)
	preparer.namespaces = append(preparer.namespaces, namespace)
	return preparer.err
}

func (runner *helmRunner) Run(_ context.Context, name string, arguments ...string) ([]byte, error) {
	runner.commands = append(runner.commands, append([]string{name}, arguments...))
	for index, argument := range arguments {
		if argument == "--values" && index+1 < len(arguments) {
			if contents, err := os.ReadFile(arguments[index+1]); err == nil {
				runner.values = string(contents)
			}
		}
	}
	if name == "kubectl" {
		return []byte("rollout complete"), nil
	}
	switch arguments[0] {
	case "version":
		major := runner.major
		if major == 0 {
			major = 3
		}
		return []byte(fmt.Sprintf("v%d.0.0", major)), nil
	case "list":
		if runner.installed {
			return []byte(`[{"name":"foundation","revision":"1","chart":"foundation-1.2.3"}]`), nil
		}
		return []byte(`[]`), nil
	case "pull":
		for index, argument := range arguments {
			if argument == "--destination" {
				path := filepath.Join(arguments[index+1], "foundation-1.2.3.tgz")
				if err := os.WriteFile(path, []byte("chart"), 0o600); err != nil {
					return nil, err
				}
			}
		}
		return []byte("Pulled: registry.example/foundation:1.2.3\nDigest: " + runner.step.Chart.Digest), nil
	case "upgrade":
		if runner.upgradeErr != nil {
			return nil, runner.upgradeErr
		}
		runner.installed = true
		return []byte("Release upgraded"), nil
	case "template":
		image := runner.step.Images[0]
		return []byte("containers:\n  - name: app\n    image: \"" + image.Repository + ":" + image.Tag + "@" + image.Digest + "\"\n"), nil
	case "status":
		return []byte(fmt.Sprintf(`{"name":"foundation","info":{"status":"deployed","description":%q},"version":1,"namespace":"example"}`, releaseDescription(runner.step))), nil
	case "get":
		return []byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: foundation\n"), nil
	case "uninstall":
		runner.installed = false
		return nil, nil
	case "rollback":
		runner.installed = true
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected helm command %q", arguments[0])
	}
}

func TestHelmExecutorSelectsCompatibleHelmThreeAndFourFlags(t *testing.T) {
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart:  controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
	}
	for _, test := range []struct {
		major   int
		present []string
		absent  []string
	}{
		{major: 3, present: []string{"--atomic", "--wait"}, absent: []string{"--rollback-on-failure", "--server-side=false"}},
		{major: 4, present: []string{"--rollback-on-failure", "--wait=watcher", "--server-side=false"}, absent: []string{"--atomic"}},
	} {
		t.Run(fmt.Sprintf("Helm%d", test.major), func(t *testing.T) {
			runner := &helmRunner{step: step, major: test.major}
			if _, err := (HelmExecutor{Runner: runner}).Apply(context.Background(), controlplane.Plan{}, step); err != nil {
				t.Fatal(err)
			}
			commands := fmt.Sprint(runner.commands)
			for _, flag := range test.present {
				if !strings.Contains(commands, flag) {
					t.Fatalf("Helm %d commands lack %s: %s", test.major, flag, commands)
				}
			}
			for _, flag := range test.absent {
				if strings.Contains(commands, flag) {
					t.Fatalf("Helm %d commands unexpectedly contain %s: %s", test.major, flag, commands)
				}
			}
		})
	}
}

func TestHelmExecutorEmergencyUninstallAndResourceRestart(t *testing.T) {
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		ReleaseDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Namespace:     "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}
	t.Run("uninstall observes absence and rollback restores revision", func(t *testing.T) {
		runner := &helmRunner{step: step, installed: true}
		executor := HelmExecutor{Runner: runner, Timeout: time.Minute}
		step.Action = "uninstall"
		receipt, err := executor.Apply(context.Background(), controlplane.Plan{}, step)
		if err != nil {
			t.Fatal(err)
		}
		report, err := executor.Observe(context.Background(), step)
		if err != nil || report.Lifecycle != controlplane.LifecycleUninstalled {
			t.Fatalf("uninstall report=%#v err=%v", report, err)
		}
		if err := executor.Rollback(context.Background(), receipt); err != nil {
			t.Fatal(err)
		}
		commands := fmt.Sprint(runner.commands)
		if !strings.Contains(commands, "--keep-history") || !strings.Contains(commands, "rollback foundation 1") {
			t.Fatalf("uninstall/rollback commands = %s", commands)
		}
	})
	t.Run("restart is bounded to a workload controller", func(t *testing.T) {
		runner := &helmRunner{step: step, installed: true}
		executor := HelmExecutor{Runner: runner, Timeout: time.Minute}
		step.Action, step.ResourceKind, step.ResourceName = "restart", "Deployment", "foundation"
		if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		if _, err := executor.Observe(context.Background(), step); err != nil {
			t.Fatal(err)
		}
		commands := fmt.Sprint(runner.commands)
		if !strings.Contains(commands, "kubectl rollout restart deployment/foundation") ||
			!strings.Contains(commands, "kubectl rollout status deployment/foundation") {
			t.Fatalf("restart commands = %s", commands)
		}
	})
}

type imagePreparer struct {
	images    []controlplane.Artifact
	access    []controlplane.RegistryAccess
	namespace string
	err       error
	// budget records how long the pre-pull was given, which is otherwise
	// invisible: the deadline lives on a context nobody else sees.
	budget time.Duration
}

func (preparer *imagePreparer) PrepareImages(
	ctx context.Context, images []controlplane.Artifact, access []controlplane.RegistryAccess, namespace string,
) error {
	if deadline, ok := ctx.Deadline(); ok {
		// Rounded because the clock advances between the deadline being set
		// and this running.
		preparer.budget = time.Until(deadline).Round(time.Minute)
	}
	preparer.images = append([]controlplane.Artifact(nil), images...)
	preparer.access = append([]controlplane.RegistryAccess(nil), access...)
	preparer.namespace = namespace
	return preparer.err
}

func TestHelmExecutorPinsDigestAndObservesReleaseMetadata(t *testing.T) {
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart:  controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
	}
	runner := &helmRunner{step: step}
	executor := HelmExecutor{Runner: runner, Timeout: time.Minute, Now: func() time.Time { return time.Unix(100, 0) }}
	receipt, err := executor.Apply(context.Background(), controlplane.Plan{}, step)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PreviousRevision != 0 {
		t.Fatalf("previous revision = %d", receipt.PreviousRevision)
	}
	report, err := executor.Observe(context.Background(), step)
	if err != nil {
		t.Fatal(err)
	}
	if report.Health != controlplane.HealthHealthy || report.RolloutStatus != controlplane.RolloutUpToDate ||
		report.ConfigStatus != controlplane.ConfigApplied || report.Lifecycle != controlplane.LifecycleInstalled ||
		report.ConfigDigest != step.ConfigDigest {
		t.Fatalf("report = %#v", report)
	}
	commands := fmt.Sprint(runner.commands)
	if !strings.Contains(commands, "--atomic") || !strings.Contains(commands, "--description") {
		t.Fatalf("upgrade was not atomic and attributable: %s", commands)
	}
	for _, status := range []string{"--deployed", "--failed", "--pending", "--uninstalled", "--superseded", "--uninstalling"} {
		if !strings.Contains(commands, status) {
			t.Fatalf("Helm 3/4 release lookup omitted %s: %s", status, commands)
		}
	}
	if strings.Contains(commands, " --all ") {
		t.Fatalf("Helm 4-incompatible --all shorthand returned: %s", commands)
	}
}

func TestValidateRenderedImagesIncludesKubernetesImageVolumes(t *testing.T) {
	container := controlplane.Artifact{Repository: "registry.example/service", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}
	asset := controlplane.Artifact{Repository: "registry.example/assets", Tag: "4.5.6", Digest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"}
	rendered := []byte(`apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      containers:
        - name: service
          image: "registry.example/service:1.2.3@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
      volumes:
        - name: assets
          image:
            reference: registry.example/assets:4.5.6@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
            pullPolicy: IfNotPresent
`)
	if err := validateRenderedImages(rendered, []controlplane.Artifact{container, asset}); err != nil {
		t.Fatal(err)
	}
	undeclared := strings.ReplaceAll(string(rendered), asset.Digest, "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff")
	if err := validateRenderedImages([]byte(undeclared), []controlplane.Artifact{container, asset}); err == nil ||
		!strings.Contains(err.Error(), "undeclared or mutable image") {
		t.Fatalf("undeclared image volume error = %v", err)
	}
}

func TestHelmExecutorRolloutPoliciesAndImagePrePull(t *testing.T) {
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart:  controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		HelmExecution: controlplane.HelmExecutionPolicy{
			RolloutStrategy: controlplane.RolloutApplyChangesNoWait, ImagePrePull: true, ImagePrePullTimeoutSeconds: 60,
		},
	}
	runner := &helmRunner{step: step}
	preparer := &imagePreparer{}
	executor := HelmExecutor{Runner: runner, Timeout: 5 * time.Minute, Images: preparer, Now: func() time.Time { return time.Unix(100, 0) }}
	if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
		t.Fatal(err)
	}
	commands := fmt.Sprint(runner.commands)
	if strings.Contains(commands, "--atomic") || strings.Contains(commands, "--wait") || !strings.Contains(commands, "--timeout 5m0s") {
		t.Fatalf("no-wait Helm arguments = %s", commands)
	}
	if len(preparer.images) != 1 || preparer.namespace != "example" {
		t.Fatalf("image preparation = %#v in %q", preparer.images, preparer.namespace)
	}
	report, err := executor.Observe(context.Background(), step)
	if err != nil {
		t.Fatal(err)
	}
	if report.Health != controlplane.HealthUnknown || report.RolloutStatus != controlplane.RolloutInGracePeriod {
		t.Fatalf("no-wait report = %#v", report)
	}
	if strings.Contains(fmt.Sprint(runner.commands), "helm get manifest") {
		t.Fatalf("no-wait rollout claimed external realization: %v", runner.commands)
	}
}

func TestHelmExecutorManagedTimeoutAndNoWaitFailureRollback(t *testing.T) {
	step := controlplane.PlanStep{
		Entity: "foundation", Namespace: "example",
		Chart:  controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		HelmExecution: controlplane.HelmExecutionPolicy{
			RolloutStrategy: controlplane.RolloutManageRollout, InstallResourceReadinessTimeoutSeconds: 3600,
		},
	}
	runner := &helmRunner{step: step}
	if _, err := (HelmExecutor{Runner: runner}).Apply(context.Background(), controlplane.Plan{}, step); err != nil {
		t.Fatal(err)
	}
	commands := fmt.Sprint(runner.commands)
	if !strings.Contains(commands, "--atomic") || !strings.Contains(commands, "--wait") || !strings.Contains(commands, "--timeout 1h0m0s") {
		t.Fatalf("managed Helm arguments = %s", commands)
	}

	step.HelmExecution.RolloutStrategy = controlplane.RolloutApplyChangesNoWait
	failing := &helmRunner{step: step, upgradeErr: fmt.Errorf("apply rejected")}
	_, err := (HelmExecutor{Runner: failing, Timeout: 11 * time.Minute}).Apply(context.Background(), controlplane.Plan{}, step)
	if err == nil || !strings.Contains(err.Error(), "apply rejected") || !strings.Contains(fmt.Sprint(failing.commands), "uninstall") {
		t.Fatalf("no-wait apply failure did not roll back: err=%v commands=%v", err, failing.commands)
	}
	if !strings.Contains(fmt.Sprint(failing.commands), "--timeout 5m0s") {
		t.Fatalf("no-wait Helm operation did not preserve its fixed timeout: %v", failing.commands)
	}
	var diagnostic DiagnosticError
	if !errors.As(err, &diagnostic) || len(diagnostic.Resources) != 1 || diagnostic.Resources[0].Kind != "HelmRelease" {
		t.Fatalf("Helm failure diagnostics = %#v", diagnostic)
	}
}

func TestHelmExecutorRejectsRegistryDigestMismatch(t *testing.T) {
	step := controlplane.PlanStep{
		Entity: "foundation", Namespace: "example",
		Chart:  controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
	}
	runner := &helmRunner{step: controlplane.PlanStep{Chart: controlplane.Artifact{Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	_, err := (HelmExecutor{Runner: runner}).Apply(context.Background(), controlplane.Plan{}, step)
	if err == nil || !strings.Contains(err.Error(), "digest does not match") {
		t.Fatalf("expected digest mismatch, got %v", err)
	}
}

func TestHelmExecutorRequiresAndInvokesExternalRegistryPreparer(t *testing.T) {
	access := controlplane.RegistryAccess{
		AssignmentID: "primary", Revision: 4, SourceRegistry: "registry.example", RuntimeRegistry: "mirror.internal",
		CredentialRef: "vault://platform/registries/mirror", ImagePullSecretName: "mirror-pull",
	}
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart:          controlplane.Artifact{Repository: "mirror.internal/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images:         []controlplane.Artifact{{Repository: "mirror.internal/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		RegistryAccess: []controlplane.RegistryAccess{access},
	}

	t.Run("missing preparer fails before Helm", func(t *testing.T) {
		runner := &helmRunner{step: step}
		_, err := (HelmExecutor{Runner: runner}).Apply(context.Background(), controlplane.Plan{}, step)
		if err == nil || !strings.Contains(err.Error(), "requires an external credential preparer") {
			t.Fatalf("expected missing preparer error, got %v", err)
		}
		if len(runner.commands) != 0 {
			t.Fatalf("Helm ran before registry preparation: %#v", runner.commands)
		}
	})

	t.Run("preparer runs with immutable assignment before Helm", func(t *testing.T) {
		runner := &helmRunner{step: step}
		preparer := &registryPreparer{}
		_, err := (HelmExecutor{Runner: runner, Registry: preparer}).Apply(context.Background(), controlplane.Plan{}, step)
		if err != nil {
			t.Fatal(err)
		}
		if len(preparer.accesses) != 1 || preparer.accesses[0] != access ||
			len(preparer.namespaces) != 1 || preparer.namespaces[0] != "example" {
			t.Fatalf("registry preparation = %#v in %#v", preparer.accesses, preparer.namespaces)
		}
		if len(runner.commands) == 0 {
			t.Fatal("Helm did not run after successful preparation")
		}
	})

	t.Run("preparer error fails before Helm", func(t *testing.T) {
		runner := &helmRunner{step: step}
		preparer := &registryPreparer{err: fmt.Errorf("credential source unavailable")}
		_, err := (HelmExecutor{Runner: runner, Registry: preparer}).Apply(context.Background(), controlplane.Plan{}, step)
		if err == nil || !strings.Contains(err.Error(), "primary revision 4") || !strings.Contains(err.Error(), "credential source unavailable") {
			t.Fatalf("expected attributable preparer error, got %v", err)
		}
		if len(runner.commands) != 0 {
			t.Fatalf("Helm ran after registry preparation failed: %#v", runner.commands)
		}
	})
}

// TestHelmExecutorPostRendererSpellingPerHelmVersion pins the Helm 4 breaking
// change that spoke acceptance caught: Helm 3 takes an executable path for
// --post-renderer, while Helm 4 resolves the value as the name of a
// postrenderer/v1 plugin and rejects any path with "plugin: not found".
//
// The plugin schema was established against Helm v4.1.1 rather than from
// documentation, which does not yet specify postrenderer/v1.
func TestHelmExecutorPostRendererSpellingPerHelmVersion(t *testing.T) {
	step := controlplane.PlanStep{
		InstallationID: "catalog", Entity: "catalog", Product: "com.palantir:catalog", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart:  controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
	}

	t.Run("Helm3 takes an executable path", func(t *testing.T) {
		runner := &helmRunner{step: step, major: 3}
		executor := HelmExecutor{Runner: runner, Environment: "prod-eu",
			PostRenderer: "/platform-spoke-agent", PostRendererPlugin: PostRendererPluginName}
		if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		commands := fmt.Sprint(runner.commands)
		if !strings.Contains(commands, "--post-renderer /platform-spoke-agent") {
			t.Fatalf("Helm 3 must be given the executable path: %s", commands)
		}
		if !strings.Contains(commands, "--post-renderer-args "+PostRenderCommand) {
			t.Fatalf("Helm 3 must be given the subcommand: %s", commands)
		}
	})

	t.Run("Helm4 takes a plugin name", func(t *testing.T) {
		runner := &helmRunner{step: step, major: 4}
		executor := HelmExecutor{Runner: runner, Environment: "prod-eu",
			PostRenderer: "/platform-spoke-agent", PostRendererPlugin: PostRendererPluginName}
		if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		commands := fmt.Sprint(runner.commands)
		if !strings.Contains(commands, "--post-renderer "+PostRendererPluginName) {
			t.Fatalf("Helm 4 must be given the plugin name: %s", commands)
		}
		// A path here is the exact failure acceptance caught.
		if strings.Contains(commands, "--post-renderer /platform-spoke-agent") {
			t.Fatalf("Helm 4 must never be given an executable path: %s", commands)
		}
		// The plugin manifest already carries the subcommand, so repeating it
		// would shift the metadata path into the wrong argument position.
		if strings.Contains(commands, "--post-renderer-args "+PostRenderCommand) {
			t.Fatalf("Helm 4 must not repeat the subcommand: %s", commands)
		}
	})

	t.Run("Helm3 never sees the plugin", func(t *testing.T) {
		// Regression: Helm 3 rejects the postrenderer/v1 type and writes the
		// load failure to stdout, which corrupted the JSON that release lookup
		// parses -- `helm list` failed before post-rendering was ever reached.
		// Installing the plugin must therefore be gated on the major version.
		root := t.TempDir()
		t.Setenv("HELM_PLUGINS", "")
		runner := &helmRunner{step: step, major: 3}
		executor := HelmExecutor{Runner: runner, Environment: "prod-eu", PluginRoot: root}
		if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
			t.Fatalf("Helm 3 must not install a plugin: %v %v", entries, err)
		}
		if os.Getenv("HELM_PLUGINS") != "" {
			t.Fatalf("Helm 3 must not set HELM_PLUGINS, got %q", os.Getenv("HELM_PLUGINS"))
		}
	})

	t.Run("Helm4 installs the plugin once", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("HELM_PLUGINS", "")
		for range 2 {
			runner := &helmRunner{step: step, major: 4}
			executor := HelmExecutor{Runner: runner, Environment: "prod-eu", PluginRoot: root}
			if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := os.Stat(filepath.Join(root, PostRendererPluginName, "plugin.yaml")); err != nil {
			t.Fatalf("Helm 4 must install the plugin: %v", err)
		}
		// Repeated installs must not grow HELM_PLUGINS.
		if plugins := os.Getenv("HELM_PLUGINS"); plugins != root {
			t.Fatalf("HELM_PLUGINS = %q, want %q", plugins, root)
		}
	})

	t.Run("no Environment means no post-renderer", func(t *testing.T) {
		runner := &helmRunner{step: step, major: 4}
		if _, err := (HelmExecutor{Runner: runner}).Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprint(runner.commands), "--post-renderer") {
			t.Fatal("metadata must never be the reason an install fails")
		}
	})
}

// TestInstallPostRendererPluginWritesAHelmFourPlugin covers the manifest Helm 4
// discovers. The shape is verified against a real Helm 4 in spoke acceptance;
// this holds the fields that acceptance would only fail on much later.
func TestInstallPostRendererPluginWritesAHelmFourPlugin(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "operator-plugins")
	t.Setenv("HELM_PLUGINS", existing)
	if err := InstallPostRendererPlugin(root, "1.2.3"); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	manifest, err := os.ReadFile(filepath.Join(root, PostRendererPluginName, "plugin.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// The plugin name and subcommand are spelled out rather than built from
	// the same constants the manifest uses. Asserting "name: " +
	// PostRendererPluginName only proves the constant equals itself: rename it
	// and both sides move together, while Helm goes on looking for the old
	// directory and the post-renderer silently stops running.
	for _, expected := range []string{
		"type: postrenderer/v1", "runtime: subprocess",
		"name: platform-apollo-metadata\n", "version: 1.2.3\n", "post-render",
	} {
		if !strings.Contains(string(manifest), expected) {
			t.Fatalf("plugin.yaml lacks %q:\n%s", expected, manifest)
		}
	}
	// An operator's own plugin directory must survive, or installing this would
	// silently disable everything else they rely on.
	plugins := os.Getenv("HELM_PLUGINS")
	if !strings.Contains(plugins, root) || !strings.Contains(plugins, existing) {
		t.Fatalf("HELM_PLUGINS = %q", plugins)
	}
}

// TestRunPostRendererStampsStream is the entrypoint Helm actually executes.
func TestRunPostRendererStampsStream(t *testing.T) {
	metadata, err := workloadmetadata.ApolloWorkloadMetadataFor("prod-eu", "catalog", "com.palantir:catalog", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(t.TempDir(), "metadata.json")
	if err := os.WriteFile(metadataPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	input := strings.NewReader("apiVersion: v1\nkind: Service\nmetadata:\n  name: catalog\n")
	if err := RunPostRenderer(metadataPath, input, &out); err != nil {
		t.Fatalf("post-render: %v", err)
	}
	if !strings.Contains(out.String(), workloadmetadata.ApolloEntityIDAnnotation) {
		t.Fatalf("stamped output = %s", out.String())
	}

	// A missing metadata file must fail loudly: Helm treats a post-renderer's
	// exit status as the fate of the release, so a silent pass-through would
	// install unlabelled objects while reporting success.
	if err := RunPostRenderer(filepath.Join(t.TempDir(), "absent.json"), input, &out); err == nil {
		t.Fatal("a missing metadata file must be an error")
	}
}

// The declared-image check inspects a helm template render, but the mutation
// that follows may be an upgrade. helm template sets .Release.IsInstall unless
// told otherwise, so without --is-upgrade a chart could be validated in one form
// and deployed in another.
func TestHelmExecutorRendersTheFormItIsAboutToApply(t *testing.T) {
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart:  controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
	}
	templateCommand := func(t *testing.T, runner *helmRunner) []string {
		t.Helper()
		for _, command := range runner.commands {
			if len(command) > 1 && command[0] == "helm" && command[1] == "template" {
				return command
			}
		}
		t.Fatal("no helm template command was issued")
		return nil
	}

	t.Run("first install renders as an install", func(t *testing.T) {
		runner := &helmRunner{step: step}
		if _, err := (HelmExecutor{Runner: runner}).Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		if command := templateCommand(t, runner); slices.Contains(command, "--is-upgrade") {
			t.Fatalf("install rendered with --is-upgrade: %v", command)
		}
	})

	t.Run("existing release renders as an upgrade", func(t *testing.T) {
		runner := &helmRunner{step: step, installed: true}
		if _, err := (HelmExecutor{Runner: runner}).Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		if command := templateCommand(t, runner); !slices.Contains(command, "--is-upgrade") {
			t.Fatalf("upgrade rendered without --is-upgrade, so the check saw the install form: %v", command)
		}
	})
}

// readinessTimeout picks between the install and upgrade budgets and falls
// back when neither is set. Choosing the wrong one silently applies an
// install's budget to an upgrade, or vice versa, and a wrong readiness
// timeout looks like a slow rollout rather than a misconfiguration.
func TestReadinessTimeoutSelectsByRevisionAndFallsBack(t *testing.T) {
	t.Parallel()
	step := controlplane.PlanStep{HelmExecution: controlplane.HelmExecutionPolicy{
		InstallResourceReadinessTimeoutSeconds: 60,
		UpgradeResourceReadinessTimeoutSeconds: 120,
	}}
	fallback := 7 * time.Minute

	for name, test := range map[string]struct {
		previousRevision int
		policy           controlplane.HelmExecutionPolicy
		want             time.Duration
	}{
		"no previous revision is an install": {previousRevision: 0, policy: step.HelmExecution, want: time.Minute},
		// Revision 1 is the first upgrade: the boundary between > 0 and >= 0.
		"revision one is an upgrade": {previousRevision: 1, policy: step.HelmExecution, want: 2 * time.Minute},
		"a later revision is too":    {previousRevision: 9, policy: step.HelmExecution, want: 2 * time.Minute},
		"an unset install budget falls back": {previousRevision: 0, policy: controlplane.HelmExecutionPolicy{
			UpgradeResourceReadinessTimeoutSeconds: 120}, want: fallback},
		"an unset upgrade budget falls back": {previousRevision: 1, policy: controlplane.HelmExecutionPolicy{
			InstallResourceReadinessTimeoutSeconds: 60}, want: fallback},
		"both unset falls back": {previousRevision: 0, want: fallback},
	} {
		got := readinessTimeout(controlplane.PlanStep{HelmExecution: test.policy}, test.previousRevision, fallback)
		if got != test.want {
			t.Errorf("%s: readinessTimeout = %v, want %v", name, got, test.want)
		}
	}
}

// Every Helm and kubectl invocation is bounded. When the executor carries no
// timeout it uses five minutes rather than none: an unbounded helm uninstall
// against a wedged cluster holds the Agent's worker forever.
func TestHelmExecutorBoundsEveryInvocation(t *testing.T) {
	t.Parallel()
	step := controlplane.PlanStep{
		Entity: "foundation", Namespace: "example",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		HelmExecution: controlplane.HelmExecutionPolicy{RolloutStrategy: controlplane.RolloutManageRollout},
	}

	for name, test := range map[string]struct {
		timeout time.Duration
		want    string
	}{
		"unset takes five minutes": {timeout: 0, want: "--timeout 5m0s"},
		"a set timeout is used":    {timeout: 90 * time.Second, want: "--timeout 1m30s"},
	} {
		runner := &helmRunner{step: step}
		if _, err := (HelmExecutor{Runner: runner, Timeout: test.timeout}).Apply(
			context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if commands := fmt.Sprint(runner.commands); !strings.Contains(commands, test.want) {
			t.Errorf("%s: arguments %s do not carry %q", name, commands, test.want)
		}
	}
}

// An install with no values still has to write a values file, and an empty one
// is not valid JSON. Writing nothing produces a file helm refuses to parse.
func TestHelmExecutorWritesAnEmptyObjectWhenAStepCarriesNoValues(t *testing.T) {
	t.Parallel()
	step := controlplane.PlanStep{
		Entity: "foundation", Namespace: "example",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		HelmExecution: controlplane.HelmExecutionPolicy{RolloutStrategy: controlplane.RolloutManageRollout},
	}
	runner := &helmRunner{step: step}
	if _, err := (HelmExecutor{Runner: runner}).Apply(context.Background(), controlplane.Plan{}, step); err != nil {
		t.Fatal(err)
	}
	if runner.values != "{}" {
		t.Errorf("values file = %q, want {}", runner.values)
	}
}

// Uninstall and rollout-status take their own copy of the same default, so
// pinning it on install says nothing about them. An unbounded helm uninstall
// against a wedged cluster holds the Agent's worker open indefinitely, and a
// rollout status with no deadline never returns.
func TestHelmExecutorBoundsUninstallAndRolloutStatus(t *testing.T) {
	t.Parallel()
	base := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		ReleaseDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Namespace:     "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}

	for name, test := range map[string]struct {
		action  string
		timeout time.Duration
		want    string
	}{
		"uninstall unset takes five minutes": {action: "uninstall", want: "--timeout 5m0s"},
		"uninstall honours a set timeout":    {action: "uninstall", timeout: 90 * time.Second, want: "--timeout 1m30s"},
		"restart unset takes five minutes":   {action: "restart", want: "--timeout 5m0s"},
		"restart honours a set timeout":      {action: "restart", timeout: 90 * time.Second, want: "--timeout 1m30s"},
	} {
		step := base
		step.Action = test.action
		if test.action == "restart" {
			step.ResourceKind, step.ResourceName = "deployment", "foundation"
		}
		runner := &helmRunner{step: step, installed: true}
		if _, err := (HelmExecutor{Runner: runner, Timeout: test.timeout}).Apply(
			context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if commands := fmt.Sprint(runner.commands); !strings.Contains(commands, test.want) {
			t.Errorf("%s: %s does not carry %q", name, commands, test.want)
		}
	}
}

// Rollback carries its own copy of the default too, on both of its paths: a
// release with no previous revision is uninstalled, one with a revision is
// rolled back. Neither may run unbounded -- a rollback is what happens after
// something already went wrong, so hanging there strands the Installation
// mid-change.
func TestHelmExecutorBoundsBothRollbackPaths(t *testing.T) {
	t.Parallel()
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Namespace: "example",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
	}

	for name, test := range map[string]struct {
		previousRevision int
		timeout          time.Duration
		wantCommand      string
		wantTimeout      string
	}{
		"no previous revision uninstalls, unset timeout": {
			previousRevision: 0, wantCommand: "uninstall", wantTimeout: "--timeout 5m0s"},
		"a previous revision rolls back, unset timeout": {
			previousRevision: 2, wantCommand: "rollback", wantTimeout: "--timeout 5m0s"},
		"a set timeout is honoured": {
			previousRevision: 2, timeout: 90 * time.Second, wantCommand: "rollback", wantTimeout: "--timeout 1m30s"},
	} {
		runner := &helmRunner{step: step, installed: true}
		err := (HelmExecutor{Runner: runner, Timeout: test.timeout}).Rollback(context.Background(),
			Receipt{Step: step, PreviousRevision: test.previousRevision})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		commands := fmt.Sprint(runner.commands)
		if !strings.Contains(commands, test.wantCommand) || !strings.Contains(commands, test.wantTimeout) {
			t.Errorf("%s: %s lacks %q or %q", name, commands, test.wantCommand, test.wantTimeout)
		}
	}

	// A rollout restart changes Pod instances rather than the release, so
	// there is no inverse to run and nothing should be invoked at all.
	restart := step
	restart.Action = "restart"
	runner := &helmRunner{step: restart}
	if err := (HelmExecutor{Runner: runner}).Rollback(context.Background(),
		Receipt{Step: restart, PreviousRevision: 3}); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 0 {
		t.Errorf("rolling back a restart ran %v", runner.commands)
	}
}

// A registry assignment carrying neither a credential reference nor a pull
// secret has nothing to prepare, so it is skipped rather than demanding a
// preparer the Environment may not have configured. Inverting that makes an
// Environment with no external registries refuse to apply anything.
func TestHelmExecutorSkipsRegistryAccessWithNothingToPrepare(t *testing.T) {
	t.Parallel()
	step := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		HelmExecution: controlplane.HelmExecutionPolicy{RolloutStrategy: controlplane.RolloutManageRollout},
	}

	t.Run("an assignment with neither needs no preparer", func(t *testing.T) {
		t.Parallel()
		bare := step
		bare.RegistryAccess = []controlplane.RegistryAccess{
			{AssignmentID: "public", SourceRegistry: "registry.example", RuntimeRegistry: "registry.example"},
		}
		// No Registry preparer configured at all: with the skip inverted this
		// fails demanding one.
		if _, err := (HelmExecutor{Runner: &helmRunner{step: bare}}).Apply(
			context.Background(), controlplane.Plan{}, bare); err != nil {
			t.Errorf("an assignment with nothing to prepare required a preparer: %v", err)
		}
	})

	for name, access := range map[string]controlplane.RegistryAccess{
		"a credential reference alone": {AssignmentID: "primary", SourceRegistry: "registry.example",
			RuntimeRegistry: "registry.example", CredentialRef: "vault://platform/registries/mirror"},
		"a pull secret alone": {AssignmentID: "primary", SourceRegistry: "registry.example",
			RuntimeRegistry: "registry.example", ImagePullSecretName: "mirror-pull"},
	} {
		t.Run(name+" requires one", func(t *testing.T) {
			t.Parallel()
			needing := step
			needing.RegistryAccess = []controlplane.RegistryAccess{access}
			_, err := (HelmExecutor{Runner: &helmRunner{step: needing}}).Apply(
				context.Background(), controlplane.Plan{}, needing)
			if err == nil || !strings.Contains(err.Error(), "external credential preparer") {
				t.Errorf("%s was applied with no preparer configured: %v", name, err)
			}
		})
	}
}

// collectRenderedImages walks a rendered manifest looking for every image
// reference, and what it misses is never applied to the digest check -- an
// image it does not find is an image nobody verified against the Plan.
func TestCollectRenderedImagesFindsEveryFormAndSurvivesMalformedNodes(t *testing.T) {
	t.Parallel()
	collect := func(t *testing.T, document string) []string {
		t.Helper()
		var node yaml.Node
		if err := yaml.Unmarshal([]byte(document), &node); err != nil {
			t.Fatal(err)
		}
		var images []string
		collectRenderedImages(&node, &images)
		return images
	}

	t.Run("finds images at every depth and in both spellings", func(t *testing.T) {
		t.Parallel()
		images := collect(t, `
spec:
  template:
    spec:
      initContainers:
        - image: registry.example/init:1
      containers:
        - image: registry.example/app:2
      volumes:
        - image:
            reference: registry.example/assets:3
            pullPolicy: IfNotPresent
`)
		want := "registry.example/init:1,registry.example/app:2,registry.example/assets:3"
		if strings.Join(images, ",") != want {
			t.Errorf("images = %v, want %v", images, want)
		}
	})

	t.Run("an image mapping without a reference contributes nothing", func(t *testing.T) {
		t.Parallel()
		// A volume image that names only a pull policy has no reference to
		// check, and inventing one would fail the digest comparison against a
		// Plan that never mentioned it.
		images := collect(t, `
volumes:
  - image:
      pullPolicy: IfNotPresent
`)
		if len(images) != 0 {
			t.Errorf("images = %v, want none", images)
		}
	})

	t.Run("a key called image that is not an image is ignored", func(t *testing.T) {
		t.Parallel()
		// A sequence under "image" is neither spelling, and treating it as one
		// would put a node's own children into the verified set.
		images := collect(t, `
image:
  - registry.example/not-a-container:1
`)
		if len(images) != 0 {
			t.Errorf("images = %v, want none", images)
		}
	})

	t.Run("a value that happens to be the word image is not a key", func(t *testing.T) {
		t.Parallel()
		// Content alternates key, value. Walking one entry at a time instead
		// of two makes every value take a turn as a key -- so a field whose
		// *value* is "image" starts naming the field after it as a container
		// image, and an arbitrary string enters the set the digest check
		// runs over.
		images := collect(t, `
metadata:
  name: image
  namespace: example
`)
		if len(images) != 0 {
			t.Errorf("images = %v, want none: no key here is called image", images)
		}
	})

	t.Run("a value that happens to be the word reference is not a key", func(t *testing.T) {
		t.Parallel()
		images := collect(t, `
image:
  kind: reference
  pullPolicy: IfNotPresent
`)
		if len(images) != 0 {
			t.Errorf("images = %v, want none: reference is a value here, not a key", images)
		}
	})

	t.Run("an odd mapping is walked without panicking", func(t *testing.T) {
		t.Parallel()
		// A mapping node's Content alternates key and value, so a node with an
		// odd length is malformed. The bound is index+1 < len rather than
		// index < len precisely so a truncated document cannot index past the
		// end while the Agent is deciding what to trust.
		node := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "image"},
			{Kind: yaml.ScalarNode, Value: "registry.example/app:1"},
			{Kind: yaml.ScalarNode, Value: "dangling"},
		}}
		var images []string
		collectRenderedImages(node, &images)
		if strings.Join(images, ",") != "registry.example/app:1" {
			t.Errorf("images = %v, want the one complete pair", images)
		}

		// The same for the nested reference walk, and the dangling entry is
		// itself "reference" so the bound is what stops the walk rather than
		// the key comparison short-circuiting before it reads past the end.
		nested := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "image"},
			{Kind: yaml.MappingNode, Content: []*yaml.Node{
				{Kind: yaml.ScalarNode, Value: "reference"},
				{Kind: yaml.ScalarNode, Value: "registry.example/assets:2"},
				{Kind: yaml.ScalarNode, Value: "reference"},
			}},
		}}
		images = nil
		collectRenderedImages(nested, &images)
		if strings.Join(images, ",") != "registry.example/assets:2" {
			t.Errorf("nested images = %v", images)
		}
	})
}

// The Helm 4 plugin name is configurable, and when it is not configured the
// built-in name is used and the plugin is installed on the spot. The existing
// spelling test always sets PostRendererPlugin to the same value the default
// produces, so neither branch was distinguishable from the other.
func TestHelmExecutorPostRendererPluginNameDefaultsOrIsOverridden(t *testing.T) {
	t.Parallel()
	step := controlplane.PlanStep{
		InstallationID: "catalog", Entity: "catalog", Product: "com.palantir:catalog", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
	}

	t.Run("a configured plugin name is used as given", func(t *testing.T) {
		t.Parallel()
		// An Environment that ships its own post-renderer names it here, and
		// substituting the built-in name would run somebody else's plugin.
		runner := &helmRunner{step: step, major: 4}
		executor := HelmExecutor{Runner: runner, Environment: "prod-eu",
			PostRenderer: "/platform-spoke-agent", PostRendererPlugin: "environment-post-renderer"}
		if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		commands := fmt.Sprint(runner.commands)
		if !strings.Contains(commands, "--post-renderer environment-post-renderer") {
			t.Errorf("the configured plugin name was not used: %s", commands)
		}
		if strings.Contains(commands, "--post-renderer "+PostRendererPluginName) {
			t.Errorf("the built-in plugin name replaced the configured one: %s", commands)
		}
	})

	t.Run("an unconfigured plugin name takes the built-in one", func(t *testing.T) {
		t.Parallel()
		runner := &helmRunner{step: step, major: 4}
		executor := HelmExecutor{Runner: runner, Environment: "prod-eu",
			PostRenderer: "/platform-spoke-agent", Version: "1.2.3", PluginRoot: t.TempDir()}
		if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatal(err)
		}
		commands := fmt.Sprint(runner.commands)
		if !strings.Contains(commands, "--post-renderer "+PostRendererPluginName) {
			t.Errorf("the built-in plugin name was not used: %s", commands)
		}
	})

	t.Run("a plugin that cannot be installed leaves the render unrendered", func(t *testing.T) {
		t.Parallel()
		// Deliberately fail-open: the metadata a post-renderer stamps is not
		// worth failing a release for, so a plugin root that cannot be written
		// drops the flags rather than the deployment. Helm must still run.
		blocker := filepath.Join(t.TempDir(), "blocker")
		if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		runner := &helmRunner{step: step, major: 4}
		executor := HelmExecutor{Runner: runner, Environment: "prod-eu",
			PostRenderer: "/platform-spoke-agent", Version: "1.2.3",
			PluginRoot: filepath.Join(blocker, "plugins")}
		if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Fatalf("a plugin that could not be installed failed the release: %v", err)
		}
		commands := fmt.Sprint(runner.commands)
		if strings.Contains(commands, "--post-renderer") {
			t.Errorf("a post-renderer was passed despite the plugin not installing: %s", commands)
		}
		if !strings.Contains(commands, "upgrade") {
			t.Errorf("Helm did not run at all: %s", commands)
		}
	})
}

// The image pre-pull runs before Helm and can take a long time on a cold
// mirror, so it has its own budget rather than sharing the release timeout.
// Nothing observed either the configured value or the default.
func TestHelmExecutorBudgetsTheImagePrePull(t *testing.T) {
	t.Parallel()
	access := controlplane.RegistryAccess{
		AssignmentID: "primary", Revision: 4, SourceRegistry: "registry.example",
		RuntimeRegistry: "registry.example", CredentialRef: "vault://platform/registries/mirror",
	}
	base := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3",
		Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		RegistryAccess: []controlplane.RegistryAccess{access},
	}

	for name, test := range map[string]struct {
		seconds int
		want    time.Duration
	}{
		"unset takes fifteen minutes": {seconds: 0, want: 15 * time.Minute},
		"a configured budget is used": {seconds: 1800, want: 30 * time.Minute},
	} {
		step := base
		step.HelmExecution = controlplane.HelmExecutionPolicy{
			RolloutStrategy: controlplane.RolloutManageRollout, ImagePrePull: true,
			ImagePrePullTimeoutSeconds: test.seconds,
		}
		images := &imagePreparer{}
		executor := HelmExecutor{
			Runner: &helmRunner{step: step}, Registry: &registryPreparer{}, Images: images,
		}
		if _, err := executor.Apply(context.Background(), controlplane.Plan{}, step); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if images.budget != test.want {
			t.Errorf("%s: pre-pull budget = %v, want %v", name, images.budget, test.want)
		}
	}
}

// Rolling back on failure is specific to the no-wait strategy. Under a managed
// rollout Helm has already unwound the release itself, so rolling back again
// would uninstall a release that the Hub still believes is installed.
func TestHelmExecutorRollsBackOnFailureOnlyForNoWait(t *testing.T) {
	t.Parallel()
	base := controlplane.PlanStep{
		InstallationID: "foundation", Entity: "foundation", Namespace: "example",
		Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3",
			Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
	}

	for name, test := range map[string]struct {
		strategy     string
		wantRollback bool
	}{
		"no-wait rolls back": {strategy: controlplane.RolloutApplyChangesNoWait, wantRollback: true},
		"managed does not":   {strategy: controlplane.RolloutManageRollout},
	} {
		step := base
		step.HelmExecution = controlplane.HelmExecutionPolicy{RolloutStrategy: test.strategy}
		runner := &helmRunner{step: step, upgradeErr: fmt.Errorf("apply rejected")}
		if _, err := (HelmExecutor{Runner: runner}).Apply(
			context.Background(), controlplane.Plan{}, step); err == nil {
			t.Errorf("%s: a failing apply was reported as success", name)
			continue
		}
		// Structurally, not by substring: the release lookup passes
		// --uninstalled and --uninstalling, which both contain "uninstall".
		rolledBack := false
		for _, command := range runner.commands {
			if len(command) >= 2 && command[0] == "helm" &&
				(command[1] == "uninstall" || command[1] == "rollback") {
				rolledBack = true
			}
		}
		if rolledBack != test.wantRollback {
			t.Errorf("%s: rolled back = %v, want %v (%v)", name, rolledBack, test.wantRollback, runner.commands)
		}
	}
}
