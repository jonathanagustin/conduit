package controlplane

import (
	"context"
	"sort"

	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// apolloEnvironmentInventoryPort answers which Environments this Hub governs,
// what is installed in them and which Namespaces they target, from the Plans
// the repository holds -- the only copy both Hub replicas can see.
//
// Apollo asked its own memory before this existed, and that memory was written
// only by the request that accepted the desired state. The replica that did not
// serve that POST listed NO Environments, NO Installations and NO Namespaces,
// and so did every replica after a restart. Empty is the dangerous answer here:
// a client reads it as "this Hub governs nothing" rather than as "this process
// was not told", which is the unknown-reads-as-none conflation the provenance
// model exists to prevent.
//
// Unlike releaseLifecycleProjectionPort this reads latestDesiredStates and NOT
// latestDeclaredStates -- command Plans included. A command stores a desired
// state with its Recalls stripped, which is why the lifecycle skips it, but it
// carries the Environment's full Entity and Namespace set and the old write
// path recorded exactly that. Excluding commands here would empty an
// Environment's Installations the moment an operator restarted a resource.
type apolloEnvironmentInventoryPort struct{ repository Repository }

func (port apolloEnvironmentInventoryPort) EnvironmentInventory(ctx context.Context) (
	[]environments.CompatibilityProjection, error,
) {
	plans, err := port.repository.ListPlans(ctx, "")
	if err != nil {
		return nil, err
	}
	states := latestDesiredStates(plans)
	// Sorted, because the caller sorts by Apollo RID and two Environments can
	// contribute the same Namespace id: map iteration would otherwise let two
	// identical requests disagree about which Environment's copy was served.
	identifiers := make([]string, 0, len(states))
	for environment := range states {
		identifiers = append(identifiers, environment)
	}
	sort.Strings(identifiers)
	inventory := make([]environments.CompatibilityProjection, 0, len(identifiers))
	for _, environment := range identifiers {
		state := states[environment]
		projection := environments.CompatibilityProjection{ID: state.Environment.ID}
		for _, entity := range state.Entities {
			projection.Entities = append(projection.Entities, environments.ProjectedEntity{
				Name: entity.Name, Product: entity.Product, Version: entity.Version,
				ReleaseChannel: entity.ReleaseChannel,
			})
		}
		for _, namespace := range state.Environment.Namespaces {
			projection.Namespaces = append(projection.Namespaces, environments.ProjectedNamespace{
				RID: namespace.RID, Name: namespace.Name,
			})
		}
		inventory = append(inventory, projection)
	}
	return inventory, nil
}

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
