package controlplane

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func resolveDesiredStateWithObserved(state DesiredState, observed EnvironmentReport, now time.Time) (DesiredState, error) {
	resolved := state
	resolved.Entities = append([]Entity(nil), state.Entities...)
	current := make(map[string]ReportedEntity, len(observed.Entities))
	for _, entity := range observed.Entities {
		current[entity.InstallationID] = entity
	}
	for index := range resolved.Entities {
		entity := &resolved.Entities[index]
		if entity.ReleaseChannel == "" && resolved.Environment.DefaultReleaseChannel != "" {
			entity.ReleaseChannel = resolved.Environment.DefaultReleaseChannel
		}
		if entity.ReleaseChannel == "" {
			continue
		}
		observedEntity := current[entity.Name]
		release, recallIDs, rationale, ineligible, found := selectReleaseForObservedEntity(
			state.Releases, state.Promotions, state.Recalls, *entity, state.Environment, observedEntity.Version, now,
		)
		if !found {
			// Saying only that nothing was eligible leaves an operator to
			// rediscover, one Release at a time, what this loop already
			// worked out and threw away.
			return DesiredState{}, fmt.Errorf(
				"Entity %s has no eligible Release for Product %s on channel %s%s",
				entity.Name, entity.Product, entity.ReleaseChannel, describeIneligibility(ineligible),
			)
		}
		entity.Version = release.Version
		entity.recallRollOff = len(recallIDs) > 0
		entity.recallIDs = recallIDs
		entity.upgradeRationale = rationale
	}
	return resolved, nil
}

// describeIneligibility renders the reasons onto the end of an error. Releases
// excluded for the same reason are named together rather than repeated, because
// a Product with twenty Releases and one missing scanner produces twenty
// identical sentences otherwise.
func describeIneligibility(reasons []ReleaseIneligibility) string {
	if len(reasons) == 0 {
		return ""
	}
	order := make([]string, 0, len(reasons))
	versions := make(map[string][]string, len(reasons))
	messages := make(map[string]string, len(reasons))
	for _, reason := range reasons {
		detail := reason.Message
		if len(reason.Subjects) > 0 {
			detail += " (" + strings.Join(reason.Subjects, ", ") + ")"
		}
		if _, seen := versions[detail]; !seen {
			order = append(order, detail)
			messages[detail] = detail
		}
		versions[detail] = append(versions[detail], reason.Version)
	}
	rendered := make([]string, 0, len(order))
	for _, detail := range order {
		rendered = append(rendered, strings.Join(versions[detail], ", ")+": "+messages[detail])
	}
	return "; " + strings.Join(rendered, "; ")
}

func selectReleaseForObservedEntity(
	releases []Release, promotions []PromotionRecord, recalls []Recall, entity Entity, environment Environment,
	currentVersion string, now time.Time,
) (Release, []string, *UpgradeRationale, []ReleaseIneligibility, bool) {
	candidates := make([]Release, 0)
	var ineligible []ReleaseIneligibility
	var currentRelease Release
	currentFound := false
	for _, release := range releases {
		if release.Product != entity.Product || !versionPattern.MatchString(release.Version) {
			continue
		}
		if !releaseOnChannel(release, promotions, entity.ReleaseChannel) {
			continue
		}
		if policyReasons := environmentPolicyIneligibility(release, environment); len(policyReasons) > 0 {
			ineligible = append(ineligible, policyReasons...)
			continue
		}
		candidates = append(candidates, release)
		if release.Version == currentVersion {
			currentRelease, currentFound = release, true
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return compareVersions(candidates[i].Version, candidates[j].Version) > 0 })
	if !currentFound {
		for _, candidate := range candidates {
			if !releaseRecalled(candidate, recalls, environment.ID) && releaseMeetsRecallFloor(candidate, recalls, environment.ID) {
				return candidate, nil, nil, ineligible, true
			}
		}
		return Release{}, nil, nil, ineligible, false
	}
	active := recallsForVersion(recalls, entity.Product, currentVersion)
	if len(active) == 0 {
		for _, candidate := range candidates {
			if !releaseRecalled(candidate, recalls, environment.ID) && releaseMeetsRecallFloor(candidate, recalls, environment.ID) {
				return candidate, nil, nil, ineligible, true
			}
		}
		return currentRelease, nil, nil, ineligible, true
	}
	allowDowngrade := true
	floor := ""
	for _, recall := range active {
		switch recall.Strategy.Type {
		case RecallFreeze:
			if recall.Strategy.FreezeUntil != nil && now.Before(*recall.Strategy.FreezeUntil) {
				return currentRelease, nil, nil, ineligible, true
			}
			allowDowngrade = false
		case RecallSelectedEnvironments:
			if !contains(recall.Strategy.Environments, environment.ID) {
				return currentRelease, nil, nil, ineligible, true
			}
			allowDowngrade = false
		case RecallAllowDowngrade:
			if floor == "" || compareVersions(recall.Strategy.MinimumVersion, floor) > 0 {
				floor = recall.Strategy.MinimumVersion
			}
		default:
			allowDowngrade = false
			minimum := recall.Strategy.MinimumVersion
			if minimum == "" {
				minimum = recall.StartingVersion
			}
			if floor == "" || compareVersions(minimum, floor) > 0 {
				floor = minimum
			}
		}
	}
	recallIDs := make([]string, 0, len(active))
	for _, recall := range active {
		recallIDs = append(recallIDs, recall.ID)
	}
	sort.Strings(recallIDs)
	for _, candidate := range candidates {
		if compareVersions(candidate.Version, currentVersion) > 0 && !releaseRecalled(candidate, recalls, environment.ID) &&
			(floor == "" || compareVersions(candidate.Version, floor) >= 0) {
			return candidate, recallIDs, nil, ineligible, true
		}
	}
	if allowDowngrade {
		for _, candidate := range candidates {
			if !releaseRecalled(candidate, recalls, environment.ID) && (floor == "" || compareVersions(candidate.Version, floor) >= 0) {
				if candidate.Version != currentVersion {
					return candidate, recallIDs, nil, ineligible, true
				}
				return candidate, nil, nil, ineligible, true
			}
		}
	}
	// Every non-recalled target has been ruled out, so the Entity is stranded on
	// a recalled Release. If that recall is a vulnerability recall, an upgrade
	// that provably does not worsen its posture is better than staying put.
	if release, rationale, ok := selectVulnerabilityRecalledUpgrade(
		candidates, currentRelease, recalls, effectiveVulnerabilityUpgradePolicy(environment), now,
	); ok {
		return release, recallIDs, rationale, ineligible, true
	}
	return currentRelease, nil, nil, ineligible, true
}

