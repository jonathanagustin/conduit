package controlplane

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// The connection setting decides where observed state comes from, so it decides
// whether this Hub may issue Plans at all. Issuing work for an Environment that
// another Hub drives would create two writers for one Environment.
func TestConnectionSettingGatesPlanIssuance(t *testing.T) {
	t.Parallel()
	for setting, wantExecutable := range map[string]bool{
		"":                              true,
		ConnectionAgentsConnected:       true,
		ConnectionUnknown:               false,
		ConnectionNoConnectionExpected:  false,
		ConnectionRelayedFromAnotherHub: false,
	} {
		t.Run("setting="+settingName(setting), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			state := foundation()
			state.Environment.ConnectionSetting = setting
			record, err := NewLifecycle(NewMemoryRepository()).CreatePlan(ctx, state)
			if err != nil {
				t.Fatal(err)
			}
			if wantExecutable {
				if record.Status != PlanAwaitingApproval || len(record.Plan.Steps) == 0 {
					t.Fatalf("record = %#v, want executable Plan", record)
				}
				return
			}
			if record.Status != PlanRecorded || len(record.Plan.Steps) != 0 {
				t.Fatalf("record = %#v, want non-executing desired-state record", record)
			}
		})
	}
}

// An Environment that switched away from agents-connected may still hold
// approved Plans authored before the switch. They must not be handed out.
func TestConnectionSettingGatesAgentLeasing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	state := foundation()
	record, err := lifecycle.CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.ApprovePlan(ctx, record.ID, "reviewer@example.test", "approved"); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.LeaseNextPlan(ctx, state.Environment.ID, "agent-1", time.Minute); err != nil {
		t.Fatalf("lease before the switch: %v", err)
	}

	relayed := foundation()
	relayed.Environment.Generation = "generation-relayed"
	relayed.Environment.ConnectionSetting = ConnectionRelayedFromAnotherHub
	// Recording the relayed generation cancels work authored while this Hub was
	// the writer, including a Plan that is already leased.
	recorded, err := lifecycle.CreatePlan(ctx, relayed)
	if err != nil {
		t.Fatal(err)
	}
	if recorded.Status != PlanRecorded {
		t.Fatalf("topology record status = %s", recorded.Status)
	}
	cancelled, err := repository.GetPlan(ctx, record.ID)
	if err != nil || cancelled.Status != PlanCancelled || cancelled.Cancellation == nil {
		t.Fatalf("active Plan was not cancelled: %#v, %v", cancelled, err)
	}
	_, err = lifecycle.LeaseNextPlan(ctx, state.Environment.ID, "agent-1", time.Minute)
	if !errors.Is(err, ErrLeaseRejected) {
		t.Fatalf("lease after switching to relayed = %v, want rejection", err)
	}
}

