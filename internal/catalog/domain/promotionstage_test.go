package domain

import (
	"strings"
	"testing"
	"time"
)

func promotionFixtureTime() time.Time { return time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC) }

// healthyFor builds evidence for an Entity that has held healthy for a duration.
func healthyFor(environment, installation string, held time.Duration) PromotionHealthEvidence {
	observed := promotionFixtureTime()
	return PromotionHealthEvidence{
		Environment: environment, InstallationID: installation,
		Health: HealthHealthy, HealthySince: observed.Add(-held), ObservedAt: observed,
	}
}

func unhealthy(environment, installation string) PromotionHealthEvidence {
	return PromotionHealthEvidence{
		Environment: environment, InstallationID: installation,
		Health: HealthUnhealthy, ObservedAt: promotionFixtureTime(),
	}
}

// A canary stage measures a proportion of a named subset, not an absolute count
// of everything reporting. Nine of ten healthy meets a 90% threshold; eight does
// not.
func TestCanaryStagePromotesOnProportionOfCanarySet(t *testing.T) {
	t.Parallel()
	policy := func() PromotionPolicy {
		return PromotionPolicy{
			Type: PromotionCanary,
			Conditions: []PromotionCondition{{
				Action: PromoteWhenHealthy, ThresholdPercent: 90, DurationSeconds: int(24 * time.Hour / time.Second),
			}},
		}
	}
	evidence := func(healthyCount int) []PromotionHealthEvidence {
		entries := make([]PromotionHealthEvidence, 0, 10)
		for index := range 10 {
			name := "api-" + string(rune('a'+index))
			if index < healthyCount {
				entries = append(entries, healthyFor("env-a", name, 25*time.Hour))
				continue
			}
			entries = append(entries, healthyFor("env-a", name, time.Hour))
		}
		return entries
	}

	nine := EvaluateCanaryStage(PromotionEvaluationRequest{Policy: policy(), Evidence: evidence(9)}, promotionFixtureTime())
	if len(nine) != 0 {
		t.Fatalf("9/10 healthy for long enough was blocked: %#v", nine)
	}
	eight := EvaluateCanaryStage(PromotionEvaluationRequest{Policy: policy(), Evidence: evidence(8)}, promotionFixtureTime())
	if len(eight) == 0 {
		t.Fatal("8/10 healthy met a 90% threshold")
	}
}

// The canary set is the filtered subset. Entities outside the filters must not
// dilute or carry the decision.
func TestCanaryStageOnlyCountsFilteredEntities(t *testing.T) {
	t.Parallel()
	request := PromotionEvaluationRequest{
		Policy: PromotionPolicy{
			Type:          PromotionCanary,
			EntityFilters: []PromotionEntityFilter{{Environments: []string{"canary-env"}}},
			Conditions: []PromotionCondition{{
				Action: PromoteWhenHealthy, ThresholdPercent: 100, DurationSeconds: 3600,
			}},
		},
		Evidence: []PromotionHealthEvidence{
			healthyFor("canary-env", "api", 2*time.Hour),
			// Outside the filter: unhealthy, and must not block the stage.
			unhealthy("production", "api"),
		},
	}
	if reasons := EvaluateCanaryStage(request, promotionFixtureTime()); len(reasons) != 0 {
		t.Fatalf("unfiltered Entity influenced the canary decision: %#v", reasons)
	}
}

// Conditions are OR'd, so one satisfied promote condition passes even when a
// stricter one does not.
func TestCanaryConditionsUseOrSemantics(t *testing.T) {
	t.Parallel()
	request := PromotionEvaluationRequest{
		Policy: PromotionPolicy{
			Type: PromotionCanary,
			Conditions: []PromotionCondition{
				{Action: PromoteWhenHealthy, ThresholdPercent: 100, DurationSeconds: int(48 * time.Hour / time.Second)},
				{Action: PromoteWhenHealthy, ThresholdPercent: 50, DurationSeconds: 3600},
			},
		},
		Evidence: []PromotionHealthEvidence{
			healthyFor("env-a", "api", 2*time.Hour), healthyFor("env-a", "web", 2*time.Hour),
		},
	}
	if reasons := EvaluateCanaryStage(request, promotionFixtureTime()); len(reasons) != 0 {
		t.Fatalf("a satisfied looser condition did not pass the stage: %#v", reasons)
	}
}

// Evidence that the Release is harming canaries outranks evidence that enough of
// them are healthy, so a met recall condition blocks regardless.
func TestRecallConditionOutranksPromoteCondition(t *testing.T) {
	t.Parallel()
	request := PromotionEvaluationRequest{
		Policy: PromotionPolicy{
			Type: PromotionCanary,
			Conditions: []PromotionCondition{
				{Action: PromoteWhenHealthy, ThresholdPercent: 50, DurationSeconds: 3600},
				{Action: RecallWhenUnhealthy, ThresholdPercent: 25},
			},
		},
		Evidence: []PromotionHealthEvidence{
			healthyFor("env-a", "api", 2*time.Hour), healthyFor("env-a", "web", 2*time.Hour),
			unhealthy("env-a", "worker"), unhealthy("env-a", "cron"),
		},
	}
	reasons := EvaluateCanaryStage(request, promotionFixtureTime())
	if !blockedBy(reasons, PromotionBlockerCanaryRecallTriggered, "recalls the Release") {
		t.Fatalf("reasons = %#v, want the recall condition to block", reasons)
	}
}

