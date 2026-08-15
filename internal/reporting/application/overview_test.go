package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/reporting/domain"
)

var fixtureTime = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

// stubSource fails exactly the panels it is told to, and records what it was
// asked for. Both halves matter: the point of this layer is that one failure
// costs one panel, and that it asks for the snapshot at the moment it was given.
type stubSource struct {
	failPlans, failInstallations, failRequests, failPromotions bool

	staleAfter time.Duration
	now        time.Time
	calls      int
}

var errUnavailable = errors.New("store unavailable")

func (source *stubSource) VisiblePlans(context.Context) ([]domain.PlanSummary, error) {
	source.calls++
	if source.failPlans {
		return nil, errUnavailable
	}
	return []domain.PlanSummary{
		{ID: "failed", Environment: "dev", CreatedAt: fixtureTime, Phase: domain.PlanPhaseFailed},
		{ID: "running", Environment: "dev", CreatedAt: fixtureTime, Phase: domain.PlanPhaseRunning},
	}, nil
}

func (source *stubSource) VisibleInstallations(
	_ context.Context, staleAfter time.Duration, now time.Time,
) ([]domain.InstallationSummary, error) {
	source.calls++
	source.staleAfter, source.now = staleAfter, now
	if source.failInstallations {
		return nil, errUnavailable
	}
	return []domain.InstallationSummary{
		{Environment: "dev", InstallationID: "broken", Health: domain.HealthUnhealthy},
		{Environment: "dev", InstallationID: "silent", Health: domain.HealthUnknown, Stale: true},
	}, nil
}

func (source *stubSource) VisibleChangeRequests(context.Context) ([]domain.ChangeRequestSummary, error) {
	source.calls++
	if source.failRequests {
		return nil, errUnavailable
	}
	return []domain.ChangeRequestSummary{{ID: "waiting", Environment: "dev", AwaitingApproval: true}}, nil
}

func (source *stubSource) VisiblePromotions(context.Context) ([]domain.PromotionSummary, error) {
	source.calls++
	if source.failPromotions {
		return nil, errUnavailable
	}
	return []domain.PromotionSummary{{ID: "stuck", Product: "app.example:service", Blocked: true}}, nil
}

func TestBuildOverviewFillsEveryPanelFromOneSnapshot(t *testing.T) {
	t.Parallel()
	source := &stubSource{}
	projection := BuildOverview(context.Background(), source, fixtureTime)

	if source.calls != 4 {
		t.Fatalf("asked the source %d times, want one per panel group", source.calls)
	}
	// The fleet must be read at the same moment and the same staleness the
	// projection reports, or the stale count describes a different fleet.
	if !source.now.Equal(fixtureTime) || source.staleAfter != domain.OverviewStaleAfter {
		t.Fatalf("fleet snapshot taken at %s/%s", source.now, source.staleAfter)
	}
	if projection.StaleAfterSeconds != int64(domain.OverviewStaleAfter/time.Second) {
		t.Fatalf("stale threshold = %d", projection.StaleAfterSeconds)
	}
	for name, panel := range map[string]string{
		"approvals":  projection.AwaitingApprovals.Status,
		"failed":     projection.FailedPlans.Status,
		"running":    projection.RunningPlans.Status,
		"unhealthy":  projection.UnhealthyEntities.Status,
		"stale":      projection.StaleReports.Status,
		"promotions": projection.BlockedPromotions.Status,
	} {
		if panel != domain.OverviewCurrent {
			t.Fatalf("%s panel = %s", name, panel)
		}
	}
	if projection.FailedPlans.Count != 1 || projection.RunningPlans.Count != 1 ||
		projection.UnhealthyEntities.Count != 1 || projection.StaleReports.Count != 1 ||
		projection.AwaitingApprovals.Count != 1 || projection.BlockedPromotions.Count != 1 {
		t.Fatalf("projection = %#v", projection)
	}
}

// One store failing must cost exactly its own panel. A reader is better served
// by five working panels and one honest gap than by a view that hides the gap
// or refuses to render.
func TestOneUnavailableStoreCostsExactlyItsOwnPanels(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		source    *stubSource
		wantEmpty []string
	}{
		"plans unavailable": {
			source: &stubSource{failPlans: true}, wantEmpty: []string{"failed", "running"},
		},
		"fleet unavailable": {
			source: &stubSource{failInstallations: true}, wantEmpty: []string{"unhealthy", "stale"},
		},
		"change requests unavailable": {
			source: &stubSource{failRequests: true}, wantEmpty: []string{"approvals"},
		},
		"promotions unavailable": {
			source: &stubSource{failPromotions: true}, wantEmpty: []string{"promotions"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			projection := BuildOverview(context.Background(), testCase.source, fixtureTime)
			panels := map[string]string{
				"approvals":  projection.AwaitingApprovals.Status,
				"failed":     projection.FailedPlans.Status,
				"running":    projection.RunningPlans.Status,
				"unhealthy":  projection.UnhealthyEntities.Status,
				"stale":      projection.StaleReports.Status,
				"promotions": projection.BlockedPromotions.Status,
			}
			unavailable := map[string]struct{}{}
			for _, panel := range testCase.wantEmpty {
				unavailable[panel] = struct{}{}
			}
			for panel, status := range panels {
				_, wantUnavailable := unavailable[panel]
				if wantUnavailable && status != domain.OverviewUnavailable {
					t.Fatalf("%s should be %s, was %s", panel, domain.OverviewUnavailable, status)
				}
				if !wantUnavailable && status != domain.OverviewCurrent {
					t.Fatalf("%s was collateral damage: %s", panel, status)
				}
			}
		})
	}
}

func TestBuildOverviewStampsAMomentWhenGivenNone(t *testing.T) {
	t.Parallel()
	before := time.Now().UTC()
	projection := BuildOverview(context.Background(), &stubSource{}, time.Time{})
	if projection.GeneratedAt.IsZero() || projection.GeneratedAt.Before(before.Add(-time.Minute)) {
		t.Fatalf("generatedAt = %s", projection.GeneratedAt)
	}
}
