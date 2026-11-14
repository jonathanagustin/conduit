package conduitcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"go.yaml.in/yaml/v4"
)

func TestSplitOptionalVersion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		coordinate string
		module     string
		version    string
		wantError  bool
	}{
		{coordinate: "example-foundation", module: "example-foundation"},
		{coordinate: "example-foundation:1.2.3", module: "example-foundation", version: "1.2.3"},
		{coordinate: "module:example-foundation", module: "example-foundation"},
		{coordinate: "module:example-foundation:1.2.3", module: "example-foundation", version: "1.2.3"},
		{coordinate: "", wantError: true},
		{coordinate: "module::1.2.3", wantError: true},
		{coordinate: "group:name:1.2.3", wantError: true},
	} {
		test := test
		t.Run(test.coordinate, func(t *testing.T) {
			t.Parallel()
			module, version, err := splitOptionalVersion(test.coordinate)
			if test.wantError {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if module != test.module || version != test.version {
				t.Fatalf("split %q as module %q version %q", test.coordinate, module, version)
			}
		})
	}
}

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func mustYAML(t *testing.T, value any) string {
	t.Helper()
	encoded, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestEnvironmentDescriptorIsGitOwnedAndStable(t *testing.T) {
	t.Parallel()
	descriptor, err := newEnvironmentDescriptor("dev-use2", "release-team")
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Environment.ID != "dev-use2" || descriptor.Environment.Generation != "1" ||
		descriptor.ContactTeam != "release-team" || descriptor.Kind != "EnvironmentDescriptor" {
		t.Fatalf("descriptor=%#v", descriptor)
	}
	if _, err := newEnvironmentDescriptor("Dev/Use2", "release-team"); err == nil {
		t.Fatal("invalid Environment ID was accepted")
	}

	// An Environment with no owning team is a state Apollo publishes and
	// Conduit's profile model already accepts, so authoring one must succeed
	// with an empty ContactTeam rather than being rejected here.
	ownerless, err := newEnvironmentDescriptor("dev-use2", "")
	if err != nil {
		t.Fatalf("ownerless Environment was rejected: %v", err)
	}
	if ownerless.ContactTeam != "" || ownerless.Environment.ID != "dev-use2" {
		t.Fatalf("descriptor=%#v", ownerless)
	}
}

// The descriptor this command authors has to carry the API group the control
// plane enforces. It carried a transposed "delivery.platform/v1alpha1" instead:
// nothing consumes EnvironmentDescriptor yet, so nothing failed, but the
// command writes a document a human commits and edits, and Validate would
// reject that document verbatim once anything read it. The guard runs the
// authored apiVersion through Validate rather than comparing it to a second
// copy of the string, because a second copy is exactly how the two came apart.
func TestEnvironmentDescriptorCarriesTheEnforcedAPIGroup(t *testing.T) {
	t.Parallel()
	descriptor, err := newEnvironmentDescriptor("dev-use2", "release-team")
	if err != nil {
		t.Fatal(err)
	}
	for _, failure := range controlplane.Validate(controlplane.DesiredState{
		APIVersion: descriptor.APIVersion, Kind: "EnvironmentDeployment", Environment: descriptor.Environment,
	}) {
		if failure.Path == "apiVersion" {
			t.Fatalf("the control plane rejects the authored apiVersion %q: %s", descriptor.APIVersion, failure.Message)
		}
	}
}

func TestGenerateSpokeManifestRequiresDigestAndExternalIdentity(t *testing.T) {
	t.Parallel()
	manifest, err := generateSpokeManifest(spokeManifestInput{
		Environment: "dev-use2", Namespace: "platform-spoke", Image: "registry.example/platform/agent:1.0.0@" + testDigest,
		HubURL: "https://hub.example", ClientTLSSecret: "spoke-client", ExternalCAConfigMap: "platform-ca",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded := mustYAML(t, manifest)
	for _, expected := range []string{"kind: Deployment", "runAsNonRoot: true", "readOnlyRootFilesystem: true", "spoke-client", "platform-ca"} {
		if !strings.Contains(encoded, expected) {
			t.Fatalf("manifest omitted %q:\n%s", expected, encoded)
		}
	}
	if _, err := generateSpokeManifest(spokeManifestInput{
		Environment: "dev-use2", Namespace: "platform-spoke", Image: "registry.example/platform/agent:latest",
		HubURL: "https://hub.example", ClientTLSSecret: "spoke-client", ExternalCAConfigMap: "platform-ca",
	}); err == nil {
		t.Fatal("mutable image reference was accepted")
	}
}

func TestInitializeHelmChartManifestDiscoversDigestAndNeverOverwrites(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	values := filepath.Join(root, "input-values.yaml")
	if err := os.WriteFile(values, []byte("image:\n  repository: registry.example/application\n  tag: 1.2.3\n  digest: "+testDigest+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "release")
	options := helmChartInitOptions{
		RepositoryURL: "oci://registry.example/charts/application:1.2.3@" + testDigest, Coordinate: "com.example:application:2.0.0",
		Name: "application", Version: "1.2.3", ValuesPath: values, OutputDir: output,
	}
	result, err := initializeHelmChartManifest(options)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readProductManifest(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProductVersion != "2.0.0" || manifest.Extensions.HelmChart.Version != "1.2.3" ||
		len(manifest.Extensions.Artifacts) != 1 || !strings.HasSuffix(manifest.Extensions.Artifacts[0].URI, "@"+testDigest) {
		t.Fatalf("manifest=%#v", manifest)
	}
	if _, err := initializeHelmChartManifest(options); err == nil || !strings.Contains(err.Error(), "without overwriting") {
		t.Fatalf("second initialization error=%v", err)
	}
}

func TestInitializeHelmChartManifestAsTGZUsesCanonicalProductRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	values := filepath.Join(root, "values.yaml")
	schema := filepath.Join(root, "values.schema.json")
	if err := os.WriteFile(values, []byte("replicaCount: 3\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(schema, []byte(`{"type":"object"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := initializeHelmChartManifest(helmChartInitOptions{
		RepositoryURL: "oci://registry.example/charts/application:7.4.1@" + testDigest,
		Coordinate:    "com.example:application:2.0.0", Name: "application", Version: "7.4.1",
		ValuesPath: values, SchemaPath: schema, OutputDir: filepath.Join(root, "out"),
		Artifacts: []string{"registry.example/application:2.0.0@" + testDigest}, AsTGZ: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result.ArchivePath) != "application-2.0.0.config.tgz" || result.ManifestPath != "" || result.ValuesPath != "" {
		t.Fatalf("result=%#v", result)
	}
	bundle, err := decodeHelmProductTGZ(result.ArchivePath)
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.ProductVersion != "2.0.0" || bundle.Manifest.Extensions.HelmChart.Version != "7.4.1" {
		t.Fatalf("bundle=%#v", bundle)
	}
}

func TestInitializeHelmChartManifestFromChartPathDerivesChartMetadataAndSchema(t *testing.T) {
	t.Parallel()
	chart := filepath.Join(t.TempDir(), "application")
	if err := os.MkdirAll(chart, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Chart.yaml": `apiVersion: v2
name: application
version: 7.4.1
description: ignored standard Helm metadata
type: application
`,
		"values.yaml":        "image:\n  repository: registry.example/application\n  tag: 2.0.0\n  digest: " + testDigest + "\n",
		"values.schema.json": `{"type":"object"}`,
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(chart, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := initializeHelmChartManifest(helmChartInitOptions{
		RepositoryURL: "oci://registry.example/charts/application:7.4.1@" + testDigest,
		Coordinate:    "com.example:application:2.0.0", ChartPath: chart, OutputDir: filepath.Join(t.TempDir(), "release"),
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := readProductManifest(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.ProductVersion != "2.0.0" || manifest.Extensions.HelmChart.Name != "application" ||
		manifest.Extensions.HelmChart.Version != "7.4.1" || filepath.Base(result.SchemaPath) != "values.schema.json" {
		t.Fatalf("result=%#v manifest=%#v", result, manifest)
	}
}

func TestResolveHelmChartInitSourceRejectsAmbiguousOrNonInstallableCharts(t *testing.T) {
	t.Parallel()
	newChart := func(t *testing.T, chartYAML string, withValues bool) string {
		t.Helper()
		chart := t.TempDir()
		if err := os.WriteFile(filepath.Join(chart, "Chart.yaml"), []byte(chartYAML), 0o600); err != nil {
			t.Fatal(err)
		}
		if withValues {
			if err := os.WriteFile(filepath.Join(chart, "values.yaml"), []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return chart
	}
	valid := "apiVersion: v2\nname: application\nversion: 1.2.3\n"
	tests := []struct {
		name    string
		options helmChartInitOptions
		want    string
	}{
		{"ambiguous fields", helmChartInitOptions{ChartPath: newChart(t, valid, true), Name: "override"}, "cannot be combined"},
		{"library chart", helmChartInitOptions{ChartPath: newChart(t, valid+"type: library\n", true)}, "not installable"},
		{"legacy chart API", helmChartInitOptions{ChartPath: newChart(t, "apiVersion: v1\nname: application\nversion: 1.2.3\n", true)}, "apiVersion must be v2"},
		{"missing values", helmChartInitOptions{ChartPath: newChart(t, valid, false)}, "values.yaml"},
		{"file not directory", helmChartInitOptions{ChartPath: filepath.Join(newChart(t, valid, true), "Chart.yaml")}, "directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := resolveHelmChartInitSource(test.options); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error=%v, want %q", err, test.want)
			}
		})
	}
}

// The generated Deployment is what a Spoke is bootstrapped from, so the parts
// that vary with input are worth pinning: an unset Agent ID takes the
// Environment's name, and the storage-class annotation exists only when asked
// for. Neither is visible in a manifest that renders successfully.
func TestGenerateSpokeManifestDefaultsAgentIDAndOmitsEmptyAnnotations(t *testing.T) {
	t.Parallel()
	base := spokeManifestInput{
		Environment: "dev-use2", Namespace: "platform-spoke",
		Image:  "registry.example/platform/agent:1.0.0@" + testDigest,
		HubURL: "https://hub.example", ClientTLSSecret: "spoke-client", ExternalCAConfigMap: "platform-ca",
	}

	podMetadata := func(t *testing.T, input spokeManifestInput) map[string]any {
		t.Helper()
		manifest, err := generateSpokeManifest(input)
		if err != nil {
			t.Fatal(err)
		}
		for _, document := range manifest["items"].([]any) {
			object := document.(map[string]any)
			if object["kind"] != "Deployment" {
				continue
			}
			template := object["spec"].(map[string]any)["template"].(map[string]any)
			return template["metadata"].(map[string]any)
		}
		t.Fatal("no Deployment in the generated manifest")
		return nil
	}

	t.Run("an unset Agent ID becomes the Environment", func(t *testing.T) {
		t.Parallel()
		// The Agent ID is what the Hub leases work to. Defaulting it to
		// something other than the Environment would hand a Spoke work
		// belonging to a different one.
		manifest, err := generateSpokeManifest(base)
		if err != nil {
			t.Fatal(err)
		}
		if encoded := mustYAML(t, manifest); !strings.Contains(encoded, "dev-use2") {
			t.Errorf("manifest does not carry the Environment as the Agent ID:\n%s", encoded)
		}
	})

	t.Run("an explicit Agent ID is kept", func(t *testing.T) {
		t.Parallel()
		explicit := base
		explicit.AgentID = "spoke-alpha"
		encoded := mustYAML(t, func() map[string]any {
			manifest, err := generateSpokeManifest(explicit)
			if err != nil {
				t.Fatal(err)
			}
			return manifest
		}())
		if !strings.Contains(encoded, "spoke-alpha") {
			t.Errorf("an explicitly set Agent ID was replaced:\n%s", encoded)
		}
	})

	t.Run("no storage class means no annotations block", func(t *testing.T) {
		t.Parallel()
		// An empty annotations map serialises as "annotations: {}", which is
		// noise in a manifest people read and diff.
		metadata := podMetadata(t, base)
		if _, present := metadata["annotations"]; present {
			t.Errorf("pod metadata carries an annotations block with nothing in it: %#v", metadata)
		}
	})

	t.Run("a storage class is annotated", func(t *testing.T) {
		t.Parallel()
		withClass := base
		withClass.StorageClass = "gp3"
		annotations, present := podMetadata(t, withClass)["annotations"].(map[string]any)
		if !present {
			t.Fatal("a configured storage class produced no annotation")
		}
		if annotations["delivery.platform/k8s-storage-class"] != "gp3" {
			t.Errorf("annotations = %#v", annotations)
		}
	})
}

// The values file is copied into the output directory unless it is already
// that file. writeNewFile refuses to overwrite, so initializing in place --
// pointing --values at the output directory's own values.yaml -- would fail
// on a file it was about to write identically.
func TestInitializeHelmChartManifestHandlesValuesAlreadyInPlace(t *testing.T) {
	t.Parallel()
	const contents = "image:\n  repository: registry.example/application\n  tag: 1.2.3\n  digest: " + testDigest + "\n"
	options := func(valuesPath, outputDir string) helmChartInitOptions {
		return helmChartInitOptions{
			RepositoryURL: "oci://registry.example/charts/application:1.2.3@" + testDigest,
			Coordinate:    "com.example:application:1.2.3",
			Name:          "application", Version: "1.2.3",
			ValuesPath: valuesPath, OutputDir: outputDir,
		}
	}

	t.Run("values already at the destination are left alone", func(t *testing.T) {
		t.Parallel()
		output := filepath.Join(t.TempDir(), "release")
		if err := os.MkdirAll(output, 0o755); err != nil {
			t.Fatal(err)
		}
		inPlace := filepath.Join(output, "values.yaml")
		if err := os.WriteFile(inPlace, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := initializeHelmChartManifest(options(inPlace, output)); err != nil {
			t.Fatalf("initializing with values already in place failed: %v", err)
		}
		// Left exactly as found, not rewritten.
		after, err := os.ReadFile(inPlace)
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != contents {
			t.Errorf("values file was rewritten:\n%s", after)
		}
	})

	t.Run("values from elsewhere are copied in", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		source := filepath.Join(root, "input-values.yaml")
		if err := os.WriteFile(source, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(root, "release")
		result, err := initializeHelmChartManifest(options(source, output))
		if err != nil {
			t.Fatal(err)
		}
		// Without the copy the release directory has a manifest naming a
		// values file that is not there.
		if _, err := os.Stat(result.ValuesPath); err != nil {
			t.Errorf("values were not written into the output directory: %v", err)
		}
	})
}
