package controlplane

import (
	"encoding/json"
	"time"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	validationdomain "github.com/jonathanagustin/conduit/internal/validation/domain"
)

// DesiredState is the reviewed, exact state for one environment. Git stores
// this document; the control plane validates it and produces an execution plan.
type DesiredState struct {
	APIVersion      string                     `json:"apiVersion"`
	Kind            string                     `json:"kind"`
	Environment     Environment                `json:"environment"`
	Releases        []Release                  `json:"releases"`
	Promotions      []PromotionRecord          `json:"promotions,omitempty"`
	Recalls         []Recall                   `json:"recalls,omitempty"`
	ReleaseChannels []ReleaseChannelDefinition `json:"releaseChannels,omitempty"`
	// ProductSettings carries per-Product operational settings that are not
	// properties of any single Release, such as how fast a version may spread
	// across a Release Channel.
	ProductSettings   []ProductSettings  `json:"productSettings,omitempty"`
	Entities          []Entity           `json:"entities"`
	OwnershipHandoffs []OwnershipHandoff `json:"ownershipHandoffs,omitempty"`
}

type ReleaseChannelDefinition struct {
	Name              string              `json:"name"`
	Description       string              `json:"description,omitempty"`
	LabelRequirements map[string][]string `json:"labelRequirements,omitempty"`
	Archived          bool                `json:"archived,omitempty"`
}

type OwnershipHandoff struct {
	InstallationID string `json:"installationId"`
	Rationale      string `json:"rationale"`
}

// ProductSettings are Product-scoped settings independent of Release metadata.
type ProductSettings struct {
	Product           string                    `json:"product"`
	RampedRollout     *RampedRolloutSettings    `json:"rampedRollout,omitempty"`
	PromotionPipeline *ProductPromotionPipeline `json:"promotionPipeline,omitempty"`
}

type ProductPromotionPipeline struct {
	Enabled bool                    `json:"enabled"`
	Stages  []ProductPromotionStage `json:"stages"`
}

type ProductPromotionStage struct {
	FromChannel string          `json:"fromChannel"`
	ToChannel   string          `json:"toChannel"`
	Policy      PromotionPolicy `json:"policy"`
}

// RampedRolloutSettings paces how a version spreads across a Release Channel.
// Windows are declared per channel and may name a channel that no promotion
// pipeline targets, which is why this is a Product setting rather than a property
// of one promotion stage.
type RampedRolloutSettings struct {
	Enabled bool                          `json:"enabled"`
	Windows []ReleaseChannelRolloutWindow `json:"windows,omitempty"`
}

// ReleaseChannelRolloutWindow is the minimum period after which every Entity on
// one channel is eligible to upgrade.
type ReleaseChannelRolloutWindow struct {
	Channel       string `json:"channel"`
	WindowSeconds int    `json:"windowSeconds"`
}

// rolloutWindowFor reports the configured window for one Product and channel.
func rolloutWindowFor(settings []ProductSettings, product, channel string) (int, bool) {
	for _, candidate := range settings {
		if candidate.Product != product || candidate.RampedRollout == nil || !candidate.RampedRollout.Enabled {
			continue
		}
		for _, window := range candidate.RampedRollout.Windows {
			if window.Channel == channel && window.WindowSeconds > 0 {
				return window.WindowSeconds, true
			}
		}
	}
	return 0, false
}

type Environment struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
	// PackageArchitecture is the operator-declared target for Zarf/UDS
	// package Plans. It is required only when a package is selected. The
	// Spoke independently checks its configured architecture before execution.
	//
	// CONDUIT-INVENTED: environments.PackageTargetArchitecture
	// Displaced-by: a published or owner-approved Environment capability contract establishing package target architecture and its admission semantics.
	PackageArchitecture string `json:"packageArchitecture,omitempty"`
	// ConnectionSetting and DeclaredStateSource place this Environment in a Hub
	// topology. They gate Plan issuance, Agent connection, relayed state
	// admission, promotion participation, and whether an imported Bundle may
	// overwrite settings. See federation.go for the resolved semantics.
	ConnectionSetting          string                       `json:"connectionSetting,omitempty"`
	DeclaredStateSource        string                       `json:"declaredStateSource,omitempty"`
	DefaultReleaseChannel      string                       `json:"defaultReleaseChannel,omitempty"`
	Labels                     map[string]string            `json:"labels,omitempty"`
	Config                     map[string]any               `json:"config,omitempty"`
	Namespaces                 []NamespaceTarget            `json:"namespaces,omitempty"`
	ArtifactSecurityPolicy     *ArtifactSecurityPolicy      `json:"artifactSecurityPolicy,omitempty"`
	ArtifactAvailabilityPolicy *ArtifactAvailabilityPolicy  `json:"artifactAvailabilityPolicy,omitempty"`
	ArtifactRegistries         []ArtifactRegistryAssignment `json:"artifactRegistries,omitempty"`
	Secrets                    []ExternalSecretAssignment   `json:"secrets,omitempty"`
	AutomaticSecurityRecall    bool                         `json:"automaticSecurityRecall,omitempty"`
	VulnerabilityUpgrades      *VulnerabilityUpgradePolicy  `json:"vulnerabilityUpgrades,omitempty"`
	AutoSuppressionThreshold   int                          `json:"autoSuppressionThreshold,omitempty"`
	Maintenance                MaintenancePolicy            `json:"maintenance,omitempty"`
	HelmExecution              *HelmExecutionDefaults       `json:"helmExecution,omitempty"`
}

