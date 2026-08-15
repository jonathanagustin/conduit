package controlplane

import (
	"context"
	"errors"
	"time"

	reportingapp "github.com/jonathanagustin/conduit/internal/reporting/application"
	reporting "github.com/jonathanagustin/conduit/internal/reporting/domain"
)

// The overview is Reporting's view. What is left here is the adapter that
// decides what this reader may see, and the aliases that keep the HTTP layer
// spelling the result as it did.
//
// The split is not cosmetic. Which installations are unhealthy, how many items
// a panel shows, and what an unanswered store looks like are Reporting's rules
// and are now testable without a repository. Who may see a plan is Governance's
// rule and stays on this side, where the grants are — and it runs before the
// projection counts anything, so a panel can never report a number the reader
// is not allowed to account for.
type (
	OverviewPanel[T any]  = reporting.Panel[T]
	OverviewChangeRequest = reporting.OverviewChangeRequest
	OverviewPlan          = reporting.OverviewPlan
	OverviewInstallation  = reporting.OverviewInstallation
	OverviewPromotion     = reporting.OverviewPromotion
	OverviewProjection    = reporting.OverviewProjection
)

const (
	maximumOverviewItems = reporting.MaximumOverviewItems
	overviewStaleAfter   = reporting.OverviewStaleAfter

	OverviewCurrent     = reporting.OverviewCurrent
	OverviewUnavailable = reporting.OverviewUnavailable
)

// errOverviewPanelUnavailable is what the adapter returns when a reader may not
// see a panel at all. Reporting draws no distinction between a store that
// failed and authority the reader does not hold: both mean this panel cannot be
// shown, and neither means there is nothing in it.
var errOverviewPanelUnavailable = errors.New("overview panel unavailable to this reader")

// overviewSource answers Reporting's port over the repository, filtering every
// record by what this principal may read.
type overviewSource struct {
	repository    Repository
	governance    *GovernanceService
	principal     Principal
	configuration HandlerOptions

	administrator bool
	effective     Principal

	// The plans are read once, up front, because they are wanted twice — for
	// the plan panels and as the fleet's desired state. A second read could
	// return a different fleet, which would put two disagreeing numbers in one
	// view.
	plans    []PlanRecord
	plansErr error
}

func (source *overviewSource) VisiblePlans(ctx context.Context) ([]reporting.PlanSummary, error) {
	if source.plansErr != nil {
		return nil, source.plansErr
	}
	visible := make([]reporting.PlanSummary, 0, len(source.plans))
	for _, plan := range source.plans {
		if !canReadPlan(ctx, source.repository, source.governance, source.principal, source.configuration, plan) {
			continue
		}
		visible = append(visible, reporting.PlanSummary{
			ID: plan.ID, Environment: plan.Plan.Environment,
			CreatedAt: plan.CreatedAt, Phase: overviewPlanPhase(plan.Status),
		})
	}
	return visible, nil
}

// overviewPlanPhase is the one place a plan status becomes something Reporting
// understands. Delivery owns what these statuses mean; translating here is what
// keeps Reporting from carrying a second copy of that vocabulary.
func overviewPlanPhase(status PlanStatus) reporting.PlanPhase {
	switch status {
	case PlanFailed:
		return reporting.PlanPhaseFailed
	case PlanRunning, PlanLeased:
		return reporting.PlanPhaseRunning
	default:
		return reporting.PlanPhaseOther
	}
}

func (source *overviewSource) VisibleInstallations(
	ctx context.Context, staleAfter time.Duration, now time.Time,
) ([]reporting.InstallationSummary, error) {
	if source.plansErr != nil {
		return nil, source.plansErr
	}
	reports, err := source.repository.ListEnvironmentReports(ctx)
	if err != nil {
		return nil, err
	}
	desired, observed := fleetInputs(source.plans, reports)
	fleet := reporting.BuildFleet(desired, observed, FleetQuery{MaxAge: staleAfter}, now)
	visible := make([]reporting.InstallationSummary, 0, len(fleet.Installations))
	for _, item := range fleet.Installations {
		if !source.administrator &&
			!source.governance.Authorize(ctx, source.effective, environmentRID(item.Environment), OperationReadResource) {
			continue
		}
		visible = append(visible, reporting.InstallationSummary{
			Environment: item.Environment, InstallationID: item.InstallationID,
			Health: item.Health, Stale: item.Stale,
		})
	}
	return visible, nil
}

func (source *overviewSource) VisibleChangeRequests(ctx context.Context) ([]reporting.ChangeRequestSummary, error) {
	requests, err := source.repository.ListChangeRequests(ctx, "")
	if err != nil {
		return nil, err
	}
	visible := make([]reporting.ChangeRequestSummary, 0, len(requests))
	for _, request := range requests {
		if !canReadChangeRequest(ctx, source.governance, source.principal, source.configuration, request) {
			continue
		}
		visible = append(visible, reporting.ChangeRequestSummary{
			ID: request.ID, Title: request.Title, Environment: request.Environment,
			AwaitingApproval: request.Status == ChangePendingApprovals,
		})
	}
	return visible, nil
}

func (source *overviewSource) VisiblePromotions(ctx context.Context) ([]reporting.PromotionSummary, error) {
	// Promotions are listed platform-wide rather than per environment, so there
	// is no per-record authorization to fall back on: a reader without the
	// platform-wide grant is refused the panel outright.
	if !source.administrator &&
		!source.governance.Authorize(ctx, source.effective, "platform/root", OperationReadResource) {
		return nil, errOverviewPanelUnavailable
	}
	promotions, err := source.repository.ListPromotions(ctx, "")
	if err != nil {
		return nil, err
	}
	visible := make([]reporting.PromotionSummary, 0, len(promotions))
	for _, promotion := range promotions {
		visible = append(visible, reporting.PromotionSummary{
			ID: promotion.ID, Product: promotion.Product, Version: promotion.Version,
			FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel,
			Blocked: promotion.Status != PromotionPassed,
		})
	}
	return visible, nil
}

// BuildOverviewProjection reads what this operator may see and hands it to
// Reporting.
func BuildOverviewProjection(
	ctx context.Context,
	repository Repository,
	governance *GovernanceService,
	principal Principal,
	configuration HandlerOptions,
	now time.Time,
) OverviewProjection {
	source := &overviewSource{
		repository: repository, governance: governance,
		principal: principal, configuration: configuration,
		administrator: isGovernanceAdministrator(ctx, governance, principal, configuration),
		effective:     effectivePrincipal(principal),
	}
	source.plans, source.plansErr = repository.ListPlans(ctx, "")
	return reportingapp.BuildOverview(ctx, source, now)
}
