package controlplane

import (
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jonathanagustin/conduit/internal/apollo/productconfiguration"
	subst "github.com/jonathanagustin/conduit/internal/apollo/substitution"
)

// A Product Definition is the pair of documents a Product ships: manifest.yml
// describes what the Product is and what it needs from the platform, and
// configuration.yml describes how one installation of it is wired. Platform
// already modelled the manifest for Helm packaging; this file adds the
// configuration document and, more importantly, the cross-document checks that
// only exist when the two are read together.
//
// Those cross-document checks are the reason this is a definition type rather
// than two independent validators. A public-proxy-endpoint that names an
// endpoint definition which does not exist, a desired-size for a volume the
// manifest never declared, a {{secret(...)}} with no matching generated_secrets
// entry: each document is individually well-formed and the pair is unusable.
// Publication is the last point at which that is cheap to find.
//
// manifest.yml is required and configuration.yml is optional, so a Product with
// no runtime configuration is a valid definition rather than a decode failure.
//
// This is a reconstructed model of the public Apollo Product Specification. It
// is not a claim of compatibility with any private Apollo service.

// Product types whose installations are described by a configuration.yml.
const (
	ApolloProductTypeHelmChart = "helm-chart.v1"
	ApolloProductTypeTerraform = "terraform.v1"
	ApolloProductTypeService   = "service.v1"
	ApolloProductTypeServiceV2 = "service.v2"
	ApolloProductTypeDaemonSet = "daemon-set.v1"
	// The Product Types page enumerates only these two, while its sibling pages
	// write service.v1 in every example — including the Product Manifest page's
	// minimal manifest. Both spellings are accepted here because a conformance
	// validator that rejected either would reject documents the specification
	// itself publishes. Execution is unaffected: the Helm adapter still requires
	// helm-chart.v1, because that is the only type it can actually run.
	ApolloProductTypeAsset = "asset.v1"
	ApolloProductTypeHelm  = "helm.v1"
)

// Managed file types and reload semantics.
const (
	ApolloManagedFileTypeJSON     = "json"
	ApolloManagedFileTypeYAML     = "yaml"
	ApolloManagedFileTypeYML      = "yml"
	ApolloManagedFileTypeTemplate = "tmpl"

	ApolloReloadTypeInstall = productconfiguration.ReloadTypeInstall
	ApolloReloadTypeRuntime = "runtime"
)

// Quorum requirements: how many replicas must be available for the Product to
// answer correctly.
const (
	ApolloQuorumAll            = "all"
	ApolloQuorumAllButOne      = "all-but-one"
	ApolloQuorumAny            = "any"
	ApolloQuorumSimpleMajority = "simple-majority"

	// ApolloDefaultQuorumRequirement is what a Product gets by staying silent.
	ApolloDefaultQuorumRequirement = ApolloQuorumAllButOne
)

// Upgrade strategies: what the platform may do to those replicas to move
// versions.
const (
	ApolloUpgradeStrategyRolling  = "rolling"
	ApolloUpgradeStrategyShutdown = "shutdown"
)

// Generated secret types.
const (
	ApolloSecretTypeRandomString = "random_string"
	ApolloSecretTypeRandomBytes  = "random_bytes"
	ApolloSecretTypeRSAPKCS8     = "rsa_pkcs8"
	ApolloSecretTypeECPKCS8      = "ec_pkcs8"
	ApolloSecretTypeSTIGPassword = "stig_password"
)

// ApolloDefaultRandomStringLength is the length a random_string takes when the
// declaration does not state one.
const ApolloDefaultRandomStringLength = 16

// Values the platform supplies when a definition and its overrides both stay
// silent. They are named constants rather than literals at the point of use
// because each one is a documented promise about what an installation gets, and
// a reader checking that promise should find one place to look.
const (
	ApolloDefaultReplication = productconfiguration.DefaultReplication
	ApolloDefaultCPU         = productconfiguration.DefaultCPU
	ApolloDefaultMemory      = productconfiguration.DefaultMemory
	// ApolloDefaultEndpointPath is what an endpoint serves when it names no path.
	ApolloDefaultEndpointPath = productconfiguration.DefaultEndpointPath
)

// Endpoint ports are drawn from a bounded range: below it are the privileged
// ports a container should not bind, and above it is the ephemeral range the
// node allocates from.
const (
	ApolloMinimumEndpointPort = 1024
	ApolloMaximumEndpointPort = 19999
)

