package uds

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"gopkg.in/yaml.v3"
)

func TestPinnedCLINativeValuesPreserveReviewedTypes(t *testing.T) {
	if os.Getenv("CONDUIT_TEST_UDS_EXECUTOR_CLI") != "1" {
		t.Skip("set CONDUIT_TEST_UDS_EXECUTOR_CLI=1 with pinned ZARF_BIN")
	}
	zarf := os.Getenv("ZARF_BIN")
	if zarf == "" {
		t.Fatal("explicit pinned ZARF_BIN is required")
	}
	root := t.TempDir()
	definition := []byte(`apiVersion: zarf.dev/v1beta1
kind: ZarfPackageConfig
metadata:
  name: native-values-fixture
  version: 0.0.0
values:
  files: [values.yaml]
  schema: values.schema.json
components:
  - name: fixture
    manifests:
      - name: fixture
        files: [config.yaml]
        enableTemplating: true
`)
	writeCLIFile(t, filepath.Join(root, "zarf.yaml"), definition)
	writeCLIFile(t, filepath.Join(root, "values.yaml"), []byte("{}\n"))
	writeCLIFile(t, filepath.Join(root, "values.schema.json"), []byte(`{"type":"object","required":["IDENTIFIER","TEXT_FALSE","BOOL","COUNT"],"additionalProperties":false,"properties":{"IDENTIFIER":{"type":"string"},"TEXT_FALSE":{"type":"string"},"BOOL":{"type":"boolean"},"COUNT":{"type":"integer"}}}`))
	writeCLIFile(t, filepath.Join(root, "config.yaml"), []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: fixture
data:
  identifier: {{ .Values.IDENTIFIER | toString | toJson }}
  text: {{ .Values.TEXT_FALSE | toString | toJson }}
  enabled: {{ .Values.BOOL | toString | toJson }}
  count: {{ .Values.COUNT | toString | toJson }}
`))
	run := func(args ...string) []byte {
		t.Helper()
		out, err := (scopedRunner{}).Run(t.Context(), zarf, args...)
		if err != nil {
			t.Fatalf("native CLI: %v\n%s", err, out)
		}
		return out
	}
	run("package", "create", root, "--confirm", "--skip-sbom", "--architecture", runtime.GOARCH, "--output", root)
	archive := filepath.Join(root, "zarf-package-native-values-fixture-"+runtime.GOARCH+"-0.0.0.tar.zst")
	step := controlplane.PlanStep{Chart: controlplane.Artifact{Kind: controlplane.ArtifactKindZarfPackage, PackageName: "native-values-fixture"}, Values: json.RawMessage(`{"IDENTIFIER":"0012","TEXT_FALSE":"false","BOOL":false,"COUNT":12}`)}
	// The formatting helper is isolated from artifact authentication here.
	// The separate trust integration test exercises mandatory inspection refusal.
	executor := Executor{ZarfBin: zarf, VerificationKeyPath: "/unused-public-key"}
	command, cleanup, err := executor.prepareZarfDeployment(step, archive, definition)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	values := command[len(command)-1]
	if command[len(command)-2] != "--values" {
		t.Fatalf("missing native values file: %q", command)
	}
	// Schema admission proves these are a string, a string, a boolean and an
	// integer before their string representations reach ConfigMap data.
	manifest := run("package", "inspect", "manifests", archive, "--values", values, "--log-level", "error", "--no-color")
	var document struct {
		Data map[string]string `yaml:"data"`
	}
	if err := yaml.Unmarshal(manifest, &document); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"identifier": "0012", "text": "false", "enabled": "false", "count": "12"} {
		if document.Data[key] != value {
			t.Fatalf("native CLI changed %s: got %q, want %q", key, document.Data[key], value)
		}
	}
	if strings.Contains(strings.ToLower(string(manifest)), "deprecat") {
		t.Fatal("native fixture emitted a deprecation warning")
	}
}
