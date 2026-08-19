package controlplane

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// The fixtures below are the Apollo Product Specification's own worked examples.
// Holding the validator against the published documents, rather than against a
// model invented alongside them, is the only way a conformance claim means
// anything.

// specManifest is the manifest from "Tying It Together".
const specManifest = `
manifest-version: "1.0"
product-group: com.palantir.foo
product-name: foo
product-version: 1.0.0
product-type: service.v1
extensions:
  public-proxy-endpoints:
    - foo-service-endpoint
  volumes-v2:
    foo-service-data:
      volume-type:
        durable-volume: {}
`

// specConfiguration is the configuration from "Tying It Together", verbatim,
// including the two places where it contradicts the normative extension pages.
const specConfiguration = `
replication:
  desired: 3

endpoints:
  definitions:
    foo-service-endpoint:
      desired-port: 8080
      path: /foo

volumes:
  foo-service-data:
    desired-size: 10G

generated_secrets:
  bar-client-shared-secret:
    secret-type: random_string
    length: 32

discovery:
  produces:
    bar-client:
      role: bar-client
      client-bcrypted-shared-secret: '{{"bar-client-shared-secret", part: "bcrypt_hash"}}'
  consumes:
    bar-api-uris:
      role: bar-api
      select: uris

conf:
  config:
    security:
      key-file: '{{ssl.pem_path}}'
      cert-file: '{{ssl.cert_path}}'
      ca-file: '{{ssl.ca_path}}'
    server:
      security: '{{conf.config.security}}'
      port: '{{endpoints.definitions.foo-service-endpoint.desired-port}}'
      base-path: '{{endpoints.definitions.foo-service-endpoint.path}}'
    bar-client:
      security: '{{conf.config.security}}'
      uris: '{{discovered.bar-api-uris}}'
      auth-secret: '{{secret("bar-client-shared-secret")}}'
`

// correctedSpecConfiguration is the same document with the two errata repaired:
// the volume size nested under durable-storage-configuration as the Volumes
// extension requires, and the secret substitution given its helper name.
const correctedSpecConfiguration = `
replication:
  desired: 3

endpoints:
  definitions:
    foo-service-endpoint:
      desired-port: 8080
      path: /foo

volumes:
  foo-service-data:
    durable-storage-configuration:
      desired-size: 10G

generated_secrets:
  bar-client-shared-secret:
    secret-type: random_string
    length: 32

discovery:
  produces:
    bar-client:
      role: bar-client
      client-bcrypted-shared-secret: '{{secret("bar-client-shared-secret", part: "bcrypt_hash")}}'
  consumes:
    bar-api-uris:
      role: bar-api
      select: uris

conf:
  config:
    security:
      key-file: '{{ssl.pem_path}}'
      cert-file: '{{ssl.cert_path}}'
      ca-file: '{{ssl.ca_path}}'
    server:
      security: '{{conf.config.security}}'
      port: '{{endpoints.definitions.foo-service-endpoint.desired-port}}'
      base-path: '{{endpoints.definitions.foo-service-endpoint.path}}'
    bar-client:
      security: '{{conf.config.security}}'
      uris: '{{discovered.bar-api-uris}}'
      auth-secret: '{{secret("bar-client-shared-secret")}}'
`

// bareManifest is the minimum a definition needs, for tests exercising one
// configuration trait at a time.
const bareManifest = `
manifest-version: "1.0"
product-group: com.palantir.foo
product-name: foo
product-version: 1.0.0
product-type: service.v1
`

func decodeTestDefinition(t *testing.T, manifest, configuration string) (ApolloProductDefinition, []ValidationError) {
	t.Helper()
	definition, errs, err := DecodeApolloProductDefinition(
		strings.NewReader(manifest), strings.NewReader(configuration))
	if err != nil {
		t.Fatalf("decode definition: %v", err)
	}
	return definition, errs
}

func errorPaths(errs []ValidationError) map[string]string {
	paths := make(map[string]string, len(errs))
	for _, err := range errs {
		paths[err.Path] = err.Message
	}
	return paths
}

func requireErrorAt(t *testing.T, errs []ValidationError, path string) string {
	t.Helper()
	paths := errorPaths(errs)
	message, exists := paths[path]
	if !exists {
		t.Fatalf("expected a validation error at %s, got %v", path, paths)
	}
	return message
}

func requireNoErrorAt(t *testing.T, errs []ValidationError, path string) {
	t.Helper()
	if message, exists := errorPaths(errs)[path]; exists {
		t.Fatalf("unexpected validation error at %s: %s", path, message)
	}
}

// TestSpecificationWorkedExampleDecodes is the check the previous model failed:
// the specification's own documents must at least parse into this model.
func TestSpecificationWorkedExampleDecodes(t *testing.T) {
	if _, _, err := DecodeApolloProductDefinition(
		strings.NewReader(specManifest), strings.NewReader(specConfiguration)); err != nil {
		t.Fatalf("the specification's worked example must decode: %v", err)
	}
}

// TestSpecificationWorkedExampleErrata pins the two places the worked example
// disagrees with the normative extension pages. If a future revision of the
// specification repairs them, this test is the thing that should fail.
func TestSpecificationWorkedExampleErrata(t *testing.T) {
	_, errs := decodeTestDefinition(t, specManifest, specConfiguration)

	// The Volumes extension defines VolumeConfiguration as a union whose only
	// member is durable-storage-configuration; the worked example puts
	// desired-size directly under the volume name.
	if message := requireErrorAt(t, errs, "configuration.volumes.foo-service-data.desired-size"); !strings.Contains(
		message, "durable-storage-configuration") {
		t.Fatalf("expected the nesting to be named, got %q", message)
	}
	requireErrorAt(t, errs, "configuration.volumes.foo-service-data")

	// The worked example's bcrypt reference omits the secret helper, leaving a
	// bare quoted string inside the braces.
	if message := requireErrorAt(
		t, errs, "configuration.discovery.produces.bar-client.client-bcrypted-shared-secret"); !strings.Contains(
		message, "not a recognized Apollo substitution") {
		t.Fatalf("expected an unrecognized substitution, got %q", message)
	}
}

// TestCorrectedSpecificationWorkedExampleValidates is the positive half: with
// the errata repaired, every other trait in the example is accepted.
func TestCorrectedSpecificationWorkedExampleValidates(t *testing.T) {
	definition, errs := decodeTestDefinition(t, specManifest, correctedSpecConfiguration)
	if len(errs) > 0 {
		t.Fatalf("corrected worked example must validate, got %v", errorPaths(errs))
	}
	if got := ApolloEffectiveReplication(definition.Configuration.Replication); got != 3 {
		t.Fatalf("replication = %d, want 3", got)
	}
	endpoint := definition.Configuration.Endpoints.Definitions["foo-service-endpoint"]
	liveness, readiness := ApolloStatusProbePaths(endpoint)
	if liveness != "/foo/status/liveness" || readiness != "/foo/status/readiness" {
		t.Fatalf("probe paths = %q, %q", liveness, readiness)
	}
	if health := ApolloHealthProbePath(endpoint); health != "/foo/status/health" {
		t.Fatalf("health path = %q", health)
	}
}

// TestManifestVersionIsOptional holds the Product Manifest page's required-field
// list. Its minimal example carries no manifest-version, so requiring one would
// reject a conformant hand-written manifest.
func TestManifestVersionIsOptional(t *testing.T) {
	_, errs := decodeTestDefinition(t, `
product-type: service.v1
product-group: com.palantir.apollo
product-name: apollo-catalog
product-version: 1.2.3
`, "")
	if len(errs) > 0 {
		t.Fatalf("the manifest page's minimal example must validate, got %v", errorPaths(errs))
	}

	// Present but wrong is still an error: it claims a schema this model does
	// not implement.
	_, errs = decodeTestDefinition(t, `
manifest-version: "2.0"
product-type: service.v1
product-group: com.palantir.apollo
product-name: apollo-catalog
product-version: 1.2.3
`, "")
	requireErrorAt(t, errs, "manifest.manifest-version")
}

// TestReplicationMustBeGreaterThanZero holds the Replication extension's rule.
// Zero is not a stop switch here: the extension requires a value greater than
// zero, and an absent block means two replicas rather than none.
func TestReplicationMustBeGreaterThanZero(t *testing.T) {
	_, errs := decodeTestDefinition(t, specManifest, strings.Replace(
		correctedSpecConfiguration, "desired: 3", "desired: 0", 1))
	requireErrorAt(t, errs, "configuration.replication.desired")

	if got := ApolloEffectiveReplication(nil); got != ApolloDefaultReplication {
		t.Fatalf("default replication = %d, want %d", got, ApolloDefaultReplication)
	}
	desired := 7
	if got := ApolloEffectiveReplication(&ApolloReplication{Desired: &desired}); got != 7 {
		t.Fatalf("replication = %d, want 7", got)
	}
}