// A canary set smaller than the declared minimum cannot carry a fleet-wide
// decision, and an empty set cannot pass by vacuous truth.
func TestCanaryStageRefusesInsufficientEvidence(t *testing.T) {
	t.Parallel()
	minimum := PromotionEvaluationRequest{
		Policy: PromotionPolicy{Type: PromotionCanary, Conditions: []PromotionCondition{{
			Action: PromoteWhenHealthy, ThresholdPercent: 100, MinimumEntityCount: 5,
		}}},
		Evidence: []PromotionHealthEvidence{healthyFor("env-a", "api", time.Hour)},
	}
	if reasons := EvaluateCanaryStage(minimum, promotionFixtureTime()); !blockedBy(reasons, PromotionBlockerCanaryTooFewEntities, "at least 5 canary Entities") {
		t.Fatalf("reasons = %#v, want the minimum count enforced", reasons)
	}

	empty := PromotionEvaluationRequest{
		Policy: PromotionPolicy{Type: PromotionCanary, Conditions: []PromotionCondition{{
			Action: PromoteWhenHealthy, ThresholdPercent: 100,
		}}},
	}
	if reasons := EvaluateCanaryStage(empty, promotionFixtureTime()); !blockedBy(reasons, PromotionBlockerCanaryNoEvidence, "no canary evidence") {
		t.Fatalf("reasons = %#v, want an empty canary set to block", reasons)
	}
}

// A timed stage waits for the healthy duration unless the stage waives it.
func TestTimedStageNoDelayWaivesTheHealthyWait(t *testing.T) {
	t.Parallel()
	policy := PromotionPolicy{
		MinimumHealthyEntities: 1, MinimumHealthyDurationSeconds: int(24 * time.Hour / time.Second),
	}
	request := PromotionEvaluationRequest{
		Policy:   policy,
		Evidence: []PromotionHealthEvidence{healthyFor("env-a", "api", time.Minute)},
	}
	if reasons := EvaluatePromotion(request, promotionFixtureTime()); len(reasons) == 0 {
		t.Fatal("a one-minute-old healthy Entity satisfied a 24-hour wait")
	}
	request.Policy.NoDelay = true
	if reasons := EvaluatePromotion(request, promotionFixtureTime()); len(reasons) != 0 {
		t.Fatalf("no-delay did not waive the wait: %#v", reasons)
	}
}

// A label requirement accepts a set of values, so "either flavor is fine" is
// expressible rather than forcing one exact value per label.
func TestLabelRequirementsAcceptAnyOfSeveralValues(t *testing.T) {
	t.Parallel()
	policy := PromotionPolicy{LabelRequirements: []PromotionLabelRequirement{{
		Label: "build-flavor", Values: []string{"fips", "standard"},
	}}}
	for value, wantAccepted := range map[string]bool{"fips": true, "standard": true, "debug": false} {
		release := Release{Labels: map[string]string{"build-flavor": value}}
		reasons := EvaluateLabelRequirements(release, policy)
		if accepted := len(reasons) == 0; accepted != wantAccepted {
			t.Fatalf("value %s accepted = %v, want %v (%#v)", value, accepted, wantAccepted, reasons)
		}
	}
	// A missing label is a failure, not a pass by absence.
	if reasons := EvaluateLabelRequirements(Release{}, policy); len(reasons) == 0 {
		t.Fatal("an absent label satisfied a label requirement")
	}
}

func TestPromotionStageValidationRejectsMixedShapes(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		policy    PromotionPolicy
		wantError string
	}{
		"canary without conditions": {
			policy:    PromotionPolicy{Type: PromotionCanary},
			wantError: "requires at least one condition",
		},
		"timed with canary conditions": {
			policy:    PromotionPolicy{Conditions: []PromotionCondition{{Action: PromoteWhenHealthy}}},
			wantError: "cannot declare canary conditions",
		},
		"unknown stage type": {
			policy:    PromotionPolicy{Type: "EVENTUALLY"},
			wantError: "promotion type must be",
		},
		"unknown condition action": {
			policy:    PromotionPolicy{Type: PromotionCanary, Conditions: []PromotionCondition{{Action: "HOPE"}}},
			wantError: "action must be",
		},
		"threshold outside a percentage": {
			policy: PromotionPolicy{Type: PromotionCanary, Conditions: []PromotionCondition{{
				Action: PromoteWhenHealthy, ThresholdPercent: 140,
			}}},
			wantError: "must be a percentage",
		},
		"label requirement without values": {
			policy:    PromotionPolicy{LabelRequirements: []PromotionLabelRequirement{{Label: "flavor"}}},
			wantError: "at least one value",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := ValidateStage(testCase.policy)
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("error = %v, want it to mention %q", err, testCase.wantError)
			}
		})
	}
	// A plain timed stage stays valid without declaring anything new.
	if err := ValidateStage(PromotionPolicy{MinimumHealthyEntities: 1}); err != nil {
		t.Fatalf("default timed stage = %v", err)
	}
}

