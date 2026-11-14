package planorchestration

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type waveLog struct {
	executed      []WaveRequest
	failWave      string
	confirmed     int
	compensations int
}

func (log *waveLog) Confirm(context.Context, string, string) error {
	log.confirmed++
	return nil
}

func (log *waveLog) Execute(_ context.Context, request WaveRequest) error {
	log.executed = append(log.executed, request)
	if request.Wave == log.failWave {
		return errWave("wave failed")
	}
	return nil
}

func (log *waveLog) Compensate(context.Context, string, string) error {
	log.compensations++
	return nil
}

type errWave string

func (e errWave) Error() string { return string(e) }

func boot(log *waveLog) *testsuite.TestWorkflowEnvironment {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterWorkflowWithOptions(PlanWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	env.RegisterActivityWithOptions(log.Confirm, activity.RegisterOptions{Name: ActConfirm})
	env.RegisterActivityWithOptions(log.Execute, activity.RegisterOptions{Name: ActExecute})
	env.RegisterActivityWithOptions(log.Compensate, activity.RegisterOptions{Name: ActCompensate})
	return env
}

func sample() Input {
	return Input{
		PlanID: "pln_sample",
		Waves: []Wave{
			{Name: "canary", Canary: true},
			{Name: "rest"},
		},
		ApprovalID:   "operator-1",
		ApprovalWait: time.Hour,
		WindowDelay:  time.Hour,
		Soak:         time.Hour,
	}
}

func TestWavesWaitForOneApprovalAndSkipTime(t *testing.T) {
	log := &waveLog{}
	env := boot(log)
	input := sample()
	message := ApprovalMessage{ApprovalID: input.ApprovalID, PlanID: input.PlanID}
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApproval, message)
		env.SignalWorkflow(SignalApproval, message)
	}, 0)
	started := time.Now()
	env.ExecuteWorkflow(PlanWorkflow, input)
	if time.Since(started) > 5*time.Second {
		t.Fatalf("workflow slept on the wall clock for %s", time.Since(started))
	}
	if !env.IsWorkflowCompleted() || env.GetWorkflowError() != nil {
		t.Fatalf("err = %v", env.GetWorkflowError())
	}
	var result Result
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "completed" || result.Waves != 2 || result.Compensated || log.confirmed != 1 || log.compensations != 0 {
		t.Fatalf("result = %#v log = %#v", result, log)
	}
	if len(log.executed) != 2 || log.executed[0].Wave != "canary" || log.executed[0].Attempt != 1 || log.executed[1].Wave != "rest" {
		t.Fatalf("executed = %#v", log.executed)
	}
}

func TestAMismatchedApprovalDoesNotRunAWave(t *testing.T) {
	log := &waveLog{}
	env := boot(log)
	input := sample()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(SignalApproval, ApprovalMessage{ApprovalID: "someone-else", PlanID: input.PlanID})
	}, 0)
	env.ExecuteWorkflow(PlanWorkflow, input)
	var result Result
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "approval_mismatch" || len(log.executed) != 0 || log.confirmed != 0 {
		t.Fatalf("result = %#v executed = %#v", result, log.executed)
	}
}

func TestAFailedWaveCompensatesOnce(t *testing.T) {
	log := &waveLog{failWave: "rest"}
	env := boot(log)
	input := sample()
	input.ApprovalID = ""
	env.ExecuteWorkflow(PlanWorkflow, input)
	var result Result
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatal(err)
	}
	if result.Outcome != "failed" || !result.Compensated || result.Waves != 1 || log.compensations != 1 {
		t.Fatalf("result = %#v compensations = %d", result, log.compensations)
	}
	if len(log.executed) != 2 || log.executed[1].PlanID != input.PlanID || log.executed[1].Attempt != 1 {
		t.Fatalf("executed = %#v", log.executed)
	}
}

func TestLifecycleKernelReadsApprovalAndCancelsThroughTheKernel(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	record := controlplane.PlanRecord{
		ID: "pln_kernel", Status: controlplane.PlanApproved,
		Plan:      controlplane.Plan{Environment: "env-dev"},
		Approvals: []controlplane.Approval{{Subject: "operator-1", Rationale: "recorded"}},
	}
	if _, err := repository.CreatePlan(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	kernel := LifecycleKernel{Lifecycle: controlplane.NewLifecycle(repository), Repository: repository}
	if err := kernel.ConfirmApproval(context.Background(), record.ID, "operator-1"); err != nil {
		t.Fatal(err)
	}
	if err := kernel.ConfirmApproval(context.Background(), record.ID, "missing"); err == nil {
		t.Fatal("missing governance record was accepted")
	}
	if err := kernel.ExecuteWave(context.Background(), WaveRequest{PlanID: record.ID, Wave: "canary", Attempt: 1}); err != nil {
		t.Fatal(err)
	}
	stored, err := repository.GetPlan(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != controlplane.PlanApproved || len(stored.Approvals) != 1 {
		t.Fatalf("wave gate changed the plan: %#v", stored)
	}
	if err := kernel.Compensate(context.Background(), record.ID, "wave rest failed"); err != nil {
		t.Fatal(err)
	}
	stored, err = repository.GetPlan(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != controlplane.PlanCancelled || stored.Cancellation == nil || stored.Lease != nil {
		t.Fatalf("compensate = %#v", stored)
	}
}
