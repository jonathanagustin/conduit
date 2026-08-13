package controlplane

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/reporting"
	"sort"
	"time"
)

const maximumOverviewItems = 5
const overviewStaleAfter = 5 * time.Minute

const (
	OverviewCurrent     = "CURRENT"
	OverviewUnavailable = "UNAVAILABLE"
)

type OverviewPanel[T any] struct {
	Status string `json:"status"`
	Count  int    `json:"count"`
	Items  []T    `json:"items"`
}

type OverviewChangeRequest struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Environment string `json:"environment"`
}

type OverviewPlan struct {
	ID          string `json:"id"`
	Environment string `json:"environment"`
}

type OverviewInstallation struct {
	Environment    string `json:"environment"`
	InstallationID string `json:"installationId"`
	Health         string `json:"health"`
}

type OverviewPromotion struct {
	ID          string `json:"id"`
	Product     string `json:"product"`
	Version     string `json:"version"`
	FromChannel string `json:"fromChannel"`
	ToChannel   string `json:"toChannel"`
}

type OverviewProjection struct {
	GeneratedAt       time.Time                            `json:"generatedAt"`
	StaleAfterSeconds int64                                `json:"staleAfterSeconds"`
	AwaitingApprovals OverviewPanel[OverviewChangeRequest] `json:"awaitingApprovals"`
	FailedPlans       OverviewPanel[OverviewPlan]          `json:"failedPlans"`
	RunningPlans      OverviewPanel[OverviewPlan]          `json:"runningPlans"`
	UnhealthyEntities OverviewPanel[OverviewInstallation]  `json:"unhealthyEntities"`
	StaleReports      OverviewPanel[struct{}]              `json:"staleReports"`
	BlockedPromotions OverviewPanel[OverviewPromotion]     `json:"blockedPromotions"`
}

func BuildOverviewProjection(
	ctx context.Context,
	repository Repository,
	governance *GovernanceService,
	principal Principal,
	configuration HandlerOptions,
	now time.Time,
) OverviewProjection {
	if now.IsZero() {
		now = time.Now()
	}
	projection := OverviewProjection{
		GeneratedAt:       now.UTC(),
		StaleAfterSeconds: int64(overviewStaleAfter / time.Second),
		AwaitingApprovals: unavailableOverviewPanel[OverviewChangeRequest](),
		FailedPlans:       unavailableOverviewPanel[OverviewPlan](),
		RunningPlans:      unavailableOverviewPanel[OverviewPlan](),
		UnhealthyEntities: unavailableOverviewPanel[OverviewInstallation](),
		StaleReports:      unavailableOverviewPanel[struct{}](),
		BlockedPromotions: unavailableOverviewPanel[OverviewPromotion](),
	}
	administrator := isGovernanceAdministrator(ctx, governance, principal, configuration)
	effective := effectivePrincipal(principal)

	plans, plansErr := repository.ListPlans(ctx, "")
	if plansErr == nil {
		visible := make([]PlanRecord, 0, len(plans))
		for _, plan := range plans {
			if canReadPlan(ctx, repository, governance, principal, configuration, plan) {
				visible = append(visible, plan)
			}
		}
		sort.Slice(visible, func(i, j int) bool {
			if !visible[i].CreatedAt.Equal(visible[j].CreatedAt) {
				return visible[i].CreatedAt.After(visible[j].CreatedAt)
			}
			return visible[i].ID < visible[j].ID
		})
		failed := make([]OverviewPlan, 0)
		running := make([]OverviewPlan, 0)
		for _, plan := range visible {
			item := OverviewPlan{ID: plan.ID, Environment: plan.Plan.Environment}
			switch plan.Status {
			case PlanFailed:
				failed = append(failed, item)
			case PlanRunning, PlanLeased:
				running = append(running, item)
			}
		}
		projection.FailedPlans = currentOverviewPanel(failed)
		projection.RunningPlans = currentOverviewPanel(running)

		if reports, err := repository.ListEnvironmentReports(ctx); err == nil {
			desired, observed := fleetInputs(plans, reports)
			fleet := reporting.BuildFleet(desired, observed, FleetQuery{MaxAge: overviewStaleAfter}, now)
			unhealthy := make([]OverviewInstallation, 0)
			stale := 0
			for _, item := range fleet.Installations {
				if !administrator && !governance.Authorize(ctx, effective, environmentRID(item.Environment), OperationReadResource) {
					continue
				}
				if item.Health != HealthHealthy && item.Health != HealthUnknown {
					unhealthy = append(unhealthy, OverviewInstallation{
						Environment: item.Environment, InstallationID: item.InstallationID, Health: item.Health,
					})
				}
				if item.Stale {
					stale++
				}
			}
			projection.UnhealthyEntities = currentOverviewPanel(unhealthy)
			projection.StaleReports = OverviewPanel[struct{}]{Status: OverviewCurrent, Count: stale, Items: []struct{}{}}
		}
	}

	if requests, err := repository.ListChangeRequests(ctx, ""); err == nil {
		awaiting := make([]OverviewChangeRequest, 0)
		for _, request := range requests {
			if request.Status == ChangePendingApprovals &&
				canReadChangeRequest(ctx, governance, principal, configuration, request) {
				awaiting = append(awaiting, OverviewChangeRequest{
					ID: request.ID, Title: request.Title, Environment: request.Environment,
				})
			}
		}
		projection.AwaitingApprovals = currentOverviewPanel(awaiting)
	}

	if administrator || governance.Authorize(ctx, effective, "platform/root", OperationReadResource) {
		if promotions, err := repository.ListPromotions(ctx, ""); err == nil {
			blocked := make([]OverviewPromotion, 0)
			for _, promotion := range promotions {
				if promotion.Status != PromotionPassed {
					blocked = append(blocked, OverviewPromotion{
						ID: promotion.ID, Product: promotion.Product, Version: promotion.Version,
						FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel,
					})
				}
			}
			projection.BlockedPromotions = currentOverviewPanel(blocked)
		}
	}
	return projection
}

func currentOverviewPanel[T any](items []T) OverviewPanel[T] {
	count := len(items)
	if len(items) > maximumOverviewItems {
		items = items[:maximumOverviewItems]
	}
	if items == nil {
		items = []T{}
	}
	return OverviewPanel[T]{Status: OverviewCurrent, Count: count, Items: items}
}

func unavailableOverviewPanel[T any]() OverviewPanel[T] {
	return OverviewPanel[T]{Status: OverviewUnavailable, Items: []T{}}
}