// blockedBy asserts both halves of a blocker: the kind an operator groups and
// filters on, and the message detail that kind alone cannot carry.
func blockedBy(blockers []PromotionBlocker, kind, messageContains string) bool {
	for _, blocker := range blockers {
		if blocker.Kind == kind && strings.Contains(blocker.Message, messageContains) {
			return true
		}
	}
	return false
}

// Evidence validation runs once per stage rather than once per condition, so
// what it rejects is rejected for the whole decision. Each rejection names the
// Environment and Installation it came from, because that is what an operator
// has to go and fix.
func TestEvidenceValidationRejectsEachWayEvidenceCanBeUnusable(t *testing.T) {
	t.Parallel()
	observed := promotionFixtureTime()
	policy := PromotionPolicy{MaximumEvidenceAgeSeconds: 3600}

	for name, testCase := range map[string]struct {
		evidence []PromotionHealthEvidence
		wantKind string
	}{
		// The same Entity reporting twice would be counted twice, and a tally
		// is the whole basis of a promotion decision.
		"the same Entity reports twice": {
			evidence: []PromotionHealthEvidence{
				healthyFor("dev", "api", time.Hour), healthyFor("dev", "api", time.Hour),
			},
			wantKind: PromotionBlockerEvidenceDuplicate,
		},
		"the Environment is not an identifier": {
			evidence: []PromotionHealthEvidence{{
				Environment: "Not A Label", InstallationID: "api",
				Health: HealthHealthy, HealthySince: observed.Add(-time.Hour), ObservedAt: observed,
			}},
			wantKind: PromotionBlockerEvidenceInvalid,
		},
		"the observation is from the future": {
			evidence: []PromotionHealthEvidence{{
				Environment: "dev", InstallationID: "api",
				Health: HealthHealthy, HealthySince: observed, ObservedAt: observed.Add(time.Minute),
			}},
			wantKind: PromotionBlockerEvidenceInvalid,
		},
		"the observation is older than the policy allows": {
			evidence: []PromotionHealthEvidence{{
				Environment: "dev", InstallationID: "api", Health: HealthHealthy,
				HealthySince: observed.Add(-4 * time.Hour), ObservedAt: observed.Add(-2 * time.Hour),
			}},
			wantKind: PromotionBlockerEvidenceStale,
		},
		// Healthy with no healthy-since is not evidence of a duration. Accepting
		// it would make the zero time the start, and every soak would have been
		// satisfied since 1 January year 1.
		"healthy since nothing": {
			evidence: []PromotionHealthEvidence{{
				Environment: "dev", InstallationID: "api", Health: HealthHealthy, ObservedAt: observed,
			}},
			wantKind: PromotionBlockerEvidenceHealthySinceInvalid,
		},
		"healthy since after it was observed": {
			evidence: []PromotionHealthEvidence{{
				Environment: "dev", InstallationID: "api", Health: HealthHealthy,
				HealthySince: observed.Add(time.Hour), ObservedAt: observed,
			}},
			wantKind: PromotionBlockerEvidenceHealthySinceInvalid,
		},
		"a health state this stage does not understand": {
			evidence: []PromotionHealthEvidence{{
				Environment: "dev", InstallationID: "api", Health: "DEGRADED", ObservedAt: observed,
			}},
			wantKind: PromotionBlockerEvidenceHealthUnknown,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			valid, blockers := ValidateEvidence(testCase.evidence, policy, observed)
			if !blockedBy(blockers, testCase.wantKind, "") {
				t.Fatalf("blockers = %#v, want one of kind %s", blockers, testCase.wantKind)
			}
			if len(valid) >= len(testCase.evidence) {
				t.Fatalf("unusable evidence was still counted: %#v", valid)
			}
			for _, blocker := range blockers {
				if len(blocker.Subjects) == 0 {
					t.Fatalf("blocker %#v does not name the Entity it is about", blocker)
				}
			}
		})
	}

	// The healthy-since guard is what stops the zero time being read as a start
	// instant. Without it this evidence would satisfy any soak duration.
	zeroSince := []PromotionHealthEvidence{{
		Environment: "dev", InstallationID: "api", Health: HealthHealthy, ObservedAt: observed,
	}}
	valid, _ := ValidateEvidence(zeroSince, PromotionPolicy{}, observed)
	if tally := CountHeld(valid, PromotionPolicy{}, 30*24*time.Hour); tally.Healthy != 0 {
		t.Fatalf("evidence healthy since the zero time satisfied a month-long soak: %#v", tally)
	}

	// Good evidence survives, and unhealthy evidence is usable: it is what the
	// unhealthy tally and the recall conditions are counted from.
	good := []PromotionHealthEvidence{healthyFor("dev", "api", time.Hour), unhealthy("dev", "worker")}
	valid, blockers := ValidateEvidence(good, policy, observed)
	if len(valid) != 2 || len(blockers) != 0 {
		t.Fatalf("valid = %#v blockers = %#v", valid, blockers)
	}
}
