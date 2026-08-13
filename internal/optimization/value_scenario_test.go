package optimization

import "testing"

func TestAssessValueScenariosReportsAssumptionSensitivity(t *testing.T) {
	criteria := []ValueCriterion{{ID: "benefit", Stakeholder: "users", UserSegment: "active", Function: "finish", Criticality: "important", Measure: "completion", Unit: "count", Scale: "CARDINAL", Baseline: "0", Target: "10", Direction: ValueMaximize}}
	estimate := func(value string) ValueEstimate {
		return ValueEstimate{Value: value, Source: "experiment", SourceRevision: "r1", AsOf: "2026-08-08T00:00:00Z", HorizonSeconds: 3600, Distribution: "point"}
	}
	scenario := func(id, first, second string) ValueScenario {
		return ValueScenario{ID: id, Source: "forecast", SourceRevision: "r1", Alternatives: []ValueAlternative{
			testValueAlternative("first", map[string]ValueEstimate{"benefit": estimate(first)}),
			testValueAlternative("second", map[string]ValueEstimate{"benefit": estimate(second)}),
		}}
	}
	got, err := AssessValueScenarios(criteria, []ValueScenario{scenario("high-demand", "12", "10"), scenario("low-demand", "8", "10")})
	if err != nil {
		t.Fatal(err)
	}
	if !got.FrontierChanges || got.Scenarios[0].Assessment.ParetoEfficient[0] != "first" || got.Scenarios[1].Assessment.ParetoEfficient[0] != "second" {
		t.Fatalf("scenario sensitivity = %#v", got)
	}
}

func TestAssessValueScenariosRejectsInventoryChanges(t *testing.T) {
	criteria := []ValueCriterion{{ID: "benefit", Stakeholder: "users", UserSegment: "active", Function: "finish", Criticality: "important", Measure: "completion", Unit: "count", Scale: "CARDINAL", Baseline: "0", Target: "10", Direction: ValueMaximize}}
	estimate := ValueEstimate{Value: "1", Source: "experiment", SourceRevision: "r1", AsOf: "2026-08-08T00:00:00Z", HorizonSeconds: 3600, Distribution: "point"}
	_, err := AssessValueScenarios(criteria, []ValueScenario{
		{ID: "one", Source: "forecast", SourceRevision: "r1", Alternatives: []ValueAlternative{testValueAlternative("a", map[string]ValueEstimate{"benefit": estimate})}},
		{ID: "two", Source: "forecast", SourceRevision: "r1", Alternatives: []ValueAlternative{testValueAlternative("b", map[string]ValueEstimate{"benefit": estimate})}},
	})
	if err == nil {
		t.Fatal("expected changed inventory to be rejected")
	}
}
