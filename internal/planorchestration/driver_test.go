package planorchestration

import (
	"context"
	"errors"
	"testing"

	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

type fakeStarter struct {
	starts  int
	signals []ApprovalMessage
}

func (fake *fakeStarter) ExecuteWorkflow(context.Context, client.StartWorkflowOptions, interface{}, ...interface{}) (client.WorkflowRun, error) {
	fake.starts++
	if fake.starts > 1 {
		return nil, serviceerror.NewWorkflowExecutionAlreadyStarted("already started", "", "run")
	}
	return nil, nil
}

func (fake *fakeStarter) SignalWorkflow(_ context.Context, _, _, _ string, arg interface{}) error {
	message, ok := arg.(ApprovalMessage)
	if !ok {
		return errors.New("signal payload")
	}
	fake.signals = append(fake.signals, message)
	return nil
}

func TestBeginIsIdempotentAndSignalNamesTheRecord(t *testing.T) {
	fake := &fakeStarter{}
	driver := TemporalDriver{Client: fake}
	first, err := driver.Begin(context.Background(), "pln_one")
	if err != nil {
		t.Fatal(err)
	}
	second, err := driver.Begin(context.Background(), "pln_one")
	if err != nil {
		t.Fatal(err)
	}
	if first.WorkflowID != WorkflowID("pln_one") || second.WorkflowID != first.WorkflowID || first.Driver != controlplane.PlanOrchestratorTemporal {
		t.Fatalf("handoff = %#v second = %#v", first, second)
	}
	if fake.starts != 2 {
		t.Fatalf("starts = %d", fake.starts)
	}
	message := ApprovalMessage{PlanID: "pln_one", ApprovalID: "operator-1"}
	if err := driver.SignalApproval(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if len(fake.signals) != 1 || fake.signals[0] != message {
		t.Fatalf("signals = %#v", fake.signals)
	}
}

func TestARepeatedWaveWritesOneAuditEvent(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	record := controlplane.PlanRecord{
		ID: "pln_wave", Status: controlplane.PlanApproved,
		Plan:      controlplane.Plan{Environment: "env-dev"},
		Approvals: []controlplane.Approval{{Subject: "operator-1"}},
	}
	if _, err := repository.CreatePlan(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	kernel := LifecycleKernel{Lifecycle: controlplane.NewLifecycle(repository), Repository: repository}
	request := WaveRequest{PlanID: record.ID, Wave: "rollout", Attempt: 1}
	if err := kernel.ExecuteWave(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if err := kernel.ExecuteWave(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	events, err := repository.ListAuthorizationAudit(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Decision != governance.DecisionAllow || events[0].Action != "plan.wave" {
		t.Fatalf("events = %#v", events)
	}
	chain, err := governance.ChainAuthorizationAudit(events)
	if err != nil || len(chain) != 1 || chain[0].ID != events[0].ID || chain[0].Digest == "" || chain[0].PreviousDigest == "" {
		t.Fatalf("chain = %#v err = %v", chain, err)
	}
	stored, err := repository.GetPlan(context.Background(), record.ID)
	if err != nil || stored.Status != controlplane.PlanApproved {
		t.Fatalf("plan = %#v err = %v", stored, err)
	}
}