func latestEligibleRelease(
	releases []Release, promotions []PromotionRecord, recalls []Recall, product, channel string, environment Environment,
) (Release, bool) {
	candidates := make([]Release, 0)
	for _, release := range releases {
		if release.Product != product || !versionPattern.MatchString(release.Version) ||
			!releaseOnChannel(release, promotions, channel) ||
			releaseRecalled(release, recalls, environment.ID) ||
			!releaseMeetsRecallFloor(release, recalls, environment.ID) ||
			!releaseSatisfiesEnvironmentPolicies(release, environment) {
			continue
		}
		candidates = append(candidates, release)
	}
	if len(candidates) == 0 {
		return Release{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		return compareVersions(candidates[i].Version, candidates[j].Version) > 0
	})
	return candidates[0], true
}

// ReleaseIneligibility is one reason a Release was not eligible for an Entity.
// Kind is a closed set so a caller can group and act on these; Subjects names
// the artifacts at fault, which is the part an operator cannot otherwise get
// at -- an assessment that covers four of a Release's five artifacts withholds
// the Release, and until now said nothing about which one was missing.
type ReleaseIneligibility struct {
	Version  string   `json:"version"`
	Kind     string   `json:"kind"`
	Message  string   `json:"message"`
	Subjects []string `json:"subjects,omitempty"`
}

// INFERRED identifiers, and upstream has a taxonomy of its own to align to rather than a blank.
// Apollo names four constraints to an operator -- "Maintenance window constraint", "Product
// dependencies constraint", "Suppression window constraint" and "Artifacts missing constraint" --
// and grades each artifact "Unavailable", "Available" or "Unchecked"
// (https://www.palantir.com/docs/apollo/core/plans-and-constraints/, reviewed 2026-08-07).
//
// The four AVAILABILITY_ kinds below are that "Artifacts missing constraint" split by why the
// assessment could not vouch for the Release; the names are ours. Two differences worth keeping in
// view:
//
//   - Upstream's per-artifact grade is not derivable here. ArtifactAvailabilityAssessment carries
//     one Outcome for the whole Release and a flat digest list, so "Unavailable" and "Unchecked"
//     cannot be told apart -- and they behave differently upstream, where unavailability does NOT
//     block "if no registry matched the artifact URI, or the registry is not accessible by the
//     Apollo Hub". This repository fails closed on both.
//   - The SECURITY_ kinds have no upstream counterpart on that page. ArtifactSecurityPolicy is a
//     policy this repository applies; nothing recovered names a vulnerability-scan constraint.
const (
	IneligiblePackageArchitecture = "PACKAGE_ARCHITECTURE"

	IneligibleSecurityAssessmentMissing    = "SECURITY_ASSESSMENT_MISSING"
	IneligibleSecurityAssessmentIncomplete = "SECURITY_ASSESSMENT_INCOMPLETE"
	IneligibleSecurityOutcomeNotPass       = "SECURITY_OUTCOME_NOT_PASS"
	IneligibleSecurityCoverageIncomplete   = "SECURITY_COVERAGE_INCOMPLETE"

	IneligibleAvailabilityAssessmentMissing    = "AVAILABILITY_ASSESSMENT_MISSING"
	IneligibleAvailabilityAssessmentIncomplete = "AVAILABILITY_ASSESSMENT_INCOMPLETE"
	IneligibleAvailabilityOutcomeNotAvailable  = "AVAILABILITY_OUTCOME_NOT_AVAILABLE"
	IneligibleAvailabilityCoverageIncomplete   = "AVAILABILITY_COVERAGE_INCOMPLETE"
)

