package controlplane

import (
	"context"
	"errors"
	"sort"

	apolloapp "github.com/jonathanagustin/conduit/internal/apollo/application"
	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// apolloCatalogPort is an anti-corruption adapter. It is the only place the
// Apollo application can reach the legacy aggregate repository, and it maps
// control-plane error identity into neutral results.
type apolloCatalogPort struct{ repository Repository }

func (port apolloCatalogPort) ListPublications(ctx context.Context, product string) ([]catalog.Publication, error) {
	return port.repository.ListProductPublications(ctx, product)
}

func (port apolloCatalogPort) CreatePublication(ctx context.Context, publication catalog.Publication) (catalog.Publication, bool, error) {
	stored, err := port.repository.CreateProductPublication(ctx, publication)
	if errors.Is(err, ErrConflict) {
		return catalog.Publication{}, true, nil
	}
	return stored, false, err
}

func (port apolloCatalogPort) CoordinateRetired(ctx context.Context, product, version string) (bool, error) {
	_, err := port.repository.GetCatalogRetirement(ctx, product, version)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// apolloReleaseLifecyclePort answers what has been withdrawn and what has been
// promoted from the Plans the repository holds, which is the only copy both Hub
// replicas can see. Apollo asked its own memory before this existed, and its
// memory was written only by the request that accepted the desired state: the
// replica that did not serve that POST reported a recalled Release as not
// recalled, and so did every replica after a restart.
//
// The cost is one repository read per Apollo catalog read. On this Hub that is
// the same unit of work every other read route already pays -- the repository
// materializes its whole state per operation either way -- and it buys an
// answer that does not depend on which process the client reached.
type apolloReleaseLifecyclePort struct{ repository Repository }

func (port apolloReleaseLifecyclePort) ReleaseLifecycle(ctx context.Context) (apolloapp.ReleaseLifecycle, error) {
	plans, err := port.repository.ListPlans(ctx, "")
	if err != nil {
		return apolloapp.ReleaseLifecycle{}, err
	}
	states := latestDeclaredStates(plans)
	// Environments in sorted order so that two Environments recalling the same
	// Release do not make the reported reason depend on map iteration, which
	// would change the answer between two identical requests.
	identifiers := make([]string, 0, len(states))
	for environment := range states {
		identifiers = append(identifiers, environment)
	}
	sort.Strings(identifiers)
	var lifecycle apolloapp.ReleaseLifecycle
	for _, environment := range identifiers {
		state := states[environment]
		for _, recall := range state.Recalls {
			lifecycle.Recalls = append(lifecycle.Recalls, environments.ProjectedRecall{
				Product: recall.Product, StartingVersion: recall.StartingVersion,
				EndingVersionExclusive: recall.EndingVersionExclusive,
				ExcludedVersions:       append([]string(nil), recall.ExcludedVersions...),
				SingleVersion:          recall.SingleVersion, Reason: recall.Reason, Reverted: recall.Reverted,
			})
		}
		for _, promotion := range state.Promotions {
			lifecycle.Promotions = append(lifecycle.Promotions, environments.ProjectedPromotion{
				Product: promotion.Product, Version: promotion.Version,
				FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel,
				Passed: promotion.Status == PromotionPassed,
			})
		}
	}
	return lifecycle, nil
}

// latestDeclaredStates is latestDesiredStates with Entity commands left out. A
// command stores a desired state whose Recalls have been stripped -- commands
// deliberately bypass recall eligibility -- so the newest plan in an
// Environment can be one that says nothing about withdrawals. Reading it as the
// Environment's declaration would report a recalled Release as not recalled the
// moment anyone issued a command there.
func latestDeclaredStates(plans []PlanRecord) map[string]DesiredState {
	declared := make([]PlanRecord, 0, len(plans))
	for _, plan := range plans {
		if plan.Command != nil {
			continue
		}
		declared = append(declared, plan)
	}
	return latestDesiredStates(declared)
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
