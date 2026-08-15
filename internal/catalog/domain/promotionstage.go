package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// A promotion stage comes in two published shapes, and they ask different
// questions. A timed stage asks "has this Release sat on the source channel long
// enough?" A canary stage asks "have the Entities I nominated as canaries stayed
// healthy?" Collapsing both into one set of thresholds loses the distinction that
// matters: a canary stage measures a *proportion* of a *named subset*, and it can
// react by recalling rather than only by declining to promote.
const (
	// PromotionTimed gates on elapsed time plus Release label requirements.
	PromotionTimed = "TIMED"
	// PromotionCanary gates on the observed health of a filtered Entity set.
	PromotionCanary = "CANARY"

	// PromoteWhenHealthy promotes once enough canaries have held healthy.
	PromoteWhenHealthy = "PROMOTE_WHEN_HEALTHY"
	// RecallWhenUnhealthy recalls the Release when enough canaries are unhealthy.
	RecallWhenUnhealthy = "RECALL_WHEN_UNHEALTHY"
)

// PromotionLabelRequirement requires a Release label to carry one of a set of
// acceptable values. A single required value is the common case but not the only
// one, so the contract is a set: "either of these two build flavors is fine" is
// not expressible with one value per label.
type PromotionLabelRequirement struct {
	Label  string   `json:"label"`
	Values []string `json:"values"`
}

// PromotionEntityFilter names the Entities that act as canaries for a stage. An
// Entity qualifies when it matches every populated dimension of the filter, and a
// stage's canary set is the union of its filters.
//
// Filtering by Entity label is not supported here: health evidence carries no
// Entity labels, and matching on labels the evaluator cannot see would silently
// select nothing.
type PromotionEntityFilter struct {
	Environments  []string `json:"environments,omitempty"`
	Installations []string `json:"installations,omitempty"`
}

func (filter PromotionEntityFilter) Matches(evidence PromotionHealthEvidence) bool {
	if len(filter.Environments) > 0 && !contains(filter.Environments, evidence.Environment) {
		return false
	}
	if len(filter.Installations) > 0 && !contains(filter.Installations, evidence.InstallationID) {
		return false
	}
	return true
}

// PromotionCondition is one canary rule. Conditions are evaluated with OR
// semantics: the stage acts as soon as any single condition is met.
type PromotionCondition struct {
	Action string `json:"action"`
	// ThresholdPercent is the share of the canary set that must be in the
	// condition's health state, expressed 0-100.
	ThresholdPercent float64 `json:"thresholdPercent"`
	// DurationSeconds is how long that state must have held. Zero means the state
	// only has to hold at the instant of evaluation.
	DurationSeconds int `json:"durationSeconds,omitempty"`
	// MinimumEntityCount refuses to act on a canary set smaller than this, so a
	// filter that happens to match one Entity cannot carry a fleet-wide decision.
	MinimumEntityCount int `json:"minimumEntityCount,omitempty"`
}

// PromotionTally is the health of one evidence set measured against one duration.
type PromotionTally struct {
	Total     int
	Healthy   int
	Unhealthy int
}

// ValidateEvidence separates usable health evidence from unusable, and is run
// once per stage rather than once per condition: a duplicate or stale report is a
// data-integrity problem regardless of which condition is being checked, and
// reporting it repeatedly would bury the decision.
func ValidateEvidence(
	evidence []PromotionHealthEvidence, policy PromotionPolicy, now time.Time,
) ([]PromotionHealthEvidence, []PromotionBlocker) {
	valid := make([]PromotionHealthEvidence, 0, len(evidence))
	blockers := make([]PromotionBlocker, 0)
	// Every blocker here is about one piece of evidence, and the Environment
	// and Installation it came from is what an operator needs to go fix.
	reject := func(kind, message, key string) {
		blockers = append(blockers, PromotionBlocker{Kind: kind, Message: message + key, Subjects: []string{key}})
	}
	seen := make(map[string]struct{}, len(evidence))
	for _, entry := range evidence {
		key := entry.Environment + "/" + entry.InstallationID
		if _, exists := seen[key]; exists {
			reject(PromotionBlockerEvidenceDuplicate, "duplicate health evidence for ", key)
			continue
		}
		seen[key] = struct{}{}
		if !dnsLabelPattern.MatchString(entry.Environment) || strings.TrimSpace(entry.InstallationID) == "" ||
			entry.ObservedAt.IsZero() || entry.ObservedAt.After(now) {
			reject(PromotionBlockerEvidenceInvalid, "invalid health evidence for ", key)
			continue
		}
		if policy.MaximumEvidenceAgeSeconds > 0 &&
			now.Sub(entry.ObservedAt) > time.Duration(policy.MaximumEvidenceAgeSeconds)*time.Second {
			reject(PromotionBlockerEvidenceStale, "stale health evidence for ", key)
			continue
		}
		switch entry.Health {
		case HealthHealthy:
			if entry.HealthySince.IsZero() || entry.HealthySince.After(entry.ObservedAt) {
				reject(PromotionBlockerEvidenceHealthySinceInvalid, "invalid healthy-since evidence for ", key)
				continue
			}
		case HealthUnhealthy:
		default:
			reject(PromotionBlockerEvidenceHealthUnknown, "unknown health state for ", key)
			continue
		}
		valid = append(valid, entry)
	}
	return valid, blockers
}