// The status endpoint serves these subpaths, which the platform turns into
// Kubernetes liveness and readiness probes. They are constants because the
// Product implements them; a Product cannot choose different ones.
const (
	ApolloLivenessSubpath  = "/status/liveness"
	ApolloReadinessSubpath = "/status/readiness"
	// ApolloHealthSubpath is the third status resource. Liveness and readiness
	// decide whether a replica is restarted or routed to; health is richer
	// operational detail that is reported rather than acted on, which is why it
	// always answers 200 and carries its state in the body.
	ApolloHealthSubpath = "/status/health"

	// ApolloProbeIntervalSeconds is how often the platform polls. It is part of
	// the contract rather than a tuning knob: a Product promises these endpoints
	// answer at this rate without a performance cost, caching if it must.
	ApolloProbeIntervalSeconds = 5
	// A probe response in this range means live, or ready. Anything outside it,
	// and any transport error, means the opposite.
	ApolloProbeHealthyMinimumStatus = 200
	ApolloProbeHealthyMaximumStatus = 399
)

// ApolloTemplateDirectory is where a Product ships the Go templates that back
// its tmpl managed files.
const ApolloTemplateDirectory = "deployment/templates"

// ApolloManagedFileMountPath is the directory managed files are rendered into
// inside the main container.
func ApolloManagedFileMountPath(productName string) string {
	return "/opt/palantir/services/" + productName + "/var/conf"
}

// ApolloProductDefinition is the manifest/configuration pair for one Product
// version, plus the template files that pair refers to.
type ApolloProductDefinition struct {
	Manifest      ApolloProductManifest      `json:"manifest"`
	Configuration ApolloProductConfiguration `json:"configuration"`
	// Templates are the file names present under deployment/templates, without
	// the directory prefix. A tmpl managed file whose template is missing is a
	// definition that renders an empty configuration file at install time, so
	// the names travel with the pair rather than being checked separately.
	Templates []string `json:"templates,omitempty"`
}

// ApolloProductConfiguration retains source callers while its document lives in compatibility.
type ApolloProductConfiguration = productconfiguration.Configuration
type ApolloResourceRequirements = productconfiguration.ResourceRequirements
type ApolloResourceQuantities = productconfiguration.ResourceQuantities
type ApolloReplication = productconfiguration.Replication
type ApolloVolumeConfiguration = productconfiguration.VolumeConfiguration
type ApolloDurableStorageConfiguration = productconfiguration.DurableStorageConfiguration
type ApolloEndpoints = productconfiguration.Endpoints
type ApolloEndpointDefinition = productconfiguration.EndpointDefinition
type ApolloManagedFile = productconfiguration.ManagedFile
type ApolloGeneratedSecret = productconfiguration.GeneratedSecret
type ApolloDiscovery = productconfiguration.Discovery
type ApolloDiscoveryProduces = productconfiguration.DiscoveryProduces
type ApolloDiscoveryConsumes = productconfiguration.DiscoveryConsumes

// ApolloMaximumVolumeSize preserves the existing compatibility validation bound.
const ApolloMaximumVolumeSize = productconfiguration.MaximumVolumeSize

// ApolloStatusProbePaths renders the liveness and readiness paths the platform
// polls, relative to the status endpoint's own path. Deriving them here keeps
// the join in one place: a Product that serves /my-app must be probed at
// /my-app/status/liveness, not at the container root.
func ApolloStatusProbePaths(endpoint ApolloEndpointDefinition) (liveness, readiness string) {
	base := strings.TrimSuffix(endpoint.EffectivePath(), "/")
	return base + ApolloLivenessSubpath, base + ApolloReadinessSubpath
}

// ApolloHealthProbePath renders the health resource the reported-state pipeline
// polls, relative to the same status endpoint.
func ApolloHealthProbePath(endpoint ApolloEndpointDefinition) string {
	return strings.TrimSuffix(endpoint.EffectivePath(), "/") + ApolloHealthSubpath
}

// DecodeApolloProductConfiguration delegates the optional structural document decoder.
func DecodeApolloProductConfiguration(reader io.Reader) (ApolloProductConfiguration, error) {
	return productconfiguration.Decode(reader)
}

// DecodeApolloProductDefinition decodes both documents and validates them as a
// pair. A nil configuration reader is the Product that ships no
// configuration.yml, which the specification allows.
func DecodeApolloProductDefinition(manifest, configuration io.Reader) (ApolloProductDefinition, []ValidationError, error) {
	return DecodeApolloProductDefinitionWithTemplates(manifest, configuration, nil)
}

// DecodeApolloProductDefinitionWithTemplates is DecodeApolloProductDefinition
// with the names of the files shipped under deployment/templates, so tmpl
// managed files can be checked against what the tarball actually contains.
func DecodeApolloProductDefinitionWithTemplates(
	manifest, configuration io.Reader, templates []string,
) (ApolloProductDefinition, []ValidationError, error) {
	decoded, _, err := DecodeApolloProductManifest(manifest)
	if err != nil {
		return ApolloProductDefinition{}, nil, err
	}
	decodedConfiguration, err := DecodeApolloProductConfiguration(configuration)
	if err != nil {
		return ApolloProductDefinition{}, nil, err
	}
	definition := ApolloProductDefinition{
		Manifest: decoded, Configuration: decodedConfiguration, Templates: templates,
	}
	return definition, ValidateApolloProductDefinition(definition), nil
}

