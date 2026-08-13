package optimization

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
)

func TestCompileCpSatModelIsDeterministicExactAndExtractsOnlyChoices(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	compiled, err := compileCpSatModel(request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := compileCpSatModel(request)
	if err != nil {
		t.Fatal(err)
	}
	if len(compiled.Model) == 0 || len(compiled.Parameters) == 0 || !bytes.Equal(compiled.Model, replayed.Model) || !bytes.Equal(compiled.Parameters, replayed.Parameters) {
		t.Fatal("CP-SAT compilation is empty or nondeterministic")
	}
	if compiled.Phase != (cpSatPhaseDescriptor{Index: 0, Kind: "OBJECTIVE", Key: "minimize_makespan", Unit: "seconds"}) || !reflect.DeepEqual(compiled.ObjectiveCoefficients, []int64{1}) {
		t.Fatalf("first exact phase = %#v", compiled)
	}
	for _, field := range []protowire.Number{31, 36, 40, 159, 160, 206} {
		if countProtoFields(compiled.Parameters, field) != 1 {
			t.Fatalf("CP-SAT parameter field %d was not set exactly once", field)
		}
	}
	if countProtoFields(compiled.Parameters, 100) != 0 {
		t.Fatal("used deprecated num_search_workers instead of num_workers")
	}

	solution := make([]int64, compiled.VariableCount)
	want := map[string]DiscreteChoice{
		"prepare":   {Task: "prepare", Lane: "lane-a", Start: 0},
		"rollout-a": {Task: "rollout-a", Lane: "lane-a", Start: 4},
		"rollout-b": {Task: "rollout-b", Lane: "lane-b", Start: 7},
	}
	for _, binding := range compiled.Bindings {
		choice := want[binding.Task]
		solution[binding.Start] = choice.Start
		task := discreteTaskByID(request.Recommendation.Problem.Tasks, binding.Task)
		solution[binding.End] = choice.Start + task.DurationSeconds
		solution[binding.LaneBooleans[choice.Lane]] = 1
	}
	solution[compiled.MakespanVariable] = 10
	bound := int64(314)
	encoded := encodeTestCpSatResponse(cpSatStatusOptimal, solution, &bound)
	response, err := parseCpSatResponse(encoded, compiled.VariableCount)
	if err != nil {
		t.Fatal(err)
	}
	choices, err := choicesFromCpSatResponse(compiled, response)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewDiscreteScheduleResult(request.Recommendation.Problem, choices)
	if err != nil {
		t.Fatal(err)
	}
	if result.MakespanSeconds != 10 || result.EstimatedTotalCostMicros == nil || *result.EstimatedTotalCostMicros != 24 {
		t.Fatalf("extracted schedule = %#v", result)
	}

	for _, binding := range compiled.Bindings {
		if len(binding.LaneBooleans) > 1 {
			for _, variable := range binding.LaneBooleans {
				solution[variable] = 1
			}
			break
		}
	}
	if _, err := choicesFromCpSatResponse(compiled, cpSatResponse{Status: cpSatStatusFeasible, Solution: solution}); err == nil || !strings.Contains(err.Error(), "multiple lanes") {
		t.Fatalf("multiple lane extraction error = %v", err)
	}
}

func TestParseCpSatResponseFailsClosedOnMalformedOrOversizedFields(t *testing.T) {
	bound := int64(0)
	valid := encodeTestCpSatResponse(cpSatStatusInfeasible, nil, &bound)
	if response, err := parseCpSatResponse(valid, 1); err != nil || response.Status != cpSatStatusInfeasible || response.InnerObjectiveLowerBound == nil {
		t.Fatalf("valid response = %#v / %v", response, err)
	}
	unknownDefault := appendFixed64Field(nil, 15, 0)
	if response, err := parseCpSatResponse(unknownDefault, 1); err != nil || response.Status != cpSatStatusUnknown {
		t.Fatalf("proto-default UNKNOWN response = %#v / %v", response, err)
	}

	tests := []struct {
		name    string
		encoded []byte
		maximum int
	}{
		{"empty", nil, 1},
		{"bad status", appendVarintField(nil, 1, 9), 1},
		{"duplicate status", append(appendVarintField(nil, 1, 2), appendVarintField(nil, 1, 2)...), 1},
		{"truncated", []byte{0x12, 0x02, 0x01}, 1},
		{"too many values", encodeTestCpSatResponse(cpSatStatusFeasible, []int64{0, 1}, nil), 1},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := parseCpSatResponse(testCase.encoded, testCase.maximum); err == nil {
				t.Fatal("accepted malformed CP-SAT response")
			}
		})
	}
}