// CountHeld tallies how much already-validated evidence held its health state for
// at least minimumDuration.
func CountHeld(
	evidence []PromotionHealthEvidence, policy PromotionPolicy, minimumDuration time.Duration,
) PromotionTally {
	tally := PromotionTally{Total: len(evidence)}
	for _, entry := range evidence {
		switch entry.Health {
		case HealthHealthy:
			duration := entry.ObservedAt.Sub(entry.HealthySince)
			if policy.ExcludeWeekends {
				duration = WeekdayDuration(entry.HealthySince, entry.ObservedAt)
			}
			if duration >= minimumDuration {
				tally.Healthy++
			}
		case HealthUnhealthy:
			tally.Unhealthy++
		}
	}
	return tally
}

// CanaryEvidence narrows evidence to the Entities a stage nominated. A stage with
// no filters treats every reporting Entity as a canary.
func CanaryEvidence(evidence []PromotionHealthEvidence, filters []PromotionEntityFilter) []PromotionHealthEvidence {
	if len(filters) == 0 {
		return evidence
	}
	selected := make([]PromotionHealthEvidence, 0, len(evidence))
	for _, entry := range evidence {
		for _, filter := range filters {
			if filter.Matches(entry) {
				selected = append(selected, entry)
				break
			}
		}
	}
	return selected
}

// EvaluateCanaryStage decides a canary stage. Conditions are OR'd, so one
// satisfied promote condition passes the stage, but a satisfied recall condition
// blocks it regardless: evidence that the Release is harming canaries outranks
// evidence that enough of them are healthy.
func EvaluateCanaryStage(request PromotionEvaluationRequest, now time.Time) []PromotionBlocker {
	if len(request.Policy.Conditions) == 0 {
		return []PromotionBlocker{{
			Kind: PromotionBlockerCanaryNoConditions, Message: "canary stage declares no conditions",
		}}
	}
	selected := CanaryEvidence(request.Evidence, request.Policy.EntityFilters)
	valid, blocking := ValidateEvidence(selected, request.Policy, now)

	// A condition is identified by its position in the stage, which is how the
	// messages have always named it and the only handle a policy gives one.
	condition := func(index int, kind, message string) PromotionBlocker {
		return PromotionBlocker{Kind: kind, Message: message, Subjects: []string{fmt.Sprintf("condition %d", index+1)}}
	}

	// Under OR semantics an unmet promote condition is not a failure as long as
	// some other promote condition is met, so its explanation is held back and
	// only reported when nothing passed. A met recall condition blocks outright.
	promoted := false
	unmet := make([]PromotionBlocker, 0, len(request.Policy.Conditions))
	for index, policyCondition := range request.Policy.Conditions {
		tally := CountHeld(valid, request.Policy, time.Duration(policyCondition.DurationSeconds)*time.Second)
		if tally.Total == 0 {
			unmet = append(unmet, condition(index, PromotionBlockerCanaryNoEvidence,
				fmt.Sprintf("condition %d has no canary evidence", index+1)))
			continue
		}
		if tally.Total < policyCondition.MinimumEntityCount {
			unmet = append(unmet, condition(index, PromotionBlockerCanaryTooFewEntities, fmt.Sprintf(
				"condition %d requires at least %d canary Entities but the filters selected %d",
				index+1, policyCondition.MinimumEntityCount, tally.Total)))
			continue
		}
		switch policyCondition.Action {
		case PromoteWhenHealthy:
			share := percentage(tally.Healthy, tally.Total)
			if share+percentageTolerance >= policyCondition.ThresholdPercent {
				promoted = true
				continue
			}
			unmet = append(unmet, condition(index, PromotionBlockerCanaryShareBelowThreshold, fmt.Sprintf(
				"condition %d requires %.4g%% of canaries healthy for %s but %.4g%% qualify",
				index+1, policyCondition.ThresholdPercent,
				time.Duration(policyCondition.DurationSeconds)*time.Second, share)))
		case RecallWhenUnhealthy:
			share := percentage(tally.Unhealthy, tally.Total)
			if share+percentageTolerance >= policyCondition.ThresholdPercent {
				blocking = append(blocking, condition(index, PromotionBlockerCanaryRecallTriggered, fmt.Sprintf(
					"condition %d recalls the Release: %.4g%% of canaries are unhealthy, at or above %.4g%%",
					index+1, share, policyCondition.ThresholdPercent)))
			}
		default:
			blocking = append(blocking, condition(index, PromotionBlockerCanaryUnsupportedAction,
				fmt.Sprintf("condition %d has unsupported action %s", index+1, policyCondition.Action)))
		}
	}
	if !promoted {
		blocking = append(blocking, unmet...)
		// Deliberately without a Subject: no one condition is at fault, the
		// stage as a whole found nothing to promote on.
		blocking = append(blocking, PromotionBlocker{
			Kind: PromotionBlockerCanaryNoConditionMet, Message: "no canary promote condition was met",
		})
	}
	// Deliberately unsorted: this is one of several runs its caller merges,
	// and the caller sorts the result. Sorting here as well would look like a
	// guarantee callers could rely on, while being unobservable either way.
	return blocking
}

