package controlplane

import (
	"math"
	"strings"
	"time"
)

// By default an Entity is never upgraded to a recalled Release. That is right
// almost always, but when the Entity's current Release is itself recalled for
// vulnerabilities, refusing every recalled target strands it on known-vulnerable
// software and blocks the step-through upgrades that would eventually reach a
// clean Release. Apollo allows this one narrow exception, gated on the upgrade
// demonstrably not worsening the Entity's security posture.
//
// The exception is deliberately narrow. It applies only when every recall
// affecting the target is a vulnerability recall, only when both Releases carry
// fresh scan evidence, only when the target's risk score is no higher than the
// current one, and never toward a Release with malware findings.
//
// Provenance (reviewed 2026-08-02):
// https://www.palantir.com/docs/apollo/managing-vulnerabilities/cve-recalled-releases-upgrades/

// Representative CVSS scores for findings a scanner reported by severity tier
// without a numeric score. An unrecognized tier is scored as high rather than
// low: a finding whose severity nobody could parse must not make a target look
// cleaner than it is.
const (
	cvssCritical    = 9.0
	cvssHigh        = 7.0
	cvssMedium      = 4.0
	cvssLow         = 0.1
	cvssUnknownTier = cvssHigh

	// riskVolumeSaturation tunes how quickly accumulated findings push the score
	// toward the maximum. It is the aggregate CVSS at which the volume component
	// reaches half of the headroom above the worst single finding.
	riskVolumeSaturation = 50.0

	// RiskScoreMaximum is the top of the bounded scale.
	RiskScoreMaximum = 10.0
)

// SpaceSettings are Hub-wide defaults. ResourceOverrides let one Environment
// diverge from them, and an override takes precedence over the Hub value so an
// operator can opt individual Environments in or out independently.
//
// Platform resolves these at authoring time into the Environment's own desired
// state rather than reading them during evaluation, because desired state is
// immutable per generation: a Hub setting that changed underneath a reviewed
// generation would silently alter what that generation means.
type SpaceSettings struct {
	VulnerabilityUpgrades VulnerabilityUpgradePolicy            `json:"vulnerabilityUpgrades"`
	ResourceOverrides     map[string]VulnerabilityUpgradePolicy `json:"resourceOverrides,omitempty"`
}

// ResolveVulnerabilityUpgrades returns the policy in force for one Environment.
func (settings SpaceSettings) ResolveVulnerabilityUpgrades(environment string) VulnerabilityUpgradePolicy {
	if override, ok := settings.ResourceOverrides[environment]; ok {
		return override
	}
	return settings.VulnerabilityUpgrades
}

// effectiveVulnerabilityUpgradePolicy reads the policy an Environment was
// authored with. An Environment that declares none does not get the exception:
// upgrading toward a recalled Release has to be opted into.
func effectiveVulnerabilityUpgradePolicy(environment Environment) VulnerabilityUpgradePolicy {
	if environment.VulnerabilityUpgrades == nil {
		return VulnerabilityUpgradePolicy{}
	}
	return *environment.VulnerabilityUpgrades
}

// ReleaseRiskScore grades a Release from 0 to 10 using the active, unsuppressed,
// out-of-grace findings its latest scan reported. 0 means no active
// vulnerabilities.
//
// The score reflects two things at once. It is never below the worst single
// finding, so one critical CVE is never masked by an otherwise quiet Release,
// and it rises with the aggregate of every finding, so the gradual accumulation
// of low-severity findings is still captured. It saturates toward 10 rather than
// reaching it by addition.
//
// Palantir manages its own formula and does not publish it, so this is
// Platform's formula, not a reproduction. What the exception actually depends on
// is the comparison between two Releases scored the same way, which this
// supports; absolute equivalence with Apollo's numbers is neither claimed nor needed.
func ReleaseRiskScore(assessment *VulnerabilityAssessment) float64 {
	if assessment == nil || len(assessment.ActiveFindings) == 0 {
		return 0
	}
	worst, total := 0.0, 0.0
	for _, finding := range assessment.ActiveFindings {
		score := findingScore(finding)
		if score > worst {
			worst = score
		}
		total += score
	}
	if worst <= 0 {
		return 0
	}
	headroom := RiskScoreMaximum - worst
	volume := headroom * total / (total + riskVolumeSaturation)
	return roundRisk(math.Min(RiskScoreMaximum, worst+volume))
}

func findingScore(finding VulnerabilityFinding) float64 {
	if finding.CVSS > 0 {
		return math.Min(finding.CVSS, RiskScoreMaximum)
	}
	switch strings.ToUpper(strings.TrimSpace(finding.Severity)) {
	case "CRITICAL":
		return cvssCritical
	case "HIGH":
		return cvssHigh
	case "MEDIUM", "MODERATE":
		return cvssMedium
	case "LOW":
		return cvssLow
	case "NONE", "INFORMATIONAL", "INFO":
		return 0
	default:
		return cvssUnknownTier
	}
}

