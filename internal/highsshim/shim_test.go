package highsshim

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	optimization "github.com/jonathanagustin/conduit/internal/optimization/solverprotocol"
)

type fakeEngine struct {
	results []EngineResult
	errors  []error
	models  []MatrixModel
	limits  []time.Duration
}

func (engine *fakeEngine) Solve(model MatrixModel, limit time.Duration) (EngineResult, error) {
	engine.models = append(engine.models, model)
	engine.limits = append(engine.limits, limit)
	index := len(engine.models) - 1
	if index < len(engine.errors) && engine.errors[index] != nil {
		return EngineResult{}, engine.errors[index]
	}
	if index >= len(engine.results) {
		return EngineResult{}, errors.New("unexpected fake solve")
	}
	return engine.results[index], nil
}

func TestStatusMappingCoversEveryHiGHS1151Status(t *testing.T) {
	request := testRequest(t, false)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	want := []optimization.SolverTermination{
		optimization.SolverError, optimization.SolverError, optimization.SolverError, optimization.SolverError,
		optimization.SolverError, optimization.SolverError, optimization.SolverError, optimization.SolverOptimal,
		optimization.SolverError, optimization.SolverError, optimization.SolverError, optimization.SolverFeasible,
		optimization.SolverFeasible, optimization.SolverTimeLimit, optimization.SolverFeasible, optimization.SolverFeasible,
		optimization.SolverFeasible, optimization.SolverFeasible, optimization.SolverError, optimization.SolverFeasible,
	}
	for status := 0; status <= 19; status++ {
		t.Run(statusName(status), func(t *testing.T) {
			raw := primaryResult(compiled, status, 0)
			engine := &fakeEngine{results: []EngineResult{raw}}
			response := solve(request, compiled, engine)
			if response.Termination != want[status] {
				t.Fatalf("status %d mapped to %s, want %s", status, response.Termination, want[status])
			}
			if status == 8 || status == 9 || status == 10 || status == 18 {
				if response.Assignments != nil {
					t.Fatalf("fail-closed status %d retained an incumbent", status)
				}
			}
			if response.BackendReportedEvidence == nil || response.BackendReportedEvidence.Status != statusName(status) {
				t.Fatalf("raw status not retained: %#v", response.BackendReportedEvidence)
			}
		})
	}
}

func TestTimeLimitAndHighsInterruptDistinguishIncumbentPresence(t *testing.T) {
	request := testRequest(t, false)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct {
		name        string
		status      int
		withPrimal  bool
		termination optimization.SolverTermination
	}{
		{name: "time limit without incumbent", status: 13, termination: optimization.SolverTimeLimit},
		{name: "time limit with incumbent", status: 13, withPrimal: true, termination: optimization.SolverTimeLimit},
		{name: "HiGHS interrupt without incumbent", status: 19, termination: optimization.SolverUnknown},
		{name: "HiGHS interrupt with incumbent", status: 19, withPrimal: true, termination: optimization.SolverFeasible},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			raw := EngineResult{ModelStatus: testCase.status}
			if testCase.withPrimal {
				raw = primaryResult(compiled, testCase.status, 0)
			}
			response := solve(request, compiled, &fakeEngine{results: []EngineResult{raw}})
			if response.Termination != testCase.termination || (response.Assignments != nil) != testCase.withPrimal {
				t.Fatalf("unexpected incumbent mapping: %#v", response)
			}
			if response.Termination == optimization.SolverOptimal {
				t.Fatal("interrupt or time limit became optimal")
			}
		})
	}
}

func TestAmbiguousOrIncompleteBinaryValuesFailClosed(t *testing.T) {
	request := testRequest(t, false)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []float64{0.5, math.NaN(), 0} {
		raw := primaryResult(compiled, 7, 0)
		raw.Columns[0] = value
		response := solve(request, compiled, &fakeEngine{results: []EngineResult{raw}})
		if response.Termination != optimization.SolverError || response.Assignments != nil || response.BackendReportedEvidence == nil {
			t.Fatalf("ambiguous value %v did not fail closed: %#v", value, response)
		}
	}
}

