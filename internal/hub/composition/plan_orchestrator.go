package composition

import (
	"context"
	"fmt"
	"strings"

	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/planorchestration"
)

// planSequencer reads PLAN_ORCHESTRATOR. Empty and native leave Plan
// lifecycle on its current path. temporal starts one worker and returns the
// callback the approve routes use. The callback does not approve, lease, or
// write reported state.
func planSequencer(
	repository controlplane.Repository, options Options, getenv func(string) string,
) (func(context.Context, string) error, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	if options.PlanOrchestrator != nil {
		return sequencerFor(options.PlanOrchestrator), nil
	}
	selected, err := controlplane.PlanOrchestratorFromEnv(getenv)
	if err != nil {
		return nil, err
	}
	if selected.Driver() != controlplane.PlanOrchestratorTemporal {
		return nil, nil
	}
	address := strings.TrimSpace(getenv("PLAN_TEMPORAL_ADDRESS"))
	if address == "" {
		address = strings.TrimSpace(getenv("PACKAGING_TEMPORAL_ADDRESS"))
	}
	if address == "" {
		return nil, fmt.Errorf("PLAN_ORCHESTRATOR=temporal requires PLAN_TEMPORAL_ADDRESS or PACKAGING_TEMPORAL_ADDRESS")
	}
	temporalClient, err := client.Dial(client.Options{HostPort: address, Namespace: "default"})
	if err != nil {
		return nil, fmt.Errorf("dial plan orchestrator: %w", err)
	}
	kernel := planorchestration.LifecycleKernel{Lifecycle: controlplane.NewLifecycle(repository), Repository: repository}
	work := planorchestration.RegisterWorker(temporalClient, planorchestration.TaskQueue, kernel)
	if err := work.Start(); err != nil {
		temporalClient.Close()
		return nil, fmt.Errorf("start plan orchestrator worker: %w", err)
	}
	return sequencerFor(planorchestration.TemporalDriver{Client: temporalClient}), nil
}

func sequencerFor(orchestrator controlplane.PlanOrchestrator) func(context.Context, string) error {
	if orchestrator == nil || orchestrator.Driver() != controlplane.PlanOrchestratorTemporal {
		return nil
	}
	return func(ctx context.Context, planID string) error {
		_, err := orchestrator.Begin(ctx, planID)
		return err
	}
}