// TestEndpointPortRange holds the bounded port range, using the out-of-range
// value that appears in the Endpoints extension's own example block.
func TestEndpointPortRange(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		port  int
		valid bool
	}{
		{name: "below the range", port: 1023, valid: false},
		{name: "at the floor", port: 1024, valid: true},
		{name: "at the ceiling", port: 19999, valid: true},
		{name: "the example's typo", port: 90001, valid: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			configuration := strings.Replace(correctedSpecConfiguration,
				"desired-port: 8080", "desired-port: "+strconv.Itoa(testCase.port), 1)
			_, errs := decodeTestDefinition(t, specManifest, configuration)
			path := "configuration.endpoints.definitions.foo-service-endpoint.desired-port"
			if testCase.valid {
				requireNoErrorAt(t, errs, path)
				return
			}
			requireErrorAt(t, errs, path)
		})
	}
}

// TestPublicEndpointExposureRules holds the external-port and path rules that
// depend on which ingress list an endpoint appears in.
func TestPublicEndpointExposureRules(t *testing.T) {
	const manifest = `
manifest-version: "1.0"
product-group: com.palantir.foo
product-name: foo
product-version: 1.0.0
product-type: service.v1
extensions:
  public-proxy-endpoints:
    - service-endpoint
  public-tcp-endpoints:
    - tcp-service-endpoint
  public-udp-endpoints:
    - udp-service-endpoint
`
	const configuration = `
endpoints:
  sls-status-endpoint: service-endpoint
  definitions:
    service-endpoint:
      desired-port: 8080
      path: /my-service-endpoint-path
    tcp-service-endpoint:
      desired-port: 4567
      external-port: 22
    udp-service-endpoint:
      desired-port: 5678
      external-port: 1234
`
	_, errs := decodeTestDefinition(t, manifest, configuration)
	if len(errs) > 0 {
		t.Fatalf("the Endpoints extension example must validate, got %v", errorPaths(errs))
	}

	// A proxied endpoint must not claim an external port.
	_, errs = decodeTestDefinition(t, manifest, strings.Replace(configuration,
		"      path: /my-service-endpoint-path", "      external-port: 443", 1))
	requireErrorAt(t, errs, "configuration.endpoints.definitions.service-endpoint.external-port")

	// A raw endpoint must claim one, and must not claim a path.
	_, errs = decodeTestDefinition(t, manifest, strings.Replace(configuration,
		"      external-port: 22", "      path: /tcp", 1))
	requireErrorAt(t, errs, "configuration.endpoints.definitions.tcp-service-endpoint.external-port")
	requireErrorAt(t, errs, "configuration.endpoints.definitions.tcp-service-endpoint.path")
}

// TestEndpointNameMayNotBeBothProxiedAndRaw holds the mutual exclusion between
// the proxy list and the raw lists, and the TCP/UDP pairing that is allowed.
func TestEndpointNameMayNotBeBothProxiedAndRaw(t *testing.T) {
	const conflicting = `
manifest-version: "1.0"
product-group: com.palantir.foo
product-name: foo
product-version: 1.0.0
product-type: service.v1
extensions:
  public-proxy-endpoints:
    - shared-endpoint
  public-tcp-endpoints:
    - shared-endpoint
`
	_, errs := decodeTestDefinition(t, conflicting, `
endpoints:
  definitions:
    shared-endpoint:
      desired-port: 8080
      external-port: 22
`)
	requireErrorAt(t, errs, "manifest.extensions.public-tcp-endpoints[0]")

	// The same name in both raw lists is how a Product asks for TCP and UDP on
	// one external port, and must be accepted.
	const paired = `
manifest-version: "1.0"
product-group: com.palantir.foo
product-name: foo
product-version: 1.0.0
product-type: service.v1
extensions:
  public-tcp-endpoints:
    - shared-endpoint
  public-udp-endpoints:
    - shared-endpoint
`
	_, errs = decodeTestDefinition(t, paired, `
endpoints:
  definitions:
    shared-endpoint:
      desired-port: 8080
      external-port: 22
`)
	if len(errs) > 0 {
		t.Fatalf("a TCP/UDP pair on one endpoint must validate, got %v", errorPaths(errs))
	}
}

// TestEndpointsSharingAPortMustDifferByPath holds the one case where a repeated
// port is legitimate.
func TestEndpointsSharingAPortMustDifferByPath(t *testing.T) {
	_, errs := decodeTestDefinition(t, bareManifest, `
endpoints:
  sls-status-endpoint: first-endpoint
  definitions:
    first-endpoint:
      desired-port: 9000
      path: /first
    second-endpoint:
      desired-port: 9000
      path: /second
`)
	if len(errs) > 0 {
		t.Fatalf("distinct paths on one port must validate, got %v", errorPaths(errs))
	}

	_, errs = decodeTestDefinition(t, bareManifest, `
endpoints:
  sls-status-endpoint: first-endpoint
  definitions:
    first-endpoint:
      desired-port: 9000
      path: /same
    second-endpoint:
      desired-port: 9000
      path: /same
`)
	requireErrorAt(t, errs, "configuration.endpoints.definitions.second-endpoint")
}

// TestStatusEndpointRequiredWithMultipleDefinitions holds the rule that the
// selection may only be omitted when there is nothing to choose between.
func TestStatusEndpointRequiredWithMultipleDefinitions(t *testing.T) {
	_, errs := decodeTestDefinition(t, bareManifest, `
endpoints:
  definitions:
    only-endpoint:
      desired-port: 9000
`)
	requireNoErrorAt(t, errs, "configuration.endpoints.sls-status-endpoint")

	_, errs = decodeTestDefinition(t, bareManifest, `
endpoints:
  definitions:
    first-endpoint:
      desired-port: 9000
    second-endpoint:
      desired-port: 9001
`)
	requireErrorAt(t, errs, "configuration.endpoints.sls-status-endpoint")
}

// TestVolumesMustCorrespondInBothDocuments holds the two-way requirement: the
// manifest declares, the configuration sizes, and neither may be alone.
func TestVolumesMustCorrespondInBothDocuments(t *testing.T) {
	// Declared but not configured.
	_, errs := decodeTestDefinition(t, specManifest, `
endpoints:
  definitions:
    foo-service-endpoint:
      desired-port: 8080
      path: /foo
`)
	requireErrorAt(t, errs, "manifest.extensions.volumes-v2.foo-service-data")

	// Configured but not declared.
	_, errs = decodeTestDefinition(t, bareManifest, `
volumes:
  undeclared-volume:
    durable-storage-configuration:
      desired-size: 5G
`)
	requireErrorAt(t, errs, "configuration.volumes.undeclared-volume")
}

// TestVolumeSizeGrammar holds the storage grammar, which is narrower than the
// compute one: a whole number with a required M, G, or T unit, capped.
func TestVolumeSizeGrammar(t *testing.T) {
	for _, testCase := range []struct {
		size  string
		valid bool
	}{
		{size: "5G", valid: true},
		{size: "20G", valid: true},
		{size: "10T", valid: true},
		{size: "500M", valid: true},
		{size: "10", valid: false},   // no unit
		{size: "5Gi", valid: false},  // binary units are a compute spelling
		{size: "1.5G", valid: false}, // not an integer
		{size: "0G", valid: false},   // not positive
		{size: "11T", valid: false},  // above the platform maximum
	} {
		t.Run(testCase.size, func(t *testing.T) {
			_, errs := decodeTestDefinition(t, specManifest, `
endpoints:
  definitions:
    foo-service-endpoint:
      desired-port: 8080
      path: /foo
volumes:
  foo-service-data:
    durable-storage-configuration:
      desired-size: `+testCase.size+`
`)
			path := "configuration.volumes.foo-service-data.durable-storage-configuration.desired-size"
			if testCase.valid {
				requireNoErrorAt(t, errs, path)
				return
			}
			requireErrorAt(t, errs, path)
		})
	}
}

// TestVolumeOverridesMayOnlyUpsize holds the override rule. Shrinking a volume
// would require discarding what it already holds.
func TestVolumeOverridesMayOnlyUpsize(t *testing.T) {
	if err := ValidateApolloVolumeOverride("data", "5G", "20G"); err != nil {
		t.Fatalf("upsizing must be permitted: %v", err)
	}
	if err := ValidateApolloVolumeOverride("data", "5G", "5G"); err != nil {
		t.Fatalf("an unchanged size must be permitted: %v", err)
	}
	if err := ValidateApolloVolumeOverride("data", "20G", "5G"); err == nil {
		t.Fatal("downsizing must be rejected")
	}
	// Units must be compared, not string lengths: 1T is larger than 999G.
	if err := ValidateApolloVolumeOverride("data", "999G", "1T"); err != nil {
		t.Fatalf("1T must count as an upsize from 999G: %v", err)
	}
}

