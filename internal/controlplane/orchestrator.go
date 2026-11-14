package controlplane

import (
	"context"
	"fmt"
	"strings"
)

const (
	// PlanOrchestratorEnv selects which driver sequences an approved Plan.
	// Empty and "native" are the same driver. The Hub process does not read
	// this until a worker is wired. The default leaves today's lifecycle path
	// in place.
	PlanOrchestratorEnv      = "PLAN_ORCHESTRATOR"
	PlanOrchestratorNative   = "native"
	PlanOrchestratorTemporal = "temporal"
)

// PlanExecutionHandoff is the orchestrator's answer for one Plan. WorkflowID
// is empty on the native driver. The native driver does not lease, approve,
// or write reported state.
type PlanExecutionHandoff struct {
	Driver     string
	PlanID     string
	WorkflowID string
}

// PlanOrchestrator sequences an approved Plan. It is not the system of record
// and it is not the Spoke reconciler.
type PlanOrchestrator interface {
	Driver() string
	Begin(ctx context.Context, planID string) (PlanExecutionHandoff, error)
}

// NativePlanOrchestrator leaves sequencing to the existing lifecycle methods
// and the Spoke. Begin records no workflow and changes no Plan.
type NativePlanOrchestrator struct{}

func (NativePlanOrchestrator) Driver() string { return PlanOrchestratorNative }

func (NativePlanOrchestrator) Begin(_ context.Context, planID string) (PlanExecutionHandoff, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return PlanExecutionHandoff{}, fmt.Errorf("%w: plan id is required", ErrInvalidTransition)
	}
	return PlanExecutionHandoff{Driver: PlanOrchestratorNative, PlanID: planID}, nil
}

// UnwiredTemporalPlanOrchestrator is the temporal selection before a worker
// exists. Begin refuses. It does not start a workflow and it does not fall
// through into a second lifecycle.
type UnwiredTemporalPlanOrchestrator struct{}

func (UnwiredTemporalPlanOrchestrator) Driver() string { return PlanOrchestratorTemporal }

func (UnwiredTemporalPlanOrchestrator) Begin(context.Context, string) (PlanExecutionHandoff, error) {
	return PlanExecutionHandoff{}, fmt.Errorf("temporal plan orchestration has no worker wired")
}

// PlanOrchestratorFromEnv resolves the driver. Unknown values fail closed.
func PlanOrchestratorFromEnv(getenv func(string) string) (PlanOrchestrator, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	switch strings.ToLower(strings.TrimSpace(getenv(PlanOrchestratorEnv))) {
	case "", PlanOrchestratorNative:
		return NativePlanOrchestrator{}, nil
	case PlanOrchestratorTemporal:
		return UnwiredTemporalPlanOrchestrator{}, nil
	default:
		return nil, fmt.Errorf("%s must be %s or %s", PlanOrchestratorEnv, PlanOrchestratorNative, PlanOrchestratorTemporal)
	}
}
