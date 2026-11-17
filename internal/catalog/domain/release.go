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

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Artifact is one immutable, digest-addressed thing a Release ships.
//
// A Release's Chart is its single deployable artifact, and Kind says what that
// artifact is. The field is named for the only kind that existed when it was
// introduced: an absent Kind is a Helm chart, which is what every Release
// written before Kind existed is and stays. Images never carry a Kind.
//
// CONDUIT-INVENTED: catalog.PackageArtifactKind
// Displaced-by: a published or owner-approved Product Release contract that states how a Zarf package or UDS bundle artifact, its architectures and its deployed package name are declared.
type Artifact struct {
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Digest     string `json:"digest"`
	// Kind is one of the ArtifactKind constants; empty means ArtifactKindHelmChart.
	Kind string `json:"kind,omitempty"`
	// Architectures lists the cluster architectures a Zarf package or UDS
	// bundle was built for. Exactly one is supported: the digest must name a
	// platform manifest, not a multiarchitecture index. Refused for Helm.
	Architectures []string `json:"architectures,omitempty"`
	// PackageName is the Zarf package name (zarf.yaml metadata.name) the
	// artifact deploys as. Zarf records a deployment under that name and
	// nothing else, so it is how the Spoke finds what it deployed. For a UDS
	// bundle it names the bundled Zarf package the Spoke observes.
	PackageName string `json:"packageName,omitempty"`
}

// The kinds of deployable artifact a Release may declare.
const (
	ArtifactKindHelmChart   = "helm-chart"
	ArtifactKindZarfPackage = "zarf-package"
	ArtifactKindUDSBundle   = "uds-bundle"
)

