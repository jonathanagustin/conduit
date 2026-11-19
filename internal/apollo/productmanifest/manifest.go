// Package productmanifest owns the compatibility Product document boundary.
// Native release admission and semantic policy remain with their callers.
package productmanifest

import "go.yaml.in/yaml/v4"

// Manifest is the current CLI Product Definition boundary. It is
// intentionally separate from the internal Release model: Apollo manifests
// contain repository locators but do not necessarily pin the chart and image
// digests required by the self-operated execution plane.
type Manifest struct {
	ManifestVersion string     `yaml:"manifest-version" json:"manifestVersion"`
	ProductGroup    string     `yaml:"product-group" json:"productGroup"`
	ProductName     string     `yaml:"product-name" json:"productName"`
	ProductVersion  string     `yaml:"product-version" json:"productVersion"`
	ProductType     string     `yaml:"product-type" json:"productType"`
	Extensions      Extensions `yaml:"extensions" json:"extensions"`
}

type Extensions struct {
	Artifacts                []Artifact          `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	HelmChart                *HelmChart          `yaml:"helm-chart,omitempty" json:"helmChart,omitempty"`
	ProductDependencies      []Dependency        `yaml:"product-dependencies,omitempty" json:"productDependencies,omitempty"`
	ProductIncompatibilities []Incompatibility   `yaml:"product-incompatibilities,omitempty" json:"productIncompatibilities,omitempty"`
	RolloutStrategy          string              `yaml:"rollout-strategy,omitempty" json:"rolloutStrategy,omitempty"`
	ResourceReadinessTimeout string              `yaml:"resource-readiness-timeout,omitempty" json:"resourceReadinessTimeout,omitempty"`
	ImagePrePullConfig       *ImagePrePullConfig `yaml:"image-pre-pull-config,omitempty" json:"imagePrePullConfig,omitempty"`
	// VolumesV2 declares the durable volumes a Product may be given. The
	// declaration lives in the manifest because it is a property of the built
	// Product; configuration.yml only sizes what is declared here.
	VolumesV2 map[string]Volume `yaml:"volumes-v2,omitempty" json:"volumesV2,omitempty"`
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

// Volume is one entry of the volumes-v2 extension. The manifest
// declares only that the Product wants a volume and what kind it is; sizing
// lives in configuration.yml, which an operator may override per installation.
// Keeping the two apart is what makes the manifest immutable for a Release
// while still letting one installation ask for more disk than another.
type Volume struct {
	VolumeType VolumeType `yaml:"volume-type" json:"volumeType"`
}

// VolumeType is a tagged union over the kinds of volume a Product
// may request. Exactly one member must be present. An empty `durable-volume: {}`
// mapping is how the specification spells the only currently known kind, so the
// member is a pointer: the difference between an absent key and an empty
// mapping is the whole signal.
type VolumeType struct {
	DurableVolume *DurableVolume `yaml:"durable-volume,omitempty" json:"durableVolume,omitempty"`
}

// DurableVolume is a volume whose contents survive Pod replacement. It
// carries no fields today; the type exists so that adding one later is not a
// breaking change to every declaration.
type DurableVolume struct{}

type ImagePrePullConfig struct {
	Timeout string `yaml:"timeout" json:"timeout"`
}

type Artifact struct {
	Type string `yaml:"type" json:"type"`
	URI  string `yaml:"uri" json:"uri"`
}

type HelmChart struct {
	Name          string `yaml:"helm-chart-name" json:"helmChartName"`
	Version       string `yaml:"helm-chart-version" json:"helmChartVersion"`
	RepositoryURL string `yaml:"helm-repository-url" json:"helmRepositoryUrl"`
}

type Dependency struct {
	ProductGroup       string `yaml:"product-group" json:"productGroup"`
	ProductName        string `yaml:"product-name" json:"productName"`
	MinimumVersion     string `yaml:"minimum-version" json:"minimumVersion"`
	MaximumVersion     string `yaml:"maximum-version" json:"maximumVersion"`
	RecommendedVersion string `yaml:"recommended-version,omitempty" json:"recommendedVersion,omitempty"`
	Optional           bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

type Incompatibility struct {
	ProductGroup   string `yaml:"product-group" json:"productGroup"`
	ProductName    string `yaml:"product-name" json:"productName"`
	ProductType    string `yaml:"product-type,omitempty" json:"productType,omitempty"`
	MinimumVersion string `yaml:"minimum-version,omitempty" json:"minimumVersion,omitempty"`
	MaximumVersion string `yaml:"maximum-version,omitempty" json:"maximumVersion,omitempty"`
}