// ValidateApolloProductDefinition validates the pair. Manifest packaging rules
// stay with the packaging validator: a service.v1 Product has no Helm chart, so
// requiring one here would reject every conformant service definition.
func ValidateApolloProductDefinition(definition ApolloProductDefinition) []ValidationError {
	errs := make([]ValidationError, 0)
	add := func(path, message string) { errs = append(errs, ValidationError{Path: path, Message: message}) }
	manifest := definition.Manifest
	// manifest-version is optional here, unlike in the packaging validator. The
	// Product Manifest page lists exactly four required fields and its minimal
	// example carries no manifest-version; apollo-cli emits one, so the execution
	// path can keep requiring it, but a hand-written conformant manifest must not
	// be rejected for omitting a field the specification never asked for.
	if manifest.ManifestVersion != "" && manifest.ManifestVersion != "1.0" {
		add("manifest.manifest-version", "must be 1.0 when present")
	}
	if !coordinatePattern.MatchString(manifest.ProductGroup + ":" + manifest.ProductName) {
		add("manifest.product", "product-group and product-name must form a group:name coordinate")
	}
	// A Product Release may declare a non-orderable version. Nothing may sort it
	// against another version, but refusing to publish it would be this validator
	// narrowing the specification rather than enforcing it.
	if !IsApolloProductVersion(manifest.ProductVersion) {
		add("manifest.product-version", "must be an Apollo orderable or non-orderable version")
	}
	switch manifest.ProductType {
	case ApolloProductTypeHelmChart, ApolloProductTypeTerraform,
		ApolloProductTypeService, ApolloProductTypeServiceV2, ApolloProductTypeDaemonSet,
		ApolloProductTypeAsset, ApolloProductTypeHelm:
	default:
		add("manifest.product-type", "must be a known Apollo Product type")
	}
	manifestErrs := make([]ValidationError, 0)
	validateApolloManifestServiceExtensions(manifest.Extensions, &manifestErrs)
	for _, err := range manifestErrs {
		errs = append(errs, ValidationError{Path: "manifest." + err.Path, Message: err.Message})
	}
	errs = append(errs, validateApolloProductConfiguration(definition)...)
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Path < errs[j].Path })
	return errs
}

func validateApolloProductConfiguration(definition ApolloProductDefinition) []ValidationError {
	errs := make([]ValidationError, 0)
	add := func(path, message string) { errs = append(errs, ValidationError{Path: path, Message: message}) }
	configuration := definition.Configuration

	if resources := configuration.Resources; resources != nil {
		requestCPU, requestMemory := validateApolloResourceQuantities("configuration.resources.requests", resources.Requests, &errs)
		limitCPU, limitMemory := validateApolloResourceQuantities("configuration.resources.limits", resources.Limits, &errs)
		if requestCPU > 0 && limitCPU > 0 && requestCPU > limitCPU {
			add("configuration.resources.requests.cpu", "must not exceed the CPU limit")
		}
		if requestMemory > 0 && limitMemory > 0 && requestMemory > limitMemory {
			add("configuration.resources.requests.memory", "must not exceed the memory limit")
		}
	}
	// Zero replicas is not the documented way to stop a workload: desired must be
	// greater than zero, and an installation is stopped through its Entity rather
	// than by publishing a Product that asks for nothing.
	if replication := configuration.Replication; replication != nil && replication.Desired != nil && *replication.Desired < 1 {
		add("configuration.replication.desired", "must be greater than zero")
	}

	validateApolloVolumeConfiguration(definition, &errs)
	definitions := validateApolloEndpoints(definition, &errs)
	validateApolloManagedFiles(definition, &errs)

	for _, name := range sortedKeys(configuration.GeneratedSecrets) {
		validateApolloGeneratedSecret("configuration.generated_secrets."+name, name, configuration.GeneratedSecrets[name], &errs)
	}

	validateApolloConfTags(configuration.Conf, &errs)
	produced, consumed := validateApolloDiscovery(configuration, &errs)
	errs = append(errs, validateApolloDefinitionSubstitutions(definition, definitions, produced, consumed)...)
	return errs
}

