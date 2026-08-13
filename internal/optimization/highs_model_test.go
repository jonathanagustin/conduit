package optimization

import (
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestCompileHiGHSModelBuildsCanonicalScaledMatrix(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs"})
	model, err := CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	if model.APIVersion != HiGHSCompiledModelAPIVersion || model.RequestID != request.ID || model.ModelDigest != request.Model.Digest || model.Seed != 0 || model.Workers != 1 {
		t.Fatalf("compiled model binding/determinism = %#v", model)
	}
	wantColumns := []HiGHSColumn{
		{Name: "x[native,amd64]", Kind: HiGHSBinaryColumn, Item: "native", Resource: "amd64", UpperBound: 1},
		{Name: "x[portable,amd64]", Kind: HiGHSBinaryColumn, Item: "portable", Resource: "amd64", UpperBound: 1},
		{Name: "x[portable,arm64]", Kind: HiGHSBinaryColumn, Item: "portable", Resource: "arm64", UpperBound: 1},
		{Name: "x[small,amd64]", Kind: HiGHSBinaryColumn, Item: "small", Resource: "amd64", UpperBound: 1},
		{Name: "x[small,arm64]", Kind: HiGHSBinaryColumn, Item: "small", Resource: "arm64", UpperBound: 1},
	}
	if !reflect.DeepEqual(model.Columns[:len(wantColumns)], wantColumns) || model.Columns[len(wantColumns)].Name != "T" || model.Columns[len(wantColumns)].Kind != HiGHSContinuousColumn {
		t.Fatalf("canonical columns = %#v", model.Columns)
	}
	if len(model.Rows) != 5 || model.Rows[0].Name != "assign[native]" || model.Rows[3].Name != "capacity[amd64]" || model.Rows[4].Name != "capacity[arm64]" {
		t.Fatalf("canonical rows = %#v", model.Rows)
	}
	wantAMD64 := []HiGHSCoefficient{{Column: 0, Value: 0.875}, {Column: 1, Value: 1}, {Column: 3, Value: 0.75}, {Column: 5, Value: -0.125}}
	wantARM64 := []HiGHSCoefficient{{Column: 2, Value: 1}, {Column: 4, Value: 0.75}, {Column: 5, Value: -0.125}}
	if !reflect.DeepEqual(model.Rows[3].Coefficients, wantAMD64) || !reflect.DeepEqual(model.Rows[4].Coefficients, wantARM64) {
		t.Fatalf("scaled capacity rows = %#v / %#v", model.Rows[3], model.Rows[4])
	}
	if got, want := model.ConstructiveMakespanUpperBound, math.Nextafter(13, math.Inf(1)); got != want || model.Columns[5].UpperBound != want {
		t.Fatalf("constructive upper = %.17g, want %.17g", got, want)
	}
	if len(model.Objectives) != 1 || model.Objectives[0].Name != "minimize_makespan" || model.Objectives[0].Unit != "normalized_load_ratio" || !reflect.DeepEqual(model.Objectives[0].Coefficients, []HiGHSCoefficient{{Column: 5, Value: 1}}) {
		t.Fatalf("primary objective = %#v", model.Objectives)
	}
}

func TestCompileHiGHSModelNormalizesExactSecondaryCosts(t *testing.T) {
	request := highsCostRequest(t, []WorkItem{
		{ID: "a", Demand: 1, EstimatedCostMicros: map[string]int64{"r1": 9_000_000_000_000_006, "r2": 9_000_000_000_000_000}},
		{ID: "b", Demand: 1, EstimatedCostMicros: map[string]int64{"r1": 8_000_000_000_000_009, "r2": 8_000_000_000_000_000}},
	})
	model, err := CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	want := &HiGHSCostNormalization{
		PerItemBaseMicros:       []HiGHSCostBase{{Item: "a", BaseMicros: 9_000_000_000_000_000}, {Item: "b", BaseMicros: 8_000_000_000_000_000}},
		OffsetMicros:            17_000_000_000_000_000,
		GlobalPositiveGCDMicros: 3,
		WorstCaseNormalized:     5,
	}
	if !reflect.DeepEqual(model.CostNormalization, want) {
		t.Fatalf("cost normalization = %#v, want %#v", model.CostNormalization, want)
	}
	if len(model.Objectives) != 2 || model.Objectives[1].Name != "minimize_estimated_cost" || model.Objectives[1].Unit != "normalized_exact_micros" {
		t.Fatalf("secondary objective = %#v", model.Objectives)
	}
	if got := model.Objectives[1].Coefficients; !reflect.DeepEqual(got, []HiGHSCoefficient{{Column: 0, Value: 2}, {Column: 2, Value: 3}}) {
		t.Fatalf("normalized cost coefficients = %#v", got)
	}
}

func TestCompileHiGHSModelExactIntegerBoundary(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		first     int64
		wantError bool
	}{
		{name: "cap", first: maximumHiGHSExactInteger - 1},
		{name: "cap plus one", first: maximumHiGHSExactInteger, wantError: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := highsCostRequest(t, []WorkItem{
				{ID: "a", Demand: 1, EstimatedCostMicros: map[string]int64{"r1": testCase.first, "r2": 0}},
				{ID: "b", Demand: 1, EstimatedCostMicros: map[string]int64{"r1": 1, "r2": 0}},
			})
			model, err := CompileHiGHSModel(request)
			if testCase.wantError {
				if err == nil || !strings.Contains(err.Error(), "exact binary64 integer cap") {
					t.Fatalf("cap+1 error = %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if model.CostNormalization.WorstCaseNormalized != maximumHiGHSExactInteger {
				t.Fatalf("boundary = %d", model.CostNormalization.WorstCaseNormalized)
			}
		})
	}
}

func TestCompileHiGHSModelRejectsUnsupportedPreflight(t *testing.T) {
	t.Run("missing eligible edge cost", func(t *testing.T) {
		request := highsCostRequest(t, []WorkItem{{ID: "a", Demand: 1, EstimatedCostMicros: map[string]int64{"r1": 1}}})
		if _, err := CompileHiGHSModel(request); err == nil || !strings.Contains(err.Error(), "lacks price") {
			t.Fatalf("missing cost error = %v", err)
		}
	})
	t.Run("normalized coefficient omission threshold", func(t *testing.T) {
		recommendation := testRecommendationRequest()
		recommendation.Problem = Problem{Resources: []Resource{{ID: "r", Capacity: 1e12}}, Items: []WorkItem{{ID: "i", Demand: 1}}}
		request, err := NewSolverRequest(recommendation, SolverSelection{PreferredBackend: "highs"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := CompileHiGHSModel(request); err == nil || !strings.Contains(err.Error(), "at or below") {
			t.Fatalf("small coefficient error = %v", err)
		}
	})
	for _, determinism := range []SolverDeterminism{{Seed: 1, Workers: 1}, {Seed: 0, Workers: 2}} {
		t.Run("determinism", func(t *testing.T) {
			request, err := NewSolverRequest(testRecommendationRequest(), SolverSelection{PreferredBackend: "highs"}, determinism, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := CompileHiGHSModel(request); err == nil || !strings.Contains(err.Error(), "seed 0 and one worker") {
				t.Fatalf("determinism error = %v", err)
			}
		})
	}
}

func highsCostRequest(t *testing.T, items []WorkItem) SolverRequest {
	t.Helper()
	recommendation := testRecommendationRequest()
	recommendation.Objective.Secondary = []string{"minimize_estimated_cost"}
	recommendation.Problem = Problem{
		CostModel: &CostModel{Currency: "USD", Source: "fixture", AsOf: "2026-08-11T18:00:00Z"},
		Resources: []Resource{{ID: "r1", Capacity: 1}, {ID: "r2", Capacity: 1}}, Items: items,
	}
	request, err := NewSolverRequest(recommendation, SolverSelection{PreferredBackend: "highs"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return request
}
