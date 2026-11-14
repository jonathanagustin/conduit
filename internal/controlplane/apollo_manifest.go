package controlplane

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"
)

// ApolloProductManifest is the current CLI Product Definition boundary. It is
// intentionally separate from the internal Release model: Apollo manifests
// contain repository locators but do not necessarily pin the chart and image
// digests required by the self-operated execution plane.
type ApolloProductManifest struct {
	ManifestVersion string                          `yaml:"manifest-version" json:"manifestVersion"`
	ProductGroup    string                          `yaml:"product-group" json:"productGroup"`
	ProductName     string                          `yaml:"product-name" json:"productName"`
	ProductVersion  string                          `yaml:"product-version" json:"productVersion"`
	ProductType     string                          `yaml:"product-type" json:"productType"`
	Extensions      ApolloProductManifestExtensions `yaml:"extensions" json:"extensions"`
}

type ApolloProductManifestExtensions struct {
	Artifacts                []ApolloManifestArtifact        `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	HelmChart                *ApolloManifestHelmChart        `yaml:"helm-chart,omitempty" json:"helmChart,omitempty"`
	ProductDependencies      []ApolloManifestDependency      `yaml:"product-dependencies,omitempty" json:"productDependencies,omitempty"`
	ProductIncompatibilities []ApolloManifestIncompatibility `yaml:"product-incompatibilities,omitempty" json:"productIncompatibilities,omitempty"`
	RolloutStrategy          string                          `yaml:"rollout-strategy,omitempty" json:"rolloutStrategy,omitempty"`
	ResourceReadinessTimeout string                          `yaml:"resource-readiness-timeout,omitempty" json:"resourceReadinessTimeout,omitempty"`
	ImagePrePullConfig       *ApolloImagePrePullConfig       `yaml:"image-pre-pull-config,omitempty" json:"imagePrePullConfig,omitempty"`
	// VolumesV2 declares the durable volumes a Product may be given. The
	// declaration lives in the manifest because it is a property of the built
	// Product; configuration.yml only sizes what is declared here.
	VolumesV2 map[string]ApolloManifestVolume `yaml:"volumes-v2,omitempty" json:"volumesV2,omitempty"`
	// PublicProxyEndpoints, PublicTCPEndpoints, and PublicUDPEndpoints request
	// ingress for endpoints the Product already defines. Each entry is the name
	// of an endpoints.definitions key rather than a repeated port, so the two
	// documents cannot disagree about which port is exposed.
	PublicProxyEndpoints []string `yaml:"public-proxy-endpoints,omitempty" json:"publicProxyEndpoints,omitempty"`
	PublicTCPEndpoints   []string `yaml:"public-tcp-endpoints,omitempty" json:"publicTcpEndpoints,omitempty"`
	PublicUDPEndpoints   []string `yaml:"public-udp-endpoints,omitempty" json:"publicUdpEndpoints,omitempty"`
	// RequireStableHostname and AllowTrafficWhenNotReady both default to false.
	// They are pointers so that an explicit false survives a round trip as an
	// explicit false: a Product author who writes it is recording a decision, and
	// rewriting their document to drop it loses that.
	RequireStableHostname    *bool `yaml:"require-stable-hostname,omitempty" json:"requireStableHostname,omitempty"`
	AllowTrafficWhenNotReady *bool `yaml:"allow-traffic-when-not-ready,omitempty" json:"allowTrafficWhenNotReady,omitempty"`
	// QuorumRequirement is how many replicas must be available for the Product to
	// answer correctly, and UpgradeStrategy is what the platform may do to those
	// replicas during an upgrade. They travel together because a rolling upgrade
	// is only safe while the quorum requirement stays satisfied.
	QuorumRequirement string               `yaml:"quorum-requirement,omitempty" json:"quorumRequirement,omitempty"`
	UpgradeStrategy   string               `yaml:"upgrade-strategy,omitempty" json:"upgradeStrategy,omitempty"`
	Other             map[string]yaml.Node `yaml:",inline" json:"-"`
}

// ApolloManifestVolume is one entry of the volumes-v2 extension. The manifest
// declares only that the Product wants a volume and what kind it is; sizing
// lives in configuration.yml, which an operator may override per installation.
// Keeping the two apart is what makes the manifest immutable for a Release
// while still letting one installation ask for more disk than another.
type ApolloManifestVolume struct {
	VolumeType ApolloManifestVolumeType `yaml:"volume-type" json:"volumeType"`
}

// ApolloManifestVolumeType is a tagged union over the kinds of volume a Product
// may request. Exactly one member must be present. An empty `durable-volume: {}`
// mapping is how the specification spells the only currently known kind, so the
// member is a pointer: the difference between an absent key and an empty
// mapping is the whole signal.
type ApolloManifestVolumeType struct {
	DurableVolume *ApolloDurableVolume `yaml:"durable-volume,omitempty" json:"durableVolume,omitempty"`
}

// ApolloDurableVolume is a volume whose contents survive Pod replacement. It
// carries no fields today; the type exists so that adding one later is not a
// breaking change to every declaration.
type ApolloDurableVolume struct{}

type ApolloImagePrePullConfig struct {
	Timeout string `yaml:"timeout" json:"timeout"`
}

type ApolloManifestArtifact struct {
	Type string `yaml:"type" json:"type"`
	URI  string `yaml:"uri" json:"uri"`
}

type ApolloManifestHelmChart struct {
	Name          string `yaml:"helm-chart-name" json:"helmChartName"`
	Version       string `yaml:"helm-chart-version" json:"helmChartVersion"`
	RepositoryURL string `yaml:"helm-repository-url" json:"helmRepositoryUrl"`
}

type ApolloManifestDependency struct {
	ProductGroup       string `yaml:"product-group" json:"productGroup"`
	ProductName        string `yaml:"product-name" json:"productName"`
	MinimumVersion     string `yaml:"minimum-version" json:"minimumVersion"`
	MaximumVersion     string `yaml:"maximum-version" json:"maximumVersion"`
	RecommendedVersion string `yaml:"recommended-version,omitempty" json:"recommendedVersion,omitempty"`
	Optional           bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type ApolloManifestIncompatibility struct {
	ProductGroup   string `yaml:"product-group" json:"productGroup"`
	ProductName    string `yaml:"product-name" json:"productName"`
	ProductType    string `yaml:"product-type,omitempty" json:"productType,omitempty"`
	MinimumVersion string `yaml:"minimum-version,omitempty" json:"minimumVersion,omitempty"`
	MaximumVersion string `yaml:"maximum-version,omitempty" json:"maximumVersion,omitempty"`
}

func DecodeApolloProductManifest(reader io.Reader) (ApolloProductManifest, []ValidationError, error) {
	decoder := yaml.NewDecoder(io.LimitReader(reader, 1<<20))
	decoder.KnownFields(true)
	var manifest ApolloProductManifest
	if err := decoder.Decode(&manifest); err != nil {
		return ApolloProductManifest{}, nil, fmt.Errorf("decode Apollo Product manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ApolloProductManifest{}, nil, errors.New("Apollo Product manifest must contain exactly one YAML document")
		}
		return ApolloProductManifest{}, nil, fmt.Errorf("decode trailing Apollo Product manifest data: %w", err)
	}
	return manifest, ValidateApolloProductManifest(manifest), nil
}

func ValidateApolloProductManifest(manifest ApolloProductManifest) []ValidationError {
	errs := make([]ValidationError, 0)
	add := func(path, message string) { errs = append(errs, ValidationError{Path: path, Message: message}) }
	if manifest.ManifestVersion != "1.0" {
		add("manifest-version", "must be 1.0")
	}
	if !coordinatePattern.MatchString(manifest.ProductGroup + ":" + manifest.ProductName) {
		add("product", "product-group and product-name must form a group:name coordinate")
	}
	if !versionPattern.MatchString(manifest.ProductVersion) {
		add("product-version", "must be an Apollo orderable version")
	}
	if manifest.ProductType != "helm-chart.v1" {
		add("product-type", "must be helm-chart.v1 for the Helm manifest adapter")
	}
	helm := manifest.Extensions.HelmChart
	if helm == nil {
		add("extensions.helm-chart", "is required")
	} else {
		if strings.TrimSpace(helm.Name) == "" {
			add("extensions.helm-chart.helm-chart-name", "must not be empty")
		}
		if !versionPattern.MatchString(helm.Version) {
			add("extensions.helm-chart.helm-chart-version", "must be an Apollo orderable version")
		}
		if !validApolloRepositoryURL(helm.RepositoryURL) {
			add("extensions.helm-chart.helm-repository-url", "must use https:// or oci://")
		}
	}
	if strategy := manifest.Extensions.RolloutStrategy; strategy != "" &&
		strategy != RolloutManageRollout && strategy != RolloutApplyChangesNoWait {
		add("extensions.rollout-strategy", "must be manageRollout or applyChangesNoWait")
	}
	if timeout := manifest.Extensions.ResourceReadinessTimeout; timeout != "" {
		if duration, err := time.ParseDuration(timeout); err != nil || duration <= 0 || duration > 24*time.Hour {
			add("extensions.resource-readiness-timeout", "must be a positive duration no greater than 24h")
		}
	}
	if config := manifest.Extensions.ImagePrePullConfig; config != nil {
		if duration, err := time.ParseDuration(config.Timeout); err != nil || duration < time.Minute || duration > 90*time.Minute {
			add("extensions.image-pre-pull-config.timeout", "must be between 1m and 90m")
		}
	}
	for index, artifact := range manifest.Extensions.Artifacts {
		path := fmt.Sprintf("extensions.artifacts[%d]", index)
		if artifact.Type != "oci" {
			add(path+".type", "must be oci")
		}
		if strings.TrimSpace(artifact.URI) == "" || strings.Contains(artifact.URI, "://") {
			add(path+".uri", "must be an OCI reference without a URL scheme")
		}
	}
	seenDependencies := make(map[string]struct{}, len(manifest.Extensions.ProductDependencies))
	for index, dependency := range manifest.Extensions.ProductDependencies {
		path := fmt.Sprintf("extensions.product-dependencies[%d]", index)
		coordinate := dependency.ProductGroup + ":" + dependency.ProductName
		if !coordinatePattern.MatchString(coordinate) {
			add(path, "product-group and product-name must form a coordinate")
		}
		if _, exists := seenDependencies[coordinate]; exists {
			add(path, "must not duplicate a Product dependency")
		}
		seenDependencies[coordinate] = struct{}{}
		validateApolloManifestRange(path, dependency.MinimumVersion, dependency.MaximumVersion, true, &errs)
		if dependency.RecommendedVersion != "" {
			if !versionPattern.MatchString(dependency.RecommendedVersion) ||
				compareVersions(dependency.RecommendedVersion, dependency.MinimumVersion) < 0 ||
				!versionWithinMaximum(dependency.RecommendedVersion, dependency.MaximumVersion) {
				add(path+".recommended-version", "must be an orderable version inside the dependency range")
			}
		}
	}
	seenIncompatibilities := make(map[string]struct{}, len(manifest.Extensions.ProductIncompatibilities))
	for index, incompatibility := range manifest.Extensions.ProductIncompatibilities {
		path := fmt.Sprintf("extensions.product-incompatibilities[%d]", index)
		coordinate := incompatibility.ProductGroup + ":" + incompatibility.ProductName
		if !coordinatePattern.MatchString(coordinate) {
			add(path, "product-group and product-name must form a coordinate")
		}
		key := coordinate + ":" + incompatibility.ProductType
		if _, exists := seenIncompatibilities[key]; exists {
			add(path, "must not duplicate a Product incompatibility")
		}
		seenIncompatibilities[key] = struct{}{}
		validateApolloManifestRange(path, incompatibility.MinimumVersion, incompatibility.MaximumVersion, false, &errs)
	}
	validateApolloManifestServiceExtensions(manifest.Extensions, &errs)
	return errs
}

// validateApolloManifestServiceExtensions checks the extensions that describe a
// running workload rather than a packaging locator. They are validated for every
// Product type because a Helm Product that declares volumes-v2 means the same
// thing by it as a service Product does.
func validateApolloManifestServiceExtensions(extensions ApolloProductManifestExtensions, errs *[]ValidationError) {
	add := func(path, message string) { *errs = append(*errs, ValidationError{Path: path, Message: message}) }
	for _, name := range sortedKeys(extensions.VolumesV2) {
		volume := extensions.VolumesV2[name]
		path := fmt.Sprintf("extensions.volumes-v2.%s", name)
		if !apolloVolumeNamePattern.MatchString(name) {
			add(path, "volume name must be alphanumerics and hyphens")
		}
		if volume.VolumeType.DurableVolume == nil {
			add(path+".volume-type", "must declare a volume type; durable-volume is the only known type")
		}
	}
	proxy := validateApolloPublicEndpointNames("extensions.public-proxy-endpoints", extensions.PublicProxyEndpoints, errs)
	validateApolloPublicEndpointNames("extensions.public-tcp-endpoints", extensions.PublicTCPEndpoints, errs)
	validateApolloPublicEndpointNames("extensions.public-udp-endpoints", extensions.PublicUDPEndpoints, errs)
	// An endpoint cannot be both proxied and raw TCP or UDP: the proxy terminates
	// TLS and routes on a path, while the raw paths forward an allocated external
	// port. TCP and UDP together are allowed, and are how a Product asks for both
	// protocols on one external port.
	for index, name := range extensions.PublicTCPEndpoints {
		if _, exists := proxy[name]; exists {
			add(fmt.Sprintf("extensions.public-tcp-endpoints[%d]", index),
				"must not also appear in public-proxy-endpoints")
		}
	}
	for index, name := range extensions.PublicUDPEndpoints {
		if _, exists := proxy[name]; exists {
			add(fmt.Sprintf("extensions.public-udp-endpoints[%d]", index),
				"must not also appear in public-proxy-endpoints")
		}
	}
	switch extensions.QuorumRequirement {
	case "", ApolloQuorumAll, ApolloQuorumAllButOne, ApolloQuorumAny, ApolloQuorumSimpleMajority:
	default:
		add("extensions.quorum-requirement", "must be all, all-but-one, any, or simple-majority")
	}
	switch extensions.UpgradeStrategy {
	case "", ApolloUpgradeStrategyRolling, ApolloUpgradeStrategyShutdown:
	default:
		add("extensions.upgrade-strategy", "must be rolling or shutdown")
	}
}

// validateApolloPublicEndpointNames checks one of the two ingress extensions and
// returns the set of names it exposed, which the cross-document checks reuse.
func validateApolloPublicEndpointNames(path string, names []string, errs *[]ValidationError) map[string]struct{} {
	add := func(path, message string) { *errs = append(*errs, ValidationError{Path: path, Message: message}) }
	seen := make(map[string]struct{}, len(names))
	for index, name := range names {
		entry := fmt.Sprintf("%s[%d]", path, index)
		if strings.TrimSpace(name) == "" {
			add(entry, "must name an endpoints.definitions entry")
			continue
		}
		if _, exists := seen[name]; exists {
			add(entry, "must not repeat an endpoint name")
		}
		seen[name] = struct{}{}
	}
	return seen
}

// ExecutableReleaseFromApolloManifest adapts only the stricter subset that can
// preserve Conduit's exact-digest execution invariant. A valid public Apollo
// manifest may still be non-executable here when it uses mutable locators.
func ExecutableReleaseFromApolloManifest(manifest ApolloProductManifest) (Release, error) {
	if errs := ValidateApolloProductManifest(manifest); len(errs) > 0 {
		return Release{}, ValidationErrors(errs)
	}
	chart, ok := artifactFromImmutableOCIReference(manifest.Extensions.HelmChart.RepositoryURL)
	if !ok {
		return Release{}, errors.New("Helm repository URL must be an immutable oci:// reference with an explicit tag and @sha256 digest")
	}
	images := make([]Artifact, 0, len(manifest.Extensions.Artifacts))
	for index, candidate := range manifest.Extensions.Artifacts {
		artifact, exact := artifactFromImmutableOCIReference(candidate.URI)
		if !exact {
			return Release{}, fmt.Errorf("extensions.artifacts[%d].uri must contain an explicit tag and @sha256 digest", index)
		}
		images = append(images, artifact)
	}
	if len(images) == 0 {
		return Release{}, errors.New("at least one exact image artifact is required")
	}
	release := Release{
		Product: manifest.ProductGroup + ":" + manifest.ProductName, Version: manifest.ProductVersion,
		Chart: chart, Images: images, Channels: defaultChannelsForVersion(manifest.ProductVersion),
		RolloutStrategy: manifest.Extensions.RolloutStrategy,
	}
	for _, dependency := range manifest.Extensions.ProductDependencies {
		release.Dependencies = append(release.Dependencies, Dependency{
			Product:        dependency.ProductGroup + ":" + dependency.ProductName,
			MinimumVersion: dependency.MinimumVersion, MaximumVersion: dependency.MaximumVersion, Optional: dependency.Optional,
		})
	}
	for _, incompatibility := range manifest.Extensions.ProductIncompatibilities {
		release.Incompatibilities = append(release.Incompatibilities, Incompatibility{
			Product:        incompatibility.ProductGroup + ":" + incompatibility.ProductName,
			MinimumVersion: incompatibility.MinimumVersion, MaximumVersion: incompatibility.MaximumVersion,
		})
	}
	if manifest.Extensions.ResourceReadinessTimeout != "" {
		duration, _ := time.ParseDuration(manifest.Extensions.ResourceReadinessTimeout)
		release.ResourceReadinessTimeoutSeconds = int(duration.Seconds())
	}
	if manifest.Extensions.ImagePrePullConfig != nil {
		duration, _ := time.ParseDuration(manifest.Extensions.ImagePrePullConfig.Timeout)
		release.ImagePrePullTimeoutSeconds = int(duration.Seconds())
	}
	return release, nil
}

func artifactFromImmutableOCIReference(reference string) (Artifact, bool) {
	reference = strings.TrimPrefix(strings.TrimSpace(reference), "oci://")
	parts := strings.Split(reference, "@")
	if len(parts) != 2 || !digestPattern.MatchString(parts[1]) {
		return Artifact{}, false
	}
	colon, slash := strings.LastIndex(parts[0], ":"), strings.LastIndex(parts[0], "/")
	if colon <= slash || colon == len(parts[0])-1 {
		return Artifact{}, false
	}
	artifact := Artifact{Repository: parts[0][:colon], Tag: parts[0][colon+1:], Digest: parts[1]}
	if artifact.Repository == "" || artifact.Tag == "latest" {
		return Artifact{}, false
	}
	return artifact, true
}

func validateApolloManifestRange(path, minimum, maximum string, required bool, errs *[]ValidationError) {
	if (required || minimum != "") && !versionPattern.MatchString(minimum) {
		*errs = append(*errs, ValidationError{Path: path + ".minimum-version", Message: "must be an Apollo orderable version"})
	}
	if (required || maximum != "") && !versionMatcherPattern.MatchString(maximum) {
		*errs = append(*errs, ValidationError{Path: path + ".maximum-version", Message: "must be an Apollo version matcher"})
	}
	if minimum != "" && maximum != "" && versionPattern.MatchString(minimum) &&
		versionMatcherPattern.MatchString(maximum) && !versionWithinMaximum(minimum, maximum) {
		*errs = append(*errs, ValidationError{Path: path, Message: "minimum-version must not exceed maximum-version"})
	}
}
