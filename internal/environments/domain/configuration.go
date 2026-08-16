package domain

import "time"

// ImagePrePullPolicy is an explicit Environment override. A nil policy uses the
// Apollo-aligned default (enabled with a 15-minute timeout); Enabled false is a
// deliberate opt-out for Environments without a native image-cache preparer.
type ImagePrePullPolicy struct {
	Enabled        bool `json:"enabled"`
	TimeoutSeconds int  `json:"timeoutSeconds,omitempty"`
}

// HelmExecutionDefaults are what an Environment applies when a Release does not
// say otherwise.
type HelmExecutionDefaults struct {
	DefaultRolloutStrategy                 string              `json:"defaultRolloutStrategy,omitempty"`
	InstallResourceReadinessTimeoutSeconds int                 `json:"installResourceReadinessTimeoutSeconds,omitempty"`
	UpgradeResourceReadinessTimeoutSeconds int                 `json:"upgradeResourceReadinessTimeoutSeconds,omitempty"`
	ImagePrePull                           *ImagePrePullPolicy `json:"imagePrePull,omitempty"`
}

// HelmExecutionPolicy is the resolved form of those defaults for one
// installation: every question answered, nothing left to fall back on.
type HelmExecutionPolicy struct {
	RolloutStrategy                        string `json:"rolloutStrategy"`
	InstallResourceReadinessTimeoutSeconds int    `json:"installResourceReadinessTimeoutSeconds,omitempty"`
	UpgradeResourceReadinessTimeoutSeconds int    `json:"upgradeResourceReadinessTimeoutSeconds,omitempty"`
	ImagePrePull                           bool   `json:"imagePrePull,omitempty"`
	ImagePrePullTimeoutSeconds             int    `json:"imagePrePullTimeoutSeconds,omitempty"`
}

// ArtifactSecurityPolicy is an Environment-owned install/promotion gate. Its
// presence requires every selected Release to carry a complete, digest-matched
// normalized vulnerability assessment.
type ArtifactSecurityPolicy struct {
	// RequiredOutcome is PASS for channels that reject active vulnerabilities,
	// or ANY when promotion only requires evidence that scanning completed.
	RequiredOutcome string `json:"requiredOutcome"`
	// MaximumScanAgeSeconds bounds how old a selected Release's vulnerability
	// assessment may be when an upgrade is proposed. Zero leaves freshness
	// unchecked, which is the prior behaviour: the policy already requires a
	// complete, passing, digest-matched scan to exist, but not a recent one, so
	// an Entity could move to a Release whose passing scan had aged out. A
	// positive value makes a stale assessment trigger a fresh scan before the
	// upgrade proceeds, so "a scan must occur before an upgrade" holds on
	// recency and not only on a scan having happened once. Freshness is enforced
	// where the version-move is decided and a clock is available -- proposal-time
	// enrichment -- not in the clock-free desired-state validation, whose purity
	// keeps a generation's meaning immutable.
	MaximumScanAgeSeconds int `json:"maximumScanAgeSeconds,omitempty"`
}

// ArtifactAvailabilityPolicy is the same kind of gate for whether the artifacts
// can actually be fetched.
type ArtifactAvailabilityPolicy struct {
	RequiredOutcome string `json:"requiredOutcome"`
}

// VulnerabilityUpgradePolicy is the resolved, Environment-effective form of the
// Hub setting that permits upgrades toward vulnerability-recalled Releases.
type VulnerabilityUpgradePolicy struct {
	// AllowRecalledForVulnerabilities enables the exception. It is off unless
	// declared, so the default behavior stays "never upgrade to a recalled
	// Release".
	AllowRecalledForVulnerabilities bool `json:"allowRecalledForVulnerabilities,omitempty"`
	// RescanSLASeconds bounds how old scan evidence may be and still authorize an
	// upgrade. Zero means evidence freshness is not checked, which is only
	// appropriate where scanning is driven synchronously.
	RescanSLASeconds int `json:"rescanSlaSeconds,omitempty"`
}

// ArtifactRegistryAssignment is a registry an Environment may pull from.
type ArtifactRegistryAssignment struct {
	ID                  string   `json:"id"`
	URL                 string   `json:"url"`
	Revision            int      `json:"revision"`
	Mirrors             []string `json:"mirrors,omitempty"`
	Authentication      string   `json:"authentication,omitempty"`
	CredentialRef       string   `json:"credentialRef,omitempty"`
	ImagePullSecretName string   `json:"imagePullSecretName,omitempty"`
}

// RegistryAccess is the resolved form of an assignment for one installation:
// which registry the artifacts came from and which one the runtime will use.
type RegistryAccess struct {
	AssignmentID        string `json:"assignmentId"`
	Revision            int    `json:"revision"`
	SourceRegistry      string `json:"sourceRegistry"`
	RuntimeRegistry     string `json:"runtimeRegistry"`
	CredentialRef       string `json:"credentialRef,omitempty"`
	ImagePullSecretName string `json:"imagePullSecretName,omitempty"`
}

// ExternalSecretAssignment identifies write-only material owned by an
// Environment-side provider. ProviderRef is opaque to the Hub; Revision is the
// immutable rotation coordinate carried into Plans.
type ExternalSecretAssignment struct {
	Name                 string   `json:"name"`
	Revision             int      `json:"revision"`
	ProviderRef          string   `json:"providerRef"`
	KubernetesSecretName string   `json:"kubernetesSecretName"`
	Keys                 []string `json:"keys"`
}

// SecretRequirement is what an installation needs from such an assignment.
type SecretRequirement struct {
	Name   string   `json:"name"`
	Secret string   `json:"secret"`
	Keys   []string `json:"keys"`
}

// RampedRolloutConstraint holds an installation back until a promotion's ramp
// window has elapsed.
type RampedRolloutConstraint struct {
	PromotionID   string    `json:"promotionId"`
	WindowSeconds int       `json:"windowSeconds"`
	StartedAt     time.Time `json:"startedAt"`
	NotBefore     time.Time `json:"notBefore"`
}

// Organization is a tenant within a namespace.
type Organization struct {
	DisplayName     string `json:"displayName"`
	PrimaryHostname string `json:"primaryHostname"`
}

// InstalledTarget is what is actually installed somewhere, as observed.
type InstalledTarget struct {
	Version      string `json:"version"`
	ConfigDigest string `json:"configDigest"`
}

// NamespaceTarget is a namespace an Environment manages, and what sits in it.
type NamespaceTarget struct {
	RID                string                     `json:"rid"`
	Name               string                     `json:"name"`
	Description        string                     `json:"description,omitempty"`
	DeletionProtected  *bool                      `json:"deletionProtected,omitempty"`
	EnrollmentRID      string                     `json:"enrollmentRid,omitempty"`
	Organizations      []Organization             `json:"organizations,omitempty"`
	Trashed            bool                       `json:"trashed,omitempty"`
	OntologyCountValid bool                       `json:"ontologyCountValid"`
	Installed          map[string]InstalledTarget `json:"installed,omitempty"`
}
