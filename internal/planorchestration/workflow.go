package planorchestration

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowName is the stable type name recorded in history.
	WorkflowName   = "plan.Execute"
	SignalApproval = "plan.Approval"
	ActConfirm     = "plan.ConfirmApproval"
	ActExecute     = "plan.ExecuteWave"
	ActCompensate  = "plan.Compensate"
)

// Wave is one ordered slice of a Plan. Canary waves soak before the next wave.
type Wave struct {
	Name   string `json:"name"`
	Canary bool   `json:"canary"`
}

// Input is the workflow argument. It carries no manifest bytes and no credentials.
type Input struct {
	PlanID       string        `json:"planId"`
	Waves        []Wave        `json:"waves"`
	ApprovalID   string        `json:"approvalId,omitempty"`
	ApprovalWait time.Duration `json:"approvalWait"`
	WindowDelay  time.Duration `json:"windowDelay"`
	Soak         time.Duration `json:"soak"`
}

// ApprovalMessage names a governance record. It is not itself an approval.
type ApprovalMessage struct {
	ApprovalID string `json:"approvalId"`
	PlanID     string `json:"planId"`
}

// WaveRequest is the idempotency key for one wave attempt.
type WaveRequest struct {
	PlanID  string `json:"planId"`
	Wave    string `json:"wave"`
	Attempt int    `json:"attempt"`
}

// Result is the workflow's bounded outcome. Plan status stays in the kernel.
type Result struct {
	PlanID      string `json:"planId"`
	Outcome     string `json:"outcome"`
	Waves       int    `json:"waves"`
	Compensated bool   `json:"compensated"`
}

// PlanWorkflow orders a window wait, an approval wait, wave execution, canary
// soak, and a compensating cancel when a wave fails.
func PlanWorkflow(ctx workflow.Context, in Input) (Result, error) {
	if in.PlanID == "" || len(in.Waves) == 0 {
		return Result{}, temporal.NewNonRetryableApplicationError(
			"plan id and at least one wave are required", "BadInput", nil)
	}
	if in.ApprovalID != "" {
		outcome, err := waitApproval(ctx, in)
		if err != nil || outcome != "" {
			if err != nil {
				return Result{}, err
			}
			return Result{PlanID: in.PlanID, Outcome: outcome}, nil
		}
	}
	for index, wave := range in.Waves {
		if wave.Name == "" {
			return Result{PlanID: in.PlanID, Outcome: "bad_wave"}, nil
		}
		if in.WindowDelay > 0 {
			if err := workflow.Sleep(ctx, in.WindowDelay); err != nil {
				return Result{}, err
			}
		}
		request := WaveRequest{PlanID: in.PlanID, Wave: wave.Name, Attempt: 1}
		if err := workflow.ExecuteActivity(activityCtx(ctx), ActExecute, request).Get(ctx, nil); err != nil {
			compensate := workflow.ExecuteActivity(activityCtx(ctx), ActCompensate, in.PlanID, "wave "+wave.Name+" failed").Get(ctx, nil)
			if compensate != nil {
				return Result{}, compensate
			}
			return Result{PlanID: in.PlanID, Outcome: "failed", Waves: index, Compensated: true}, nil
		}
		if wave.Canary && in.Soak > 0 {
			if err := workflow.Sleep(ctx, in.Soak); err != nil {
				return Result{}, err
			}
		}
	}
	return Result{PlanID: in.PlanID, Outcome: "completed", Waves: len(in.Waves)}, nil
}

func waitApproval(ctx workflow.Context, in Input) (string, error) {
	wait := in.ApprovalWait
	if wait <= 0 {
		wait = 15 * time.Minute
	}
	var message ApprovalMessage
	signal := workflow.GetSignalChannel(ctx, SignalApproval)
	timedOut := false
	selector := workflow.NewSelector(ctx)
	selector.AddReceive(signal, func(channel workflow.ReceiveChannel, _ bool) {
		channel.Receive(ctx, &message)
	})
	selector.AddFuture(workflow.NewTimer(ctx, wait), func(workflow.Future) { timedOut = true })
	selector.Select(ctx)
	if timedOut || message.ApprovalID == "" {
		return "approval_wait_elapsed", nil
	}
	var extra ApprovalMessage
	for signal.ReceiveAsync(&extra) {
	}
	if message.ApprovalID != in.ApprovalID || message.PlanID != in.PlanID {
		return "approval_mismatch", nil
	}
	if err := workflow.ExecuteActivity(activityCtx(ctx), ActConfirm, in.PlanID, in.ApprovalID).Get(ctx, nil); err != nil {
		return "approval_rejected", nil
	}
	return "", nil
}

func activityCtx(ctx workflow.Context) workflow.Context {
	return workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
}
