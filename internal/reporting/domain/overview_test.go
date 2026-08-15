package domain

import (
	"fmt"
	"testing"
	"time"
)

var overviewFixtureTime = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func TestCurrentPanelBoundsItemsWithoutUnderstatingTheCount(t *testing.T) {
	t.Parallel()
	items := make([]OverviewPlan, 0, MaximumOverviewItems+2)
	for index := 0; index < MaximumOverviewItems+2; index++ {
		items = append(items, OverviewPlan{ID: fmt.Sprintf("plan-%02d", index)})
	}
	panel := CurrentPanel(items)
	// The count is what is wrong; the items are only what fits on the screen.
	if panel.Count != MaximumOverviewItems+2 || len(panel.Items) != MaximumOverviewItems {
		t.Fatalf("panel = %#v", panel)
	}
	if panel.Status != OverviewCurrent {
		t.Fatalf("status = %s", panel.Status)
	}
	// An answered panel with nothing in it serves an empty list, not null: the
	// console distinguishes "nothing wrong" from "no answer" by status, and a
	// null would make that a second, silent signal.
	empty := CurrentPanel[OverviewPlan](nil)
	if empty.Status != OverviewCurrent || empty.Count != 0 || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty panel = %#v", empty)
	}
}

func TestUnansweredPanelsAreUnavailableRatherThanEmpty(t *testing.T) {
	t.Parallel()
	projection := NewOverviewProjection(overviewFixtureTime)
	if projection.StaleAfterSeconds != int64(OverviewStaleAfter/time.Second) {
		t.Fatalf("stale threshold = %d", projection.StaleAfterSeconds)
	}
	if !projection.GeneratedAt.Equal(overviewFixtureTime) {
		t.Fatalf("generatedAt = %s", projection.GeneratedAt)
	}
	for name, status := range map[string]string{
		"approvals":  projection.AwaitingApprovals.Status,
		"failed":     projection.FailedPlans.Status,
		"running":    projection.RunningPlans.Status,
		"unhealthy":  projection.UnhealthyEntities.Status,
		"stale":      projection.StaleReports.Status,
		"promotions": projection.BlockedPromotions.Status,
	} {
		if status != OverviewUnavailable {
			t.Fatalf("%s panel started %s, not %s", name, status, OverviewUnavailable)
		}
	}
}

func TestPlanPanelsSplitByPhaseNewestFirstWithStableTies(t *testing.T) {
	t.Parallel()
	plans := []PlanSummary{
		{ID: "older-failed", CreatedAt: overviewFixtureTime.Add(-time.Hour), Phase: PlanPhaseFailed},
		{ID: "newest-failed", CreatedAt: overviewFixtureTime, Phase: PlanPhaseFailed},
		{ID: "running", CreatedAt: overviewFixtureTime, Phase: PlanPhaseRunning},
		{ID: "settled", CreatedAt: overviewFixtureTime, Phase: PlanPhaseOther},
	}
	failed, running := PlanPanels(plans)
	if failed.Count != 2 || failed.Items[0].ID != "newest-failed" {
		t.Fatalf("failed panel = %#v", failed)
	}
	if running.Count != 1 || running.Items[0].ID != "running" {
		t.Fatalf("running panel = %#v", running)
	}
	// A phase this view does not name belongs to neither panel, rather than
	// falling into one of them by default.
	if failed.Count+running.Count != 3 {
		t.Fatalf("a plan in an unnamed phase was counted: failed=%d running=%d", failed.Count, running.Count)
	}

	// Equal timestamps are broken by identifier, so the same snapshot always
	// shows the same five.
	tied := []PlanSummary{
		{ID: "b", CreatedAt: overviewFixtureTime, Phase: PlanPhaseFailed},
		{ID: "a", CreatedAt: overviewFixtureTime, Phase: PlanPhaseFailed},
	}
	first, _ := PlanPanels(tied)
	second, _ := PlanPanels([]PlanSummary{tied[1], tied[0]})
	if first.Items[0].ID != "a" || second.Items[0].ID != "a" {
		t.Fatalf("tie-break is not stable: %#v then %#v", first.Items, second.Items)
	}
}

func TestPlanPanelsDoNotReorderTheCallersSlice(t *testing.T) {
	t.Parallel()
	plans := []PlanSummary{
		{ID: "older", CreatedAt: overviewFixtureTime.Add(-time.Hour), Phase: PlanPhaseFailed},
		{ID: "newer", CreatedAt: overviewFixtureTime, Phase: PlanPhaseFailed},
	}
	PlanPanels(plans)
	if plans[0].ID != "older" {
		t.Fatal("the caller's snapshot was sorted in place")
	}
}

func TestInstallationPanelsCountStaleAndTreatUnknownAsNotUnhealthy(t *testing.T) {
	t.Parallel()
	unhealthy, stale := InstallationPanels([]InstallationSummary{
		{Environment: "dev", InstallationID: "broken", Health: HealthUnhealthy},
		{Environment: "dev", InstallationID: "fine", Health: HealthHealthy},
		// Nobody has reported on this one. It is stale, and saying it is also
		// unhealthy would report the same silence twice.
		{Environment: "dev", InstallationID: "silent", Health: HealthUnknown, Stale: true},
	})
	if unhealthy.Count != 1 || unhealthy.Items[0].InstallationID != "broken" {
		t.Fatalf("unhealthy panel = %#v", unhealthy)
	}
	if unhealthy.Items[0].Health != HealthUnhealthy {
		t.Fatalf("the health that made it unhealthy was dropped: %#v", unhealthy.Items[0])
	}
	// The stale panel is a count. Listing the installations would be a second
	// fleet view.
	if stale.Status != OverviewCurrent || stale.Count != 1 || len(stale.Items) != 0 {
		t.Fatalf("stale panel = %#v", stale)
	}
}

func TestApprovalAndPromotionPanelsShowOnlyWhatTheirOwnerFlagged(t *testing.T) {
	t.Parallel()
	approvals := AwaitingApprovalsPanel([]ChangeRequestSummary{
		{ID: "waiting", Title: "Review dev", Environment: "dev", AwaitingApproval: true},
		{ID: "settled", Title: "Already approved", Environment: "dev"},
	})
	if approvals.Count != 1 || approvals.Items[0].ID != "waiting" {
		t.Fatalf("approvals panel = %#v", approvals)
	}
	if approvals.Items[0].Title != "Review dev" || approvals.Items[0].Environment != "dev" {
		t.Fatalf("approval item lost its context: %#v", approvals.Items[0])
	}

	promotions := BlockedPromotionsPanel([]PromotionSummary{
		{ID: "stuck", Product: "app.example:service", Version: "1.0.0",
			FromChannel: "STAGING", ToChannel: "RELEASE", Blocked: true},
		{ID: "passed", Product: "app.example:service", Version: "1.1.0"},
	})
	if promotions.Count != 1 || promotions.Items[0].ID != "stuck" {
		t.Fatalf("promotions panel = %#v", promotions)
	}
	if promotions.Items[0].FromChannel != "STAGING" || promotions.Items[0].ToChannel != "RELEASE" {
		t.Fatalf("promotion item lost the channels that make it readable: %#v", promotions.Items[0])
	}
}