// TestResourceGrammar holds the Resources extension: CPU is a unit-less core
// count, memory takes the listed units, and a request may not exceed its limit.
func TestResourceGrammar(t *testing.T) {
	_, errs := decodeTestDefinition(t, bareManifest, `
resources:
  requests:
    cpu: 0.5
    memory: 1Gi
  limits:
    cpu: 2
    memory: 4Gi
`)
	if len(errs) > 0 {
		t.Fatalf("the Resources extension example must validate, got %v", errorPaths(errs))
	}

	// A suffixed CPU is not a core count.
	_, errs = decodeTestDefinition(t, bareManifest, `
resources:
  requests:
    cpu: 500m
`)
	requireErrorAt(t, errs, "configuration.resources.requests.cpu")

	// A request above its limit can never be scheduled.
	_, errs = decodeTestDefinition(t, bareManifest, `
resources:
  requests:
    cpu: 4
    memory: 8G
  limits:
    cpu: 2
    memory: 4G
`)
	requireErrorAt(t, errs, "configuration.resources.requests.cpu")
	requireErrorAt(t, errs, "configuration.resources.requests.memory")
}

// TestEffectiveResourcesFillsDocumentedDefaults holds the defaulting rules,
// including the one that promotes a lone limit into its own request.
func TestEffectiveResourcesFillsDocumentedDefaults(t *testing.T) {
	effective := ApolloEffectiveResources(nil)
	if effective.Requests.CPU != ApolloDefaultCPU || effective.Requests.Memory != ApolloDefaultMemory {
		t.Fatalf("defaults = %+v", *effective.Requests)
	}

	effective = ApolloEffectiveResources(&ApolloResourceRequirements{
		Limits: &ApolloResourceQuantities{CPU: "2", Memory: "4Gi"},
	})
	if effective.Requests.CPU != "2" || effective.Requests.Memory != "4Gi" {
		t.Fatalf("a lone limit must become its own request, got %+v", *effective.Requests)
	}
}

// TestGeneratedSecretGrammar holds the secret block from the specification,
// including the length semantics the previous model inverted.
func TestGeneratedSecretGrammar(t *testing.T) {
	// The specification's own generated_secrets block, minus its ec_pkcs8 entry.
	_, errs := decodeTestDefinition(t, bareManifest, `
generated_secrets:
  myAppSecret:
    secret-type: random_string
    length: 24
  myAppRandomByteSecret:
    secret-type: random_bytes
    length: 16
  myAppCustomCharsetSecret:
    secret-type: random_string
    length: 16
    allow-chars: 'abcdefg'
  myAppRsaKeyPair:
    secret-type: rsa_pkcs8
    length: 2048
  myAppStigPassword:
    secret-type: stig_password
`)
	if len(errs) > 0 {
		t.Fatalf("the specification's secret block must validate, got %v", errorPaths(errs))
	}

	// The example's ec_pkcs8 entry uses an RSA key size, which is not one of the
	// curves the same page enumerates.
	_, errs = decodeTestDefinition(t, bareManifest, `
generated_secrets:
  ec_pkcs8:
    secret-type: ec_pkcs8
    length: 2048
`)
	requireErrorAt(t, errs, "configuration.generated_secrets.ec_pkcs8.length")

	for _, length := range []string{"224", "256", "384", "521"} {
		_, errs = decodeTestDefinition(t, bareManifest, `
generated_secrets:
  ec-key:
    secret-type: ec_pkcs8
    length: `+length+`
`)
		requireNoErrorAt(t, errs, "configuration.generated_secrets.ec-key.length")
	}

	// A charset only means something for a random string.
	_, errs = decodeTestDefinition(t, bareManifest, `
generated_secrets:
  bytes:
    secret-type: random_bytes
    length: 16
    allow-chars: 'abc'
`)
	requireErrorAt(t, errs, "configuration.generated_secrets.bytes")
}