// validateApolloVolumeConfiguration checks the volumes section against the
// manifest in both directions. A configured volume with no declaration is a size
// the platform will not provision; a declared volume with no configuration is a
// mount the Product expects and will not get.
func validateApolloVolumeConfiguration(definition ApolloProductDefinition, errs *[]ValidationError) {
	add := func(path, message string) { *errs = append(*errs, ValidationError{Path: path, Message: message}) }
	declaredVolumes := definition.Manifest.Extensions.VolumesV2
	configured := definition.Configuration.Volumes
	for _, name := range sortedKeys(configured) {
		errorPath := "configuration.volumes." + name
		declared, exists := declaredVolumes[name]
		if !exists {
			add(errorPath, "must name a volume declared by the volumes-v2 manifest extension")
		}
		volume := configured[name]
		if strings.TrimSpace(volume.DesiredSize) != "" {
			add(errorPath+".desired-size", "must be nested under durable-storage-configuration")
		}
		if volume.DurableStorageConfiguration == nil {
			add(errorPath, "must declare durable-storage-configuration")
			continue
		}
		// The two unions have to agree: durable-storage-configuration is the
		// configuration for a durable-volume, and pairing it with any other
		// declared type would configure storage the volume does not have.
		if exists && declared.VolumeType.DurableVolume == nil {
			add(errorPath+".durable-storage-configuration",
				"volume is not declared as a durable-volume")
		}
		validateApolloVolumeSize(
			errorPath+".durable-storage-configuration.desired-size",
			volume.DurableStorageConfiguration.DesiredSize, errs)
	}
	for _, name := range sortedKeys(declaredVolumes) {
		if _, exists := configured[name]; !exists {
			add("manifest.extensions.volumes-v2."+name,
				"must have corresponding configuration under the volumes key")
		}
	}
}

// validateApolloVolumeSize checks a desired-size against the storage grammar,
// which is narrower than the compute one: a whole number of megabytes,
// gigabytes, or terabytes, with the unit required.
func validateApolloVolumeSize(path, value string, errs *[]ValidationError) {
	add := func(message string) { *errs = append(*errs, ValidationError{Path: path, Message: message}) }
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		add("must be a positive integer followed by M, G, or T")
		return
	}
	match := apolloVolumeSizePattern.FindStringSubmatch(trimmed)
	if match == nil {
		add("must be a positive integer followed by M, G, or T")
		return
	}
	amount, err := strconv.ParseFloat(match[1], 64)
	if err != nil || amount <= 0 {
		add("must be greater than zero")
		return
	}
	size := amount * apolloVolumeSizeScales[match[2]]
	maximum, _ := parseApolloVolumeSize(ApolloMaximumVolumeSize)
	if size > maximum {
		add("must not exceed " + ApolloMaximumVolumeSize)
	}
}

// parseApolloVolumeSize converts a storage quantity to a comparable magnitude.
func parseApolloVolumeSize(value string) (float64, bool) {
	match := apolloVolumeSizePattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, false
	}
	amount, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return amount * apolloVolumeSizeScales[match[2]], true
}

// ValidateApolloVolumeOverride checks an installation's replacement size for one
// volume. Only upsizing is permitted: a smaller volume cannot hold what the
// current one does, so the platform would have to discard data to satisfy it.
func ValidateApolloVolumeOverride(name, current, override string) error {
	existing, ok := parseApolloVolumeSize(current)
	if !ok {
		return fmt.Errorf("volume %s has an unparseable current size %q", name, current)
	}
	requested, ok := parseApolloVolumeSize(override)
	if !ok {
		return fmt.Errorf("volume %s override size %q must be a positive integer followed by M, G, or T", name, override)
	}
	if requested < existing {
		return fmt.Errorf("volume %s may only be upsized: %s is smaller than %s", name, override, current)
	}
	return nil
}

