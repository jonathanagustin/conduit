package composition

import (
	"context"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type recordingOrchestrator struct{ ids []string }

func (recordingOrchestrator) Driver() string { return controlplane.PlanOrchestratorTemporal }

func (recorder *recordingOrchestrator) Begin(_ context.Context, planID string) (controlplane.PlanExecutionHandoff, error) {
	recorder.ids = append(recorder.ids, planID)
	return controlplane.PlanExecutionHandoff{Driver: controlplane.PlanOrchestratorTemporal, PlanID: planID, WorkflowID: "plan-execute-" + planID}, nil
}

func TestPlanSequencerLeavesTheNativePathAlone(t *testing.T) {
	for _, value := range []string{"", "native", "NATIVE"} {
		callback, err := planSequencer(controlplane.NewMemoryRepository(), Options{}, func(string) string { return value })
		if err != nil || callback != nil {
			t.Fatalf("%q callback=%v err=%v", value, callback != nil, err)
		}
	}
}

func TestPlanSequencerRejectsAnUnknownSwitchAndATemporalSwitchWithoutAnAddress(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	if _, err := planSequencer(repository, Options{}, func(string) string { return "other" }); err == nil {
		t.Fatal("unknown switch was accepted")
	}
	_, err := planSequencer(repository, Options{}, func(key string) string {
		if key == controlplane.PlanOrchestratorEnv {
			return controlplane.PlanOrchestratorTemporal
		}
		return ""
	})
	if err == nil {
		t.Fatal("temporal without an address was accepted")
	}
}

func TestPlanSequencerUsesTheSuppliedDriver(t *testing.T) {
	recorder := &recordingOrchestrator{}
	callback, err := planSequencer(controlplane.NewMemoryRepository(), Options{PlanOrchestrator: recorder}, func(string) string { return "" })
	if err != nil || callback == nil {
		t.Fatalf("callback err=%v", err)
	}
	if err := callback(context.Background(), "pln_supplied"); err != nil {
		t.Fatal(err)
	}
	if len(recorder.ids) != 1 || recorder.ids[0] != "pln_supplied" {
		t.Fatalf("ids = %#v", recorder.ids)
	}
}
