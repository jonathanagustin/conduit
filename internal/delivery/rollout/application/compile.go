// Package application assembles a rollout from the ports and refuses one that
// cannot be scheduled.
//
// This is the use case, so it orchestrates and validates but holds no rules of
// its own: what makes a rollout impossible lives in the domain, where it can be
// tested without a fleet.
package application

import (
	"context"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/rollout/ports"
)

// Compiler turns the current fleet into a rollout problem.
type Compiler struct {
	Lanes        ports.LaneSource
	Steps        ports.StepSource
	Dependencies ports.DependencySource
	Windows      ports.WindowSource
}

// Compile gathers the pieces and returns a validated rollout.
//
// A missing source is a programming error rather than an empty result: a
// rollout compiled without knowing its lanes would validate as "no lanes" and
// send someone looking at the fleet instead of at the wiring.
func (compiler Compiler) Compile(ctx context.Context, origin string, horizonSeconds int64) (domain.Rollout, error) {
	if compiler.Lanes == nil || compiler.Steps == nil {
		return domain.Rollout{}, fmt.Errorf("rollout compiler is missing a lane or step source")
	}

	lanes, err := compiler.Lanes.RolloutLanes(ctx)
	if err != nil {
		return domain.Rollout{}, fmt.Errorf("read rollout lanes: %w", err)
	}

	steps, err := compiler.Steps.RolloutSteps(ctx)
	if err != nil {
		return domain.Rollout{}, fmt.Errorf("read rollout steps: %w", err)
	}

	// Ordering and maintenance are optional inputs. A rollout with neither is
	// unusual but not wrong — a single independent step in a fleet with no
	// freeze is exactly that.
	var dependencies []domain.Dependency
	if compiler.Dependencies != nil {
		if dependencies, err = compiler.Dependencies.RolloutDependencies(ctx); err != nil {
			return domain.Rollout{}, fmt.Errorf("read rollout dependencies: %w", err)
		}
	}

	var windows []domain.Window
	if compiler.Windows != nil {
		if windows, err = compiler.Windows.MaintenanceWindows(ctx); err != nil {
			return domain.Rollout{}, fmt.Errorf("read maintenance windows: %w", err)
		}
	}

	rollout := domain.Rollout{
		Origin:         origin,
		HorizonSeconds: horizonSeconds,
		Steps:          steps,
		Lanes:          lanes,
		Dependencies:   dependencies,
		Windows:        windows,
	}

	if err := rollout.Validate(); err != nil {
		return domain.Rollout{}, fmt.Errorf("rollout cannot be scheduled: %w", err)
	}
	return rollout, nil
}