// An Environment configured to hold no observed state cannot vouch for a
// Release. Counting its evidence would let a canary pass on health nobody
// measured.
func TestNonParticipatingEnvironmentBlocksPromotion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	disconnected := foundation()
	disconnected.Environment.ID = "air-gapped"
	disconnected.Environment.ConnectionSetting = ConnectionNoConnectionExpected
	if _, err := repository.CreatePlan(ctx, PlanRecord{
		ID: "pln_disconnected", Plan: Plan{Environment: "air-gapped", Generation: disconnected.Environment.Generation},
		DesiredState: &disconnected, Status: PlanAwaitingApproval, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	observed := time.Now().UTC().Add(-2 * time.Hour)
	record, err := lifecycle.EvaluatePromotion(ctx, PromotionEvaluationRequest{
		Release:     Release{Product: "app.example:example-api", Version: "0.1.0"},
		FromChannel: "RELEASE", ToChannel: "STABLE",
		Policy: PromotionPolicy{MinimumHealthyEntities: 1, MinimumHealthyDurationSeconds: 60},
		Evidence: []PromotionHealthEvidence{{
			Environment: "air-gapped", InstallationID: "example-api",
			Health: HealthHealthy, HealthySince: observed, ObservedAt: observed,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionBlocked {
		t.Fatalf("status = %s, want %s", record.Status, PromotionBlocked)
	}
	if !containsSubstring(record.Reasons, "cannot participate in promotion") {
		t.Fatalf("reasons = %#v", record.Reasons)
	}
	// The Environment is the whole point of this blocker -- it is what the
	// operator has to reconfigure -- and reading it back out of the sentence
	// is not something a caller should have to do.
	if !blockedBy(record.Blockers, PromotionBlockerEnvironmentCannotVouch, ConnectionNoConnectionExpected) {
		t.Fatalf("blockers = %#v", record.Blockers)
	}
	for _, blocker := range record.Blockers {
		if blocker.Kind == PromotionBlockerEnvironmentCannotVouch &&
			(len(blocker.Subjects) != 1 || blocker.Subjects[0] != "air-gapped") {
			t.Fatalf("subjects = %v, want [air-gapped]", blocker.Subjects)
		}
	}
}

// Evidence outside a canary filter is irrelevant to the decision, including its
// topology. A disconnected production Environment must not block a stage whose
// filter explicitly selects only the connected canary Environment.
func TestCanaryTopologyChecksOnlyFilteredEnvironments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	disconnected := foundation()
	disconnected.Environment.ID = "production"
	disconnected.Environment.ConnectionSetting = ConnectionNoConnectionExpected
	if _, err := repository.CreatePlan(ctx, PlanRecord{
		ID: "pln_production", Plan: Plan{Environment: "production", Generation: disconnected.Environment.Generation},
		DesiredState: &disconnected, Status: PlanAwaitingApproval, CreatedAt: promotionFixtureTime(),
	}); err != nil {
		t.Fatal(err)
	}
	// The canary Environment is seeded too, and it was not before. This case is
	// about the FILTER -- that an excluded Environment which cannot vouch does
	// not block -- and it used to make that point while the included one was an
	// Environment this Hub had never reviewed, vouching only because an unknown
	// Environment defaulted to AGENTS_CONNECTED. It proved the filter worked by
	// relying on the hole beside it.
	canary := foundation()
	canary.Environment.ID = "canary-env"
	if _, err := repository.CreatePlan(ctx, PlanRecord{
		ID: "pln_canary", Plan: Plan{Environment: "canary-env", Generation: canary.Environment.Generation},
		DesiredState: &canary, Status: PlanAwaitingApproval, CreatedAt: promotionFixtureTime(),
	}); err != nil {
		t.Fatal(err)
	}
	record, err := lifecycle.EvaluatePromotion(ctx, PromotionEvaluationRequest{
		Release:     Release{Product: "app.example:example-api", Version: "0.1.0", Channels: []string{"RELEASE"}},
		FromChannel: "RELEASE", ToChannel: "STABLE",
		Policy: PromotionPolicy{
			Type:          PromotionCanary,
			EntityFilters: []PromotionEntityFilter{{Environments: []string{"canary-env"}}},
			Conditions:    []PromotionCondition{{Action: PromoteWhenHealthy, ThresholdPercent: 100}},
		},
		Evidence: []PromotionHealthEvidence{
			healthyFor("canary-env", "api", time.Hour),
			unhealthy("production", "api"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionPassed {
		t.Fatalf("status = %s, reasons = %#v", record.Status, record.Reasons)
	}
}

// A Hub that owns an Environment's settings rejects a Bundle that would
// overwrite them, naming the published error so an operator can match it to the
// documentation. Handing ownership upstream first is what unblocks the import.
func TestSettingsImportRespectsDeclaredStateSource(t *testing.T) {
	t.Parallel()
	for source, wantAdmitted := range map[string]bool{
		SourceEditOnThisHub:    false,
		SourceEditOnAnotherHub: true,
		SourceCopyOfAnotherHub: true,
	} {
		t.Run("source="+source, func(t *testing.T) {
			t.Parallel()
			err := admitSettingsImport(EnvironmentFederation{
				Environment: "dev-use2", DeclaredStateSource: source, Present: true,
			})
			if wantAdmitted {
				if err != nil {
					t.Fatalf("admitSettingsImport = %v, want admitted", err)
				}
				return
			}
			if err == nil {
				t.Fatal("authoritative Hub admitted a settings overwrite")
			}
			if !strings.Contains(err.Error(), ErrorCannotImportInEnvironmentWithAuthoritativeDeclaredState) {
				t.Fatalf("error = %v, want the published authoritative-source rejection", err)
			}
			if !errors.Is(err, ErrConflict) {
				t.Fatalf("error = %v, want a conflict", err)
			}
		})
	}

	// An Environment this Hub has never reviewed is created by the first Bundle
	// that carries it, so there are no local settings to protect.
	if err := admitSettingsImport(EnvironmentFederation{
		Environment: "dev-use2", DeclaredStateSource: SourceEditOnThisHub,
	}); err != nil {
		t.Fatalf("first import of an unknown Environment = %v, want admitted", err)
	}

	// An unusable value is reported distinctly from a deliberate authoritative
	// source, so the operator can tell a misconfiguration from a policy.
	err := admitSettingsImport(EnvironmentFederation{
		Environment: "dev-use2", DeclaredStateSource: "EDIT_SOMEWHERE", Present: true,
	})
	if err == nil || !strings.Contains(err.Error(), ErrorInvalidTargetDeclaredStateSource) {
		t.Fatalf("error = %v, want the published invalid-source rejection", err)
	}
}

// A read-only copy exists so a standby Hub can take over unchanged, so it must
// refuse even a single local Entity config override.
func TestCopyOfAnotherHubRefusesBreakGlassEntityConfig(t *testing.T) {
	t.Parallel()
	for source, wantAllowed := range map[string]bool{
		SourceEditOnThisHub:    true,
		SourceEditOnAnotherHub: true,
		SourceCopyOfAnotherHub: false,
	} {
		t.Run("source="+source, func(t *testing.T) {
			t.Parallel()
			state := foundation()
			state.Environment.DeclaredStateSource = source
			state.Entities[0].ConfigOverrides = "0.1.0:\n  overrides:\n    replicaCount: 3\n"
			errs := Validate(state)
			overrideRejected := false
			for _, err := range errs {
				if strings.HasSuffix(err.Path, "configOverrides") {
					overrideRejected = true
				}
			}
			if overrideRejected == wantAllowed {
				t.Fatalf("errors = %#v, wantAllowed = %v", errs, wantAllowed)
			}
		})
	}
}

func TestFederationRejectsUnrecognizedValues(t *testing.T) {
	t.Parallel()
	state := foundation()
	state.Environment.ConnectionSetting = "SOMETIMES_CONNECTED"
	state.Environment.DeclaredStateSource = "EDIT_ANYWHERE"
	errs := Validate(state)
	paths := map[string]bool{}
	for _, err := range errs {
		paths[err.Path] = true
	}
	if !paths["environment.connectionSetting"] || !paths["environment.declaredStateSource"] {
		t.Fatalf("errors = %#v", errs)
	}
}

func settingName(setting string) string {
	if setting == "" {
		return "undeclared"
	}
	return setting
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

func containsSubstring(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

// A promotion blocked because an Environment cannot vouch for the Release is a
// record the Hub produced itself -- and until this it could not survive its own
// validation. Whether an Environment participates is a property of the
// Environment, and the Plan validator has no repository to read it from, so
// re-deriving the decision from the policy and evidence alone always came back
// with that blocker missing. Any Plan carrying such a promotion was rejected.
func TestPromotionBlockedByEnvironmentTopologySurvivesPlanValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Date(2026, time.August, 3, 12, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	disconnected := foundation()
	disconnected.Environment.ID = "air-gapped"
	disconnected.Environment.ConnectionSetting = ConnectionNoConnectionExpected
	if _, err := repository.CreatePlan(ctx, PlanRecord{
		ID: "pln_topology", Plan: Plan{Environment: "air-gapped", Generation: disconnected.Environment.Generation},
		DesiredState: &disconnected, Status: PlanAwaitingApproval, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	release := Release{Product: "app.example:example-api", Version: "0.1.0"}
	record, err := lifecycle.EvaluatePromotion(ctx, PromotionEvaluationRequest{
		Release: release, FromChannel: "RELEASE", ToChannel: "STABLE",
		Policy: PromotionPolicy{MinimumHealthyEntities: 1, MinimumHealthyDurationSeconds: 60},
		Evidence: []PromotionHealthEvidence{{
			Environment: "air-gapped", InstallationID: "example-api",
			Health: HealthHealthy, HealthySince: now.Add(-2 * time.Hour), ObservedAt: now.Add(-2 * time.Hour),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !blockedBy(record.Blockers, PromotionBlockerEnvironmentCannotVouch, "air-gapped") {
		t.Fatalf("fixture no longer produces a topology blocker: %#v", record.Blockers)
	}
	validate := func(promotion PromotionRecord) []string {
		var errs []ValidationError
		validatePromotions([]PromotionRecord{promotion}, []Release{release}, &errs)
		paths := make([]string, 0, len(errs))
		for _, item := range errs {
			paths = append(paths, item.Path)
		}
		return paths
	}

	if paths := validate(record); len(paths) != 0 {
		t.Fatalf("the Hub's own record did not validate: %v", paths)
	}

	// Trusting these blockers only ever keeps a promotion from passing, which
	// is the safe direction. The dangerous direction is unchanged: a record
	// cannot claim PASSED while a derivable blocker still derives.
	t.Run("cannot be used to claim a promotion passed", func(t *testing.T) {
		forged := cloneJSON(record)
		forged.Status = PromotionPassed
		forged.Blockers = nil
		forged.Reasons = nil
		id, err := promotionIdentity(forged)
		if err != nil {
			t.Fatal(err)
		}
		forged.ID = id
		if paths := validate(forged); !containsSubstring(paths, ".status") {
			t.Fatalf("a promotion with an underivable pass was accepted: %v", paths)
		}
	})

	// A blocker of any other kind is still the validator's to re-derive, so
	// this cannot become a general way to attach unverifiable reasons.
	t.Run("does not extend trust to other blocker kinds", func(t *testing.T) {
		forged := cloneJSON(record)
		forged.Blockers = append(forged.Blockers, PromotionBlocker{
			Kind: PromotionBlockerEvidenceStale, Message: "stale health evidence for elsewhere/thing",
			Subjects: []string{"elsewhere/thing"},
		})
		sortBlockers(forged.Blockers)
		forged.Reasons = blockerMessages(forged.Blockers)
		id, err := promotionIdentity(forged)
		if err != nil {
			t.Fatal(err)
		}
		forged.ID = id
		if paths := validate(forged); !containsSubstring(paths, ".status") {
			t.Fatalf("an invented blocker of another kind was accepted: %v", paths)
		}
	})
}

// TestAnUnreviewedEnvironmentCannotVouchForARelease is the fail-open case.
//
// Promotion evidence is caller-supplied, so the Environment name in it is a
// caller-controlled field. An Environment this Hub has never reviewed resolved
// to the defaulted topology -- AGENTS_CONNECTED, itself a declared divergence
// from Apollo's UNKNOWN -- which tracks health, so it vouched. Naming an
// Environment that does not exist here was enough to make health evidence
// count toward a promotion.
//
// federation.md states the rule this broke: "an Environment holding no observed
// state cannot vouch for a Release".
func TestAnUnreviewedEnvironmentCannotVouchForARelease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)

	record, err := lifecycle.EvaluatePromotion(ctx, PromotionEvaluationRequest{
		Release:     Release{Product: "app.example:example-api", Version: "0.1.0", Channels: []string{"RELEASE"}},
		FromChannel: "RELEASE", ToChannel: "STABLE",
		Policy: PromotionPolicy{
			Type:       PromotionCanary,
			Conditions: []PromotionCondition{{Action: PromoteWhenHealthy, ThresholdPercent: 100}},
		},
		// An Environment this Hub has never seen, named by the caller.
		Evidence: []PromotionHealthEvidence{healthyFor("never-seen-here", "api", time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionBlocked {
		t.Fatalf("an unreviewed Environment vouched for a Release: status = %s", record.Status)
	}
	if !blockedBy(record.Blockers, PromotionBlockerEnvironmentCannotVouch, "never-seen-here") {
		t.Fatalf("the blocker does not name the Environment that could not vouch: %#v", record.Blockers)
	}
}

// And an Environment the Hub has reviewed still vouches, so the rule refuses
// absence rather than refusing promotion.
func TestAReviewedEnvironmentStillVouches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := foundation()
	if _, err := repository.CreatePlan(ctx, PlanRecord{
		ID: "pln_reviewed", Plan: Plan{Environment: state.Environment.ID, Generation: state.Environment.Generation},
		DesiredState: &state, Status: PlanAwaitingApproval, CreatedAt: promotionFixtureTime(),
	}); err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)

	record, err := lifecycle.EvaluatePromotion(ctx, PromotionEvaluationRequest{
		Release:     Release{Product: "app.example:example-api", Version: "0.1.0", Channels: []string{"RELEASE"}},
		FromChannel: "RELEASE", ToChannel: "STABLE",
		Policy: PromotionPolicy{
			Type:       PromotionCanary,
			Conditions: []PromotionCondition{{Action: PromoteWhenHealthy, ThresholdPercent: 100}},
		},
		Evidence: []PromotionHealthEvidence{healthyFor(state.Environment.ID, "example-api", time.Hour)},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, blocker := range record.Blockers {
		if blocker.Kind == PromotionBlockerEnvironmentCannotVouch {
			t.Fatalf("a reviewed Environment was refused: %#v", blocker)
		}
	}
}