type MaintenancePolicy struct {
	Downtime   *WeeklyMaintenanceWindow `json:"downtime,omitempty"`
	NoDowntime *WeeklyMaintenanceWindow `json:"noDowntime,omitempty"`
	Overrides  []MaintenanceOverride    `json:"overrides,omitempty"`
}

type EntityMaintenancePolicy struct {
	Downtime                         *WeeklyMaintenanceWindow `json:"downtime,omitempty"`
	NoDowntime                       *WeeklyMaintenanceWindow `json:"noDowntime,omitempty"`
	OverrideProductMaintenanceWindow bool                     `json:"overrideProductMaintenanceWindow,omitempty"`
}

type Entity struct {
	Name           string            `json:"name"`
	Product        string            `json:"product"`
	Version        string            `json:"version"`
	Labels         map[string]string `json:"labels,omitempty"`
	ReleaseChannel string            `json:"releaseChannel,omitempty"`
	Namespace      string            `json:"namespace,omitempty"`
	// HelmReleaseName overrides the Helm release this Entity manages. It defaults
	// to the Entity name and exists to adopt a pre-existing Helm release whose
	// name Platform did not choose.
	// Provenance (reviewed 2026-08-02):
	// https://www.palantir.com/docs/apollo/managing-environments/environment-settings/
	HelmReleaseName string `json:"helmReleaseName,omitempty"`
	// Stack scopes dependency resolution: a Product dependency is satisfied only
	// by an Entity on the same stack, unless DependencyStackOverrides names where
	// to look for that specific dependency. An Environment that declares no
	// stacks has one implicit stack, so this changes nothing until it is used.
	Stack                    string                              `json:"stack,omitempty"`
	DependencyStackOverrides map[string]string                   `json:"dependencyStackOverrides,omitempty"`
	DependencyGroups         []string                            `json:"dependencyGroups,omitempty"`
	DependencyGroupOverrides map[string][]string                 `json:"dependencyGroupOverrides,omitempty"`
	IgnoredDependencies      []string                            `json:"ignoredDependencies,omitempty"`
	RequiresDowntime         bool                                `json:"requiresDowntime,omitempty"`
	Maintenance              EntityMaintenancePolicy             `json:"maintenance,omitempty"`
	HelmValues               json.RawMessage                     `json:"helmValues,omitempty"`
	ConfigOverrides          string                              `json:"configOverrides,omitempty"`
	Configuration            *ManagedInstallationConfiguration   `json:"configuration,omitempty"`
	Universal                *UniversalInstallationConfiguration `json:"universal,omitempty"`
	SecretRequirements       []SecretRequirement                 `json:"secretRequirements,omitempty"`
	HealthQuorum             string                              `json:"healthQuorum,omitempty"`
	recallRollOff            bool
	recallIDs                []string
	upgradeRationale         *UpgradeRationale
}

// Plan step actions. A reconcile step converges an installation on desired
// state; an uninstall step removes one the new generation no longer declares.
const (
	planActionReconcile = "reconcile"
	planActionUninstall = "uninstall"
	PlanActionUnmanage  = "unmanage"

	RolloutManageRollout      = catalog.RolloutManageRollout
	RolloutApplyChangesNoWait = catalog.RolloutApplyChangesNoWait
)

type HealthTarget struct {
	InstallationID string `json:"installationId"`
	Entity         string `json:"entity"`
	Product        string `json:"product"`
	Version        string `json:"version"`
	Namespace      string `json:"namespace"`
	Quorum         string `json:"quorum"`
}

type HealthObservation struct {
	InstallationID string              `json:"installationId"`
	ObservedAt     time.Time           `json:"observedAt"`
	Details        EntityHealthDetails `json:"details"`
}

// ValidationError retains the legacy source name for a native diagnostic.
type ValidationError = validationdomain.Problem
