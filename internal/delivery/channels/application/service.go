// Package application reads channel subscriptions from the plans and reports
// the hub already stores.
package application

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/channels/domain"
)

// Plans is the existing plan and report store.
type Plans interface {
	ListPlans(context.Context, string) ([]controlplane.PlanRecord, error)
	ListEnvironmentReports(context.Context) ([]controlplane.EnvironmentReport, error)
}

// Service builds the subscription view.
type Service struct {
	Plans Plans
}

// Board returns the view. allow drops environments the caller cannot read.
// A nil allow keeps every environment.
func (service Service) Board(ctx context.Context, allow func(environment string) bool) (domain.View, error) {
	if allow == nil {
		allow = func(string) bool { return true }
	}
	plans, err := service.Plans.ListPlans(ctx, "")
	if err != nil {
		return domain.View{}, err
	}
	reports, err := service.Plans.ListEnvironmentReports(ctx)
	if err != nil {
		return domain.View{}, err
	}
	latest := map[string]controlplane.PlanRecord{}
	for _, plan := range plans {
		current, found := latest[plan.Plan.Environment]
		if plan.Plan.Environment == "" {
			continue
		}
		if !found || current.CreatedAt.Before(plan.CreatedAt) || (current.CreatedAt.Equal(plan.CreatedAt) && current.ID < plan.ID) {
			latest[plan.Plan.Environment] = plan
		}
	}
	environments := make([]environmentRef, 0, len(latest))
	for id, plan := range latest {
		if !allow(id) {
			continue
		}
		environment, ok := environmentFromPlan(plan)
		if !ok {
			continue
		}
		environments = append(environments, environment)
	}
	observed := map[string]string{}
	for _, report := range reports {
		if !allow(report.Environment) {
			continue
		}
		for _, entity := range report.Entities {
			if entity.Product == "" || entity.Version == "" {
				continue
			}
			observed[report.Environment+"\x00"+entity.Product] = entity.Version
		}
	}
	return boardFrom(environments, observed), nil
}
