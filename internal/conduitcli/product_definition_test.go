package conduitcli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const definitionManifest = `manifest-version: "1.0"
product-group: com.example.services
product-name: catalog
product-version: 3.1.0
product-type: service.v1
extensions:
  volumes-v2:
    data:
      volume-type:
        durable-volume: {}
  public-proxy-endpoints:
    - https-api
  require-stable-hostname: true
`

const definitionConfiguration = `resources:
  requests:
    cpu: 0.5
    memory: 1Gi
replication:
  desired: 2
volumes:
  data:
    durable-storage-configuration:
      desired-size: 40G
endpoints:
  definitions:
    https-api:
      desired-port: 8443
      path: /catalog
  sls-status-endpoint: https-api
generated_secrets:
  db-password:
    secret-type: random_string
    length: 32
conf:
  password: '{{secret("db-password")}}'
  dataDirectory: '{{volumes.data.PathOnDisk}}'
managed_files:
  var/conf/runtime.yml:
    type: yaml
    reload-type: runtime
    content:
      port: '{{endpoints.definitions.https-api.desired-port}}'
`

func writeDefinitionFiles(t *testing.T, manifest, configuration string) (string, string) {
	t.Helper()
	directory := t.TempDir()
	manifestPath := filepath.Join(directory, "manifest.yml")
	configurationPath := filepath.Join(directory, "configuration.yml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configurationPath, []byte(configuration), 0o600); err != nil {
		t.Fatal(err)
	}
	return manifestPath, configurationPath
}

func runConduit(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var output bytes.Buffer
	root := NewRoot(Runtime{Stdout: &output, Stderr: &output, Getenv: func(string) string { return "" }})
	root.SetArgs(args)
	root.SilenceUsage, root.SilenceErrors = true, true
	err := root.Execute()
	return output.String(), err
}