// percentageTolerance absorbs binary floating-point error so a threshold written
// as 90 is met by exactly nine of ten canaries.
const percentageTolerance = 1e-9

func percentage(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) * 100 / float64(total)
}

// EvaluateLabelRequirements checks both label contracts: the single-valued map
// kept for existing policies, and the set-valued requirements a published stage
// uses.
func EvaluateLabelRequirements(release Release, policy PromotionPolicy) []PromotionBlocker {
	blockers := make([]PromotionBlocker, 0)
	// The Subject is the label name rather than the whole requirement: it is
	// what the operator has to set on the Release to clear the blocker.
	for key, expected := range policy.RequiredLabels {
		if release.Labels[key] != expected {
			blockers = append(blockers, PromotionBlocker{
				Kind:    PromotionBlockerRequiredLabelMissing,
				Message: fmt.Sprintf("required label %s=%s is missing", key, expected), Subjects: []string{key},
			})
		}
	}
	for _, requirement := range policy.LabelRequirements {
		value, present := release.Labels[requirement.Label]
		if !present || !contains(requirement.Values, value) {
			blockers = append(blockers, PromotionBlocker{
				Kind: PromotionBlockerRequiredLabelNotAllowed,
				Message: fmt.Sprintf("required label %s must be one of %s",
					requirement.Label, strings.Join(requirement.Values, ", ")),
				Subjects: []string{requirement.Label},
			})
		}
	}
	return blockers
}

// ValidateStage refuses a policy that cannot be evaluated.
//
// It returns the reason and not a sentinel. Which error class an invalid stage
// belongs to is the caller's vocabulary -- out here it has always been an
// invalid transition -- and wrapping it here would either duplicate that
// decision or change the message an operator reads.
func ValidateStage(policy PromotionPolicy) error {
	switch StageType(policy) {
	case PromotionTimed:
		if len(policy.Conditions) > 0 || len(policy.EntityFilters) > 0 {
			return errors.New("a timed stage cannot declare canary conditions or filters")
		}
	case PromotionCanary:
		if len(policy.Conditions) == 0 {
			return errors.New("a canary stage requires at least one condition")
		}
		for index, condition := range policy.Conditions {
			if condition.Action != PromoteWhenHealthy && condition.Action != RecallWhenUnhealthy {
				return fmt.Errorf("condition %d action must be %s or %s",
					index+1, PromoteWhenHealthy, RecallWhenUnhealthy)
			}
			if condition.ThresholdPercent < 0 || condition.ThresholdPercent > 100 {
				return fmt.Errorf("condition %d threshold must be a percentage", index+1)
			}
			if condition.DurationSeconds < 0 || condition.MinimumEntityCount < 0 {
				return fmt.Errorf("condition %d cannot declare negative bounds", index+1)
			}
		}
	default:
		return fmt.Errorf("promotion type must be %s or %s", PromotionTimed, PromotionCanary)
	}
	for index, requirement := range policy.LabelRequirements {
		if strings.TrimSpace(requirement.Label) == "" || len(requirement.Values) == 0 {
			return fmt.Errorf("label requirement %d needs a label and at least one value", index+1)
		}
	}
	return nil
}

func StageType(policy PromotionPolicy) string {
	if strings.TrimSpace(policy.Type) == "" {
		return PromotionTimed
	}
	return strings.TrimSpace(policy.Type)
}