// validateApolloEndpoints checks the endpoint definitions and the manifest's
// ingress extensions against each other, and returns the definitions so the
// substitution checks can reuse them.
func validateApolloEndpoints(definition ApolloProductDefinition, errs *[]ValidationError) map[string]ApolloEndpointDefinition {
	add := func(path, message string) { *errs = append(*errs, ValidationError{Path: path, Message: message}) }
	definitions := map[string]ApolloEndpointDefinition{}
	configuration := definition.Configuration
	if configuration.Endpoints != nil {
		definitions = configuration.Endpoints.Definitions
	}

	// A raw endpoint has no request path to route on and needs an external port
	// allocated; a proxied endpoint is the reverse. Which list a name appears in
	// decides which of the two fields the definition must carry.
	raw := map[string]string{}
	for _, name := range definition.Manifest.Extensions.PublicTCPEndpoints {
		raw[name] = "public-tcp-endpoints"
	}
	for _, name := range definition.Manifest.Extensions.PublicUDPEndpoints {
		if existing, exists := raw[name]; exists {
			raw[name] = existing + " and public-udp-endpoints"
			continue
		}
		raw[name] = "public-udp-endpoints"
	}
	proxied := make(map[string]struct{}, len(definition.Manifest.Extensions.PublicProxyEndpoints))
	for _, name := range definition.Manifest.Extensions.PublicProxyEndpoints {
		proxied[name] = struct{}{}
	}

	// Two endpoints may share a port only when their paths distinguish them.
	type route struct {
		port int
		path string
	}
	routes := map[route]string{}

	for _, name := range sortedKeys(definitions) {
		errorPath := "configuration.endpoints.definitions." + name
		endpoint := definitions[name]
		if !apolloEndpointNamePattern.MatchString(name) {
			add(errorPath, "endpoint name must be kebab-cased from a-z and 0-9")
		}
		if endpoint.DesiredPort < ApolloMinimumEndpointPort || endpoint.DesiredPort > ApolloMaximumEndpointPort {
			add(errorPath+".desired-port", fmt.Sprintf("must be between %d and %d",
				ApolloMinimumEndpointPort, ApolloMaximumEndpointPort))
		}
		if endpoint.Path != "" && !strings.HasPrefix(endpoint.Path, "/") {
			add(errorPath+".path", "must be an absolute path")
		}
		if endpoint.ExternalPort != 0 && (endpoint.ExternalPort < 1 || endpoint.ExternalPort > 65535) {
			add(errorPath+".external-port", "must be between 1 and 65535")
		}
		if endpoint.DesiredPort != 0 {
			key := route{port: endpoint.DesiredPort, path: endpoint.EffectivePath()}
			if previous, exists := routes[key]; exists {
				add(errorPath, "must not repeat the port and path of "+previous)
			} else {
				routes[key] = name
			}
		}
		if list, exposed := raw[name]; exposed {
			if endpoint.ExternalPort == 0 {
				add(errorPath+".external-port", "must be set for an endpoint in "+list)
			}
			if endpoint.Path != "" {
				add(errorPath+".path", "must not be set for an endpoint in "+list)
			}
			continue
		}
		if endpoint.ExternalPort != 0 {
			add(errorPath+".external-port",
				"must only be set for an endpoint in public-tcp-endpoints or public-udp-endpoints")
		}
	}

	if configuration.Endpoints != nil {
		if len(definitions) == 0 {
			add("configuration.endpoints.definitions", "must define at least one endpoint")
		}
		status := configuration.Endpoints.SLSStatusEndpoint
		switch {
		case status != "":
			if _, exists := definitions[status]; !exists {
				add("configuration.endpoints.sls-status-endpoint", "must name an endpoints.definitions entry")
			}
		case len(definitions) > 1:
			// With one endpoint the choice is unambiguous. With more, an absent
			// selection means the platform would have to guess which port serves
			// the status resource it polls to decide the Product is alive.
			add("configuration.endpoints.sls-status-endpoint",
				"must be set when more than one endpoint is defined")
		}
	}

	for index, name := range definition.Manifest.Extensions.PublicProxyEndpoints {
		if _, exists := definitions[name]; name != "" && !exists {
			add(fmt.Sprintf("manifest.extensions.public-proxy-endpoints[%d]", index),
				"must name an endpoints.definitions entry")
		}
	}
	for index, name := range definition.Manifest.Extensions.PublicTCPEndpoints {
		if _, exists := definitions[name]; name != "" && !exists {
			add(fmt.Sprintf("manifest.extensions.public-tcp-endpoints[%d]", index),
				"must name an endpoints.definitions entry")
		}
	}
	return definitions
}

func validateApolloManagedFiles(definition ApolloProductDefinition, errs *[]ValidationError) {
	add := func(path, message string) { *errs = append(*errs, ValidationError{Path: path, Message: message}) }
	configuration := definition.Configuration
	templates := make(map[string]struct{}, len(definition.Templates))
	for _, template := range definition.Templates {
		templates[path.Base(template)] = struct{}{}
	}
	for _, name := range sortedKeys(configuration.ManagedFiles) {
		file := configuration.ManagedFiles[name]
		errorPath := "configuration.managed_files." + name
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			add(errorPath, "must be a relative path inside the Product directory")
		}
		fileType := file.Type
		if file.Shorthand {
			// The shorthand's type is the key's extension, so an unknown
			// extension is not a defaulting question: there is no type to use.
			fileType = strings.TrimPrefix(path.Ext(name), ".")
			if fileType == "" {
				add(errorPath, "shorthand managed file name must have a type extension")
			}
		}
		switch fileType {
		case ApolloManagedFileTypeJSON, ApolloManagedFileTypeYAML, ApolloManagedFileTypeYML, ApolloManagedFileTypeTemplate:
		default:
			add(errorPath+".type", "must be json, yaml, yml, or tmpl")
		}
		switch file.ReloadType {
		case "", ApolloReloadTypeInstall, ApolloReloadTypeRuntime:
		default:
			add(errorPath+".reload-type", "must be install or runtime")
		}
		if fileType == ApolloManagedFileTypeTemplate {
			// A tmpl file renders from the whole conf tree through its template,
			// so it needs the template rather than a content selector.
			if definition.Templates != nil {
				if _, exists := templates[name+".tmpl"]; !exists {
					add(errorPath, fmt.Sprintf("requires a %s/%s.tmpl template", ApolloTemplateDirectory, name))
				}
			}
			continue
		}
		content, isString := file.Content.(string)
		switch {
		case file.Content == nil:
			add(errorPath+".content", "must name a portion of the conf block")
		case isString:
			if _, exists := subst.LookupConfPath(configuration.Conf, []string{content}); !exists {
				add(errorPath+".content", "must name a key of the conf block")
			}
		}
	}
}

