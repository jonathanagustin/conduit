package controlplane

import (
	"context"
	"testing"
)

func TestNativePlanOrchestratorDoesNotTouchTheStore(t *testing.T) {
	repository := NewMemoryRepository()
	orchestrator, err := PlanOrchestratorFromEnv(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if orchestrator.Driver() != PlanOrchestratorNative {
		t.Fatalf("driver = %s", orchestrator.Driver())
	}
	handoff, err := orchestrator.Begin(context.Background(), "pln_native")
	if err != nil {
		t.Fatal(err)
	}
	if handoff.Driver != PlanOrchestratorNative || handoff.PlanID != "pln_native" || handoff.WorkflowID != "" {
		t.Fatalf("handoff = %#v", handoff)
	}
	plans, err := repository.ListPlans(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Fatalf("native begin wrote %d plans", len(plans))
	}
	if _, err := orchestrator.Begin(context.Background(), "  "); err == nil {
		t.Fatal("blank plan id was accepted")
	}
}

func TestPlanOrchestratorSwitchDefaultsToNative(t *testing.T) {
	for _, value := range []string{"", "native", "NATIVE", " native "} {
		orchestrator, err := PlanOrchestratorFromEnv(func(string) string { return value })
		if err != nil {
			t.Fatal(err)
		}
		if orchestrator.Driver() != PlanOrchestratorNative {
			t.Fatalf("%q selected %s", value, orchestrator.Driver())
		}
	}
	temporal, err := PlanOrchestratorFromEnv(func(string) string { return PlanOrchestratorTemporal })
	if err != nil {
		t.Fatal(err)
	}
	if temporal.Driver() != PlanOrchestratorTemporal {
		t.Fatalf("driver = %s", temporal.Driver())
	}
	repository := NewMemoryRepository()
	if _, err := temporal.Begin(context.Background(), "pln_temporal"); err == nil {
		t.Fatal("unwired temporal driver started a plan")
	}
	plans, err := repository.ListPlans(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Fatalf("temporal skeleton wrote %d plans", len(plans))
	}
	if _, err := PlanOrchestratorFromEnv(func(string) string { return "other" }); err == nil {
		t.Fatal("unknown driver was accepted")
	}
}
