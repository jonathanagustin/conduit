// Package domain owns what a Release is: the product and version it names, the
// artifacts it ships, what it depends on and conflicts with, and the security
// and availability evidence gathered about it.
//
// It also owns the questions that can be answered from that alone — whether a
// Release has been recalled, which Release Channels it has reached, and whether
// it may cross the boundary into the next one. All are properties of the
// catalogue, not of any environment that might install from it, and none needs
// to know that environments exist.
//
// A promotion is decided here in full: the label contracts, the health evidence
// a stage will accept, how long a state must have held, and what a canary set
// has to show. What is not here is anything that had to be read or written —
// loading the policy, persisting the decision, and refusing a paused stage stay
// with the caller that owns the store.
//
// Nothing here reaches outward. Where a caller's own vocabulary is needed —
// a promotion record, say — this package defines the few fields it actually
// reads and lets the caller convert. That is what keeps the dependency pointing
// inward: the monolith knows about Catalog; Catalog knows about nobody.
package domain

import "time"

// Artifact is one immutable, digest-addressed thing a Release ships.
type Artifact struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
}

// Dependency is another Product this Release needs, within a version range.
type Dependency struct {
	Product        string `json:"product"`
	MinimumVersion string `json:"minimumVersion"`
	MaximumVersion string `json:"maximumVersion"`
	Optional       bool   `json:"optional,omitempty"`
}

// Incompatibility is another Product this Release cannot sit alongside.
type Incompatibility struct {
	Product        string `json:"product"`
	MinimumVersion string `json:"minimumVersion,omitempty"`
	MaximumVersion string `json:"maximumVersion,omitempty"`
}

// WeeklyMaintenanceWindow is a recurring period during which work may happen.
type WeeklyMaintenanceWindow struct {
	TimeZone  string   `json:"timeZone"`
	Intervals []string `json:"intervals"`
}

// VulnerabilityAssessment is provider-neutral. Tenable.sc, OCI image scanners,
// and SBOM scanners normalize their outcomes into this exact-artifact record.
type VulnerabilityAssessment struct {
	Provider                 string                           `json:"provider"`
	RunID                    string                           `json:"runId"`
	Status                   string                           `json:"status"`
	Outcome                  string                           `json:"outcome"`
	ArtifactDigests          []string                         `json:"artifactDigests"`
	ActiveVulnerabilities    int                              `json:"activeVulnerabilities"`
	SuppressedFindings       int                              `json:"suppressedFindings"`
	SuppressedFindingDetails []SuppressedVulnerabilityFinding `json:"suppressedFindingDetails,omitempty"`
	GracePeriodFindings      int                              `json:"gracePeriodFindings"`
	// ActiveFindings carries the per-CVE detail behind ActiveVulnerabilities. It
	// is what makes a Release's risk score computable, so a scanner that reports
	// only counts cannot authorize an upgrade toward a recalled Release.
	ActiveFindings []VulnerabilityFinding `json:"activeFindings,omitempty"`
	// AssessedAt is when the scan completed, checked against the re-scan SLA.
	AssessedAt time.Time `json:"assessedAt,omitempty"`
}

// VulnerabilityFinding is one active CVE contributing to a Release's risk score.
// CVSS is authoritative when present; Severity supplies a representative value
// when the scanner published no numeric score.
type VulnerabilityFinding struct {
	ID       string  `json:"id"`
	CVSS     float64 `json:"cvss,omitempty"`
	Severity string  `json:"severity,omitempty"`
}

type (
	VulnerabilitySuppressionScope    string
	VulnerabilitySuppressionCategory string
)

const (
	VulnerabilitySuppressionGlobal       VulnerabilitySuppressionScope = "GLOBAL"
	VulnerabilitySuppressionScanner      VulnerabilitySuppressionScope = "SCANNER"
	VulnerabilitySuppressionImage        VulnerabilitySuppressionScope = "IMAGE"
	VulnerabilitySuppressionImagePrefix  VulnerabilitySuppressionScope = "IMAGE_PREFIX"
	VulnerabilitySuppressionDistribution VulnerabilitySuppressionScope = "DISTRIBUTION"

	VulnerabilitySuppressionFalsePositive    VulnerabilitySuppressionCategory = "FALSE_POSITIVE"
	VulnerabilitySuppressionWontFix          VulnerabilitySuppressionCategory = "WONT_FIX"
	VulnerabilitySuppressionVendorDependency VulnerabilitySuppressionCategory = "VENDOR_DEPENDENCY"
	VulnerabilitySuppressionPlannedFix       VulnerabilitySuppressionCategory = "PLANNED_FIX"
)

