package controlplane

import (
	"context"

	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// apolloReportingPort narrows lifecycle state to reviewed Plan projections and
// normalized Environment reports. The Apollo package never imports PlanRecord,
// Repository, or any control-plane implementation type.
type apolloReportingPort struct{ repository Repository }

func (port apolloReportingPort) LatestPlan(ctx context.Context, environment string) (environments.PlanProjection, error) {
	plans, err := port.repository.ListPlans(ctx, environment)
	if err != nil || len(plans) == 0 {
		return environments.PlanProjection{}, err
	}
	latest := latestPlanRecords(plans)[environment]
	projection := environments.PlanProjection{ID: latest.ID, Steps: make([]environments.PlanStepProjection, 0, len(latest.Plan.Steps))}
	for _, step := range latest.Plan.Steps {
		projection.Steps = append(projection.Steps, environments.PlanStepProjection{
			InstallationID: step.InstallationID, Entity: step.Entity, Product: step.Product,
			Version: step.Version, Namespace: step.Namespace,
		})
	}
	return projection, nil
}

func (port apolloReportingPort) GetReport(ctx context.Context, environment string) (environments.Report, error) {
	return port.repository.GetEnvironmentReport(ctx, environment)
}

func (port apolloReportingPort) PutReport(ctx context.Context, report environments.Report) (environments.Report, error) {
	return port.repository.PutEnvironmentReport(ctx, report)
}