var (
	artifactDigestPattern  = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
	zarfPackageNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]*$`)
	packageArchitectures   = []string{"amd64", "arm64"}
)

// ArtifactProblem is one thing wrong with an artifact's package declaration,
// named by the field it concerns.
type ArtifactProblem struct {
	Field   string
	Message string
}

// EffectiveKind resolves the kind an artifact deploys as. An artifact with no
// Kind is a Helm chart.
func (artifact Artifact) EffectiveKind() string {
	if kind := strings.TrimSpace(artifact.Kind); kind != "" {
		return kind
	}
	return ArtifactKindHelmChart
}

// IsPackage reports whether the artifact is a Zarf package or a UDS bundle,
// the two kinds the Zarf/UDS executor deploys.
func (artifact Artifact) IsPackage() bool {
	kind := artifact.EffectiveKind()
	return kind == ArtifactKindZarfPackage || kind == ArtifactKindUDSBundle
}

// SupportsArchitecture reports whether a package artifact was built for the
// given architecture. A Helm chart supports every architecture.
func (artifact Artifact) SupportsArchitecture(architecture string) bool {
	if !artifact.IsPackage() {
		return true
	}
	architecture = strings.TrimSpace(architecture)
	for _, candidate := range artifact.Architectures {
		if candidate == architecture {
			return true
		}
	}
	return false
}

// PackageProblems reports what is wrong with the kind-specific part of a
// deployable artifact. It returns nothing for a well-formed artifact.
//
// A Zarf package or UDS bundle must be pinned by digest. A tag names whatever
// the registry currently holds, so a tag-only reference is refused outright,
// and a tag beside a digest is refused too: the Spoke deploys the digest, and
// the tag would be a claim nobody checked.
func (artifact Artifact) PackageProblems() []ArtifactProblem {
	var problems []ArtifactProblem
	add := func(field, message string) {
		problems = append(problems, ArtifactProblem{Field: field, Message: message})
	}
	switch artifact.EffectiveKind() {
	case ArtifactKindHelmChart:
		if len(artifact.Architectures) > 0 {
			add("architectures", "applies only to a zarf-package or uds-bundle")
		}
		if artifact.PackageName != "" {
			add("packageName", "applies only to a zarf-package or uds-bundle")
		}
		return problems
	case ArtifactKindZarfPackage, ArtifactKindUDSBundle:
	default:
		add("kind", "must be helm-chart, zarf-package or uds-bundle")
		return problems
	}
	if !artifactDigestPattern.MatchString(artifact.Digest) {
		add("digest", "a "+artifact.Kind+" must be pinned by a lowercase sha256 digest; a tag is mutable and is refused")
	}
	if artifact.Tag != "" {
		add("tag", "must be empty for a "+artifact.Kind+": it is deployed by digest, and a tag is a mutable reference")
	}
	if strings.Contains(artifact.Repository, "@") || repositoryCarriesTag(artifact.Repository) {
		add("repository", "must be an OCI repository without a tag or digest")
	}
	if !zarfPackageNamePattern.MatchString(artifact.PackageName) {
		add("packageName", "must be the Zarf package name: lowercase letters, digits and hyphens")
	}
	if len(artifact.Architectures) == 0 {
		add("architectures", "must list at least one architecture the package was built for")
	}
	if len(artifact.Architectures) > 1 {
		add("architectures", "must name exactly one target architecture and pin its platform manifest; multiarchitecture index resolution is not supported")
	}
	seen := make(map[string]struct{}, len(artifact.Architectures))
	for _, architecture := range artifact.Architectures {
		known := false
		for _, candidate := range packageArchitectures {
			known = known || candidate == architecture
		}
		if !known {
			add("architectures", "architecture "+strconv.Quote(architecture)+" is not one of amd64, arm64")
		}
		if _, duplicate := seen[architecture]; duplicate {
			add("architectures", "architecture "+strconv.Quote(architecture)+" is listed twice")
		}
		seen[architecture] = struct{}{}
	}
	return problems
}

// repositoryCarriesTag reports whether the last path segment of an OCI
// repository has a ":tag" suffix. A colon before the first slash is a port.
func repositoryCarriesTag(repository string) bool {
	repository = strings.TrimPrefix(strings.TrimSpace(repository), "oci://")
	slash := strings.LastIndexByte(repository, '/')
	return slash >= 0 && strings.Contains(repository[slash+1:], ":")
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

// VulnerabilitySuppressionProvenance is what the scanner's control plane
// recorded about an approved suppression, preserved verbatim.
//
// Every field here is an external assertion, including ChangeRequestID. It is
// NOT a Conduit crq_ and is never resolved against this Hub's Change Requests:
// validation requires only that it is non-empty. That is deliberate rather than
// unimplemented. Conduit does not create or remove provider-side suppressions --
// they stay with the scanner/identity control plane -- so there is no local
// record the field could name, and an assessment relayed from another Hub
// carries the approving system's reference, which this Hub could not resolve
// even in principle.
//
// The security boundary for a suppression is therefore governance approval of
// the Plan that carries it, not any property of these fields. Anything stronger
// needs Hub-witnessed assessments, which is a separate and much larger design.
type VulnerabilitySuppressionProvenance struct {
	ProviderID  string                           `json:"providerId"`
	Scope       VulnerabilitySuppressionScope    `json:"scope"`
	ScopeValue  string                           `json:"scopeValue,omitempty"`
	Category    VulnerabilitySuppressionCategory `json:"category"`
	Rationale   string                           `json:"rationale"`
	RequestedBy string                           `json:"requestedBy"`
	ApprovedBy  string                           `json:"approvedBy"`
	// ChangeRequestID references the approving system's change record. See the
	// type comment: it is an external reference, not a Conduit Change Request.
	ChangeRequestID string    `json:"changeRequestId"`
	CreatedAt       time.Time `json:"createdAt"`
	ApprovedAt      time.Time `json:"approvedAt"`
	ValidUntil      time.Time `json:"validUntil"`
	// PlannedFixReference is the external evidence of the scheduled resolution
	// behind a PLANNED_FIX suppression: the tracking reference for the fix the
	// suppression holds a finding open until. Like every field here it is an
	// external assertion, not resolved against this Hub. It is required for a
	// PLANNED_FIX suppression and must be empty for any other category, so a
	// planned fix cannot be claimed without naming what the plan is -- ValidUntil
	// already says when, this says what.
	PlannedFixReference string `json:"plannedFixReference,omitempty"`
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