func TestTwoPhaseCostPreservesPrimaryAndEmitsExactScopedProofs(t *testing.T) {
	request := testRequest(t, true)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	primary := primaryResult(compiled, 7, assignmentColumn(t, compiled, "job", "a"))
	secondary := primaryResult(compiled, 7, assignmentColumn(t, compiled, "job", "b"))
	secondary.ObjectiveValue, secondary.BestBound, secondary.Gap = 0, 0, 0
	engine := &fakeEngine{results: []EngineResult{primary, secondary}}
	response := solve(request, compiled, engine)
	if response.Termination != optimization.SolverOptimal || response.Assignments == nil || len(*response.Assignments) != 1 || (*response.Assignments)[0].Resource != "b" {
		t.Fatalf("unexpected secondary optimum: %#v", response)
	}
	proofs := response.BackendReportedEvidence.Objectives
	want := []optimization.SolverObjectiveProof{
		{Objective: "minimize_makespan", Unit: "normalized_load_ratio", Incumbent: "1", BestBound: "1", Gap: "0"},
		{Objective: "minimize_estimated_cost", Unit: "micros", Incumbent: "200003", BestBound: "200003", Gap: "0"},
	}
	if !reflect.DeepEqual(proofs, want) {
		t.Fatalf("wrong exact objective proofs:\n got %#v\nwant %#v", proofs, want)
	}
	if len(engine.models) != 2 || engine.models[1].RowUpper[len(engine.models[1].RowUpper)-1] != math.Nextafter(1, math.Inf(1)) {
		t.Fatalf("secondary model did not bind primary optimum: %#v", engine.models)
	}
	if engine.models[0].ColumnCost[len(engine.models[0].ColumnCost)-1] != 1 || engine.models[1].ColumnCost[assignmentColumn(t, compiled, "job", "a")] != 1 {
		t.Fatalf("phase objectives were not switched: %#v", engine.models)
	}
}

func TestTwoPhasePrimaryMakespanDominatesCheaperSlowerChoice(t *testing.T) {
	base := testRequest(t, true)
	recommendation := base.Recommendation
	recommendation.Problem.Resources[0].Capacity = 2
	request := newProtocolRequest(t, recommendation, base.Selection, base.Determinism, base.Limits)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	primary := primaryResult(compiled, 7, assignmentColumn(t, compiled, "job", "a"))
	primary.Columns[len(primary.Columns)-1], primary.ObjectiveValue, primary.BestBound = 0.5, 0.5, 0.5
	secondary := primaryResult(compiled, 7, assignmentColumn(t, compiled, "job", "b"))
	secondary.ObjectiveValue, secondary.BestBound = 0, 0
	engine := &fakeEngine{results: []EngineResult{primary, secondary}}
	response := solve(request, compiled, engine)
	if response.Termination != optimization.SolverError || response.Assignments != nil {
		t.Fatalf("cheaper but primary-worse allocation escaped the lexicographic bound: %#v", response)
	}
	if got, want := engine.models[1].RowUpper[len(engine.models[1].RowUpper)-1], math.Nextafter(0.5, math.Inf(1)); got != want {
		t.Fatalf("secondary primary bound = %v, want %v", got, want)
	}
}

func TestSecondaryTimeoutWithoutIncumbentPreservesPrimaryProofAndChoice(t *testing.T) {
	request := testRequest(t, true)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	primary := primaryResult(compiled, 7, assignmentColumn(t, compiled, "job", "a"))
	secondary := EngineResult{ModelStatus: 13, PrimalSolutionStatus: 0}
	response := solve(request, compiled, &fakeEngine{results: []EngineResult{primary, secondary}})
	if response.Termination != optimization.SolverTimeLimit || response.Assignments == nil || (*response.Assignments)[0].Resource != "a" {
		t.Fatalf("primary incumbent was not preserved: %#v", response)
	}
	if len(response.BackendReportedEvidence.Objectives) != 1 || response.BackendReportedEvidence.Objectives[0].Objective != "minimize_makespan" {
		t.Fatalf("primary proof prefix was not preserved: %#v", response.BackendReportedEvidence)
	}
}

func TestSecondaryFailClosedStatusCannotLeakPrimaryProofWithoutResult(t *testing.T) {
	request := testRequest(t, true)
	compiled, err := optimization.CompileHiGHSModel(request)
	if err != nil {
		t.Fatal(err)
	}
	primary := primaryResult(compiled, 7, assignmentColumn(t, compiled, "job", "a"))
	secondary := EngineResult{ModelStatus: 18, PrimalSolutionStatus: 0}
	response := solve(request, compiled, &fakeEngine{results: []EngineResult{primary, secondary}})
	if response.Termination != optimization.SolverError || response.Assignments != nil || len(response.BackendReportedEvidence.Objectives) != 0 {
		t.Fatalf("fail-closed secondary status leaked incumbent evidence: %#v", response)
	}
}

