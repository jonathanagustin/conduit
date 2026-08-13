package optimization

import "testing"

func TestAssessExperimentRequiresPrimaryGuardrailsAndSample(t *testing.T) {
	plan := ExperimentPlan{ID: "exp-1", Hypothesis: "faster response improves completion", Control: "current", Treatment: "candidate", AssignmentUnit: "account", RandomizationUnit: "account", AllocationControl: "1/2", AllocationTreatment: "1/2", AnalysisID: "welch-v1", StoppingRule: "fixed-horizon", MinimumSampleSize: 100, MaximumDurationSec: 86400, Metrics: []ExperimentMetric{{ID: "completion", Role: ExperimentPrimary, Direction: ValueMaximize, Unit: "ratio"}, {ID: "cost", Role: ExperimentGuardrail, Direction: ValueMinimize, Unit: "usd/account", HarmLimit: "1/200"}}}
	outcome := ExperimentOutcome{PlanID: "exp-1", AnalysisRevision: "sha256:analysis", StartedAt: "2026-08-08T00:00:00Z", EndedAt: "2026-08-08T12:00:00Z", ExposureCost: "125/2", CostUnit: "USD", Effects: []ExperimentEffect{{MetricID: "completion", Estimate: "1/20", LowerBound: "1/100", UpperBound: "9/100", ControlCount: 120, TreatmentCount: 121}, {MetricID: "cost", Estimate: "-1/10", LowerBound: "-1/5", UpperBound: "-1/100", ControlCount: 120, TreatmentCount: 121}}}
	decision, err := AssessExperiment(plan, outcome)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Eligible || !decision.PrimaryPassed || !decision.GuardrailsPassed {
		t.Fatalf("decision = %#v", decision)
	}

	outcome.Effects[1].Estimate = "0"
	outcome.Effects[1].UpperBound = "1/200"
	decision, err = AssessExperiment(plan, outcome)
	if err != nil {
		t.Fatal(err)
	}
	if !decision.Eligible || !decision.GuardrailsPassed {
		t.Fatalf("non-inferiority boundary = %#v", decision)
	}

	outcome.Effects[1].UpperBound = "1/100"
	decision, err = AssessExperiment(plan, outcome)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Eligible || decision.GuardrailsPassed {
		t.Fatalf("decision = %#v", decision)
	}
}

func TestAssessExperimentRejectsUnregisteredAndUnderpoweredEvidence(t *testing.T) {
	plan := ExperimentPlan{ID: "exp-1", Hypothesis: "candidate helps", Control: "a", Treatment: "b", AssignmentUnit: "user", RandomizationUnit: "user", AllocationControl: "1/2", AllocationTreatment: "1/2", AnalysisID: "fixed-horizon-v1", StoppingRule: "fixed-horizon", MinimumSampleSize: 10, MaximumDurationSec: 3600, Metrics: []ExperimentMetric{{ID: "value", Role: ExperimentPrimary, Direction: ValueMaximize, Unit: "count"}}}
	outcome := ExperimentOutcome{PlanID: "exp-1", AnalysisRevision: "r1", StartedAt: "2026-08-08T00:00:00Z", EndedAt: "2026-08-08T00:30:00Z", ExposureCost: "0", CostUnit: "USD", Effects: []ExperimentEffect{{MetricID: "value", Estimate: "2", LowerBound: "1", UpperBound: "3", ControlCount: 9, TreatmentCount: 10}}}
	decision, err := AssessExperiment(plan, outcome)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Eligible || len(decision.Reasons) != 1 {
		t.Fatalf("decision = %#v", decision)
	}
	outcome.Effects[0].MetricID = "post-hoc"
	if _, err := AssessExperiment(plan, outcome); err == nil {
		t.Fatal("expected unregistered metric error")
	}
}
