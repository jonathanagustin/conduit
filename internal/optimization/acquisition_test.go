package optimization

import "testing"

// Plane identity is part of the observation contract; semantically named but
// differently staged state cannot satisfy a requirement.

func TestOptimizeAcquisitionCallsFindsExactCheapestCompleteCover(t *testing.T) {
	requirements := []StateRequirement{{ID: "desired", Plane: StateDesired, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 60}, {ID: "observed", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10}, {ID: "cost", Plane: StateBilled, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 86400}}
	calls := []AcquisitionCall{
		{ID: "combined-stale", Source: "cache", Method: "GET", Endpoint: "/snapshot", Consistency: "eventual", CostUnits: 1, Observations: []FieldObservation{{Requirement: "desired", Plane: StateDesired, Fidelity: FidelityExact, MaximumStalenessSeconds: 60}, {Requirement: "observed", Plane: StateObserved, Fidelity: FidelityExact, MaximumStalenessSeconds: 30}}},
		{ID: "hub", Source: "hub", Method: "GET", Endpoint: "/state", Consistency: "linearizable", CostUnits: 2, Observations: []FieldObservation{{Requirement: "desired", Plane: StateDesired, Fidelity: FidelityExact, MaximumStalenessSeconds: 1}, {Requirement: "observed", Plane: StateObserved, Fidelity: FidelityExact, MaximumStalenessSeconds: 1}}},
		{ID: "billing", Source: "provider", Method: "GET", Endpoint: "/cost", Consistency: "finalized", CostUnits: 3, Observations: []FieldObservation{{Requirement: "cost", Plane: StateBilled, Fidelity: FidelityExact, MaximumStalenessSeconds: 3600}}},
	}
	plan, err := OptimizeAcquisitionCalls(requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Complete || !plan.Optimal || plan.TotalCostUnits != 5 || len(plan.Calls) != 2 || len(plan.Unknown) != 0 || plan.Calls[0].ID != "billing" || plan.Calls[1].ID != "hub" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestOptimizeAcquisitionCallsRejectsPlaneMismatchAndCostOverflow(t *testing.T) {
	requirement := []StateRequirement{{ID: "state", Plane: StateObserved, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 1}}
	wrong := []AcquisitionCall{{ID: "call", Source: "source", Method: "GET", Endpoint: "/state", Consistency: "strong", CostUnits: 1, Observations: []FieldObservation{{Requirement: "state", Plane: StateDesired, Fidelity: FidelityExact, MaximumStalenessSeconds: 0}}}}
	plan, err := OptimizeAcquisitionCalls(requirement, wrong)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Complete || plan.Optimal || len(plan.Unknown) != 1 {
		t.Fatalf("plan = %#v", plan)
	}
	overflow := append(wrong, AcquisitionCall{ID: "other", Source: "source", Method: "GET", Endpoint: "/other", Consistency: "strong", CostUnits: 1<<63 - 1})
	overflow[0].CostUnits = 1
	if _, err := OptimizeAcquisitionCalls(requirement, overflow); err == nil {
		t.Fatal("accepted overflowing cost domain")
	}
}

func TestOptimizeAcquisitionCallsDistinguishesSampledAndEstimatedEvidence(t *testing.T) {
	requirements := []StateRequirement{{ID: "latency", Plane: StateExperienced, MinimumFidelity: FidelitySampled, MaximumStalenessSeconds: 30}}
	calls := []AcquisitionCall{{ID: "sample", Source: "rum", Method: "GET", Endpoint: "/sample", Consistency: "windowed", CostUnits: 1, Observations: []FieldObservation{{Requirement: "latency", Plane: StateExperienced, Fidelity: FidelitySampled, MaximumStalenessSeconds: 10}}}}
	plan, err := OptimizeAcquisitionCalls(requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Complete || plan.Calls[0].Observations[0].Fidelity != FidelitySampled {
		t.Fatalf("plan = %#v", plan)
	}
	calls[0].Observations[0].Fidelity = FidelityEstimated
	plan, err = OptimizeAcquisitionCalls(requirements, calls)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Complete || len(plan.Unknown) != 1 {
		t.Fatalf("estimated evidence satisfied sampled requirement: %#v", plan)
	}
}

func TestOptimizeAcquisitionCallsReportsUnknownInsteadOfWeakeningRequirement(t *testing.T) {
	plan, err := OptimizeAcquisitionCalls([]StateRequirement{{ID: "user-latency", Plane: StateExperienced, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 30}}, []AcquisitionCall{{ID: "aggregate", Source: "metrics", Method: "GET", Endpoint: "/p95", Consistency: "windowed", CostUnits: 1, Observations: []FieldObservation{{Requirement: "user-latency", Plane: StateExperienced, Fidelity: FidelityAggregated, MaximumStalenessSeconds: 10}}}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Complete || plan.Optimal || len(plan.Calls) != 0 || len(plan.Unknown) != 1 || plan.Unknown[0] != "user-latency" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestOptimizeAcquisitionCallsRejectsUnboundedCallCoordinates(t *testing.T) {
	requirements := []StateRequirement{{ID: "desired", Plane: StateDesired, MinimumFidelity: FidelityExact, MaximumStalenessSeconds: 10}}
	base := AcquisitionCall{ID: "call", Source: "source", Method: "GET", Endpoint: "/state", Consistency: "snapshot", CostUnits: 1, Observations: []FieldObservation{{Requirement: "desired", Plane: StateDesired, Fidelity: FidelityExact, MaximumStalenessSeconds: 1}}}
	for _, mutate := range []func(*AcquisitionCall){
		func(call *AcquisitionCall) { call.ID = "call\nforged" },
		func(call *AcquisitionCall) { call.Source = "provider\tforeign" },
		func(call *AcquisitionCall) { call.Method = "GET\rPOST" },
		func(call *AcquisitionCall) { call.Consistency = "snapshot\nweak" },
	} {
		call := base
		mutate(&call)
		if _, err := OptimizeAcquisitionCalls(requirements, []AcquisitionCall{call}); err == nil {
			t.Fatalf("accepted malformed call coordinate: %#v", call)
		}
	}
}