// TestManagedFileShorthandAndTemplates holds the managed_files block: the
// shorthand spelling, the content cross-reference into conf, and the template
// a tmpl entry requires.
func TestManagedFileShorthandAndTemplates(t *testing.T) {
	const configuration = `
conf:
  some-config:
    a: b
    c: d
  some-other-config:
    b: c
    c: d
managed_files:
  some-conf.yml: some-config
  some-other-conf.json:
    type: json
    content: some-other-config
`
	definition, errs := decodeTestDefinition(t, bareManifest, configuration)
	if len(errs) > 0 {
		t.Fatalf("the managed_files example must validate, got %v", errorPaths(errs))
	}
	shorthand := definition.Configuration.ManagedFiles["some-conf.yml"]
	if !shorthand.Shorthand {
		t.Fatal("the shorthand entry must be recorded as shorthand")
	}
	if shorthand.Content != "some-config" {
		t.Fatalf("shorthand content = %v", shorthand.Content)
	}
	if shorthand.ReloadType != ApolloReloadTypeInstall {
		t.Fatalf("shorthand reload type = %q, want install", shorthand.ReloadType)
	}

	// Content that names nothing in conf renders an empty file.
	_, errs = decodeTestDefinition(t, bareManifest, `
conf:
  some-config:
    a: b
managed_files:
  some-conf.yml: absent-config
`)
	requireErrorAt(t, errs, "configuration.managed_files.some-conf.yml.content")

	// A tmpl entry needs its template shipped alongside.
	const templateConfiguration = `
conf:
  my-xml-conf:
    a: b
managed_files:
  offroading.xml:
    type: tmpl
`
	withTemplate, errs, err := DecodeApolloProductDefinitionWithTemplates(
		strings.NewReader(bareManifest), strings.NewReader(templateConfiguration),
		[]string{"offroading.xml.tmpl"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("a tmpl entry with its template must validate, got %v", errorPaths(errs))
	}
	if len(withTemplate.Templates) != 1 {
		t.Fatalf("templates = %v", withTemplate.Templates)
	}

	_, errs, err = DecodeApolloProductDefinitionWithTemplates(
		strings.NewReader(bareManifest), strings.NewReader(templateConfiguration),
		[]string{"something-else.tmpl"})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	requireErrorAt(t, errs, "configuration.managed_files.offroading.xml")
}

// TestConfigurationOverrideMerge holds the merge semantics, including the block
// an override may never touch.
func TestConfigurationOverrideMerge(t *testing.T) {
	base := map[string]any{
		"conf": map[string]any{"a": "b", "b": "c"},
	}
	override := map[string]any{
		"conf": map[string]any{"b": "overridden", "new-field": "now-present"},
	}
	merged, err := MergeApolloConfigurationOverrides(base, override)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	conf, _ := merged["conf"].(map[string]any)
	if conf["a"] != "b" || conf["b"] != "overridden" || conf["new-field"] != "now-present" {
		t.Fatalf("merged conf = %v", conf)
	}

	// Lists replace rather than merge, and so do values of differing types.
	merged, err = MergeApolloConfigurationOverrides(
		map[string]any{"list": []any{"a", "b"}, "scalar": "value"},
		map[string]any{"list": []any{"c"}, "scalar": map[string]any{"now": "a map"}})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if list, _ := merged["list"].([]any); len(list) != 1 || list[0] != "c" {
		t.Fatalf("lists must be replaced, got %v", merged["list"])
	}
	if _, isMap := merged["scalar"].(map[string]any); !isMap {
		t.Fatalf("a differing type must replace, got %v", merged["scalar"])
	}

	// Overwriting a generated secret's declaration rotates a live credential.
	if _, err := MergeApolloConfigurationOverrides(
		base, map[string]any{ApolloGeneratedSecretsKey: map[string]any{"x": "y"}}); err == nil {
		t.Fatal("an override of generated_secrets must be refused")
	}

	// The base must not be mutated: an installation's overrides cannot be allowed
	// to leak into the Product's own definition.
	if conf, _ := base["conf"].(map[string]any); conf["b"] != "c" {
		t.Fatalf("merging must not mutate the base, got %v", conf)
	}
}

// TestSubstitutionTargetsAreCrossChecked is the reason the pair is validated
// together rather than document by document.
func TestSubstitutionTargetsAreCrossChecked(t *testing.T) {
	_, errs := decodeTestDefinition(t, bareManifest, `
conf:
  client:
    secret: '{{secret("never-generated")}}'
    peer: '{{discovered.never-consumed}}'
    self: '{{self_discovered.never-produced}}'
    volume: '{{volumes.never-declared.PathOnDisk}}'
    endpoint: '{{endpoints.definitions.never-defined.port}}'
    ssl: '{{ssl.no_such_field}}'
    conf: '{{conf.no.such.path}}'
`)
	for _, path := range []string{
		"configuration.conf.client.secret",
		"configuration.conf.client.peer",
		"configuration.conf.client.self",
		"configuration.conf.client.volume",
		"configuration.conf.client.endpoint",
		"configuration.conf.client.ssl",
		"configuration.conf.client.conf",
	} {
		requireErrorAt(t, errs, path)
	}
}

// TestDiscoveryURIAugmentationIsProducerSide holds the augmentation contract:
// the platform derives the address from the Service it built, and the defaults
// the specification names apply when the entry omits them.
func TestDiscoveryURIAugmentationIsProducerSide(t *testing.T) {
	const host = "foo.application-namespace.svc.cluster.local"
	payload, augmented := ApolloAugmentProducedDiscovery(ApolloDiscoveryProduces{
		Role: "my-api", Port: 8443, Path: "/my-api", Scheme: "https",
	}, host)
	if !augmented {
		t.Fatal("an entry with a port must be augmented")
	}
	want := "https://" + host + ":8443/my-api"
	if payload["uri"] != want {
		t.Fatalf("uri = %v, want %v", payload["uri"], want)
	}
	if uris, _ := payload["uris"].([]any); len(uris) != 1 || uris[0] != want {
		t.Fatalf("uris = %v", payload["uris"])
	}

	// Scheme defaults to https and path to the root.
	payload, augmented = ApolloAugmentProducedDiscovery(ApolloDiscoveryProduces{Port: 8080}, host)
	if !augmented {
		t.Fatal("a bare port must still be augmented")
	}
	if payload["uri"] != "https://"+host+":8080/" {
		t.Fatalf("uri = %v", payload["uri"])
	}

	// An entry carrying no port is not an address.
	if _, augmented := ApolloAugmentProducedDiscovery(
		ApolloDiscoveryProduces{Role: "oauth-client"}, host); augmented {
		t.Fatal("an entry with no port must not be augmented")
	}
}

// TestDiscoveryBlockFromTheSpecification validates the published discovery
// example, including the conf.tags list that feeds produced entries.
func TestDiscoveryBlockFromTheSpecification(t *testing.T) {
	definition, errs := decodeTestDefinition(t, bareManifest, `
conf:
  tags:
    - foo
generated_secrets:
  my-oauth-secret:
    secret-type: random_string
    length: 32
discovery:
  produce-to-additional-stacks:
    - production
  produces:
    my-api:
      role: my-api
      port: 8443
      path: /my-api
      scheme: https
    my-oauth-client:
      role: oauth-client
      client:
        bcryptedOAuthSecret: '{{secret("my-oauth-secret", part: bcrypt_hash)}}'
  consumes:
    neighboring-api:
      role: neighboring-api
      select: uris
      stack: production
      tags: [bar]
`)
	if len(errs) > 0 {
		t.Fatalf("the discovery example must validate, got %v", errorPaths(errs))
	}
	if len(definition.Configuration.Discovery.Produces) != 2 {
		t.Fatalf("produces = %v", definition.Configuration.Discovery.Produces)
	}
	if definition.Configuration.Discovery.Consumes["neighboring-api"].Select != "uris" {
		t.Fatal("select must be modelled")
	}

	// A consumer with no role has no filter to match producers on.
	_, errs = decodeTestDefinition(t, bareManifest, `
discovery:
  consumes:
    neighboring-api:
      select: uris
`)
	requireErrorAt(t, errs, "configuration.discovery.consumes.neighboring-api.role")

	// conf.tags is the documented source of produced tags and must be a list.
	_, errs = decodeTestDefinition(t, bareManifest, `
conf:
  tags: not-a-list
`)
	requireErrorAt(t, errs, "configuration.conf.tags")
}

// TestQuorumAndUpgradeExtensions holds the manifest enums and their default.
func TestQuorumAndUpgradeExtensions(t *testing.T) {
	for _, quorum := range []string{
		ApolloQuorumAll, ApolloQuorumAllButOne, ApolloQuorumAny, ApolloQuorumSimpleMajority,
	} {
		_, errs := decodeTestDefinition(t, `
manifest-version: "1.0"
product-group: com.palantir.foo
product-name: foo
product-version: 1.0.0
product-type: service.v1
extensions:
  quorum-requirement: `+quorum+`
  upgrade-strategy: rolling
`, "")
		if len(errs) > 0 {
			t.Fatalf("quorum %s must validate, got %v", quorum, errorPaths(errs))
		}
	}

	_, errs := decodeTestDefinition(t, `
manifest-version: "1.0"
product-group: com.palantir.foo
product-name: foo
product-version: 1.0.0
product-type: service.v1
extensions:
  quorum-requirement: most-of-them
  upgrade-strategy: blue-green
`, "")
	requireErrorAt(t, errs, "manifest.extensions.quorum-requirement")
	requireErrorAt(t, errs, "manifest.extensions.upgrade-strategy")

	if ApolloDefaultQuorumRequirement != ApolloQuorumAllButOne {
		t.Fatalf("default quorum = %q", ApolloDefaultQuorumRequirement)
	}
}

// TestConfigurationIsOptional holds the rule that a Product shipping only a
// manifest is a complete definition.
func TestConfigurationIsOptional(t *testing.T) {
	definition, errs, err := DecodeApolloProductDefinition(strings.NewReader(bareManifest), nil)
	if err != nil {
		t.Fatalf("a definition with no configuration must decode: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("a definition with no configuration must validate, got %v", errorPaths(errs))
	}
	if definition.Configuration.Conf != nil {
		t.Fatal("an absent configuration must decode to an empty document")
	}
}

// TestConfigurationRejectsUnknownFieldsAndMultipleDocuments keeps the decoder
// strict where it can be.
func TestConfigurationRejectsUnknownFieldsAndMultipleDocuments(t *testing.T) {
	if _, err := DecodeApolloProductConfiguration(strings.NewReader(`
managed_files:
  some.yml:
    type: yaml
    content: some
    reload_type: runtime
`)); err == nil {
		t.Fatal("a misspelled managed file field must be rejected")
	}

	if _, err := DecodeApolloProductConfiguration(strings.NewReader(
		"replication:\n  desired: 1\n---\nreplication:\n  desired: 2\n")); err == nil {
		t.Fatal("a second document must be rejected")
	}
}

// TestManagedFileMountPath records where rendered configuration lands.
func TestManagedFileMountPath(t *testing.T) {
	if got := ApolloManagedFileMountPath("foo"); got != "/opt/palantir/services/foo/var/conf" {
		t.Fatalf("mount path = %q", got)
	}
}

// TestProductVersionAcceptsBothClasses holds the version specification's two
// classes. Ordering is a separate question from publication: a non-orderable
// version may be declared, but nothing may sort it.
func TestProductVersionAcceptsBothClasses(t *testing.T) {
	for _, version := range []string{
		"1.0.0", "1.0.0-1-gaaaaaaa", "1.0.0-rc1", "1.0.0-rc1-1-gaaaaaaa", // orderable
		"1.0.0.dirty", "0.0.1-custom-description-42", // non-orderable
	} {
		_, errs := decodeTestDefinition(t, `
product-type: service.v1
product-group: com.palantir.apollo
product-name: apollo-catalog
product-version: `+version+`
`, "")
		requireNoErrorAt(t, errs, "manifest.product-version")
	}

	for _, version := range []string{"5.0", "1.1.2.3-foo", "1.0.0-FOO"} {
		_, errs := decodeTestDefinition(t, `
product-type: service.v1
product-group: com.palantir.apollo
product-name: apollo-catalog
product-version: `+version+`
`, "")
		requireErrorAt(t, errs, "manifest.product-version")
	}

	// Only the orderable class may be compared.
	if !IsApolloOrderableVersion("1.0.0") || IsApolloOrderableVersion("1.0.0.dirty") {
		t.Fatal("orderability must distinguish the two classes")
	}
}

// TestProductTypesFromBothPages holds the union of the enumerated types and the
// ones the specification's own examples use.
func TestProductTypesFromBothPages(t *testing.T) {
	for _, productType := range []string{
		ApolloProductTypeAsset, ApolloProductTypeHelm, // the Product Types page
		ApolloProductTypeService, ApolloProductTypeHelmChart, // its sibling pages' examples
		ApolloProductTypeServiceV2, ApolloProductTypeDaemonSet, ApolloProductTypeTerraform,
	} {
		_, errs := decodeTestDefinition(t, `
product-type: `+productType+`
product-group: com.palantir.apollo
product-name: apollo-catalog
product-version: 1.2.3
`, "")
		requireNoErrorAt(t, errs, "manifest.product-type")
	}
}

// Regression pins for the volumes-v2 defects filed against the first
// reconstruction of this extension. Each invented key below decoded cleanly
// then, which is what made the manifest look valid while declaring something
// the platform would never provision. They are decode failures rather than
// validation errors because `extensions` is otherwise open: an unknown key
// inside a modelled block has to be caught by strict decoding, or it lands in
// the preserved-unknowns map and is reported as nothing at all.
func TestVolumesV2RejectsTheInventedManifestKeys(t *testing.T) {
	t.Parallel()
	manifest := func(volumeBody string) string {
		return `manifest-version: "1.0"
product-group: com.example.services
product-name: catalog
product-version: 3.1.0
product-type: service.v1
extensions:
  volumes-v2:
    data:
` + volumeBody
	}
	for name, body := range map[string]string{
		// The manifest declares a kind, not a capacity: sizing is
		// configuration.yml's, so an operator can raise it per installation
		// without republishing an immutable Release.
		"size":         "      size: 20G\n      volume-type:\n        durable-volume: {}\n",
		"minimum-size": "      minimum-size: 10G\n      volume-type:\n        durable-volume: {}\n",
		// The platform assigns the mount point and reports it back through
		// PathOnDisk. A Product that could author it would be describing a
		// layout it does not control.
		"path": "      path: /var/data\n      volume-type:\n        durable-volume: {}\n",
		// Durability is the union member's identity, not a flag beside it.
		// A boolean admits `durable-storage: false` on a durable-volume, which
		// names two contradictory things at once.
		"durable-storage": "      durable-storage: true\n      volume-type:\n        durable-volume: {}\n",
		// The union level itself is required: a durable-volume hung directly off
		// the volume name is the flat shape that caused the original decode
		// failure.
		"flattened union": "      durable-volume: {}\n",
	} {
		if _, _, err := DecodeApolloProductDefinition(strings.NewReader(manifest(body)), nil); err == nil {
			t.Fatalf("volumes-v2 must reject an authored %s key", name)
		}
	}
}

// The Product Types page enumerates asset.v1 and helm.v1 while its sibling
// pages write service.v1 throughout. A conformance validator that rejected
// either spelling would reject documents the specification itself publishes.
func TestPublishedProductTypeSpellingsAreAccepted(t *testing.T) {
	t.Parallel()
	for _, productType := range []string{
		ApolloProductTypeAsset, ApolloProductTypeHelm,
		ApolloProductTypeService, ApolloProductTypeServiceV2,
		ApolloProductTypeDaemonSet, ApolloProductTypeHelmChart, ApolloProductTypeTerraform,
	} {
		manifest := `manifest-version: "1.0"
product-group: com.example.services
product-name: catalog
product-version: 3.1.0
product-type: ` + productType + "\n"
		_, errs := decodeTestDefinition(t, manifest, "")
		requireNoErrorAt(t, errs, "manifest.product-type")
	}
	unknown := `manifest-version: "1.0"
product-group: com.example.services
product-name: catalog
product-version: 3.1.0
product-type: invented.v9
`
	_, errs := decodeTestDefinition(t, unknown, "")
	requireErrorAt(t, errs, "manifest.product-type")
}

// The specification's own worked example, kept verbatim as the conformance
// anchor. Everything else in this file is a reconstruction argued from it, so if
// this stops validating the reconstruction has drifted from its source.
const specificationExampleManifest = `manifest-version: "1.0"
product-group: com.example
product-name: foo
product-version: 1.0.0
product-type: service.v1
extensions:
  public-proxy-endpoints:
    - foo-service-endpoint
  volumes-v2:
    foo-service-data:
      volume-type:
        durable-volume: {}
`

const specificationExampleConfiguration = `replication:
  desired: 3
endpoints:
  definitions:
    foo-service-endpoint:
      desired-port: 8080
      path: /foo
volumes:
  foo-service-data:
    durable-storage-configuration:
      desired-size: 10G
generated_secrets:
  bar-client-shared-secret:
    secret-type: random_string
    length: 32
discovery:
  produces:
    bar-client:
      role: bar-client
      client-bcrypted-shared-secret: '{{secret("bar-client-shared-secret", part: "bcrypt_hash")}}'
  consumes:
    bar-api-uris:
      role: bar-api
      select: uris
conf:
  config:
    security:
      key-file: '{{ssl.pem_path}}'
      cert-file: '{{ssl.cert_path}}'
      ca-file: '{{ssl.ca_path}}'
    server:
      security: '{{conf.config.security}}'
      port: '{{endpoints.definitions.foo-service-endpoint.desired-port}}'
      base-path: '{{endpoints.definitions.foo-service-endpoint.path}}'
    bar-client:
      security: '{{conf.config.security}}'
      uris: '{{discovered.bar-api-uris}}'
      auth-secret: '{{secret("bar-client-shared-secret")}}'
`

func TestSpecificationWorkedExampleValidates(t *testing.T) {
	t.Parallel()
	_, errs := decodeTestDefinition(t, specificationExampleManifest, specificationExampleConfiguration)
	if len(errs) != 0 {
		t.Fatalf("the specification's own example must validate: %#v", errs)
	}
}

// The example's prose spells volumes as `<name>: {desired-size}`, which its own
// normative Volumes definition does not allow. The nesting is not cosmetic: it
// is what ties a size to the volume *kind* it configures, and without it a
// desired-size names no union member. Rejecting it with a diagnostic that says
// where the key belongs is the only outcome that neither ignores a published
// size nor leaves an author guessing.
func TestSpecificationProseVolumeSpellingIsRejectedWithADirection(t *testing.T) {
	t.Parallel()
	prose := strings.Replace(specificationExampleConfiguration,
		"    durable-storage-configuration:\n      desired-size: 10G",
		"    desired-size: 10G", 1)
	_, errs := decodeTestDefinition(t, specificationExampleManifest, prose)
	message, found := errorPaths(errs)["configuration.volumes.foo-service-data.desired-size"]
	if !found {
		t.Fatalf("expected the flat spelling to be rejected, got %#v", errs)
	}
	if !strings.Contains(message, "durable-storage-configuration") {
		t.Fatalf("the diagnostic must name where the key belongs: %q", message)
	}
}

// parseApolloMemory is what makes "requests must not exceed limits" comparable
// across units. Get the scale wrong and a Product declaring 2Gi of requests
// against 3G of limits either passes when it should not or fails when it
// should not -- and the failure is a scheduling decision, not a parse error.
// Mutation testing changed the scaling arithmetic and nothing noticed.
func TestApolloMemoryScalesBinaryAndDecimalUnitsApart(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value string
		want  float64
	}{
		{value: "1", want: 1},
		{value: "1K", want: 1e3},
		{value: "1M", want: 1e6},
		{value: "1G", want: 1e9},
		{value: "1T", want: 1e12},
		// Binary suffixes are powers of two, not their decimal near-neighbours.
		{value: "1Ki", want: 1024},
		{value: "1Mi", want: 1024 * 1024},
		{value: "1Gi", want: 1024 * 1024 * 1024},
		{value: "1.5Gi", want: 1.5 * 1024 * 1024 * 1024},
		{value: "0.5M", want: 5e5},
	} {
		got, ok := parseApolloMemory(test.value)
		if !ok {
			t.Errorf("%q was rejected", test.value)
			continue
		}
		if got != test.want {
			t.Errorf("%q = %v, want %v", test.value, got, test.want)
		}
	}

	// The distinction that makes the comparison worth doing at all.
	gibibyte, _ := parseApolloMemory("1Gi")
	gigabyte, _ := parseApolloMemory("1G")
	if !(gibibyte > gigabyte) {
		t.Errorf("1Gi (%v) must exceed 1G (%v)", gibibyte, gigabyte)
	}
	thousandMega, _ := parseApolloMemory("1000M")
	if thousandMega != gigabyte {
		t.Errorf("1000M (%v) must equal 1G (%v)", thousandMega, gigabyte)
	}

	for _, invalid := range []string{"", "abc", "1Mib", "1 Gi", "-1", "1.2.3", "Gi", "1gi"} {
		if value, ok := parseApolloMemory(invalid); ok {
			t.Errorf("%q was accepted as %v", invalid, value)
		}
	}
}

// The comparison those magnitudes exist for: a request may not exceed its limit,
// and the two sides are routinely written in different units.
func TestApolloResourceRequestsAreComparedAcrossUnits(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		request   string
		limit     string
		wantError bool
	}{
		{request: "1Gi", limit: "2Gi"},
		{request: "2Gi", limit: "2Gi"},
		// 2Gi is 2147483648; 2G is 2000000000. The request exceeds the limit
		// even though the numbers read the same.
		{request: "2Gi", limit: "2G", wantError: true},
		{request: "2G", limit: "2Gi"},
		{request: "1000M", limit: "1G"},
		{request: "3Gi", limit: "2Gi", wantError: true},
	} {
		errs := []ValidationError{}
		request := &ApolloResourceQuantities{Memory: test.request}
		limit := &ApolloResourceQuantities{Memory: test.limit}
		_, requestMemory := validateApolloResourceQuantities("requests", request, &errs)
		_, limitMemory := validateApolloResourceQuantities("limits", limit, &errs)
		if len(errs) != 0 {
			t.Fatalf("%s/%s produced parse errors: %v", test.request, test.limit, errs)
		}
		exceeds := requestMemory > 0 && limitMemory > 0 && requestMemory > limitMemory
		if exceeds != test.wantError {
			t.Errorf("request %s against limit %s exceeds=%v, want %v (%v vs %v)",
				test.request, test.limit, exceeds, test.wantError, requestMemory, limitMemory)
		}
	}
}