// validateApolloConfTags checks the reserved conf key that feeds discovery tags.
// It is the only part of the conf tree whose shape Platform constrains.
func validateApolloConfTags(conf map[string]any, errs *[]ValidationError) {
	add := func(path, message string) { *errs = append(*errs, ValidationError{Path: path, Message: message}) }
	raw, exists := conf["tags"]
	if !exists {
		return
	}
	values, ok := raw.([]any)
	if !ok {
		add("configuration.conf.tags", "must be a list of tag names")
		return
	}
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		tag, ok := value.(string)
		if !ok || !apolloNamePattern.MatchString(tag) {
			add(fmt.Sprintf("configuration.conf.tags[%d]", index), "must be a tag name")
			continue
		}
		if _, exists := seen[tag]; exists {
			add(fmt.Sprintf("configuration.conf.tags[%d]", index), "must not repeat a tag")
		}
		seen[tag] = struct{}{}
	}
}

func validateApolloDiscovery(
	configuration ApolloProductConfiguration, errs *[]ValidationError,
) (map[string]struct{}, map[string]struct{}) {
	add := func(path, message string) { *errs = append(*errs, ValidationError{Path: path, Message: message}) }
	produced := map[string]struct{}{}
	consumed := map[string]struct{}{}
	if configuration.Discovery == nil {
		return produced, consumed
	}
	for index, stack := range configuration.Discovery.ProduceToAdditionalStacks {
		if !apolloNamePattern.MatchString(stack) {
			add(fmt.Sprintf("configuration.discovery.produce-to-additional-stacks[%d]", index), "must be a stack name")
		}
	}
	for _, name := range sortedKeys(configuration.Discovery.Produces) {
		errorPath := "configuration.discovery.produces." + name
		produces := configuration.Discovery.Produces[name]
		if !apolloNamePattern.MatchString(name) {
			add(errorPath, "must be a discovery name")
		}
		produced[name] = struct{}{}
		if produces.Role != "" && !apolloNamePattern.MatchString(produces.Role) {
			add(errorPath+".role", "must be a discovery role name")
		}
		if produces.Port != 0 && (produces.Port < 1 || produces.Port > 65535) {
			add(errorPath+".port", "must be between 1 and 65535")
		}
		if produces.Path != "" && !strings.HasPrefix(produces.Path, "/") {
			add(errorPath+".path", "must be an absolute path")
		}
		switch produces.Scheme {
		case "", "http", "https":
		default:
			add(errorPath+".scheme", "must be http or https")
		}
		// scheme and path only mean anything once a port makes the entry a URI
		// candidate; stating them without one is a reference that will resolve to
		// no uri at install time.
		if produces.Port == 0 && (produces.Scheme != "" || produces.Path != "") {
			add(errorPath+".port", "must be set for an entry declaring a scheme or path")
		}
	}
	for _, name := range sortedKeys(configuration.Discovery.Consumes) {
		errorPath := "configuration.discovery.consumes." + name
		consumes := configuration.Discovery.Consumes[name]
		if !apolloNamePattern.MatchString(name) {
			add(errorPath, "must be a discovery name")
		}
		consumed[name] = struct{}{}
		if consumes.Role == "" {
			add(errorPath+".role", "must name the role the entry consumes")
		} else if !apolloNamePattern.MatchString(consumes.Role) {
			add(errorPath+".role", "must be a discovery role name")
		}
		if consumes.Stack != "" && !apolloNamePattern.MatchString(consumes.Stack) {
			add(errorPath+".stack", "must be a stack name")
		}
		if consumes.Select != "" && !apolloNamePattern.MatchString(consumes.Select) {
			add(errorPath+".select", "must name a field of the produced entry")
		}
		seen := make(map[string]struct{}, len(consumes.Tags))
		for index, tag := range consumes.Tags {
			if !apolloNamePattern.MatchString(tag) {
				add(fmt.Sprintf("%s.tags[%d]", errorPath, index), "must be a tag name")
			}
			if _, exists := seen[tag]; exists {
				add(fmt.Sprintf("%s.tags[%d]", errorPath, index), "must not repeat a tag")
			}
			seen[tag] = struct{}{}
		}
	}
	return produced, consumed
}

