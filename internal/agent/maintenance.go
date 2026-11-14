package agent

import (
	"context"
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// Only the Worker's already-authorized imperative command path sets this key.
// Temporary maintenance admission never turns an ordinary Plan into a command.
type imperativeMaintenanceKey struct{}

func maintenanceAllowsStep(ctx context.Context, step controlplane.PlanStep, now time.Time) error {
	if ctx.Value(imperativeMaintenanceKey{}) == true {
		return nil
	}
	if !controlplane.PlanStepWithinMaintenanceWindow(step, now) {
		return errors.New("maintenance window closed before workload mutation")
	}
	return nil
}
func maintenanceAllowsPlan(ctx context.Context, plan controlplane.Plan, now time.Time) error {
	if ctx.Value(imperativeMaintenanceKey{}) == true {
		return nil
	}
	if !controlplane.PlanWithinMaintenanceWindow(plan, now) {
		return errors.New("maintenance window closed before material mutation")
	}
	return nil
}
func (executor HelmExecutor) checkMaintenance(ctx context.Context, step controlplane.PlanStep) error {
	now := time.Now
	if executor.Now != nil {
		now = executor.Now
	}
	return maintenanceAllowsStep(ctx, step, now().UTC())
}
