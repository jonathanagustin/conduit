package controlplane

import (
	"context"
	environmentapp "github.com/jonathanagustin/conduit/internal/environments/application"
	environments "github.com/jonathanagustin/conduit/internal/environments/domain"
)

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

func (port environmentInventoryProjectionPort) EnvironmentInventory(ctx context.Context) ([]environments.CompatibilityProjection, error) {
	return (environmentapp.ProjectionReader{Source: environmentProjectionSource{repository: port.repository}}).EnvironmentInventory(ctx)
}