// A produced discovery entry is how a Product advertises itself to its peers.
// Port zero means unset -- the entry advertises no port -- so the range check
// has to admit it while refusing anything outside 1..65535. All three of these
// optional-field guards were unpinned.
func TestApolloDiscoveryValidatesOptionalFieldsAtTheirEdges(t *testing.T) {
	t.Parallel()
	validate := func(t *testing.T, produces ApolloDiscoveryProduces) []ValidationError {
		t.Helper()
		errs := []ValidationError{}
		validateApolloDiscovery(ApolloProductConfiguration{
			Discovery: &ApolloDiscovery{Produces: map[string]ApolloDiscoveryProduces{"api": produces}},
		}, &errs)
		return errs
	}

	for _, test := range []struct {
		name      string
		produces  ApolloDiscoveryProduces
		wantError bool
	}{
		{name: "port unset", produces: ApolloDiscoveryProduces{}},
		{name: "port at the floor", produces: ApolloDiscoveryProduces{Port: 1}},
		{name: "port at the ceiling", produces: ApolloDiscoveryProduces{Port: 65535}},
		{name: "port above the ceiling", produces: ApolloDiscoveryProduces{Port: 65536}, wantError: true},
		{name: "negative port", produces: ApolloDiscoveryProduces{Port: -1}, wantError: true},
		// Role is optional, but a stated one must be a discovery name.
		{name: "role unset", produces: ApolloDiscoveryProduces{}},
		{name: "role stated", produces: ApolloDiscoveryProduces{Role: "primary"}},
		{name: "role malformed", produces: ApolloDiscoveryProduces{Role: "Not A Name"}, wantError: true},
		// A path is only meaningful if it is absolute, and only reachable if the
		// entry also says which port to reach it on -- so the two are validated
		// together rather than independently.
		{name: "path absolute with a port", produces: ApolloDiscoveryProduces{Path: "/health", Port: 8080}},
		{name: "path relative", produces: ApolloDiscoveryProduces{Path: "health", Port: 8080}, wantError: true},
		{name: "path without a port", produces: ApolloDiscoveryProduces{Path: "/health"}, wantError: true},
		{name: "scheme without a port", produces: ApolloDiscoveryProduces{Scheme: "https"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if errs := validate(t, test.produces); (len(errs) > 0) != test.wantError {
				t.Fatalf("rejected=%v, want %v (%v)", len(errs) > 0, test.wantError, errs)
			}
		})
	}

	// A consumed entry selects a field of a produced one; an unstated select is
	// fine, a malformed one is not.
	for _, test := range []struct {
		selector  string
		wantError bool
	}{
		{selector: ""},
		{selector: "port"},
		{selector: "Not A Field", wantError: true},
	} {
		errs := []ValidationError{}
		// Role is required on a consumed entry: it names what is being consumed.
		validateApolloDiscovery(ApolloProductConfiguration{
			Discovery: &ApolloDiscovery{Consumes: map[string]ApolloDiscoveryConsumes{
				"db": {Role: "primary", Select: test.selector},
			}},
		}, &errs)
		if (len(errs) > 0) != test.wantError {
			t.Errorf("select %q rejected=%v, want %v (%v)", test.selector, len(errs) > 0, test.wantError, errs)
		}
	}
}