func roundRisk(value float64) float64 {
	return math.Round(value*100) / 100
}

// recallIsVulnerabilityOnly reports whether every recall affecting a version was
// derived from vulnerability evidence. A user-created or otherwise unexplained
// recall keeps the Release ineligible: an operator who recalled a Release for a
// reason the scanner cannot see must not be overridden by a risk comparison.
func recallIsVulnerabilityOnly(recalls []Recall, product, version string) bool {
	applicable := recallsForVersion(recalls, product, version)
	if len(applicable) == 0 {
		return false
	}
	for _, recall := range applicable {
		if recall.Source != RecallSourceSecurity {
			return false
		}
	}
	return true
}

// scanEvidenceFresh reports whether a Release carries a completed scan recent
// enough to authorize an upgrade. Stale or missing evidence blocks the exception
// rather than being treated as an absence of findings.
func scanEvidenceFresh(release Release, policy VulnerabilityUpgradePolicy, now time.Time) bool {
	assessment := release.VulnerabilityAssessment
	if assessment == nil || assessment.Status != "COMPLETE" ||
		!assessmentCoversReleaseImages(assessment.ArtifactDigests, release) ||
		!completeRiskEvidence(assessment) {
		return false
	}
	if policy.RescanSLASeconds <= 0 {
		return true
	}
	if assessment.AssessedAt.IsZero() {
		return false
	}
	sla := time.Duration(policy.RescanSLASeconds) * time.Second
	if sla <= 0 {
		return false
	}
	age := now.Sub(assessment.AssessedAt)
	return age >= 0 && age <= sla
}

// completeRiskEvidence prevents aggregate vulnerability counts from being
// mistaken for the per-finding evidence needed to compare two Releases. A
// scanner may continue to report counts only for ordinary policy gates, but it
// cannot authorize the recalled-Release exception without one unique finding
// record for every active vulnerability.
func completeRiskEvidence(assessment *VulnerabilityAssessment) bool {
	if assessment == nil || assessment.ActiveVulnerabilities != len(assessment.ActiveFindings) {
		return false
	}
	seen := make(map[string]struct{}, len(assessment.ActiveFindings))
	for _, finding := range assessment.ActiveFindings {
		id := strings.TrimSpace(finding.ID)
		if id == "" || math.IsNaN(finding.CVSS) || math.IsInf(finding.CVSS, 0) ||
			finding.CVSS < 0 || finding.CVSS > RiskScoreMaximum {
			return false
		}
		if _, duplicate := seen[id]; duplicate {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

// releaseHasMalwareFindings reports whether a Release must never be advanced to.
// Absent malware evidence is not treated as clean.
func releaseHasMalwareFindings(release Release) bool {
	assessment := release.MalwareAssessment
	if assessment == nil {
		return true
	}
	return assessment.Status != "COMPLETE" || assessment.Findings > 0 || assessment.Outcome == "FAIL"
}

// selectVulnerabilityRecalledUpgrade finds the best target for an Entity stranded
// on a vulnerability-recalled Release. Candidates must be strictly newer, be
// recalled only for vulnerabilities, carry fresh scan evidence, be free of
// malware findings, and score no worse than the Release the Entity is on.
//
// Candidates are expected newest-first; the first eligible one wins, so the
// Entity advances as far as the evidence allows in one step.
func selectVulnerabilityRecalledUpgrade(
	candidates []Release, current Release, recalls []Recall,
	policy VulnerabilityUpgradePolicy, now time.Time,
) (Release, *UpgradeRationale, bool) {
	if !policy.AllowRecalledForVulnerabilities {
		return Release{}, nil, false
	}
	// The Entity must actually be stranded: this exception exists to improve a
	// posture already degraded by a vulnerability recall, not to reach recalled
	// Releases from a healthy one.
	if !recallIsVulnerabilityOnly(recalls, current.Product, current.Version) ||
		!scanEvidenceFresh(current, policy, now) {
		return Release{}, nil, false
	}
	currentScore := ReleaseRiskScore(current.VulnerabilityAssessment)
	for _, candidate := range candidates {
		if compareVersions(candidate.Version, current.Version) <= 0 ||
			!recallIsVulnerabilityOnly(recalls, candidate.Product, candidate.Version) ||
			!scanEvidenceFresh(candidate, policy, now) ||
			releaseHasMalwareFindings(candidate) {
			continue
		}
		candidateScore := ReleaseRiskScore(candidate.VulnerabilityAssessment)
		if candidateScore > currentScore {
			continue
		}
		return candidate, &UpgradeRationale{
			Reason: "target Release is recalled only for vulnerabilities and its risk score is " +
				"lower than or equal to the current Release",
			CurrentVersion: current.Version, TargetVersion: candidate.Version,
			CurrentRiskScore: currentScore, TargetRiskScore: candidateScore,
		}, true
	}
	return Release{}, nil, false
}