func validateApolloGeneratedSecret(path, name string, secret ApolloGeneratedSecret, errs *[]ValidationError) {
	add := func(field, message string) {
		*errs = append(*errs, ValidationError{Path: path + field, Message: message})
	}
	if !apolloNamePattern.MatchString(name) {
		add("", "secret name must be alphanumeric with hyphens or underscores")
	}
	if secret.Length < 0 {
		add(".length", "must not be negative")
	}
	charsetTyped := secret.SecretType == ApolloSecretTypeRandomString
	if !charsetTyped && (secret.AllowChars != "" || secret.DisallowChars != "") {
		add("", "allow-chars and disallow-chars apply only to random_string")
	}
	if secret.AllowChars != "" && secret.DisallowChars != "" {
		add("", "must not set both allow-chars and disallow-chars")
	}
	switch secret.SecretType {
	case ApolloSecretTypeRandomString, ApolloSecretTypeRandomBytes, ApolloSecretTypeSTIGPassword:
	case ApolloSecretTypeRSAPKCS8:
		switch secret.Length {
		case 0, 2048, 3072, 4096:
		default:
			add(".length", "must be 2048, 3072, or 4096")
		}
	case ApolloSecretTypeECPKCS8:
		// The standard curves the specification names. Its own example writes
		// 2048 here, which is an RSA key size and not one of the curves it lists
		// two paragraphs earlier; the enumerated values win.
		switch secret.Length {
		case 0, 224, 256, 384, 521:
		default:
			add(".length", "must be 224, 256, 384, or 521")
		}
	default:
		add(".secret-type",
			"must be random_string, random_bytes, rsa_pkcs8, ec_pkcs8, or stig_password")
	}
}

// validateApolloDefinitionSubstitutions walks every string the definition
// carries and checks that each substitution names something the definition
// declares. Template managed files are skipped: their content is a Go template
// rendered from the substituted conf tree, and its braces are not this
// language's.
func validateApolloDefinitionSubstitutions(
	definition ApolloProductDefinition, definitions map[string]ApolloEndpointDefinition,
	produced, consumed map[string]struct{},
) []ValidationError {
	errs := make([]ValidationError, 0)
	configuration := definition.Configuration
	check := func(path, value string) {
		substitutions, err := subst.Parse(value)
		if err != nil {
			errs = append(errs, ValidationError{Path: path, Message: err.Error()})
			return
		}
		for _, substitution := range substitutions {
			if message := apolloSubstitutionTargetError(substitution, definition, definitions, produced, consumed); message != "" {
				errs = append(errs, ValidationError{Path: path, Message: message})
			}
		}
	}
	walkApolloStrings("configuration.conf", configuration.Conf, check)
	if configuration.Discovery != nil {
		for _, name := range sortedKeys(configuration.Discovery.Produces) {
			produces := configuration.Discovery.Produces[name]
			for _, key := range sortedKeys(produces.Payload) {
				node := produces.Payload[key]
				var value any
				if err := node.Decode(&value); err != nil {
					continue
				}
				walkApolloStrings(
					fmt.Sprintf("configuration.discovery.produces.%s.%s", name, key), value, check)
			}
		}
	}
	return errs
}

func apolloSubstitutionTargetError(
	substitution subst.Substitution, definition ApolloProductDefinition,
	definitions map[string]ApolloEndpointDefinition, produced, consumed map[string]struct{},
) string {
	switch substitution.Kind {
	case subst.KindSecret:
		if _, exists := definition.Configuration.GeneratedSecrets[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no generated_secrets entry", substitution.Raw)
		}
	case subst.KindDiscovered:
		if _, exists := consumed[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no discovery.consumes entry", substitution.Raw)
		}
	case subst.KindSelfDiscovered:
		if _, exists := produced[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no discovery.produces entry", substitution.Raw)
		}
	case subst.KindConf:
		if _, exists := subst.LookupConfPath(
			definition.Configuration.Conf, substitution.ConfSegments()); !exists {
			return fmt.Sprintf("substitution %s names no conf entry", substitution.Raw)
		}
	case subst.KindVolume:
		if _, exists := definition.Manifest.Extensions.VolumesV2[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no volumes-v2 volume", substitution.Raw)
		}
	case subst.KindEndpoint:
		if _, exists := definitions[substitution.Argument]; !exists {
			return fmt.Sprintf("substitution %s names no endpoints.definitions entry", substitution.Raw)
		}
		if _, known := subst.CanonicalEndpointField(substitution.Field); !known {
			return fmt.Sprintf("substitution %s names an unknown endpoint field", substitution.Raw)
		}
	case subst.KindSSL:
		if _, known := subst.SSLFile(substitution.Argument); !known {
			return fmt.Sprintf("substitution %s names an unknown ssl field", substitution.Raw)
		}
	}
	return ""
}

func walkApolloStrings(path string, value any, visit func(path, value string)) {
	switch typed := value.(type) {
	case string:
		visit(path, typed)
	case map[string]any:
		for _, key := range sortedKeys(typed) {
			walkApolloStrings(path+"."+key, typed[key], visit)
		}
	case map[any]any:
		keys := make([]string, 0, len(typed))
		converted := make(map[string]any, len(typed))
		for key, child := range typed {
			name := fmt.Sprint(key)
			keys = append(keys, name)
			converted[name] = child
		}
		sort.Strings(keys)
		for _, key := range keys {
			walkApolloStrings(path+"."+key, converted[key], visit)
		}
	case []any:
		for index, child := range typed {
			walkApolloStrings(fmt.Sprintf("%s[%d]", path, index), child, visit)
		}
	}
}

