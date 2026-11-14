package planorchestration

import (
	"context"
	"fmt"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// LifecycleKernel binds workflow activities to kernel methods. Confirm and
// wave execution read the Plan. Compensate cancels through the lifecycle.
// None of these methods approve or write reported state.
type LifecycleKernel struct {
	Lifecycle  *controlplane.Lifecycle
	Repository controlplane.Repository
}

func (kernel LifecycleKernel) ConfirmApproval(ctx context.Context, planID, approvalID string) error {
	record, err := kernel.plan(ctx, planID)
	if err != nil {
		return err
	}
	for _, approval := range record.Approvals {
		if approval.Subject == approvalID {
			return nil
		}
	}
	return fmt.Errorf("governance record %s is not on plan %s", approvalID, planID)
}

func (kernel LifecycleKernel) ExecuteWave(ctx context.Context, request WaveRequest) error {
	if request.PlanID == "" || request.Wave == "" || request.Attempt < 1 {
		return fmt.Errorf("wave key must include plan, wave, and attempt")
	}
	record, err := kernel.plan(ctx, request.PlanID)
	if err != nil {
		return err
	}
	switch record.Status {
	case controlplane.PlanApproved, controlplane.PlanLeased, controlplane.PlanRunning:
		return nil
	default:
		return fmt.Errorf("plan %s is %s and wave %s attempt %d cannot run", request.PlanID, record.Status, request.Wave, request.Attempt)
	}
}

func (kernel LifecycleKernel) Compensate(ctx context.Context, planID, reason string) error {
	if kernel.Lifecycle == nil {
		return fmt.Errorf("lifecycle is required")
	}
	_, err := kernel.Lifecycle.CancelPlan(ctx, planID, "plan-orchestrator", reason)
	return err
}

func (kernel LifecycleKernel) plan(ctx context.Context, planID string) (controlplane.PlanRecord, error) {
	if kernel.Repository == nil {
		return controlplane.PlanRecord{}, fmt.Errorf("repository is required")
	}
	return kernel.Repository.GetPlan(ctx, planID)
}
