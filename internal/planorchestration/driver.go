package planorchestration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

const TaskQueue = "plan-execution"

// starter is the slice of the Temporal client the driver uses. A test can
// supply a fake. The production caller passes a real client.
type starter interface {
	ExecuteWorkflow(ctx context.Context, options client.StartWorkflowOptions, workflow interface{}, args ...interface{}) (client.WorkflowRun, error)
	SignalWorkflow(ctx context.Context, workflowID string, runID string, signalName string, arg interface{}) error
}

// TemporalDriver starts one workflow per Plan and signals it with a governance
// record id. It does not approve, lease, or write reported state.
type TemporalDriver struct {
	Client starter
	Queue  string
}

func (driver TemporalDriver) Driver() string { return controlplane.PlanOrchestratorTemporal }

func (driver TemporalDriver) Begin(ctx context.Context, planID string) (controlplane.PlanExecutionHandoff, error) {
	planID = strings.TrimSpace(planID)
	if planID == "" {
		return controlplane.PlanExecutionHandoff{}, fmt.Errorf("%w: plan id is required", controlplane.ErrInvalidTransition)
	}
	if driver.Client == nil {
		return controlplane.PlanExecutionHandoff{}, errors.New("temporal plan orchestration has no client")
	}
	workflowID := WorkflowID(planID)
	_, err := driver.Client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:                    workflowID,
		TaskQueue:             driver.queue(),
		WorkflowIDReusePolicy: enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	}, WorkflowName, Input{
		PlanID: planID,
		Waves:  []Wave{{Name: "rollout"}},
	})
	var already *serviceerror.WorkflowExecutionAlreadyStarted
	if err != nil && !errors.As(err, &already) {
		return controlplane.PlanExecutionHandoff{}, err
	}
	return controlplane.PlanExecutionHandoff{
		Driver: controlplane.PlanOrchestratorTemporal, PlanID: planID, WorkflowID: workflowID,
	}, nil
}

// SignalApproval names a governance record for a Plan workflow that is waiting.
// The activity reads that record back. The signal is not an approval.
func (driver TemporalDriver) SignalApproval(ctx context.Context, message ApprovalMessage) error {
	if driver.Client == nil {
		return errors.New("temporal plan orchestration has no client")
	}
	if strings.TrimSpace(message.PlanID) == "" || strings.TrimSpace(message.ApprovalID) == "" {
		return fmt.Errorf("%w: plan id and approval id are required", controlplane.ErrInvalidTransition)
	}
	return driver.Client.SignalWorkflow(ctx, WorkflowID(message.PlanID), "", SignalApproval, message)
}

func (driver TemporalDriver) queue() string {
	if driver.Queue != "" {
		return driver.Queue
	}
	return TaskQueue
}

// WorkflowID is stable for a Plan, so a restarted Hub addresses the same run.
func WorkflowID(planID string) string { return "plan-execute-" + planID }

// RegisterWorker binds the workflow and the kernel activities. The caller
// starts and stops the worker. Activities write the kernel's audit chain.
func RegisterWorker(c client.Client, queue string, kernel LifecycleKernel) worker.Worker {
	if queue == "" {
		queue = TaskQueue
	}
	work := worker.New(c, queue, worker.Options{})
	work.RegisterWorkflowWithOptions(PlanWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	work.RegisterActivityWithOptions(kernel.ConfirmApproval, activity.RegisterOptions{Name: ActConfirm})
	work.RegisterActivityWithOptions(kernel.ExecuteWave, activity.RegisterOptions{Name: ActExecute})
	work.RegisterActivityWithOptions(kernel.Compensate, activity.RegisterOptions{Name: ActCompensate})
	return work
}