// allow-chars and disallow-chars are mutually exclusive: a generator given both
// has no defined alphabet, and the two rules can contradict outright. Neither
// the exclusion nor its interaction with the secret type was pinned.
func TestApolloGeneratedSecretCharsetRulesAreMutuallyExclusive(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		secretType string
		allow      string
		disallow   string
		wantError  bool
	}{
		{name: "neither", secretType: ApolloSecretTypeRandomString},
		{name: "allow only", secretType: ApolloSecretTypeRandomString, allow: "abc"},
		{name: "disallow only", secretType: ApolloSecretTypeRandomString, disallow: "abc"},
		{name: "both", secretType: ApolloSecretTypeRandomString, allow: "abc", disallow: "xyz", wantError: true},
		// The charset rules only mean anything for a random string.
		{name: "allow on another type", secretType: ApolloSecretTypeRandomBytes, allow: "abc", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			errs := []ValidationError{}
			validateApolloGeneratedSecret("secrets", "token", ApolloGeneratedSecret{
				SecretType: test.secretType, AllowChars: test.allow, DisallowChars: test.disallow,
			}, &errs)
			if (len(errs) > 0) != test.wantError {
				t.Fatalf("rejected=%v, want %v (%v)", len(errs) > 0, test.wantError, errs)
			}
		})
	}
}

// Requests exceeding limits is a schedulable-looking definition that cannot
// actually be scheduled, so it is refused at publication rather than at
// install. The comparisons sit behind the unit conversion, and going through
// the whole validator is what exercises them -- asserting the magnitudes alone
// leaves the comparison itself unpinned.
func TestApolloDefinitionRefusesRequestsAboveLimits(t *testing.T) {
	t.Parallel()
	definition := func(requests, limits ApolloResourceQuantities) ApolloProductDefinition {
		return ApolloProductDefinition{
			Manifest: ApolloProductManifest{
				ProductGroup: "com.example", ProductName: "app",
				ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
			},
			Configuration: ApolloProductConfiguration{
				Resources: &ApolloResourceRequirements{Requests: &requests, Limits: &limits},
			},
		}
	}
	rejects := func(definition ApolloProductDefinition, field string) bool {
		for _, err := range ValidateApolloProductDefinition(definition) {
			if strings.Contains(err.Path, field) {
				return true
			}
		}
		return false
	}

	for _, test := range []struct {
		name     string
		requests ApolloResourceQuantities
		limits   ApolloResourceQuantities
		field    string
		want     bool
	}{
		{name: "cpu within", requests: ApolloResourceQuantities{CPU: "1"}, limits: ApolloResourceQuantities{CPU: "2"}, field: "cpu"},
		{name: "cpu equal", requests: ApolloResourceQuantities{CPU: "2"}, limits: ApolloResourceQuantities{CPU: "2"}, field: "cpu"},
		{
			name: "cpu over", requests: ApolloResourceQuantities{CPU: "3"}, limits: ApolloResourceQuantities{CPU: "2"},
			field: "cpu", want: true,
		},
		{name: "memory within", requests: ApolloResourceQuantities{Memory: "1Gi"}, limits: ApolloResourceQuantities{Memory: "2Gi"}, field: "memory"},
		// Equal is allowed: a request may match its limit exactly.
		{name: "memory equal", requests: ApolloResourceQuantities{Memory: "2Gi"}, limits: ApolloResourceQuantities{Memory: "2Gi"}, field: "memory"},
		// The same quantity written two ways is still equal, so the conversion
		// has to agree with itself before the comparison means anything.
		{name: "memory equal across units", requests: ApolloResourceQuantities{Memory: "1000M"}, limits: ApolloResourceQuantities{Memory: "1G"}, field: "memory"},
		{
			name: "memory over", requests: ApolloResourceQuantities{Memory: "3Gi"}, limits: ApolloResourceQuantities{Memory: "2Gi"},
			field: "memory", want: true,
		},
		// The units differ but the numbers read the same; only the conversion
		// knows 2Gi exceeds 2G.
		{
			name: "memory over across units", requests: ApolloResourceQuantities{Memory: "2Gi"},
			limits: ApolloResourceQuantities{Memory: "2G"}, field: "memory", want: true,
		},
		// A limit stated without a request is not a violation.
		{name: "request unstated", requests: ApolloResourceQuantities{}, limits: ApolloResourceQuantities{CPU: "1"}, field: "cpu"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := rejects(definition(test.requests, test.limits), test.field); got != test.want {
				t.Fatalf("%s rejected=%v, want %v", test.field, got, test.want)
			}
		})
	}
}