// ApolloAugmentProducedDiscovery adds uri and uris to a produced discovery entry
// whose port makes it addressable. Augmentation is producer-side and automatic:
// the platform knows the Kubernetes Service it created for the Product, so a
// consumer that asks for uris gets an address that matches what was actually
// built rather than one the Product author typed and has to keep in step.
//
// It reports whether the entry was a candidate. An entry with no valid port
// carries information other than an address and is returned unchanged.
func ApolloAugmentProducedDiscovery(entry ApolloDiscoveryProduces, host string) (map[string]any, bool) {
	payload := map[string]any{}
	for key, node := range entry.Payload {
		var value any
		if err := node.Decode(&value); err == nil {
			payload[key] = value
		}
	}
	if entry.Role != "" {
		payload["role"] = entry.Role
	}
	if entry.Port < 1 || entry.Port > 65535 || strings.TrimSpace(host) == "" {
		return payload, false
	}
	scheme := entry.Scheme
	if scheme == "" {
		scheme = "https"
	}
	requestPath := entry.Path
	if requestPath == "" {
		requestPath = "/"
	}
	uri := fmt.Sprintf("%s://%s:%d%s", scheme, host, entry.Port, requestPath)
	payload["port"] = entry.Port
	payload["scheme"] = scheme
	payload["path"] = requestPath
	payload["uri"] = uri
	payload["uris"] = []any{uri}
	return payload, true
}

// ApolloGeneratedSecretsKey retains the source name for the protected document block.
const ApolloGeneratedSecretsKey = productconfiguration.GeneratedSecretsKey

// MergeApolloConfigurationOverrides delegates compatibility document overlay semantics.
func MergeApolloConfigurationOverrides(base, override map[string]any) (map[string]any, error) {
	return productconfiguration.MergeOverrides(base, override)
}

var (
	apolloNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)
	// Endpoint names are kebab-cased over a deliberately narrow character set:
	// they become DNS and Service object names downstream, where case is not
	// preserved.
	apolloEndpointNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	apolloVolumeNamePattern   = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	// Storage sizes are a whole number of megabytes, gigabytes, or terabytes.
	apolloVolumeSizePattern = regexp.MustCompile(`^([0-9]+)(M|G|T)$`)
	// Memory accepts the decimal and binary units the Resources extension lists,
	// and a bare byte count.
	apolloMemoryPattern = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(Ki|K|Mi|M|Gi|G|T)?$`)
	// CPU is a unit-less core count, so a suffixed value is rejected rather than
	// silently read as some other unit.
	apolloCPUPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)
)

var (
	apolloVolumeSizeScales = map[string]float64{"M": 1e6, "G": 1e9, "T": 1e12}
	apolloMemoryScales     = map[string]float64{
		"": 1, "K": 1e3, "M": 1e6, "G": 1e9, "T": 1e12,
		"Ki": 1 << 10, "Mi": 1 << 20, "Gi": 1 << 30,
	}
)

// parseApolloMemory converts a memory quantity to a comparable magnitude. The
// result is only used for ordering comparisons, never round-tripped back into a
// document, so float imprecision cannot corrupt a declared value.
func parseApolloMemory(value string) (float64, bool) {
	match := apolloMemoryPattern.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return 0, false
	}
	number, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return 0, false
	}
	return number * apolloMemoryScales[match[2]], true
}

func validateApolloResourceQuantities(
	path string, quantities *ApolloResourceQuantities, errs *[]ValidationError,
) (float64, float64) {
	if quantities == nil {
		return 0, 0
	}
	add := func(field, message string) {
		*errs = append(*errs, ValidationError{Path: path + field, Message: message})
	}
	var cpu, memory float64
	if trimmed := strings.TrimSpace(quantities.CPU); trimmed != "" {
		parsed, err := strconv.ParseFloat(trimmed, 64)
		switch {
		case !apolloCPUPattern.MatchString(trimmed) || err != nil:
			add(".cpu", "must be a unit-less number of cores")
		case parsed <= 0:
			add(".cpu", "must be greater than zero")
		default:
			cpu = parsed
		}
	}
	if trimmed := strings.TrimSpace(quantities.Memory); trimmed != "" {
		parsed, ok := parseApolloMemory(trimmed)
		switch {
		case !ok:
			add(".memory", "must be a number optionally suffixed Ki, K, Mi, M, Gi, G, or T")
		case parsed <= 0:
			add(".memory", "must be greater than zero")
		default:
			memory = parsed
		}
	}
	return cpu, memory
}

// ApolloEffectiveResources retains the existing configuration default resolver.
func ApolloEffectiveResources(resources *ApolloResourceRequirements) ApolloResourceRequirements {
	return productconfiguration.EffectiveResources(resources)
}

// ApolloEffectiveReplication retains explicit replica counts, including zero.
func ApolloEffectiveReplication(replication *ApolloReplication) int {
	return productconfiguration.EffectiveReplication(replication)
}

func sortedKeys[Value any](values map[string]Value) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
