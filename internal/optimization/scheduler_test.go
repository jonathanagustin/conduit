package optimization

import (
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestSolveBalancesMutationWorkAndMeasuresRelaxationGap(t *testing.T) {
	problem := Problem{Resources: []Resource{{ID: "worker-b", Capacity: 1}, {ID: "worker-a", Capacity: 1}}, Items: []WorkItem{
		{ID: "m1", Demand: 8}, {ID: "m2", Demand: 7}, {ID: "m3", Demand: 6}, {ID: "m4", Demand: 5}, {ID: "m5", Demand: 4},
	}}
	got, err := Solve(problem)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(got.RelaxedMakespan-15) > 1e-6 || math.Abs(got.RoundedMakespan-17) > 1e-6 {
		t.Fatalf("makespans = %v/%v, want 15/17", got.RelaxedMakespan, got.RoundedMakespan)
	}
	if math.Abs(got.OptimalityGap-(17.0/15.0-1)) > 1e-6 {
		t.Fatalf("gap = %v", got.OptimalityGap)
	}
	again, err := Solve(problem)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Assignments, again.Assignments) {
		t.Fatalf("non-deterministic assignments: %#v / %#v", got.Assignments, again.Assignments)
	}
}

func TestSolveHonorsHardEligibility(t *testing.T) {
	problem := Problem{Resources: []Resource{{ID: "amd64", Capacity: 2}, {ID: "arm64", Capacity: 1}}, Items: []WorkItem{
		{ID: "portable", Demand: 4}, {ID: "native", Demand: 3, Eligible: []string{"arm64"}},
	}}
	got, err := Solve(problem)
	if err != nil {
		t.Fatal(err)
	}
	for _, assignment := range got.Assignments {
		if assignment.Item == "native" && assignment.Resource != "arm64" {
			t.Fatalf("hard constraint violated: %#v", assignment)
		}
	}
	if got.RelaxedMakespan < 3-1e-6 {
		t.Fatalf("lower bound = %v, cannot satisfy pinned work", got.RelaxedMakespan)
	}
	for item, fractions := range got.Fractions {
		total := 0.0
		for _, fraction := range fractions {
			total += fraction.Share
		}
		if math.Abs(total-1) > 1e-6 {
			t.Fatalf("fractions for %s total %v", item, total)
		}
	}
}

func TestSolveReportsImprovementOverIncumbentOrdering(t *testing.T) {
	problem := Problem{Resources: []Resource{{ID: "a", Capacity: 1}, {ID: "b", Capacity: 1}}}
	for i := 1; i <= 6; i++ {
		problem.Items = append(problem.Items, WorkItem{ID: fmt.Sprintf("small-%d", i), Demand: 2})
	}
	for i := 1; i <= 3; i++ {
		problem.Items = append(problem.Items, WorkItem{ID: fmt.Sprintf("z-large-%d", i), Demand: 3})
	}
	got, err := Solve(problem)
	if err != nil {
		t.Fatal(err)
	}
	if got.BaselineMakespan != 12 || got.RoundedMakespan != 11 || got.Improvement <= 0 {
		t.Fatalf("measurement = %#v", got)
	}
}

func TestSolveMeasuresExactEstimatedCostWithProvenance(t *testing.T) {
	problem := Problem{
		CostModel: &CostModel{Currency: "USD", Source: "price-list-v7", AsOf: "2026-08-08T00:00:00Z"},
		Resources: []Resource{{ID: "a", Capacity: 1}, {ID: "b", Capacity: 1}},
		Items:     []WorkItem{{ID: "job", Demand: 1, EstimatedCostMicros: map[string]int64{"a": 500_001, "b": 200_003}}},
	}
	got, err := Solve(problem)
	if err != nil {
		t.Fatal(err)
	}
	if got.Assignments[0].Resource != "b" || got.BaselineEstimatedCostMicros == nil || *got.BaselineEstimatedCostMicros != 500_001 || got.RoundedEstimatedCostMicros == nil || *got.RoundedEstimatedCostMicros != 200_003 || got.EstimatedSavingsMicros == nil || *got.EstimatedSavingsMicros != 299_998 {
		t.Fatalf("cost measurement = %#v", got)
	}
	problem.CostModel.Currency = "EUR"
	problem.Items[0].EstimatedCostMicros[got.Assignments[0].Resource] = 1
	if got.CostModel == nil || got.CostModel.Currency != "USD" || *got.Assignments[0].EstimatedCostMicros != 200_003 {
		t.Fatalf("compiler-owned result aliases mutable problem input: %#v", got)
	}
}

func TestSolveDoesNotInventUnknownCost(t *testing.T) {
	problem := Problem{
		CostModel: &CostModel{Currency: "USD", Source: "price-list-v7", AsOf: "2026-08-08T00:00:00Z"},
		Resources: []Resource{{ID: "a", Capacity: 1}}, Items: []WorkItem{{ID: "job", Demand: 1}},
	}
	got, err := Solve(problem)
	if err != nil {
		t.Fatal(err)
	}
	if got.RoundedEstimatedCostMicros != nil {
		t.Fatalf("unknown cost reported as exact: %#v", got)
	}
}

func TestSolveRejectsInvalidProblems(t *testing.T) {
	cases := []Problem{
		{Items: []WorkItem{{ID: "x", Demand: 1}}},
		{Resources: []Resource{{ID: "r", Capacity: 1}}, Items: []WorkItem{{ID: "x", Demand: 0}}},
		{Resources: []Resource{{ID: "r", Capacity: 1}}, Items: []WorkItem{{ID: "x", Demand: 1, Eligible: []string{"missing"}}}},
	}
	for _, problem := range cases {
		if _, err := Solve(problem); err == nil {
			t.Fatalf("accepted invalid problem: %#v", problem)
		}
	}
}