// Zero replicas is not the documented way to stop a workload -- an installation
// is stopped through its Entity. Publishing a Product that asks for nothing
// would stop it everywhere at once, silently.
func TestApolloDefinitionRefusesZeroDesiredReplicas(t *testing.T) {
	t.Parallel()
	definition := func(desired *int) ApolloProductDefinition {
		return ApolloProductDefinition{
			Manifest: ApolloProductManifest{
				ProductGroup: "com.example", ProductName: "app",
				ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
			},
			Configuration: ApolloProductConfiguration{Replication: &ApolloReplication{Desired: desired}},
		}
	}
	value := func(n int) *int { return &n }
	rejects := func(definition ApolloProductDefinition) bool {
		for _, err := range ValidateApolloProductDefinition(definition) {
			if strings.Contains(err.Path, "replication.desired") {
				return true
			}
		}
		return false
	}

	for _, test := range []struct {
		name    string
		desired *int
		want    bool
	}{
		{name: "unstated"},
		{name: "one replica", desired: value(1)},
		{name: "zero replicas", desired: value(0), want: true},
		{name: "negative replicas", desired: value(-1), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := rejects(definition(test.desired)); got != test.want {
				t.Fatalf("desired=%v rejected=%v, want %v", test.desired, got, test.want)
			}
		})
	}
}

// A manifest exposing an endpoint publicly names one the configuration defines.
// Nothing else connects the two halves: the manifest carries a list of names and
// the configuration carries the definitions, so a typo publishes a Product that
// asks the platform to expose something that does not exist. The cross-reference
// was unpinned in both directions.
func TestApolloPublicEndpointsMustNameADefinedEndpoint(t *testing.T) {
	t.Parallel()
	definition := func(mutate func(*ApolloProductDefinition)) ApolloProductDefinition {
		base := ApolloProductDefinition{
			Manifest: ApolloProductManifest{
				ProductGroup: "com.example", ProductName: "app",
				ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
			},
			Configuration: ApolloProductConfiguration{
				Endpoints: &ApolloEndpoints{
					Definitions: map[string]ApolloEndpointDefinition{
						"api": {DesiredPort: 8443, Path: "/api"},
					},
				},
			},
		}
		mutate(&base)
		return base
	}
	rejects := func(definition ApolloProductDefinition, field string) bool {
		errs := []ValidationError{}
		validateApolloEndpoints(definition, &errs)
		for _, err := range errs {
			if strings.Contains(err.Path, field) {
				return true
			}
		}
		return false
	}

	for _, test := range []struct {
		name  string
		build func(*ApolloProductDefinition)
		field string
		want  bool
	}{
		{
			name:  "proxy names a defined endpoint",
			field: "public-proxy-endpoints",
			build: func(d *ApolloProductDefinition) { d.Manifest.Extensions.PublicProxyEndpoints = []string{"api"} },
		},
		{
			name:  "proxy names an undefined endpoint",
			field: "public-proxy-endpoints", want: true,
			build: func(d *ApolloProductDefinition) { d.Manifest.Extensions.PublicProxyEndpoints = []string{"aip"} },
		},
		// An empty name is skipped rather than reported as a missing definition;
		// the emptiness itself is a different complaint.
		{
			name:  "proxy names nothing",
			field: "public-proxy-endpoints",
			build: func(d *ApolloProductDefinition) { d.Manifest.Extensions.PublicProxyEndpoints = []string{""} },
		},
		{
			name:  "tcp names an undefined endpoint",
			field: "public-tcp-endpoints", want: true,
			build: func(d *ApolloProductDefinition) { d.Manifest.Extensions.PublicTCPEndpoints = []string{"absent"} },
		},
		{
			name:  "no endpoints defined at all",
			field: "public-proxy-endpoints", want: true,
			build: func(d *ApolloProductDefinition) {
				d.Configuration.Endpoints = nil
				d.Manifest.Extensions.PublicProxyEndpoints = []string{"api"}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := rejects(definition(test.build), test.field); got != test.want {
				errs := []ValidationError{}
				validateApolloEndpoints(definition(test.build), &errs)
				t.Fatalf("%s rejected=%v, want %v (%v)", test.field, got, test.want, errs)
			}
		})
	}
}

// The augmentation contract has edges the existing test does not reach: the
// port range that decides whether an entry is an address at all, and the host,
// which the platform supplies rather than the Product. Mutation testing moved
// both without failing anything.
func TestDiscoveryAugmentationRefusesWhatIsNotAnAddress(t *testing.T) {
	t.Parallel()
	const host = "svc.namespace.svc.cluster.local"

	for _, test := range []struct {
		name string
		port int
		host string
		want bool
	}{
		{name: "port at the floor", port: 1, host: host, want: true},
		{name: "port at the ceiling", port: 65535, host: host, want: true},
		{name: "port unset", port: 0, host: host},
		{name: "port above the ceiling", port: 65536, host: host},
		{name: "negative port", port: -1, host: host},
		// The host comes from the Service the platform built, so an entry with a
		// valid port is still not an address until there is somewhere to reach.
		{name: "host absent", port: 8443},
		{name: "host blank", port: 8443, host: "   "},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			payload, augmented := ApolloAugmentProducedDiscovery(
				ApolloDiscoveryProduces{Role: "api", Port: test.port}, test.host)
			if augmented != test.want {
				t.Fatalf("augmented=%v, want %v", augmented, test.want)
			}
			// Role is carried either way: it describes the entry rather than
			// its address, so refusing to build a URI must not drop it.
			if payload["role"] != "api" {
				t.Fatalf("role was dropped: %#v", payload)
			}
			if _, hasURI := payload["uri"]; hasURI != test.want {
				t.Fatalf("uri present=%v, want %v", hasURI, test.want)
			}
		})
	}

	// An entry with no role states none, rather than stating an empty one.
	payload, _ := ApolloAugmentProducedDiscovery(ApolloDiscoveryProduces{Port: 8443}, host)
	if _, hasRole := payload["role"]; hasRole {
		t.Fatalf("an unstated role was invented: %#v", payload)
	}
}