// SuppressedVulnerabilityFinding is the exact per-finding evidence behind a
// scanner's suppressed count. Provider-side authoring remains external, but a
// suppression cannot influence Platform policy unless the scanner preserves
// its scope, approval, rationale, expiry, and Change Request attribution.
type SuppressedVulnerabilityFinding struct {
	FindingID       string                             `json:"findingId"`
	VulnerabilityID string                             `json:"vulnerabilityId"`
	ArtifactDigest  string                             `json:"artifactDigest"`
	Suppression     VulnerabilitySuppressionProvenance `json:"suppression"`
}

type VulnerabilitySuppressionProvenance struct {
	ProviderID      string                           `json:"providerId"`
	Scope           VulnerabilitySuppressionScope    `json:"scope"`
	ScopeValue      string                           `json:"scopeValue,omitempty"`
	Category        VulnerabilitySuppressionCategory `json:"category"`
	Rationale       string                           `json:"rationale"`
	RequestedBy     string                           `json:"requestedBy"`
	ApprovedBy      string                           `json:"approvedBy"`
	ChangeRequestID string                           `json:"changeRequestId"`
	CreatedAt       time.Time                        `json:"createdAt"`
	ApprovedAt      time.Time                        `json:"approvedAt"`
	ValidUntil      time.Time                        `json:"validUntil"`
}

// MalwareAssessment is provider-neutral evidence that the exact immutable
// artifacts were scanned for malicious content before offline distribution.
type MalwareAssessment struct {
	Provider        string    `json:"provider"`
	RunID           string    `json:"runId"`
	Status          string    `json:"status"`
	Outcome         string    `json:"outcome"`
	ArtifactDigests []string  `json:"artifactDigests"`
	Findings        int       `json:"findings"`
	AssessedAt      time.Time `json:"assessedAt,omitempty"`
}

// ArtifactAvailabilityAssessment is evidence that the artifacts a Release names
// can actually be fetched. A Release whose images cannot be pulled is not a
// Release anyone can install, however clean its security scan.
type ArtifactAvailabilityAssessment struct {
	Provider        string   `json:"provider"`
	RunID           string   `json:"runId"`
	Status          string   `json:"status"`
	Outcome         string   `json:"outcome"`
	ArtifactDigests []string `json:"artifactDigests"`
}

// Release is one immutable version of a Product.
type Release struct {
	Product                         string                          `json:"product"`
	Version                         string                          `json:"version"`
	Chart                           Artifact                        `json:"chart"`
	Images                          []Artifact                      `json:"images"`
	Dependencies                    []Dependency                    `json:"dependencies,omitempty"`
	Incompatibilities               []Incompatibility               `json:"incompatibilities,omitempty"`
	MaintenanceWindow               *WeeklyMaintenanceWindow        `json:"maintenanceWindow,omitempty"`
	Channels                        []string                        `json:"channels,omitempty"`
	Labels                          map[string]string               `json:"labels,omitempty"`
	VulnerabilityAssessment         *VulnerabilityAssessment        `json:"vulnerabilityAssessment,omitempty"`
	MalwareAssessment               *MalwareAssessment              `json:"malwareAssessment,omitempty"`
	ArtifactAvailability            *ArtifactAvailabilityAssessment `json:"artifactAvailability,omitempty"`
	RolloutStrategy                 string                          `json:"rolloutStrategy,omitempty"`
	ResourceReadinessTimeoutSeconds int                             `json:"resourceReadinessTimeoutSeconds,omitempty"`
	ImagePrePullTimeoutSeconds      int                             `json:"imagePrePullTimeoutSeconds,omitempty"`
}

// AssessmentCoversArtifacts reports whether an assessment examined every
// artifact given. Coverage is checked rather than assumed because a scanner
// that examined some of a Release and passed is not evidence that the Release
// passed — and the difference is invisible in the outcome field alone.
func AssessmentCoversArtifacts(digests []string, artifacts []Artifact) bool {
	covered := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		covered[digest] = struct{}{}
	}
	for _, artifact := range artifacts {
		if _, exists := covered[artifact.Digest]; !exists {
			return false
		}
	}
	return true
}

// AssessmentCoversReleaseImages reports whether an assessment covers every image
// a Release ships.
func AssessmentCoversReleaseImages(digests []string, release Release) bool {
	return AssessmentCoversArtifacts(digests, release.Images)
}

// UncoveredArtifacts names what an assessment did not examine, so a caller can
// say which artifacts rather than only that coverage was incomplete. It reports
// repository:tag rather than digests, because that is the form a human reads,
// and deduplicates them: one missing image referenced twice is one gap.
func UncoveredArtifacts(digests []string, artifacts []Artifact) []string {
	covered := make(map[string]struct{}, len(digests))
	for _, digest := range digests {
		covered[digest] = struct{}{}
	}
	var uncovered []string
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		if _, exists := covered[artifact.Digest]; exists {
			continue
		}
		reference := artifact.Repository + ":" + artifact.Tag
		if _, duplicate := seen[reference]; duplicate {
			continue
		}
		seen[reference] = struct{}{}
		uncovered = append(uncovered, reference)
	}
	return uncovered
}