func releaseSatisfiesEnvironmentPolicies(release Release, environment Environment) bool {
	return len(environmentPolicyIneligibility(release, environment)) == 0
}

// environmentPolicyIneligibility reports every policy reason a Release is not
// eligible, rather than the first. An operator fixing them one at a time,
// learning of the next only after a full plan resolution, is how a single
// misconfigured Release costs an afternoon.
func environmentPolicyIneligibility(release Release, environment Environment) []ReleaseIneligibility {
	var reasons []ReleaseIneligibility
	if reason := packageArchitectureIneligibility(release, environment); reason != nil {
		reasons = append(reasons, *reason)
	}
	add := func(kind, message string, subjects ...string) {
		reasons = append(reasons, ReleaseIneligibility{
			Version: release.Version, Kind: kind, Message: message, Subjects: subjects,
		})
	}
	if policy := environment.ArtifactSecurityPolicy; policy != nil {
		assessment := release.VulnerabilityAssessment
		switch {
		case assessment == nil:
			add(IneligibleSecurityAssessmentMissing,
				"the Environment requires a vulnerability assessment and the Release carries none")
		case assessment.Status != "COMPLETE":
			add(IneligibleSecurityAssessmentIncomplete,
				"the vulnerability assessment is "+assessment.Status+", not COMPLETE")
		default:
			if policy.RequiredOutcome == "PASS" && assessment.Outcome != "PASS" {
				add(IneligibleSecurityOutcomeNotPass,
					"the Environment requires a PASS vulnerability outcome and the assessment is "+assessment.Outcome)
			}
			if uncovered := uncoveredArtifacts(assessment.ArtifactDigests, release.Images); len(uncovered) > 0 {
				add(IneligibleSecurityCoverageIncomplete,
					"the vulnerability assessment does not cover every image in the Release", uncovered...)
			}
		}
	}
	if environment.ArtifactAvailabilityPolicy != nil {
		assessment := release.ArtifactAvailability
		switch {
		case assessment == nil:
			add(IneligibleAvailabilityAssessmentMissing,
				"the Environment requires an artifact availability assessment and the Release carries none")
		case assessment.Status != "COMPLETE":
			add(IneligibleAvailabilityAssessmentIncomplete,
				"the artifact availability assessment is "+assessment.Status+", not COMPLETE")
		default:
			if assessment.Outcome != "AVAILABLE" {
				add(IneligibleAvailabilityOutcomeNotAvailable,
					"the artifact availability outcome is "+assessment.Outcome+", not AVAILABLE")
			}
			// The per-artifact detail the whole-Release Outcome cannot carry.
			// An assessment that simply omits an artifact withholds the
			// Release just as firmly as one that reports it missing.
			if uncovered := uncoveredArtifacts(assessment.ArtifactDigests, releaseArtifacts(release)); len(uncovered) > 0 {
				add(IneligibleAvailabilityCoverageIncomplete,
					"the artifact availability assessment does not cover every artifact in the Release", uncovered...)
			}
		}
	}
	return reasons
}

// packageArchitectureIneligibility applies to selected package Releases only:
// a catalog may contain artifacts for other targets without blocking a Helm
// installation or forcing the target to choose the newest incompatible one.
func packageArchitectureIneligibility(release Release, environment Environment) *ReleaseIneligibility {
	if !release.Chart.IsPackage() {
		return nil
	}
	message := ""
	if environment.PackageArchitecture == "" {
		message = "the Environment must declare packageArchitecture before a Zarf package or UDS bundle can be planned"
	} else if !release.Chart.SupportsArchitecture(environment.PackageArchitecture) {
		message = fmt.Sprintf("the package supports %s, not the Environment packageArchitecture %s", strings.Join(release.Chart.Architectures, ", "), environment.PackageArchitecture)
	}
	if message == "" {
		return nil
	}
	return &ReleaseIneligibility{Version: release.Version, Kind: IneligiblePackageArchitecture, Message: message}
}