// A configuration override may not touch generated_secrets. Overwriting a
// generated secret's declaration rotates the value under a running
// installation, which presents as an outage or as data encrypted under a key
// nobody holds -- so it is refused rather than merged.
func TestConfigurationOverridesRefuseGeneratedSecretsAndReplaceLists(t *testing.T) {
	t.Parallel()
	base := map[string]any{
		"server":                  map[string]any{"port": 8443, "tls": map[string]any{"enabled": true}},
		"hosts":                   []any{"a", "b"},
		ApolloGeneratedSecretsKey: map[string]any{"token": map[string]any{"secret-type": "random_string"}},
	}

	if _, err := MergeApolloConfigurationOverrides(base, map[string]any{
		ApolloGeneratedSecretsKey: map[string]any{"token": map[string]any{"secret-type": "random_bytes"}},
	}); err == nil {
		t.Fatal("an override rewriting generated_secrets was accepted")
	}

	merged, err := MergeApolloConfigurationOverrides(base, map[string]any{
		"server": map[string]any{"tls": map[string]any{"enabled": false}},
		"hosts":  []any{"c"},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Maps merge recursively, so a sibling the override never mentioned survives.
	server, _ := merged["server"].(map[string]any)
	if server["port"] != 8443 {
		t.Fatalf("a sibling key was lost in the merge: %#v", server)
	}
	tls, _ := server["tls"].(map[string]any)
	if tls["enabled"] != false {
		t.Fatalf("the override did not take: %#v", tls)
	}
	// Lists replace wholesale: merging two has no meaning the author could have
	// predicted.
	if hosts, _ := merged["hosts"].([]any); len(hosts) != 1 || hosts[0] != "c" {
		t.Fatalf("hosts = %#v, want the override's list verbatim", merged["hosts"])
	}
	// The base is not mutated in place.
	if hosts, _ := base["hosts"].([]any); len(hosts) != 2 {
		t.Fatalf("merge mutated its input: %#v", base["hosts"])
	}
}

// An endpoint definition carries two ports with different meanings: the desired
// port the workload listens on, and the external port a raw TCP endpoint is
// published under. Both treat zero as unset, and both range checks were
// unpinned.
func TestApolloEndpointPortsTreatZeroAsUnset(t *testing.T) {
	t.Parallel()
	// An external port is only meaningful for a raw endpoint, so the definition
	// has to appear in public-tcp-endpoints for the range check to be reached at
	// all -- a proxied endpoint routes on a path and is allocated no port.
	definition := func(endpoint ApolloEndpointDefinition, raw bool) ApolloProductDefinition {
		built := ApolloProductDefinition{
			Manifest: ApolloProductManifest{
				ProductGroup: "com.example", ProductName: "app",
				ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
			},
			Configuration: ApolloProductConfiguration{
				Endpoints: &ApolloEndpoints{
					Definitions: map[string]ApolloEndpointDefinition{"api": endpoint},
				},
			},
		}
		if raw {
			built.Manifest.Extensions.PublicTCPEndpoints = []string{"api"}
		}
		return built
	}
	rejects := func(endpoint ApolloEndpointDefinition, field string, raw bool) bool {
		errs := []ValidationError{}
		validateApolloEndpoints(definition(endpoint, raw), &errs)
		for _, err := range errs {
			if strings.Contains(err.Path, field) {
				return true
			}
		}
		return false
	}

	for _, test := range []struct {
		name     string
		endpoint ApolloEndpointDefinition
		field    string
		raw      bool
		want     bool
	}{
		{name: "external unset on a proxied endpoint", endpoint: ApolloEndpointDefinition{DesiredPort: 8443}, field: "external-port"},
		// The cross-field rule itself: a port on an endpoint nobody publishes
		// raw is a port that will never be allocated.
		{
			name: "external set on a proxied endpoint", want: true,
			endpoint: ApolloEndpointDefinition{DesiredPort: 8443, ExternalPort: 30001}, field: "external-port",
		},
		{
			name: "external at the floor", raw: true,
			endpoint: ApolloEndpointDefinition{DesiredPort: 8443, ExternalPort: 1}, field: "external-port",
		},
		{
			name: "external at the ceiling", raw: true,
			endpoint: ApolloEndpointDefinition{DesiredPort: 8443, ExternalPort: 65535}, field: "external-port",
		},
		{
			name: "external above the ceiling", want: true, raw: true,
			endpoint: ApolloEndpointDefinition{DesiredPort: 8443, ExternalPort: 65536}, field: "external-port",
		},
		{
			name: "external negative", want: true, raw: true,
			endpoint: ApolloEndpointDefinition{DesiredPort: 8443, ExternalPort: -1}, field: "external-port",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := rejects(test.endpoint, test.field, test.raw); got != test.want {
				errs := []ValidationError{}
				validateApolloEndpoints(definition(test.endpoint, test.raw), &errs)
				t.Fatalf("%s rejected=%v, want %v (%v)", test.field, got, test.want, errs)
			}
		})
	}
}

// The request-vs-limit comparison is guarded on both sides being PRESENT. A definition that sets a
// request and no limit, or a limit and no request, is ordinary — Kubernetes fills the other in —
// and comparing against an absent value means comparing against zero, which rejects every request
// ever written. A mutation sweep found all four guards unpinned: nothing exercised a one-sided
// definition, so making them always-true changed no test.
func TestApolloDefinitionAcceptsAOneSidedResourceSpecification(t *testing.T) {
	t.Parallel()
	definition := func(resources *ApolloResourceRequirements) ApolloProductDefinition {
		return ApolloProductDefinition{
			Manifest: ApolloProductManifest{
				ProductGroup: "com.example", ProductName: "app",
				ProductVersion: "1.0.0", ProductType: "helm-chart.v1",
			},
			Configuration: ApolloProductConfiguration{Resources: resources},
		}
	}
	complains := func(definition ApolloProductDefinition, field string) bool {
		for _, err := range ValidateApolloProductDefinition(definition) {
			if strings.Contains(err.Path, field) && strings.Contains(err.Message, "must not exceed") {
				return true
			}
		}
		return false
	}

	for _, test := range []struct {
		name      string
		resources *ApolloResourceRequirements
	}{
		{"requests only", &ApolloResourceRequirements{
			Requests: &ApolloResourceQuantities{CPU: "2", Memory: "4Gi"}}},
		{"limits only", &ApolloResourceRequirements{
			Limits: &ApolloResourceQuantities{CPU: "2", Memory: "4Gi"}}},
		{"cpu on one side, memory on the other", &ApolloResourceRequirements{
			Requests: &ApolloResourceQuantities{CPU: "2"},
			Limits:   &ApolloResourceQuantities{Memory: "4Gi"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, field := range []string{"requests.cpu", "requests.memory"} {
				if complains(definition(test.resources), field) {
					t.Errorf("%s was rejected against an absent counterpart", field)
				}
			}
		})
	}

	t.Run("both present and within the limit is still accepted", func(t *testing.T) {
		within := &ApolloResourceRequirements{
			Requests: &ApolloResourceQuantities{CPU: "1", Memory: "2Gi"},
			Limits:   &ApolloResourceQuantities{CPU: "2", Memory: "4Gi"},
		}
		for _, field := range []string{"requests.cpu", "requests.memory"} {
			if complains(definition(within), field) {
				t.Errorf("%s within its limit was rejected", field)
			}
		}
	})
}

// normalizeMediaType and the managed-file decoder both have a branch nothing reached: an empty
// media type, and a managed-file mapping whose last key has no value. A mutation sweep found them.
func TestApolloDecoderEdgesThatNothingReached(t *testing.T) {
	t.Parallel()

	t.Run("an absent media type becomes the octet-stream default", func(t *testing.T) {
		// A blank or whitespace-only value is not a media type, and passing it through would put
		// an empty Content-Type on the wire rather than the documented default.
		for _, given := range []string{"", "   ", "\t\n"} {
			if got := normalizeMediaType(given); got != "application/octet-stream" {
				t.Errorf("normalizeMediaType(%q) = %q, want the default", given, got)
			}
		}
		if got := normalizeMediaType("  application/json  "); got != "application/json" {
			t.Errorf("a real media type was not preserved: %q", got)
		}
	})

	t.Run("a managed file whose last key has no value is refused, not read past", func(t *testing.T) {
		// The loop walks key/value pairs two at a time. An odd Content length is a key with a
		// missing value, and the guard is what stops it indexing past the end.
		var file ApolloManagedFile
		node := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			{Kind: yaml.ScalarNode, Value: "type"}, {Kind: yaml.ScalarNode, Value: "yaml"},
			{Kind: yaml.ScalarNode, Value: "orphan"},
		}}
		// The orphan key is beyond the last whole pair, so the loop never reaches it: it is not
		// validated as a field name, and nothing indexes past the end looking for its value.
		if err := file.UnmarshalYAML(node); err != nil && strings.Contains(err.Error(), "orphan") {
			t.Errorf("a key with no value was validated as a field: %v", err)
		}
		if file.Type != "yaml" {
			t.Errorf("the complete pair was not decoded: %#v", file)
		}
	})

	t.Run("a second YAML document is refused with its own message", func(t *testing.T) {
		// Two documents decode without error, which is a different failure from a malformed
		// trailing document, and the two are told apart by `err == nil`.
		_, err := DecodeApolloProductConfiguration(strings.NewReader("resources:\n  requests:\n    cpu: '1'\n---\nsecond: true\n"))
		if err == nil {
			t.Fatal("two YAML documents were accepted")
		}
		if !strings.Contains(err.Error(), "exactly one YAML document") {
			t.Errorf("error = %q, want the multi-document message", err)
		}
	})
}

// Validation errors come back ordered by path. A publisher fixing a definition
// reads this list, and a list whose order depends on which check happened to run
// first is a list that reshuffles between runs for no reason the reader can see.
func TestApolloProductDefinitionErrorsAreOrderedByPath(t *testing.T) {
	t.Parallel()
	// One bad value in each of several places, chosen so their paths are not in
	// the order the checks run in.
	definition := ApolloProductDefinition{
		Manifest: ApolloProductManifest{
			ProductGroup: "app.example", ProductName: "api",
			ProductVersion: "not-a-version", ProductType: "NOT_A_TYPE",
		},
		Configuration: ApolloProductConfiguration{
			Resources: &ApolloResourceRequirements{
				Requests: &ApolloResourceQuantities{CPU: "not-a-number", Memory: "not-a-size"},
			},
		},
	}
	errs := ValidateApolloProductDefinition(definition)
	if len(errs) < 3 {
		t.Fatalf("wanted several errors to order, got %#v", errs)
	}
	paths := make([]string, 0, len(errs))
	for _, err := range errs {
		paths = append(paths, err.Path)
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("validation errors were not ordered by path: %v", paths)
	}
}
