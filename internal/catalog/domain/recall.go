package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Recall withdraws a range of a Product's versions from selection.
type Recall struct {
	ID                     string         `json:"id"`
	Product                string         `json:"product"`
	StartingVersion        string         `json:"startingVersion"`
	EndingVersionExclusive string         `json:"endingVersionExclusive,omitempty"`
	ExcludedVersions       []string       `json:"excludedVersions,omitempty"`
	Strategy               RecallStrategy `json:"strategy"`
	Reason                 string         `json:"reason"`
	Reverted               bool           `json:"reverted,omitempty"`
	SingleVersion          bool           `json:"singleVersion,omitempty"`
	Source                 string         `json:"source,omitempty"`
	AssessmentRunID        string         `json:"assessmentRunId,omitempty"`
}

// RecallStrategy says what should happen to what is already installed.
type RecallStrategy struct {
	Type           string     `json:"type"`
	MinimumVersion string     `json:"minimumVersion,omitempty"`
	FreezeUntil    *time.Time `json:"freezeUntil,omitempty"`
	Environments   []string   `json:"environments,omitempty"`
}

const (
	RecallRollForward          = "ROLL_FORWARD"
	RecallAllowDowngrade       = "ALLOW_DOWNGRADE"
	RecallFreeze               = "FREEZE"
	RecallSelectedEnvironments = "SELECTED_ENVIRONMENTS"

	// A recall's source decides whether a risk comparison may ever override it.
	// An operator who recalled a Release for a reason no scanner can see must not
	// be second-guessed by evidence that only describes vulnerabilities.
	RecallSourceManual   = "MANUAL"
	RecallSourceSecurity = "SECURITY"
)

// RecallsForVersion is every live recall covering one Product version.
func RecallsForVersion(recalls []Recall, product, version string) []Recall {
	result := []Recall{}
	for _, recall := range recalls {
		if !recall.Reverted && recall.Product == product && RecallAppliesToVersion(recall, version) {
			result = append(result, recall)
		}
	}
	return result
}

// RecallAppliesToVersion decides whether one recall covers one version.
func RecallAppliesToVersion(recall Recall, version string) bool {
	if !VersionPattern.MatchString(recall.StartingVersion) || !VersionPattern.MatchString(version) ||
		CompareVersions(version, recall.StartingVersion) < 0 {
		return false
	}
	if recall.SingleVersion && version != recall.StartingVersion {
		return false
	}
	if recall.EndingVersionExclusive != "" && VersionPattern.MatchString(recall.EndingVersionExclusive) &&
		CompareVersions(version, recall.EndingVersionExclusive) >= 0 {
		return false
	}
	return !contains(recall.ExcludedVersions, version)
}

// ReleaseRecalled is the single predicate every selection path routes through: entity resolution,
// channel resolution, and Bundle export all ask it and drop the Release if it answers true.
//
// KNOWN GAP, recorded rather than fixed. Upstream excludes recalled Releases the same way -- "by
// default, Apollo will not upgrade an Entity to a recalled Release", and the behaviour is "also
// available on Export Pipelines" -- but it keeps an exception this predicate cannot express:
// "CVE-recalled Releases are only considered when there is no eligible non-recalled target", where
// the target improves the Entity's security profile
// (https://www.palantir.com/docs/apollo/managing-vulnerabilities/cve-recalled-releases-upgrades/,
// reviewed 2026-08-07).
//
// Here a recall is absolute, so a Product whose every Release is CVE-recalled resolves to nothing
// at all, where upstream would still offer the least-bad one. RecallSourceSecurity already
// distinguishes those recalls and is used for command blocking, so the input exists; what is
// missing is the fallback, and adding it changes what gets installed and shipped rather than what
// is reported about it.
func ReleaseRecalled(release Release, recalls []Recall, environment string) bool {
	for _, recall := range recalls {
		if recall.Reverted || recall.Product != release.Product || !RecallAppliesToVersion(recall, release.Version) {
			continue
		}
		// Selected-Environment roll-off freezes other Environments on their
		// current version, but still prevents this Release from being selected
		// for a new installation anywhere.
		_ = environment
		return true
	}
	return false
}