func TestConduitVerifiesAConformantProductDefinition(t *testing.T) {
	t.Parallel()
	manifestPath, configurationPath := writeDefinitionFiles(t, definitionManifest, definitionConfiguration)
	output, err := runConduit(t, "product-release", "definition", "verify",
		"--manifest", manifestPath, "--configuration", configurationPath, "--print-definition")
	if err != nil {
		t.Fatalf("verify failed: %v (%s)", err, output)
	}
	var result struct {
		Valid       bool   `json:"valid"`
		ProductType string `json:"productType"`
		Definition  struct {
			Configuration struct {
				GeneratedSecrets map[string]struct {
					SecretType string `json:"secretType"`
				} `json:"generatedSecrets"`
			} `json:"configuration"`
		} `json:"definition"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("%v (%s)", err, output)
	}
	if !result.Valid || result.ProductType != "service.v1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Definition.Configuration.GeneratedSecrets["db-password"].SecretType != "random_string" {
		t.Fatalf("definition = %+v", result.Definition)
	}
}

// A manifest with no configuration.yml is still a definition: a Product that
// takes no installation configuration must not be forced to ship an empty file.
func TestConduitVerifiesADefinitionWithoutConfiguration(t *testing.T) {
	t.Parallel()
	manifest := `manifest-version: "1.0"
product-group: com.example.services
product-name: catalog
product-version: 3.1.0
product-type: service.v1
`
	manifestPath, _ := writeDefinitionFiles(t, manifest, "")
	if _, err := runConduit(t, "product-release", "definition", "verify", "--manifest", manifestPath); err != nil {
		t.Fatal(err)
	}
}

func TestConduitRejectsAnInconsistentProductDefinition(t *testing.T) {
	t.Parallel()
	configuration := strings.Replace(definitionConfiguration,
		`'{{secret("db-password")}}'`, `'{{secret("absent")}}'`, 1)
	manifestPath, configurationPath := writeDefinitionFiles(t, definitionManifest, configuration)
	output, err := runConduit(t, "product-release", "definition", "verify",
		"--manifest", manifestPath, "--configuration", configurationPath)
	if err == nil {
		t.Fatalf("expected a validation failure, got %s", output)
	}
	if !strings.Contains(err.Error(), "configuration.conf.password") ||
		!strings.Contains(err.Error(), "generated_secrets") {
		t.Fatalf("error = %v", err)
	}
}

func TestApolloProductDefinitionSchemaIsExposedAndAgreesWithTheValidator(t *testing.T) {
	t.Parallel()
	output, err := runConduit(t, "product-release", "definition", "schema")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(output) != strings.TrimSpace(string(apolloProductDefinitionSchema)) {
		t.Fatal("definition schema output differs from the embedded contract")
	}
	var schema map[string]any
	if err := json.Unmarshal(apolloProductDefinitionSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["$schema"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Fatalf("schema dialect = %v", schema["$schema"])
	}
	// The enums a Product author reads out of the schema have to be the same
	// set the Go validator enforces. Drift here means the CLI advertises a
	// vocabulary the control plane rejects.
	for _, expectation := range []struct {
		pointer []string
		values  []string
	}{
		{[]string{"$defs", "manifest", "properties", "product-type"}, []string{
			controlplane.ApolloProductTypeAsset, controlplane.ApolloProductTypeDaemonSet,
			controlplane.ApolloProductTypeHelm, controlplane.ApolloProductTypeHelmChart,
			controlplane.ApolloProductTypeService, controlplane.ApolloProductTypeServiceV2,
			controlplane.ApolloProductTypeTerraform,
		}},
		{[]string{"$defs", "managedFile", "properties", "type"},
			[]string{
				controlplane.ApolloManagedFileTypeJSON, controlplane.ApolloManagedFileTypeTemplate,
				controlplane.ApolloManagedFileTypeYAML, controlplane.ApolloManagedFileTypeYML,
			}},
		{[]string{"$defs", "managedFile", "properties", "reload-type"},
			[]string{controlplane.ApolloReloadTypeInstall, controlplane.ApolloReloadTypeRuntime}},
		{[]string{"$defs", "configuration", "properties", "generated_secrets", "additionalProperties", "properties", "secret-type"},
			[]string{
				controlplane.ApolloSecretTypeECPKCS8, controlplane.ApolloSecretTypeRSAPKCS8,
				controlplane.ApolloSecretTypeRandomBytes, controlplane.ApolloSecretTypeRandomString,
				controlplane.ApolloSecretTypeSTIGPassword,
			}},
		{[]string{"$defs", "extensions", "properties", "quorum-requirement"},
			[]string{
				controlplane.ApolloQuorumAll, controlplane.ApolloQuorumAllButOne,
				controlplane.ApolloQuorumAny, controlplane.ApolloQuorumSimpleMajority,
			}},
		{[]string{"$defs", "extensions", "properties", "upgrade-strategy"},
			[]string{controlplane.ApolloUpgradeStrategyRolling, controlplane.ApolloUpgradeStrategyShutdown}},
	} {
		actual := schemaEnum(t, schema, expectation.pointer)
		expected := append([]string(nil), expectation.values...)
		sort.Strings(expected)
		if strings.Join(actual, ",") != strings.Join(expected, ",") {
			t.Fatalf("%s enum = %v, validator accepts %v", strings.Join(expectation.pointer, "/"), actual, expected)
		}
	}
}

func schemaEnum(t *testing.T, schema map[string]any, pointer []string) []string {
	t.Helper()
	current := any(schema)
	for _, segment := range pointer {
		object, ok := current.(map[string]any)
		if !ok {
			t.Fatalf("schema path %s is not an object", strings.Join(pointer, "/"))
		}
		current, ok = object[segment]
		if !ok {
			t.Fatalf("schema path %s does not exist", strings.Join(pointer, "/"))
		}
	}
	object, ok := current.(map[string]any)
	if !ok {
		t.Fatalf("schema path %s is not an object", strings.Join(pointer, "/"))
	}
	values, ok := object["enum"].([]any)
	if !ok {
		t.Fatalf("schema path %s has no enum", strings.Join(pointer, "/"))
	}
	enum := make([]string, 0, len(values))
	for _, value := range values {
		enum = append(enum, value.(string))
	}
	sort.Strings(enum)
	return enum
}

// The volume size grammar is the acceptance gate for durable-volume
// declarations, so the schema an author reads and the validator that admits
// their document have to agree about it — including where they deliberately do
// not.
func TestVolumeSizeGrammarAgreesBetweenSchemaAndValidator(t *testing.T) {
	t.Parallel()
	var schema map[string]any
	if err := json.Unmarshal(apolloProductDefinitionSchema, &schema); err != nil {
		t.Fatal(err)
	}
	defs := schema["$defs"].(map[string]any)
	pattern := defs["volumeSize"].(map[string]any)["pattern"].(string)
	sizePattern, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("schema volumeSize pattern does not compile: %v", err)
	}

	const manifest = `manifest-version: "1.0"
product-group: com.example.services
product-name: catalog
product-version: 3.1.0
product-type: service.v1
extensions:
  volumes-v2:
    data:
      volume-type:
        durable-volume: {}
`
	validatorAccepts := func(size string) bool {
		configuration := "volumes:\n  data:\n    durable-storage-configuration:\n      desired-size: " + size + "\n"
		_, errs, err := controlplane.DecodeApolloProductDefinition(
			strings.NewReader(manifest), strings.NewReader(configuration))
		if err != nil {
			return false
		}
		for _, item := range errs {
			if strings.HasPrefix(item.Path, "configuration.volumes.data.durable-storage-configuration") {
				return false
			}
		}
		return true
	}

	for _, size := range []string{"1M", "500M", "40G", "1T", "10T", "10000G"} {
		if !sizePattern.MatchString(size) || !validatorAccepts(size) {
			t.Fatalf("%s must be accepted by both schema (%t) and validator (%t)",
				size, sizePattern.MatchString(size), validatorAccepts(size))
		}
	}
	for _, size := range []string{"0G", "1.5G", "1Gi", "1K", "1P", "1024", "11T"} {
		if sizePattern.MatchString(size) || validatorAccepts(size) {
			t.Fatalf("%s must be refused by both schema (%t) and validator (%t)",
				size, sizePattern.MatchString(size), validatorAccepts(size))
		}
	}
	// The one documented divergence: a regular expression cannot compare
	// magnitudes across units, so the schema bounds terabyte values only and the
	// validator catches the same excess written in gigabytes. The schema says so
	// in its own description; this pins that the split is where it claims.
	const ceilingInSmallerUnit = "10001G"
	if !sizePattern.MatchString(ceilingInSmallerUnit) {
		t.Fatal("the schema pattern is expected to admit an over-ceiling gigabyte value")
	}
	if validatorAccepts(ceilingInSmallerUnit) {
		t.Fatal("the validator must reject an over-ceiling value written in gigabytes")
	}
	description := defs["volumeSize"].(map[string]any)["description"].(string)
	if !strings.Contains(description, controlplane.ApolloMaximumVolumeSize) ||
		!strings.Contains(description, ceilingInSmallerUnit) {
		t.Fatalf("the schema must document where it stops enforcing the ceiling: %q", description)
	}
}