func TestRunStrictInputAndNoMalformedOutputLeakage(t *testing.T) {
	request := testRequest(t, false)
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	unknown := append([]byte(nil), encoded[:len(encoded)-1]...)
	unknown = append(unknown, []byte(`,"secret":"do-not-copy"}`)...)
	for _, testCase := range []struct {
		name  string
		input []byte
	}{
		{name: "unknown field", input: unknown},
		{name: "trailing document", input: append(append([]byte(nil), encoded...), []byte(` {}`)...)},
		{name: "malformed", input: []byte(`{"apiVersion":`)},
		{name: "oversized", input: []byte(strings.Repeat(" ", maximumInputBytes+1))},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var output bytes.Buffer
			if err := Run(bytes.NewReader(testCase.input), &output, &fakeEngine{}); err == nil {
				t.Fatal("invalid input unexpectedly succeeded")
			}
			if output.Len() != 0 {
				t.Fatalf("invalid input leaked output %q", output.String())
			}
		})
	}
}

func TestRunEngineFailureEmitsOnlyBoundedGenericEnvelope(t *testing.T) {
	request := testRequest(t, false)
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	secret := "authorization=super-secret"
	engine := &fakeEngine{results: []EngineResult{{}}, errors: []error{errors.New(secret)}}
	var output bytes.Buffer
	if err := Run(bytes.NewReader(encoded), &output, engine); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), secret) || strings.Contains(output.String(), "authorization") {
		t.Fatalf("engine diagnostic leaked to stdout: %s", output.String())
	}
	var response optimization.ExternalSolverProcessResponse
	if err := json.Unmarshal(output.Bytes(), &response); err != nil || response.Termination != optimization.SolverError || response.Explanation != "HiGHS engine failed" {
		t.Fatalf("unexpected generic error envelope: err=%v response=%#v", err, response)
	}
}

func testRequest(t *testing.T, cost bool) optimization.SolverRequest {
	t.Helper()
	item := optimization.WorkItem{ID: "job", Demand: 1, Eligible: []string{"a", "b"}}
	problem := optimization.Problem{
		Items: []optimization.WorkItem{item}, Resources: []optimization.Resource{{ID: "a", Capacity: 1}, {ID: "b", Capacity: 1}},
		MakespanUnit: "seconds",
	}
	secondary := []string(nil)
	if cost {
		problem.Items[0].EstimatedCostMicros = map[string]int64{"a": 500001, "b": 200003}
		problem.CostModel = &optimization.CostModel{Currency: "USD", Source: "fixture", AsOf: "2026-08-11T00:00:00Z"}
		secondary = []string{"minimize_estimated_cost"}
	}
	recommendation := optimization.RecommendationRequest{
		APIVersion: optimization.EvidenceAPIVersion, Kind: "RecommendationRequest",
		Snapshot:  optimization.Snapshot{ID: "snap_" + strings.Repeat("0", 64), Source: "fixture", Revision: "v1", CapturedAt: "2026-08-11T00:00:00Z"},
		Objective: optimization.Objective{Primary: "minimize_makespan", Secondary: secondary}, Problem: problem,
	}
	return newProtocolRequest(
		t, recommendation, optimization.SolverSelection{PreferredBackend: "highs"},
		optimization.SolverDeterminism{Seed: 0, Workers: 1},
		optimization.SolverLimits{TimeLimitMilliseconds: 30_000, MemoryLimitBytes: 1 << 30},
	)
}

func newProtocolRequest(
	t *testing.T,
	recommendation optimization.RecommendationRequest,
	selection optimization.SolverSelection,
	determinism optimization.SolverDeterminism,
	limits optimization.SolverLimits,
) optimization.SolverRequest {
	t.Helper()
	request, err := optimization.NewSolverRequest(recommendation, selection, determinism, limits)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func primaryResult(compiled optimization.HiGHSCompiledModel, status, selected int) EngineResult {
	values := make([]float64, len(compiled.Columns))
	values[selected] = 1
	values[len(values)-1] = 1
	return EngineResult{
		ModelStatus: status, PrimalSolutionStatus: primalFeasibleStatus,
		ObjectiveValue: 1, BestBound: 1, Gap: 0, Columns: values,
	}
}

func assignmentColumn(t *testing.T, compiled optimization.HiGHSCompiledModel, item, resource string) int {
	t.Helper()
	for index, column := range compiled.Columns {
		if column.Item == item && column.Resource == resource {
			return index
		}
	}
	t.Fatalf("assignment column %s -> %s not found", item, resource)
	return -1
}
