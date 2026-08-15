package conduitcli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Apollo added --values and --values-schema to `product-release create` in
// apollo-cli 0.811.0. Conduit had the same two inputs under --default-config
// and --config-schema, so an operator following Apollo's documentation met
// "unknown flag" on a path that already worked.

const valuesFlagsDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func writeValuesFlagFixture(t *testing.T, productType string) (manifestPath, values, schema string) {
	t.Helper()
	directory := t.TempDir()
	manifestPath = filepath.Join(directory, "manifest.yml")
	values, schema = filepath.Join(directory, "values.yaml"), filepath.Join(directory, "values.schema.json")
	manifest := `manifest-version: "1.0"
product-group: com.example
product-name: application
product-version: 2.0.0
product-type: ` + productType + `
extensions:
  helm-chart:
    helm-chart-name: application
    helm-chart-version: 7.4.1
    helm-repository-url: oci://registry.example/charts/application:7.4.1@sha256:` + valuesFlagsDigest + `
  artifacts:
    - type: oci
      uri: registry.example/application:2.0.0@sha256:` + valuesFlagsDigest + `
`
	for path, contents := range map[string]string{
		manifestPath: manifest, values: "replicaCount: 3\n", schema: `{"type":"object"}`,
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return manifestPath, values, schema
}

func runValuesFlagCreate(t *testing.T, arguments ...string) (*fakeService, error) {
	t.Helper()
	service := &fakeService{}
	var output bytes.Buffer
	root := NewRoot(testRuntime(&output, service))
	root.SetArgs(append([]string{
		"--server", "http://127.0.0.1:8081", "--development-subject", "developer",
		"product-release", "create",
	}, arguments...))
	return service, root.Execute()
}

// TestProductReleaseCreateAcceptsApolloValuesSpelling is the parity itself:
// Apollo's flag names must reach the same bundle Conduit's own names do.
func TestProductReleaseCreateAcceptsApolloValuesSpelling(t *testing.T) {
	t.Parallel()
	manifestPath, values, schema := writeValuesFlagFixture(t, "helm-chart.v1")
	service, err := runValuesFlagCreate(t,
		"--manifest", manifestPath, "--values", values, "--values-schema", schema)
	if err != nil {
		t.Fatal(err)
	}
	if string(service.helmBundle.DefaultConfig) != "replicaCount: 3\n" ||
		string(service.helmBundle.ConfigSchema) != `{"type":"object"}` {
		t.Fatalf("bundle=%#v", service.helmBundle)
	}
}

// TestProductReleaseCreateRefusesBothSpellingsOfOneFlag guards the case that
// would otherwise be resolved by discarding one of two files the operator
// explicitly named.
func TestProductReleaseCreateRefusesBothSpellingsOfOneFlag(t *testing.T) {
	t.Parallel()
	manifestPath, values, schema := writeValuesFlagFixture(t, "helm-chart.v1")
	for name, arguments := range map[string][]string{
		"values and default-config": {"--values", values, "--default-config", values},
		"values-schema and config-schema": {
			"--values-schema", schema, "--config-schema", schema,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := runValuesFlagCreate(t, append([]string{"--manifest", manifestPath}, arguments...)...)
			if err == nil {
				t.Fatal("accepted two spellings of one flag, so one of the two files was dropped silently")
			}
			if !strings.Contains(err.Error(), "the same flag") {
				t.Fatalf("error does not say why: %v", err)
			}
		})
	}
}

// TestProductReleaseCreateRefusesHelmValuesOnTerraformProduct covers the
// behaviour Apollo published alongside the flags, and a defect that predates
// them: the Terraform path never builds the Helm bundle, so a values file
// passed with a terraform.v1 manifest was accepted and then dropped.
func TestProductReleaseCreateRefusesHelmValuesOnTerraformProduct(t *testing.T) {
	t.Parallel()
	manifestPath, values, schema := writeValuesFlagFixture(t, "terraform.v1")
	for name, arguments := range map[string][]string{
		"apollo spelling":  {"--values", values},
		"conduit spelling": {"--default-config", values},
		"schema":           {"--values-schema", schema},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := runValuesFlagCreate(t, append([]string{"--manifest", manifestPath}, arguments...)...)
			if err == nil {
				t.Fatal("published a Terraform Release while silently dropping the Helm configuration")
			}
			if !strings.Contains(err.Error(), "terraform.v1") {
				t.Fatalf("error does not name the product type: %v", err)
			}
		})
	}
}
