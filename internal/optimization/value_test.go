package optimization

import "testing"

func testValueAlternative(id string, estimates map[string]ValueEstimate) ValueAlternative {
	return ValueAlternative{ID: id, Estimates: estimates, LifecycleCost: &LifecycleCost{Currency: "USD", PriceSource: "price-list", PriceSourceRevision: "r1", AsOf: "2026-08-08T00:00:00Z", HorizonSeconds: 31_536_000}, TimeToValueSeconds: 3600, DiscountRate: "1/20", DiscountBasis: "annual", Assumptions: []string{"demand remains within observed bounds"}}
}

func TestAssessValuePreservesParetoTradeoffsAndHardFunctions(t *testing.T) {
	criteria := []ValueCriterion{{ID: "availability", Stakeholder: "operators", UserSegment: "service consumers", Function: "serve traffic", Criticality: "critical", Measure: "successful requests", Unit: "ppm", Scale: "CARDINAL", Baseline: "999000", Target: "999900", Direction: ValueMaximize, HardMinimum: "999000"}, {ID: "lifecycle-cost", Stakeholder: "owner", UserSegment: "portfolio", Function: "fund service", Criticality: "required", Measure: "total lifecycle cost", Unit: "USD-micros", Scale: "CARDINAL", Baseline: "10000000", Target: "8000000", Direction: ValueMinimize}}
	estimate := func(value string) ValueEstimate {
		return ValueEstimate{Value: value, Source: "experiment", SourceRevision: "run-1", AsOf: "2026-08-08T00:00:00Z", HorizonSeconds: 31_536_000, Distribution: "point"}
	}
	alternatives := []ValueAlternative{
		testValueAlternative("balanced", map[string]ValueEstimate{"availability": estimate("999500"), "lifecycle-cost": estimate("8000000")}),
		testValueAlternative("dominated", map[string]ValueEstimate{"availability": estimate("999100"), "lifecycle-cost": estimate("9000000")}),
		testValueAlternative("premium", map[string]ValueEstimate{"availability": estimate("999900"), "lifecycle-cost": estimate("12000000")}),
		testValueAlternative("unsafe-cheap", map[string]ValueEstimate{"availability": estimate("998000"), "lifecycle-cost": estimate("1000000")}),
		testValueAlternative("unknown", map[string]ValueEstimate{"availability": estimate("999999")}),
	}
	got, err := AssessValue(criteria, alternatives)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ParetoEfficient) != 2 || got.ParetoEfficient[0] != "balanced" || got.ParetoEfficient[1] != "premium" {
		t.Fatalf("frontier = %#v", got.ParetoEfficient)
	}
	if len(got.Dominated["dominated"]) != 1 || got.Dominated["dominated"][0] != "balanced" {
		t.Fatalf("domination = %#v", got.Dominated)
	}
	if len(got.Infeasible["unsafe-cheap"]) != 1 || len(got.InsufficientEvidence["unknown"]) != 1 {
		t.Fatalf("evidence classification = %#v", got)
	}
}

func TestAssessValueUsesConservativeUncertaintyBounds(t *testing.T) {
	criteria := []ValueCriterion{{ID: "benefit", Stakeholder: "users", UserSegment: "active users", Function: "finish work", Criticality: "important", Measure: "tasks", Unit: "count", Scale: "CARDINAL", Baseline: "8", Target: "12", Direction: ValueMaximize}}
	estimate := func(value, lower, upper string) ValueEstimate {
		return ValueEstimate{Value: value, LowerBound: lower, UpperBound: upper, Source: "trial", SourceRevision: "r1", AsOf: "2026-08-08T00:00:00Z", HorizonSeconds: 3600, Distribution: "confidence-95"}
	}
	got, err := AssessValue(criteria, []ValueAlternative{testValueAlternative("uncertain-high", map[string]ValueEstimate{"benefit": estimate("12", "8", "16")}), testValueAlternative("certain", map[string]ValueEstimate{"benefit": estimate("10", "10", "10")})})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ParetoEfficient) != 2 {
		t.Fatalf("overlapping uncertainty invented dominance: %#v", got)
	}
}
