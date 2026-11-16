package agent

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/apollo/workloadmetadata"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// This test exercises the post-render chain against a real Helm binary rather
// than against a fake runner.
//
// It exists because the unit tests did not catch a real failure: they asserted
// the flags Platform builds and ran the mutation directly, so nothing observed
// Helm itself resolving --post-renderer. Helm 4 changed that flag from "path to
// an executable" to "name of a postrenderer/v1 plugin", and the break only
// surfaced in cluster acceptance, minutes into a run.
//
// It skips when helm is absent so it does not turn a missing tool into a
// failure, and it reads the installed Helm's major version rather than assuming
// one, so the same test covers both lanes CI builds.
func TestPostRenderChainAgainstRealHelm(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		// Skipping is right on a developer machine without helm, but in CI it
		// would restore exactly the blind spot this test exists to close, so CI
		// sets PLATFORM_REQUIRE_HELM and gets a failure instead.
		if os.Getenv("PLATFORM_REQUIRE_HELM") == "1" {
			t.Fatal("PLATFORM_REQUIRE_HELM is set but helm is not on PATH")
		}
		t.Skip("helm is not installed")
	}

	// Point the plugin at a stub that behaves like the agent's post-render
	// subcommand. Building the real binary here would make a unit test depend on
	// the whole command tree.
	root := t.TempDir()
	stub := filepath.Join(root, "agent-stub")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexec \"$0.real\" \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stub+".real", []byte(
		"#!/bin/sh\n# $1 is the subcommand, $2 the metadata file.\n"+
			"if [ \"$1\" != \""+PostRenderCommand+"\" ]; then echo \"bad subcommand: $1\" >&2; exit 2; fi\n"+
			"if [ ! -f \"$2\" ]; then echo \"missing metadata: $2\" >&2; exit 3; fi\n"+
			"sed 's/^kind: Deployment/kind: Deployment # stamped/'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	pluginRoot := filepath.Join(root, "plugins")
	t.Setenv("HELM_PLUGINS", "")
	if err := InstallPostRendererPlugin(pluginRoot, "9.9.9"); err != nil {
		t.Fatalf("install plugin: %v", err)
	}
	// InstallPostRendererPlugin points the manifest at the test binary; redirect
	// it at the stub, which is the only part this test cannot reuse verbatim.
	manifestPath := filepath.Join(pluginRoot, PostRendererPluginName, "plugin.yaml")
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	redirected := strings.Replace(string(manifest), executable, stub, 1)
	if redirected == string(manifest) {
		t.Fatalf("plugin manifest did not name this binary:\n%s", manifest)
	}
	if err := os.WriteFile(manifestPath, []byte(redirected), 0o644); err != nil {
		t.Fatal(err)
	}

	chart := filepath.Join(root, "chart")
	if err := os.MkdirAll(filepath.Join(chart, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chart, "Chart.yaml"),
		[]byte("apiVersion: v2\nname: probe\nversion: 0.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chart, "templates", "deployment.yaml"),
		[]byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: probe\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	metadata, err := workloadmetadata.ApolloWorkloadMetadataFor("prod-eu", "catalog", "com.palantir:catalog", "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(root, "metadata.json")
	if err := os.WriteFile(metadataPath, encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	major := helmMajorVersion(t, helm)
	arguments := []string{"template", "probe", chart}
	if major >= 4 {
		arguments = append(arguments, "--post-renderer", PostRendererPluginName, "--post-renderer-args", metadataPath)
	} else {
		arguments = append(arguments,
			"--post-renderer", stub, "--post-renderer-args", PostRenderCommand, "--post-renderer-args", metadataPath)
	}
	command := exec.Command(helm, arguments...)
	command.Env = append(os.Environ(), "HELM_PLUGINS="+os.Getenv("HELM_PLUGINS"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("helm %d template with post-renderer: %v\n%s", major, err, output)
	}
	if !strings.Contains(string(output), "# stamped") {
		t.Fatalf("helm %d did not run the post-renderer:\n%s", major, output)
	}

	// With the plugin installed, helm's machine-readable output must stay
	// parseable. A Helm that cannot load the plugin reports the failure on
	// stdout, which corrupts the JSON that release lookup is decoded from -- the
	// install then fails at `helm list`, nowhere near the post-renderer, with an
	// error that names neither.
	// helm env is used because it runs without a cluster and has a strict
	// KEY="value" line format, so any plugin-load noise on stdout shows up as a
	// line that does not match -- the same contamination that broke JSON
	// decoding, detectable offline.
	envCommand := exec.Command(helm, "env")
	envCommand.Env = append(os.Environ(), "HELM_PLUGINS="+os.Getenv("HELM_PLUGINS"))
	envOutput, err := envCommand.Output()
	if err != nil {
		t.Fatalf("helm env with the plugin installed: %v", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(envOutput)), "\n") {
		if !helmEnvLine.MatchString(line) {
			t.Fatalf("the installed plugin contaminated helm's stdout with %q\n%s", line, envOutput)
		}
	}
}

// helmEnvLine matches one KEY="value" line of `helm env` output.
var helmEnvLine = regexp.MustCompile(`^[A-Z0-9_]+="[^"]*"$`)

// helmVersionLine matches the version token Helm prints, e.g. "v3.16.3+gcfd0749"
// or "v4.1.1". It is anchored to a line so unrelated output cannot match.
var helmVersionLine = regexp.MustCompile(`(?m)^v?(\d+)\.\d+`)

// helmMajorVersion reports the installed Helm's major version.
//
// It reads stdout only and locates the version token by pattern rather than
// trusting the first line: Helm writes advisory warnings (for example, a
// group-readable kubeconfig) to stderr, and a previous implementation used
// CombinedOutput and compared the major *lexically*, so a leading "WARNING:..."
// parsed as a major of "WARNING: ..." which compares greater than "4". That made
// a Helm 3 look like a Helm 4, sending the test down the plugin lane that only
// Helm 4 supports. The comparison is numeric here for the same reason.
func helmMajorVersion(t *testing.T, helm string) int {
	t.Helper()
	command := exec.Command(helm, "version", "--short")
	var stderr strings.Builder
	command.Stderr = &stderr
	stdout, err := command.Output()
	if err != nil {
		t.Fatalf("helm version: %v\nstdout: %s\nstderr: %s", err, stdout, stderr.String())
	}
	match := helmVersionLine.FindSubmatch(stdout)
	if match == nil {
		t.Fatalf("unreadable helm version %q (stderr: %s)", stdout, stderr.String())
	}
	major, err := strconv.Atoi(string(match[1]))
	if err != nil {
		t.Fatalf("unreadable helm major version %q", match[1])
	}
	if major >= 4 {
		return 4
	}
	return 3
}

// Confirms against a real Helm binary that .Release.IsUpgrade actually changes
// what a chart renders, which is what makes the --is-upgrade flag on the
// validation render load-bearing rather than cosmetic. Without it, a chart that
// keys an image off IsUpgrade passes the declared-image check in its install
// form and then deploys its upgrade form unchecked.
func TestReleaseIsUpgradeChangesWhatAChartRenders(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		if os.Getenv("PLATFORM_REQUIRE_HELM") == "1" {
			t.Fatal("PLATFORM_REQUIRE_HELM is set but helm is not on PATH")
		}
		t.Skip("helm is not installed")
	}

	chart := t.TempDir()
	if err := os.WriteFile(filepath.Join(chart, "Chart.yaml"),
		[]byte("apiVersion: v2\nname: divergent\nversion: 0.1.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(chart, "templates"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chart, "templates", "deployment.yaml"),
		[]byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: app\nspec:\n"+
			"  template:\n    spec:\n      containers:\n        - name: app\n"+
			"          image: {{ if .Release.IsUpgrade }}registry.example/undeclared:9.9.9@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee{{ else }}registry.example/declared:1.0.0@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd{{ end }}\n"),
		0o600); err != nil {
		t.Fatal(err)
	}

	// Read stdout only: the rendered manifests are parsed as YAML below, and Helm
	// writes advisory warnings (for example, a group-readable kubeconfig) to
	// stderr. Folding those in with CombinedOutput prepends non-YAML lines to the
	// document and the parse fails with an error that names neither Helm nor the
	// warning.
	render := func(extra ...string) string {
		arguments := append([]string{"template", "app", chart}, extra...)
		command := exec.Command(helm, arguments...)
		var stderr strings.Builder
		command.Stderr = &stderr
		stdout, err := command.Output()
		if err != nil {
			t.Fatalf("helm %v: %v\nstdout: %s\nstderr: %s", arguments, err, stdout, stderr.String())
		}
		return string(stdout)
	}

	install, upgrade := render(), render("--is-upgrade")
	if !strings.Contains(install, "registry.example/declared:1.0.0@sha256:"+strings.Repeat("d", 64)) {
		t.Fatalf("install render lacks the declared image:\n%s", install)
	}
	if !strings.Contains(upgrade, "registry.example/undeclared:9.9.9@sha256:"+strings.Repeat("e", 64)) {
		t.Fatalf("--is-upgrade did not change the render, so the flag would be inert:\n%s", upgrade)
	}

	// The gap this closes: validating the install render would have admitted the
	// undeclared image that the upgrade actually deploys.
	declared := []controlplane.Artifact{{
		Repository: "registry.example/declared", Tag: "1.0.0", Digest: "sha256:" + strings.Repeat("d", 64),
	}}
	if err := validateRenderedImages([]byte(install), declared); err != nil {
		t.Fatalf("install render should satisfy the declared set: %v", err)
	}
	if err := validateRenderedImages([]byte(upgrade), declared); err == nil {
		t.Fatal("the upgrade render deploys an undeclared image and must be rejected")
	}
}
