// Package ports declares what Delivery needs from other contexts in order to
// describe a rollout, and nothing more.
//
// These are narrow query ports on purpose. Delivery does not want an
// Environment or a Product Definition — it wants lanes, steps, ordering and
// windows. Naming exactly that keeps the contexts exchanging immutable
// snapshots rather than each other's aggregates, and it means the adapters can
// be replaced without the domain noticing.
package ports

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
)

// LaneSource answers where rollout work may run. Implemented over the
// Environments context.
type LaneSource interface {
	RolloutLanes(ctx context.Context) ([]domain.Lane, error)
}

// StepSource answers what work a rollout consists of. Implemented over
// Delivery's own plans and the entities they install.
type StepSource interface {
	RolloutSteps(ctx context.Context) ([]domain.Step, error)
}

// DependencySource answers what must precede what. Implemented over the
// Catalog context, whose product dependencies are the ordering that matters.
type DependencySource interface {
	RolloutDependencies(ctx context.Context) ([]domain.Dependency, error)
}

// WindowSource answers when a lane refuses work. Maintenance is Delivery's own
// concern, so this is usually satisfied locally.
type WindowSource interface {
	MaintenanceWindows(ctx context.Context) ([]domain.Window, error)
}