func TestCpSatSemanticDigestBindsPhaseAndExcludesVolatileSolverFields(t *testing.T) {
	base := encodeTestCpSatResponse(cpSatStatusFeasible, []int64{1, 2, 3}, pointerToInt64(4))
	leftRaw := append(append([]byte(nil), base...), appendFixed64Field(nil, 15, 1)...)
	rightRaw := append(encodeTestCpSatResponse(cpSatStatusFeasible, []int64{9, 8, 7}, pointerToInt64(4)), appendFixed64Field(nil, 15, 2)...)
	if digestBytes(leftRaw) == digestBytes(rightRaw) {
		t.Fatal("raw timing mutation did not change raw protobuf digest")
	}
	if _, err := parseCpSatResponse(leftRaw, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := parseCpSatResponse(rightRaw, 3); err != nil {
		t.Fatal(err)
	}
	proof := DiscreteCpSatPhaseProof{Index: 0, Kind: "OBJECTIVE", Key: "minimize_makespan", Unit: "seconds", ModelDigest: testExecutableDigest, ParametersDigest: testExecutableDigest, Status: "FEASIBLE", Incumbent: "3", BestBound: "2", Gap: "1"}
	leftDigest := cpSatPhaseSemanticDigest("dsolreq_"+strings.Repeat("a", 64), proof)
	rightDigest := cpSatPhaseSemanticDigest("dsolreq_"+strings.Repeat("a", 64), proof)
	if leftDigest != rightDigest {
		t.Fatalf("volatile timing changed semantic digest: %s != %s", leftDigest, rightDigest)
	}
	proof.Key = "different"
	if cpSatPhaseSemanticDigest("dsolreq_"+strings.Repeat("a", 64), proof) == leftDigest {
		t.Fatal("phase descriptor mutation did not change semantic digest")
	}
}

func TestCompileCpSatModelRejectsUnsafeEnvelopeWithoutCompositeOverflow(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	request.Determinism.Seed = 1
	request.ID, _ = discreteSolverRequestID(request)
	if _, err := compileCpSatModel(request); err == nil || !strings.Contains(err.Error(), "seed zero") {
		t.Fatalf("seed bound error = %v", err)
	}

	recommendation := testDiscreteRecommendation()
	for index := range recommendation.Problem.Tasks {
		for lane := range recommendation.Problem.Tasks[index].EstimatedCostMicros {
			recommendation.Problem.Tasks[index].EstimatedCostMicros[lane] = maximumDiscreteQuantity
		}
	}
	request, err := NewDiscreteSolverRequest(recommendation, SolverSelection{PreferredBackend: "cp-sat"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	planner, err := newCpSatPhasePlanner(request)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := planner.compileNextPhase()
	if err != nil || compiled.Phase.Key != "minimize_makespan" {
		t.Fatalf("large first phase = %#v / %v", compiled.Phase, err)
	}

	recommendation.Problem.HorizonSeconds = maximumDiscreteHorizonSeconds
	request, err = NewDiscreteSolverRequest(recommendation, SolverSelection{PreferredBackend: "cp-sat"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	planner, err = newCpSatPhasePlanner(request)
	if err != nil {
		t.Fatalf("iterative exact phases rejected valid bounded request: %v", err)
	}
	compiled, err = planner.compileNextPhase()
	if err != nil {
		t.Fatal(err)
	}
	if err := planner.acceptExactPhase(compiled, 10); err != nil {
		t.Fatal(err)
	}
	compiled, err = planner.compileNextPhase()
	if err != nil || compiled.Phase.Key != "minimize_estimated_cost" {
		t.Fatalf("large cost phase = %#v / %v", compiled.Phase, err)
	}

	builder := &cpSatProtoBuilder{}
	builder.addVariable(0, maximumCpSatIntegerMagnitude+1)
	if builder.err == nil || !strings.Contains(builder.err.Error(), "variable domain") {
		t.Fatalf("unsafe variable domain error = %v", builder.err)
	}
}

func TestCpSatPhasePlannerUsesOrderedObjectivesThenCanonicalLaneAndStart(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	planner, err := newCpSatPhasePlanner(request)
	if err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"minimize_makespan", "minimize_estimated_cost", "prepare", "rollout-a", "rollout-b"}
	for index, wantKey := range wantKeys {
		compiled, err := planner.compileNextPhase()
		if err != nil {
			t.Fatal(err)
		}
		if compiled.Phase.Index != index || compiled.Phase.Key != wantKey {
			t.Fatalf("phase %d = %#v", index, compiled.Phase)
		}
		value := int64(10 + index)
		if compiled.Phase.Kind == "CANONICAL_CHOICE" {
			if compiled.ObjectiveCoefficients[0] != 1 {
				t.Fatalf("canonical start coefficient = %d", compiled.ObjectiveCoefficients[0])
			}
			for rank, coefficient := range compiled.ObjectiveCoefficients[1:] {
				if coefficient != int64(rank)*(request.Recommendation.Problem.HorizonSeconds+1) {
					t.Fatalf("canonical lane coefficient %d = %d", rank, coefficient)
				}
			}
		}
		if err := planner.acceptExactPhase(compiled, value); err != nil {
			t.Fatal(err)
		}
	}
	if !planner.complete() {
		t.Fatal("exact phase plan did not complete")
	}
}

func TestCompileCpSatModelBoundsTopologyExpansionWithoutDependingOnEmittedConstraints(t *testing.T) {
	recommendation := testDiscreteRecommendation()
	recommendation.Problem.Precedences = nil
	recommendation.Problem.ConcurrencyRules = nil
	recommendation.Problem.MaintenanceWindows = nil
	recommendation.Problem.Lanes = nil
	eligible := make([]string, 0, maximumDiscreteLanes)
	costs := make(map[string]int64, maximumDiscreteLanes)
	for index := 0; index < maximumDiscreteLanes; index++ {
		laneID := fmt.Sprintf("lane-%03d", index)
		eligible = append(eligible, laneID)
		costs[laneID] = 1
		recommendation.Problem.Lanes = append(recommendation.Problem.Lanes, DiscreteLane{
			ID: laneID, Capacity: map[string]int64{"cpu": 1, "slots": 1}, Topology: map[string]string{"zone": "same-zone"},
		})
	}
	recommendation.Problem.Tasks = []DiscreteTask{
		{ID: "task-a", DurationSeconds: 1, EarliestStart: 0, LatestFinish: 20, EligibleLanes: eligible, Demand: map[string]int64{"cpu": 1, "slots": 1}, EstimatedCostMicros: costs},
		{ID: "task-b", DurationSeconds: 1, EarliestStart: 0, LatestFinish: 20, EligibleLanes: eligible, Demand: map[string]int64{"cpu": 1, "slots": 1}, EstimatedCostMicros: costs},
	}
	recommendation.Problem.TopologyRules = nil
	for index := 0; index < 123; index++ {
		recommendation.Problem.TopologyRules = append(recommendation.Problem.TopologyRules, DiscreteTopologyRule{
			ID: fmt.Sprintf("rule-%03d", index), Tasks: []string{"task-a", "task-b"}, Key: "zone", Relation: "SAME",
		})
	}
	request, err := NewDiscreteSolverRequest(recommendation, SolverSelection{PreferredBackend: "cp-sat"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := compileCpSatModel(request); err == nil || !strings.Contains(err.Error(), "topology expansion") {
		t.Fatalf("unbounded topology expansion error = %v", err)
	}
}

func encodeTestCpSatResponse(status int32, solution []int64, lowerBound *int64) []byte {
	encoded := appendVarintField(nil, 1, uint64(status))
	encoded = appendPackedInt64(encoded, 2, solution)
	if lowerBound != nil {
		encoded = appendVarintField(encoded, 29, uint64(*lowerBound))
	}
	return encoded
}

func discreteTaskByID(tasks []DiscreteTask, id string) DiscreteTask {
	for _, task := range tasks {
		if task.ID == id {
			return task
		}
	}
	return DiscreteTask{}
}

func countProtoFields(encoded []byte, field protowire.Number) int {
	count := 0
	for len(encoded) > 0 {
		number, wireType, size := protowire.ConsumeTag(encoded)
		if size < 0 {
			return -1
		}
		encoded = encoded[size:]
		valueSize := protowire.ConsumeFieldValue(number, wireType, encoded)
		if valueSize < 0 {
			return -1
		}
		if number == field {
			count++
		}
		encoded = encoded[valueSize:]
	}
	return count
}
