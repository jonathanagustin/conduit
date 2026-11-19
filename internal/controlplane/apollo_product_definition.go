package controlplane

import (
	"io"

	"github.com/jonathanagustin/conduit/internal/apollo/productconfiguration"
	"github.com/jonathanagustin/conduit/internal/apollo/productdefinition"
	"github.com/jonathanagustin/conduit/internal/apollo/productmanifest"
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
	ApolloProductTypeHelmChart = productmanifest.ProductTypeHelmChart
	ApolloProductTypeTerraform = productmanifest.ProductTypeTerraform
	ApolloProductTypeService   = productmanifest.ProductTypeService
	ApolloProductTypeServiceV2 = productmanifest.ProductTypeServiceV2
	ApolloProductTypeDaemonSet = productmanifest.ProductTypeDaemonSet
	// The Product Types page enumerates only these two, while its sibling pages
	// write service.v1 in every example — including the Product Manifest page's
	// minimal manifest. Both spellings are accepted here because a conformance
	// validator that rejected either would reject documents the specification
	// itself publishes. Execution is unaffected: the Helm adapter still requires
	// helm-chart.v1, because that is the only type it can actually run.
	ApolloProductTypeAsset = productmanifest.ProductTypeAsset
	ApolloProductTypeHelm  = productmanifest.ProductTypeHelm
)

// Managed file types and reload semantics.
const (
	ApolloManagedFileTypeJSON     = productconfiguration.ManagedFileTypeJSON
	ApolloManagedFileTypeYAML     = productconfiguration.ManagedFileTypeYAML
	ApolloManagedFileTypeYML      = productconfiguration.ManagedFileTypeYML
	ApolloManagedFileTypeTemplate = productconfiguration.ManagedFileTypeTemplate

	ApolloReloadTypeInstall = productconfiguration.ReloadTypeInstall
	ApolloReloadTypeRuntime = productconfiguration.ReloadTypeRuntime
)

// Quorum requirements: how many replicas must be available for the Product to
// answer correctly.
const (
	ApolloQuorumAll            = productmanifest.QuorumAll
	ApolloQuorumAllButOne      = productmanifest.QuorumAllButOne
	ApolloQuorumAny            = productmanifest.QuorumAny
	ApolloQuorumSimpleMajority = productmanifest.QuorumSimpleMajority

	// ApolloDefaultQuorumRequirement is what a Product gets by staying silent.
	ApolloDefaultQuorumRequirement = ApolloQuorumAllButOne
)

// Upgrade strategies: what the platform may do to those replicas to move
// versions.
const (
	ApolloUpgradeStrategyRolling  = productmanifest.UpgradeStrategyRolling
	ApolloUpgradeStrategyShutdown = productmanifest.UpgradeStrategyShutdown
)

// Generated secret types.
const (
	ApolloSecretTypeRandomString = productconfiguration.SecretTypeRandomString
	ApolloSecretTypeRandomBytes  = productconfiguration.SecretTypeRandomBytes
	ApolloSecretTypeRSAPKCS8     = productconfiguration.SecretTypeRSAPKCS8
	ApolloSecretTypeECPKCS8      = productconfiguration.SecretTypeECPKCS8
	ApolloSecretTypeSTIGPassword = productconfiguration.SecretTypeSTIGPassword
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
	ApolloMinimumEndpointPort = productconfiguration.MinimumEndpointPort
	ApolloMaximumEndpointPort = productconfiguration.MaximumEndpointPort
)

// The status endpoint serves these subpaths, which the platform turns into
// Kubernetes liveness and readiness probes. They are constants because the
// Product implements them; a Product cannot choose different ones.
const (
	ApolloLivenessSubpath  = productconfiguration.LivenessSubpath
	ApolloReadinessSubpath = productconfiguration.ReadinessSubpath
	// ApolloHealthSubpath is the third status resource. Liveness and readiness
	// decide whether a replica is restarted or routed to; health is richer
	// operational detail that is reported rather than acted on, which is why it
	// always answers 200 and carries its state in the body.
	ApolloHealthSubpath = productconfiguration.HealthSubpath

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
const ApolloTemplateDirectory = productconfiguration.TemplateDirectory

// ApolloManagedFileMountPath is the directory managed files are rendered into
// inside the main container.
func ApolloManagedFileMountPath(productName string) string {
	return productconfiguration.ManagedFileMountPath(productName)
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
	return productconfiguration.StatusProbePaths(endpoint)
}

// ApolloHealthProbePath renders the health resource the reported-state pipeline
// polls, relative to the same status endpoint.
func ApolloHealthProbePath(endpoint ApolloEndpointDefinition) string {
	return productconfiguration.HealthProbePath(endpoint)
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
	return productdefinition.Validate(definitionValidationInput(definition))
}

// definitionValidationInput keeps the legacy carrier and its decoding identity intact.
func definitionValidationInput(definition ApolloProductDefinition) productdefinition.Input {
	return productdefinition.Input{Manifest: definition.Manifest, Configuration: definition.Configuration, Templates: definition.Templates}
}

// ValidateApolloVolumeOverride retains the existing compatibility entry point.
func ValidateApolloVolumeOverride(name, current, override string) error {
	return productdefinition.ValidateVolumeOverride(name, current, override)
}

func validateApolloEndpoints(definition ApolloProductDefinition, errs *[]ValidationError) map[string]ApolloEndpointDefinition {
	return productdefinition.ValidateEndpoints(definitionValidationInput(definition), errs)
}

func validateApolloDiscovery(configuration ApolloProductConfiguration, errs *[]ValidationError) (map[string]struct{}, map[string]struct{}) {
	return productdefinition.ValidateDiscovery(configuration, errs)
}

func validateApolloGeneratedSecret(path, name string, secret ApolloGeneratedSecret, errs *[]ValidationError) {
	productdefinition.ValidateGeneratedSecret(path, name, secret, errs)
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
	return productconfiguration.AugmentProducedDiscovery(entry, host)
}

// ApolloGeneratedSecretsKey retains the source name for the protected document block.
const ApolloGeneratedSecretsKey = productconfiguration.GeneratedSecretsKey

// MergeApolloConfigurationOverrides delegates compatibility document overlay semantics.
func MergeApolloConfigurationOverrides(base, override map[string]any) (map[string]any, error) {
	return productconfiguration.MergeOverrides(base, override)
}

// parseApolloMemory retains the existing comparison-only quantity entry point.
func parseApolloMemory(value string) (float64, bool) {
	return productconfiguration.ParseMemory(value)
}

// validateApolloResourceQuantities retains native diagnostic paths and ordering.
func validateApolloResourceQuantities(path string, quantities *ApolloResourceQuantities, errs *[]ValidationError) (float64, float64) {
	return productconfiguration.ValidateResourceQuantities(path, quantities, errs)
}

// ApolloEffectiveResources retains the existing configuration default resolver.
func ApolloEffectiveResources(resources *ApolloResourceRequirements) ApolloResourceRequirements {
	return productconfiguration.EffectiveResources(resources)
}

// ApolloEffectiveReplication retains explicit replica counts, including zero.
func ApolloEffectiveReplication(replication *ApolloReplication) int {
	return productconfiguration.EffectiveReplication(replication)
}
