package controlplane

import (
	"context"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

// environmentProjectionPlanReader grants durable Plan reads, not mutation or
// approval authority. Both projections deliberately retain their own selection
// semantics for command Plans.
type environmentProjectionPlanReader interface {
	ListPlans(context.Context, string) ([]PlanRecord, error)
}

// environmentProjectionSource is the legacy adapter for native snapshot reads.
// It retains the original latest-Plan selectors and selected-state JSON cloning.
type environmentProjectionSource struct {
	repository environmentProjectionPlanReader
}

func (source environmentProjectionSource) InventoryByEnvironment(ctx context.Context) (map[string]environments.CompatibilityProjection, error) {
	plans, err := source.repository.ListPlans(ctx, "")
	if err != nil {
		return nil, err
	}
	states := latestDesiredStates(plans)
	inventory := make(map[string]environments.CompatibilityProjection, len(states))
	for environment, state := range states {
		projection := environments.CompatibilityProjection{ID: state.Environment.ID}
		for _, entity := range state.Entities {
			projection.Entities = append(projection.Entities, environments.ProjectedEntity{Name: entity.Name, Product: entity.Product, Version: entity.Version, ReleaseChannel: entity.ReleaseChannel})
		}
		for _, namespace := range state.Environment.Namespaces {
			projection.Namespaces = append(projection.Namespaces, environments.ProjectedNamespace{RID: namespace.RID, Name: namespace.Name})
		}
		inventory[environment] = projection
	}
	return inventory, nil
}

func (source environmentProjectionSource) LifecycleByEnvironment(ctx context.Context) (map[string]environments.ReleaseLifecycle, error) {
	plans, err := source.repository.ListPlans(ctx, "")
	if err != nil {
		return nil, err
	}
	states := latestDeclaredStates(plans)
	snapshots := make(map[string]environments.ReleaseLifecycle, len(states))
	for environment, state := range states {
		var lifecycle environments.ReleaseLifecycle
		for _, recall := range state.Recalls {
			lifecycle.Recalls = append(lifecycle.Recalls, environments.ProjectedRecall{Product: recall.Product, StartingVersion: recall.StartingVersion, EndingVersionExclusive: recall.EndingVersionExclusive, ExcludedVersions: recall.ExcludedVersions, SingleVersion: recall.SingleVersion, Reason: recall.Reason, Reverted: recall.Reverted})
		}
		for _, promotion := range state.Promotions {
			lifecycle.Promotions = append(lifecycle.Promotions, environments.ProjectedPromotion{Product: promotion.Product, Version: promotion.Version, FromChannel: promotion.FromChannel, ToChannel: promotion.ToChannel, Passed: promotion.Status == PromotionPassed})
		}
		snapshots[environment] = lifecycle
	}
	return snapshots, nil
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
