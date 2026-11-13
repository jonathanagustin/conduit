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

// labelledIn returns evidence carrying the Environment labels the Hub stamps on
// before it decides. The submitter never sets these; see stampEnvironmentLabels.
func labelledIn(entry PromotionHealthEvidence, labels map[string]string) PromotionHealthEvidence {
	entry.EnvironmentLabels = labels
	return entry
}

// A canary tier named Environment by Environment cannot follow the fleet: the
// next Environment stood up joins the fleet and not the tier, and the stage goes
// on deciding a fleet-wide promotion on the Environments somebody remembered to
// list. Selecting on an Environment label makes tier membership a property of the
// Environment, so labelling a new Environment extends the tier with no edit to
// the policy -- and un-labelling one removes it, which is the same guarantee run
// backwards.
func TestCanarySetSelectedByEnvironmentLabelFollowsTheFleet(t *testing.T) {
	t.Parallel()
	canary := map[string]string{"tier": "canary"}
	production := map[string]string{"tier": "production"}
	// One policy, written once, naming no Environment at all.
	policy := PromotionPolicy{
		Type: PromotionCanary,
		EntityFilters: []PromotionEntityFilter{{
			EnvironmentLabels: []PromotionLabelRequirement{{Label: "tier", Values: []string{"canary"}}},
		}},
		Conditions: []PromotionCondition{{Action: PromoteWhenHealthy, ThresholdPercent: 100, DurationSeconds: 3600}},
	}
	fleet := []PromotionHealthEvidence{
		labelledIn(healthyFor("dev-use2", "api", 2*time.Hour), canary),
		labelledIn(unhealthy("prod-use2", "api"), production),
		// Labelled with nothing at all: an Environment the Hub holds no labels
		// for is not admitted to a tier on the strength of reporting.
		unhealthy("unreviewed-use2", "api"),
	}

	names := func(evidence []PromotionHealthEvidence) []string {
		selected := make([]string, 0, len(evidence))
		for _, entry := range evidence {
			selected = append(selected, entry.Environment)
		}
		return selected
	}

	t.Run("selects a labelled Environment the policy never names", func(t *testing.T) {
		if selected := names(CanaryEvidence(fleet, policy.EntityFilters)); len(selected) != 1 || selected[0] != "dev-use2" {
			t.Fatalf("canary set = %v, want only the tier=canary Environment dev-use2", selected)
		}
		// And the unhealthy Environments outside the tier do not block it.
		if reasons := EvaluateCanaryStage(
			PromotionEvaluationRequest{Policy: policy, Evidence: fleet}, promotionFixtureTime(),
		); len(reasons) != 0 {
			t.Fatalf("an Environment outside the labelled tier decided the stage: %#v", reasons)
		}
	})

	t.Run("a new Environment joins the tier with no policy edit", func(t *testing.T) {
		grown := append(append([]PromotionHealthEvidence(nil), fleet...),
			labelledIn(unhealthy("canary-use3", "api"), canary))
		if selected := names(CanaryEvidence(grown, policy.EntityFilters)); len(selected) != 2 ||
			selected[0] != "dev-use2" || selected[1] != "canary-use3" {
			t.Fatalf("canary set after the fleet grew = %v, want dev-use2 and canary-use3", selected)
		}
		// The point of extending the tier is that the new Environment can now
		// stop a promotion. A tier that admitted it but never let it decide
		// would be membership in name only.
		if reasons := EvaluateCanaryStage(
			PromotionEvaluationRequest{Policy: policy, Evidence: grown}, promotionFixtureTime(),
		); !blockedBy(reasons, PromotionBlockerCanaryShareBelowThreshold, "condition 1") {
			t.Fatalf("a newly labelled unhealthy canary did not block the stage: %#v", reasons)
		}
	})

	t.Run("every requirement has to hold", func(t *testing.T) {
		// "tier=canary AND region=use2". An Environment satisfying one half is a
		// wider canary set than the operator asked for.
		both := []PromotionEntityFilter{{EnvironmentLabels: []PromotionLabelRequirement{
			{Label: "tier", Values: []string{"canary"}}, {Label: "region", Values: []string{"use2"}},
		}}}
		partial := []PromotionHealthEvidence{
			labelledIn(healthyFor("dev-use3", "api", 2*time.Hour), map[string]string{"tier": "canary", "region": "use3"}),
		}
		if selected := CanaryEvidence(partial, both); len(selected) != 0 {
			t.Fatalf("canary set = %v, want nothing: region=use3 does not satisfy region=use2", names(selected))
		}
	})

	t.Run("a set of acceptable values admits any of them", func(t *testing.T) {
		either := []PromotionEntityFilter{{EnvironmentLabels: []PromotionLabelRequirement{
			{Label: "tier", Values: []string{"canary", "early-access"}},
		}}}
		early := []PromotionHealthEvidence{
			labelledIn(healthyFor("early-use2", "api", 2*time.Hour), map[string]string{"tier": "early-access"}),
		}
		if selected := CanaryEvidence(early, either); len(selected) != 1 {
			t.Fatalf("canary set = %v, want the tier=early-access Environment", names(selected))
		}
	})

	t.Run("a filter declaring no labels is unaffected", func(t *testing.T) {
		// The name-list shape has to keep deciding exactly as it did, or every
		// promotion record written before this field replays differently.
		named := []PromotionEntityFilter{{Environments: []string{"prod-use2"}}}
		if selected := names(CanaryEvidence(fleet, named)); len(selected) != 1 || selected[0] != "prod-use2" {
			t.Fatalf("name-list canary set = %v, want only prod-use2", selected)
		}
	})
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
	// The message must describe blocking, not recalling. Conduit produces no
	// Recall from a canary evaluation, and a blocker that says it does tells an
	// operator the Release has been withdrawn fleet-wide when it has not.
	if !blockedBy(reasons, PromotionBlockerCanaryRecallTriggered, "blocks the promotion") {
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
		// A canary filter whose label requirement names no values matches
		// nothing, so the stage would report "no canary evidence" while the
		// operator reads a policy that plainly nominates a tier.
		"entity filter label requirement without values": {
			policy: PromotionPolicy{
				Type: PromotionCanary,
				EntityFilters: []PromotionEntityFilter{{
					EnvironmentLabels: []PromotionLabelRequirement{{Label: "tier"}},
				}},
				Conditions: []PromotionCondition{{Action: PromoteWhenHealthy, ThresholdPercent: 100}},
			},
			wantError: "entity filter 1 environment label requirement 1",
		},
		"entity filter label requirement without a label": {
			policy: PromotionPolicy{
				Type: PromotionCanary,
				EntityFilters: []PromotionEntityFilter{{
					EnvironmentLabels: []PromotionLabelRequirement{{Label: "  ", Values: []string{"canary"}}},
				}},
				Conditions: []PromotionCondition{{Action: PromoteWhenHealthy, ThresholdPercent: 100}},
			},
			wantError: "entity filter 1 environment label requirement 1",
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

// TestCanaryRecallConditionBlocksRatherThanRecalling pins the divergence
// declared in docs/operations/TENANT-FIDELITY.md.
//
// RECALL_WHEN_UNHEALTHY keeps Apollo's published name and does not do what the
// name says: Conduit blocks the promotion and creates no Recall. The reason is
// evidence provenance -- PromotionEvaluationRequest.Evidence is caller-
// submitted and unattested, so a fleet-wide withdrawal derived from it would be
// a denial-of-service primitive.
//
// Both halves are asserted, because either alone would pass while the other
// regressed: the message must describe blocking, and the stage's return type
// must remain blockers only. If a future change makes this produce a real
// recall, this test fails and sends the author to the declaration.
func TestCanaryRecallConditionBlocksRatherThanRecalling(t *testing.T) {
	t.Parallel()
	request := PromotionEvaluationRequest{
		Policy: PromotionPolicy{
			Type: PromotionCanary,
			Conditions: []PromotionCondition{
				{Action: RecallWhenUnhealthy, ThresholdPercent: 25},
			},
		},
		Evidence: []PromotionHealthEvidence{
			unhealthy("env-a", "worker"), unhealthy("env-a", "cron"),
		},
	}
	blockers := EvaluateCanaryStage(request, promotionFixtureTime())

	triggered := false
	for _, blocker := range blockers {
		if blocker.Kind != PromotionBlockerCanaryRecallTriggered {
			continue
		}
		triggered = true
		// The message an operator reads. "recalls the Release" told them the
		// Release had been withdrawn everywhere, which was never true.
		if strings.Contains(blocker.Message, "recalls the Release") {
			t.Errorf("blocker asserts a recall Conduit does not perform: %q", blocker.Message)
		}
		if !strings.Contains(blocker.Message, "blocks the promotion") {
			t.Errorf("blocker does not say what it actually does: %q", blocker.Message)
		}
	}
	if !triggered {
		t.Fatalf("the recall condition did not fire at all: %#v", blockers)
	}
}
