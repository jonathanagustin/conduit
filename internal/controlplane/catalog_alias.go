package controlplane

import "github.com/jonathanagustin/conduit/internal/catalog"

// The catalogue — what a Release is, what it ships, whether it has been
// recalled, and which Channels it has reached — now lives in internal/catalog.
// These aliases keep this package's several hundred call sites spelled as they
// were; the definitions moved, the names did not.
type (
	Release                            = catalog.Release
	Artifact                           = catalog.Artifact
	ArtifactAvailabilityAssessment     = catalog.ArtifactAvailabilityAssessment
	Dependency                         = catalog.Dependency
	Incompatibility                    = catalog.Incompatibility
	MalwareAssessment                  = catalog.MalwareAssessment
	Recall                             = catalog.Recall
	RecallStrategy                     = catalog.RecallStrategy
	SuppressedVulnerabilityFinding     = catalog.SuppressedVulnerabilityFinding
	VulnerabilityAssessment            = catalog.VulnerabilityAssessment
	VulnerabilityFinding               = catalog.VulnerabilityFinding
	VulnerabilitySuppressionCategory   = catalog.VulnerabilitySuppressionCategory
	VulnerabilitySuppressionProvenance = catalog.VulnerabilitySuppressionProvenance
	VulnerabilitySuppressionScope      = catalog.VulnerabilitySuppressionScope
	WeeklyMaintenanceWindow            = catalog.WeeklyMaintenanceWindow
)

const (
	VulnerabilitySuppressionGlobal       = catalog.VulnerabilitySuppressionGlobal
	VulnerabilitySuppressionScanner      = catalog.VulnerabilitySuppressionScanner
	VulnerabilitySuppressionImage        = catalog.VulnerabilitySuppressionImage
	VulnerabilitySuppressionImagePrefix  = catalog.VulnerabilitySuppressionImagePrefix
	VulnerabilitySuppressionDistribution = catalog.VulnerabilitySuppressionDistribution

	VulnerabilitySuppressionFalsePositive    = catalog.VulnerabilitySuppressionFalsePositive
	VulnerabilitySuppressionWontFix          = catalog.VulnerabilitySuppressionWontFix
	VulnerabilitySuppressionVendorDependency = catalog.VulnerabilitySuppressionVendorDependency
	VulnerabilitySuppressionPlannedFix       = catalog.VulnerabilitySuppressionPlannedFix

	RecallRollForward          = catalog.RecallRollForward
	RecallAllowDowngrade       = catalog.RecallAllowDowngrade
	RecallFreeze               = catalog.RecallFreeze
	RecallSelectedEnvironments = catalog.RecallSelectedEnvironments
	RecallSourceManual         = catalog.RecallSourceManual
	RecallSourceSecurity       = catalog.RecallSourceSecurity
)

// Version semantics moved wholesale: parsing, ordering, and the Channels a
// version implies are properties of the catalogue and of nothing else.
var (
	versionPattern        = catalog.VersionPattern
	versionMatcherPattern = catalog.VersionMatcherPattern
)

func compareVersions(left, right string) int { return catalog.CompareVersions(left, right) }

func versionWithinMaximum(version, matcher string) bool {
	return catalog.VersionWithinMaximum(version, matcher)
}

func defaultChannelsForVersion(version string) []string {
	return catalog.DefaultChannelsForVersion(version)
}

// IsApolloOrderableVersion reports whether a version can be ordered against
// others. Anything that sorts or compares versions must ask this first.
func IsApolloOrderableVersion(value string) bool { return catalog.IsApolloOrderableVersion(value) }

// IsApolloProductVersion reports whether a version is one a Product Release may
// declare, orderable or not.
func IsApolloProductVersion(value string) bool { return catalog.IsApolloProductVersion(value) }

func recallsForVersion(recalls []Recall, product, version string) []Recall {
	return catalog.RecallsForVersion(recalls, product, version)
}

func recallAppliesToVersion(recall Recall, version string) bool {
	return catalog.RecallAppliesToVersion(recall, version)
}

func releaseRecalled(release Release, recalls []Recall, environment string) bool {
	return catalog.ReleaseRecalled(release, recalls, environment)
}

func releaseMeetsRecallFloor(release Release, recalls []Recall, environment string) bool {
	return catalog.ReleaseMeetsRecallFloor(release, recalls, environment)
}

func uncoveredArtifacts(digests []string, artifacts []Artifact) []string {
	return catalog.UncoveredArtifacts(digests, artifacts)
}

func assessmentCoversReleaseImages(digests []string, release Release) bool {
	return catalog.AssessmentCoversReleaseImages(digests, release)
}

// catalogPromotions narrows this package's promotion records to the five fields
// Catalog reads. Everything else a PromotionRecord carries — approvals,
// conditions, health evidence, the environments involved — has no bearing on
// which Channels a Release has reached, and passing it would make Catalog
// depend on Governance for no gain.
func catalogPromotions(promotions []PromotionRecord) []catalog.Promotion {
	narrowed := make([]catalog.Promotion, 0, len(promotions))
	for _, promotion := range promotions {
		narrowed = append(narrowed, catalog.Promotion{
			Product:     promotion.Product,
			Version:     promotion.Version,
			FromChannel: promotion.FromChannel,
			ToChannel:   promotion.ToChannel,
			Passed:      promotion.Status == PromotionPassed,
		})
	}
	return narrowed
}

func effectiveReleaseChannels(release Release, promotions []PromotionRecord) []string {
	return catalog.EffectiveChannels(release, catalogPromotions(promotions))
}

func releaseOnChannel(release Release, promotions []PromotionRecord, channel string) bool {
	return catalog.ReleaseOnChannel(release, catalogPromotions(promotions), channel)
}

func catalogHasReleaseOnChannel(releases []Release, promotions []PromotionRecord, product, channel string) bool {
	return catalog.HasReleaseOnChannel(releases, catalogPromotions(promotions), product, channel)
}