// ReleaseMeetsRecallFloor reports whether a Release is at or above the version
// a recall's roll-forward strategy insists on. It is a separate question from
// ReleaseRecalled: a Release can be outside every recalled range and still sit
// below the floor a recall established for its Product.
func ReleaseMeetsRecallFloor(release Release, recalls []Recall, environment string) bool {
	for _, recall := range recalls {
		if recall.Reverted || recall.Product != release.Product ||
			!VersionPattern.MatchString(recall.StartingVersion) {
			continue
		}
		floor := ""
		switch recall.Strategy.Type {
		case "", RecallRollForward:
			floor = recall.Strategy.MinimumVersion
			if floor == "" {
				floor = recall.StartingVersion
			}
		case RecallAllowDowngrade:
			floor = recall.Strategy.MinimumVersion
		case RecallSelectedEnvironments:
			if !contains(recall.Strategy.Environments, environment) {
				continue
			}
		}
		if floor != "" && VersionPattern.MatchString(floor) && CompareVersions(release.Version, floor) < 0 {
			return false
		}
	}
	return true
}

// contains is restated here rather than imported. Reaching back to the monolith
// for a three-line slice search would be an edge pointing the wrong way, and the
// whole point of this package is that it has none.
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// LiveRecalls narrows a submitted set to the recalls actually in force.
//
// A reverted recall is kept in the set rather than deleted from it -- that is
// how a withdrawal is recorded, and every selection path already skips one --
// so anything that reports what is recalled has to skip them here too. The
// order is the submitted order, because a caller announcing these has nothing
// better to sort by than the order an operator wrote them in.
func LiveRecalls(recalls []Recall) []Recall {
	live := make([]Recall, 0, len(recalls))
	for _, recall := range recalls {
		if !recall.Reverted {
			live = append(live, recall)
		}
	}
	return live
}

// OpenEnded reports whether a recall has no upper bound, so that every version
// the Product will ever publish above its starting version is covered.
//
// It is a property of the range and of nothing else: a recall with an ending
// version stops at it, a single-version recall covers exactly one, and anything
// else runs forever. Excluded versions punch holes in the range without closing
// it, which is why they are not consulted -- a recall excluding 2.1.3 still
// covers 2.1.4 and everything after it.
func OpenEnded(recall Recall) bool {
	return !recall.SingleVersion && recall.EndingVersionExclusive == ""
}

// WithAutomaticSecurityRecalls opens a recall for every Release whose
// vulnerability assessment completed and failed with active vulnerabilities.
//
// This is the one place in Conduit where a recall opens without a person
// writing it, so it belongs with the rule for what a recall is rather than with
// the surface that happened to receive the Releases. The caller decides whether
// its Environment asked for this; the rule for what gets recalled is here.
//
// The identity is derived from the Product, the version and the assessment run,
// so the same failing assessment produces the same recall ID on every
// evaluation and on every replica. Nothing durable stores it: the recall exists
// because the assessment does.
//
// A Release already covered by a live recall does not get a second one. Two
// recalls over one version withdraw it exactly as thoroughly as one, and the
// duplicate would announce a withdrawal that had already happened.
func WithAutomaticSecurityRecalls(recalls []Recall, releases []Release) []Recall {
	opened := append([]Recall(nil), recalls...)
	for _, release := range releases {
		assessment := release.VulnerabilityAssessment
		if assessment == nil || assessment.Status != "COMPLETE" || assessment.Outcome != "FAIL" ||
			assessment.ActiveVulnerabilities <= 0 {
			continue
		}
		identity := sha256.Sum256([]byte(release.Product + "\x00" + release.Version + "\x00" + assessment.RunID))
		recall := Recall{
			ID: "security-" + hex.EncodeToString(identity[:8]), Product: release.Product,
			StartingVersion: release.Version, SingleVersion: true,
			Strategy: RecallStrategy{Type: RecallRollForward, MinimumVersion: release.Version},
			Reason:   "automatic security recall from assessment " + assessment.RunID,
			Source:   RecallSourceSecurity, AssessmentRunID: assessment.RunID,
		}
		duplicate := false
		for _, existing := range opened {
			if existing.ID == recall.ID || (!existing.Reverted && RecallAppliesToVersion(existing, release.Version)) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			opened = append(opened, recall)
		}
	}
	return opened
}