func validateRecalls(recalls []Recall, errs *[]ValidationError) {
	seenIDs := make(map[string]struct{}, len(recalls))
	for index, recall := range recalls {
		path := fmt.Sprintf("recalls[%d]", index)
		add := func(field, message string) {
			if field != "" {
				field = "." + field
			}
			*errs = append(*errs, ValidationError{Path: path + field, Message: message})
		}
		if !identifierPattern.MatchString(recall.ID) {
			add("id", "must be a stable identifier")
		}
		if _, exists := seenIDs[recall.ID]; exists {
			add("id", "must be unique")
		}
		seenIDs[recall.ID] = struct{}{}
		if !coordinatePattern.MatchString(recall.Product) {
			add("product", "must be a group:name coordinate")
		}
		if !versionPattern.MatchString(recall.StartingVersion) {
			add("startingVersion", "must be an orderable semantic version")
		}
		if recall.EndingVersionExclusive != "" {
			if !versionPattern.MatchString(recall.EndingVersionExclusive) {
				add("endingVersionExclusive", "must be an orderable semantic version")
			} else if versionPattern.MatchString(recall.StartingVersion) &&
				compareVersions(recall.StartingVersion, recall.EndingVersionExclusive) >= 0 {
				add("endingVersionExclusive", "must be greater than the starting version")
			}
		}
		if strings.TrimSpace(recall.Reason) == "" {
			add("reason", "must not be empty")
		}
		if recall.Source != "" && recall.Source != RecallSourceManual && recall.Source != RecallSourceSecurity {
			add("source", "must be "+RecallSourceManual+" or "+RecallSourceSecurity)
		}
		if recall.Source == RecallSourceSecurity && strings.TrimSpace(recall.AssessmentRunID) == "" {
			add("assessmentRunId", "is required for a "+RecallSourceSecurity+" recall")
		}
		if recall.Source != RecallSourceSecurity && recall.AssessmentRunID != "" {
			add("assessmentRunId", "must be empty unless source is "+RecallSourceSecurity)
		}
		if recall.SingleVersion && (recall.EndingVersionExclusive != "" || len(recall.ExcludedVersions) > 0) {
			add("singleVersion", "cannot be combined with an ending version or exclusions")
		}
		seenExcluded := make(map[string]struct{}, len(recall.ExcludedVersions))
		for excludedIndex, version := range recall.ExcludedVersions {
			excludedPath := fmt.Sprintf("excludedVersions[%d]", excludedIndex)
			if !versionPattern.MatchString(version) {
				add(excludedPath, "must be an orderable semantic version")
			}
			if _, exists := seenExcluded[version]; exists {
				add(excludedPath, "must be unique")
			}
			seenExcluded[version] = struct{}{}
		}
		switch recall.Strategy.Type {
		case "", RecallRollForward:
			if recall.Strategy.MinimumVersion != "" && !versionPattern.MatchString(recall.Strategy.MinimumVersion) {
				add("strategy.minimumVersion", "must be an orderable semantic version")
			}
			if recall.Strategy.FreezeUntil != nil || len(recall.Strategy.Environments) > 0 {
				add("strategy", "ROLL_FORWARD accepts only an optional minimumVersion")
			}
		case RecallAllowDowngrade:
			if !versionPattern.MatchString(recall.Strategy.MinimumVersion) {
				add("strategy.minimumVersion", "is required and must be an orderable semantic version")
			} else if versionPattern.MatchString(recall.StartingVersion) &&
				compareVersions(recall.Strategy.MinimumVersion, recall.StartingVersion) >= 0 {
				add("strategy.minimumVersion", "must be older than the starting version")
			}
		case RecallFreeze:
			if recall.Strategy.FreezeUntil == nil {
				add("strategy.freezeUntil", "is required for a freeze")
			}
		case RecallSelectedEnvironments:
			if len(recall.Strategy.Environments) == 0 {
				add("strategy.environments", "must contain at least one Environment")
			}
			seenEnvironments := make(map[string]struct{}, len(recall.Strategy.Environments))
			for environmentIndex, environment := range recall.Strategy.Environments {
				environmentPath := fmt.Sprintf("strategy.environments[%d]", environmentIndex)
				if !dnsLabelPattern.MatchString(environment) {
					add(environmentPath, "must be a DNS label")
				}
				if _, exists := seenEnvironments[environment]; exists {
					add(environmentPath, "must be unique")
				}
				seenEnvironments[environment] = struct{}{}
			}
		default:
			add("strategy.type", "must be ROLL_FORWARD, ALLOW_DOWNGRADE, FREEZE, or SELECTED_ENVIRONMENTS")
		}
	}
}
