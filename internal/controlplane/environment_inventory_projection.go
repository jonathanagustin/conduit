package controlplane

import (
	"context"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
	"sort"
)

// environmentProjectionPlanReader grants durable Plan reads, not mutation or
// approval authority. Both projections deliberately retain their own selection
// semantics for command Plans.
type environmentProjectionPlanReader interface {
	ListPlans(context.Context, string) ([]PlanRecord, error)
}

// environmentInventoryProjectionPort answers which Environments this Hub governs,
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
type environmentInventoryProjectionPort struct {
	repository environmentProjectionPlanReader
}

func (port environmentInventoryProjectionPort) EnvironmentInventory(ctx context.Context) (
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
