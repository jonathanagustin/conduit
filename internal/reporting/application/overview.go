// Package application builds Reporting's views from a source that has already
// decided what the reader may see.
//
// It contains no rule and no lookup — the rules are in the domain and the
// lookup is behind a port. What it contributes is the one thing neither can:
// deciding that a store which did not answer costs exactly its own panel and
// nothing else.
package application

import (
	"context"
	"time"

	"github.com/jonathanagustin/conduit/internal/reporting/domain"
	"github.com/jonathanagustin/conduit/internal/reporting/ports"
)

// BuildOverview assembles the operator's overview from one authorized snapshot.
//
// Every panel starts unavailable and is only replaced once its source has
// answered, so a failure anywhere leaves an honest gap instead of an empty
// panel that reads as good news.
func BuildOverview(ctx context.Context, source ports.OverviewSource, now time.Time) domain.OverviewProjection {
	if now.IsZero() {
		now = time.Now()
	}
	projection := domain.NewOverviewProjection(now)

	if plans, err := source.VisiblePlans(ctx); err == nil {
		projection.FailedPlans, projection.RunningPlans = domain.PlanPanels(plans)
	}
	if installations, err := source.VisibleInstallations(ctx, domain.OverviewStaleAfter, now); err == nil {
		projection.UnhealthyEntities, projection.StaleReports = domain.InstallationPanels(installations)
	}
	if requests, err := source.VisibleChangeRequests(ctx); err == nil {
		projection.AwaitingApprovals = domain.AwaitingApprovalsPanel(requests)
	}
	if promotions, err := source.VisiblePromotions(ctx); err == nil {
		projection.BlockedPromotions = domain.BlockedPromotionsPanel(promotions)
	}
	return projection
}
