package controlplane

import (
	"context"

	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// environmentReportingProjectionPort narrows lifecycle state to reviewed Plan projections and
// normalized Environment reports. The Apollo package never imports PlanRecord,
// Repository, or any control-plane implementation type.
type environmentReportReadRepository interface {
	ListPlans(context.Context, string) ([]PlanRecord, error)
	GetEnvironmentReport(context.Context, string) (environments.Report, error)
}

type environmentReportingProjectionRepository interface {
	environmentReportReadRepository
	PutEnvironmentReport(context.Context, environments.Report) (environments.Report, error)
}

type environmentReportingProjectionPort struct {
	repository environmentReportingProjectionRepository
}

func (port environmentReportingProjectionPort) LatestPlan(ctx context.Context, environment string) (environments.PlanProjection, error) {
	return latestEnvironmentPlanProjection(ctx, port.repository, environment)
}

func latestEnvironmentPlanProjection(ctx context.Context, repository environmentReportReadRepository, environment string) (environments.PlanProjection, error) {
	plans, err := repository.ListPlans(ctx, environment)
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

func (port environmentReportingProjectionPort) GetReport(ctx context.Context, environment string) (environments.Report, error) {
	return port.repository.GetEnvironmentReport(ctx, environment)
}

func (port environmentReportingProjectionPort) PutReport(ctx context.Context, report environments.Report) (environments.Report, error) {
	return port.repository.PutEnvironmentReport(ctx, report)
}
