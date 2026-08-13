package optimization

import "testing"

func TestAssessValuePoliciesMakesWeightSensitivityExplicit(t *testing.T) {
	criteria := []ValueCriterion{
		{ID: "benefit", Stakeholder: "users", UserSegment: "active", Function: "finish", Criticality: "important", Measure: "completion", Unit: "count", Scale: "CARDINAL", Baseline: "0", Target: "10", Direction: ValueMaximize, HardMinimum: "4"},
		{ID: "cost", Stakeholder: "owner", UserSegment: "portfolio", Function: "fund", Criticality: "required", Measure: "cost", Unit: "USD-micros", Scale: "CARDINAL", Baseline: "100", Target: "0", Direction: ValueMinimize},
	}
	estimate := func(value string) ValueEstimate {
		return ValueEstimate{Value: value, Source: "trial", SourceRevision: "r1", AsOf: "2026-08-08T00:00:00Z", HorizonSeconds: 3600, Distribution: "point"}
	}
	alternatives := []ValueAlternative{testValueAlternative("benefit-first", map[string]ValueEstimate{"benefit": estimate("10"), "cost": estimate("80")}), testValueAlternative("cost-first", map[string]ValueEstimate{"benefit": estimate("5"), "cost": estimate("20")}), testValueAlternative("unsafe-free", map[string]ValueEstimate{"benefit": estimate("3"), "cost": estimate("0")})}
	policies := []ValuePolicy{{ID: "users", Source: "stakeholder-policy", SourceRevision: "1", Weights: map[string]string{"benefit": "4/5", "cost": "1/5"}}, {ID: "owner", Source: "stakeholder-policy", SourceRevision: "2", Weights: map[string]string{"benefit": "1/5", "cost": "4/5"}}}
	got, err := AssessValuePolicies(criteria, alternatives, policies)
	if err != nil {
		t.Fatal(err)
	}
	if !got.SelectionChanges || got.Policies[0].Selected[0] != "cost-first" || got.Policies[1].Selected[0] != "benefit-first" {
		t.Fatalf("sensitivity = %#v", got)
	}
	if _, scored := got.Policies[0].Scores["unsafe-free"]; scored {
		t.Fatal("hard-infeasible alternative was traded into weighted scoring")
	}
}
