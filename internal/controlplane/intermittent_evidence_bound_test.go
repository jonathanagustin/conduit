package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"
)

// An Environment declared INTERMITTENT_AGENTS reports when it next connects, so
// its health is honestly "as of last connection". Any evidence-age bound
// shorter than that cadence refuses it -- correctly, since the operator asked
// for evidence no older than N and this Environment cannot supply it. What was
// wrong was the sentence: "stale health evidence" is what an outage looks like,
// and it sent operators to investigate an Environment behaving exactly as
// configured.
//
// The fix adds a blocker naming the conflict. It does NOT waive the bound, and
// these tests are written to fail if a later change makes it do so.

func planWithConnection(environment, setting string) PlanRecord {
	return PlanRecord{
		ID: "plan-" + environment, Plan: Plan{Environment: environment},
		DesiredState: &DesiredState{Environment: Environment{
			ID: environment, Generation: "1", ConnectionSetting: setting,
			DeclaredStateSource: SourceEditOnThisHub,
		}},
	}
}

func boundConflictFixture(t *testing.T, setting string, evidenceAge time.Duration) []PromotionBlocker {
	t.Helper()
	repository := NewMemoryRepository()
	if _, err := repository.CreatePlan(context.Background(), planWithConnection("edge", setting)); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	blockers, err := intermittentEvidenceBoundConflicts(context.Background(), repository,
		[]PromotionHealthEvidence{{
			Environment: "edge", InstallationID: "example-api",
			Health: HealthHealthy, ObservedAt: now.Add(-evidenceAge),
		}},
		PromotionPolicy{MaximumEvidenceAgeSeconds: 300}, now)
	if err != nil {
		t.Fatal(err)
	}
	return blockers
}

func TestIntermittentEvidenceOlderThanTheBoundNamesTheConflict(t *testing.T) {
	t.Parallel()
	blockers := boundConflictFixture(t, ConnectionIntermittentAgents, time.Hour)
	if len(blockers) != 1 {
		t.Fatalf("expected one conflict blocker, got %#v", blockers)
	}
	if blockers[0].Kind != PromotionBlockerEvidenceBoundConflict {
		t.Errorf("blocker kind = %q", blockers[0].Kind)
	}
	// The message has to name both halves of the disagreement, because naming
	// only the Environment reads as a fault and naming only the policy reads as
	// a misconfiguration. It is the pair that is wrong.
	if !strings.Contains(blockers[0].Message, "edge") ||
		!strings.Contains(blockers[0].Message, ConnectionIntermittentAgents) ||
		!strings.Contains(blockers[0].Message, "evidence age bound") {
		t.Errorf("the message does not name the conflict: %q", blockers[0].Message)
	}
}

// Fresh evidence from an intermittent Environment is not a conflict. Without
// this the blocker could be raised for every intermittent Environment, which
// would turn a working promotion into a permanently blocked one.
func TestIntermittentEvidenceWithinTheBoundIsNotAConflict(t *testing.T) {
	t.Parallel()
	if blockers := boundConflictFixture(t, ConnectionIntermittentAgents, time.Minute); len(blockers) != 0 {
		t.Fatalf("fresh evidence raised a conflict: %#v", blockers)
	}
}

// A continuously-reporting Environment with stale evidence is an outage, and
// must keep saying so. Without this the fix could relabel every stale report as
// a configuration conflict, which is the opposite failure and a worse one.
func TestStaleEvidenceFromAContinuousEnvironmentIsNotRelabelled(t *testing.T) {
	t.Parallel()
	if blockers := boundConflictFixture(t, ConnectionAgentsConnected, time.Hour); len(blockers) != 0 {
		t.Fatalf("an outage was relabelled as a configuration conflict: %#v", blockers)
	}
}

// The bound is still enforced. This is the constraint the whole design turns
// on: the new blocker accompanies EVIDENCE_STALE rather than replacing it, so
// the promotion is refused either way and no canary passes on health nobody
// measured recently.
func TestTheEvidenceBoundIsStillEnforcedForAnIntermittentEnvironment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := repository.CreatePlan(ctx, planWithConnection("edge", ConnectionIntermittentAgents)); err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)
	now := lifecycle.now().UTC()

	record, err := lifecycle.EvaluatePromotion(ctx, PromotionEvaluationRequest{
		Release:     Release{Product: "app.example:example-api", Version: "0.1.0"},
		FromChannel: "RELEASE", ToChannel: "STABLE",
		Policy: PromotionPolicy{MaximumEvidenceAgeSeconds: 300},
		Evidence: []PromotionHealthEvidence{{
			Environment: "edge", InstallationID: "example-api",
			Health: HealthHealthy, HealthySince: now.Add(-3 * time.Hour), ObservedAt: now.Add(-time.Hour),
		}},
		EvaluationStartedAt: now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != PromotionBlocked {
		t.Fatalf("an intermittent Environment's stale evidence passed the age bound: %s", record.Status)
	}
	kinds := map[string]bool{}
	for _, blocker := range record.Blockers {
		kinds[blocker.Kind] = true
	}
	if !kinds[PromotionBlockerEvidenceStale] {
		t.Errorf("EVIDENCE_STALE was not derived, so the bound was waived: %#v", record.Blockers)
	}
	if !kinds[PromotionBlockerEvidenceBoundConflict] {
		t.Errorf("the conflict was not named alongside it: %#v", record.Blockers)
	}
}

// A record carrying the new kind has to survive the replay validator, which has
// no repository and so can never re-derive it. It is trusted for the same
// reason ENVIRONMENT_CANNOT_VOUCH is: it only ever adds a blocker.
func TestARecordCarryingTheConflictReproducesUnderReplay(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := repository.CreatePlan(ctx, planWithConnection("edge", ConnectionIntermittentAgents)); err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)
	now := lifecycle.now().UTC()
	record, err := lifecycle.EvaluatePromotion(ctx, PromotionEvaluationRequest{
		Release:     Release{Product: "app.example:example-api", Version: "0.1.0"},
		FromChannel: "RELEASE", ToChannel: "STABLE",
		Policy: PromotionPolicy{MaximumEvidenceAgeSeconds: 300},
		Evidence: []PromotionHealthEvidence{{
			Environment: "edge", InstallationID: "example-api",
			Health: HealthHealthy, HealthySince: now.Add(-3 * time.Hour), ObservedAt: now.Add(-time.Hour),
		}},
		EvaluationStartedAt: now.Add(-2 * time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}

	errs := []ValidationError{}
	validatePromotions([]PromotionRecord{record},
		[]Release{{Product: "app.example:example-api", Version: "0.1.0"}}, &errs)
	if len(errs) != 0 {
		t.Fatalf("a record the Hub produced failed to reproduce under replay: %#v", errs)
	}
}
