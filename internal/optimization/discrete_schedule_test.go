package optimization

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestDiscreteSolverConformanceCorpus keeps the discrete backend's required
// constraint classes together as one replayable corpus. It intentionally
// exercises the compiler-owned native reference and explicit operational
// outcomes; an external candidate must consume the same requests and cannot
// turn this local corpus into admission evidence.
func TestDiscreteSolverConformanceCorpus(t *testing.T) {
	type corpusCase struct {
		name   string
		class  string
		mutate func(*DiscreteRecommendationRequest)
	}
	cases := []corpusCase{
		{name: "equality-and-precedence", class: "equality", mutate: func(*DiscreteRecommendationRequest) {}},
		{name: "architecture-and-failure-domain", class: "architecture", mutate: func(*DiscreteRecommendationRequest) {}},
		{name: "failure-domain", class: "failure-domain", mutate: func(*DiscreteRecommendationRequest) {}},
		{name: "maintenance-window", class: "maintenance", mutate: func(*DiscreteRecommendationRequest) {}},
		{name: "pdb-concurrency", class: "pdb", mutate: func(*DiscreteRecommendationRequest) {}},
		{name: "disk-pressure", class: "disk-pressure", mutate: func(value *DiscreteRecommendationRequest) {
			for index := range value.Problem.Lanes {
				value.Problem.Lanes[index].Capacity["disk"] = 10
			}
			for index := range value.Problem.Tasks {
				value.Problem.Tasks[index].Demand["disk"] = 1
			}
		}},
		{name: "exact-boundary", class: "boundary", mutate: func(value *DiscreteRecommendationRequest) {
			value.Problem.HorizonSeconds = 15
			value.Problem.Tasks[1].LatestFinish = 5
		}},
	}
	covered := map[string]bool{}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			recommendation := testDiscreteRecommendation()
			testCase.mutate(&recommendation)
			request, err := NewDiscreteSolverRequest(recommendation, SolverSelection{PreferredBackend: NativeBackendName}, SolverDeterminism{Seed: 0, Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: runtime.GOARCH}}, func() time.Time { return time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC) })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != SolverFeasible || receipt.Result == nil {
				t.Fatalf("corpus case returned %#v", receipt)
			}
			if err := VerifyDiscreteSolverReceipt(request, receipt); err != nil {
				t.Fatal(err)
			}
			covered[testCase.class] = true
		})
	}
	// Timeout and infeasible are explicit receipt classes, not malformed
	// requests. Keep them in this corpus without pretending a local fixture is
	// a production backend result.
	for _, testCase := range []struct {
		name        string
		termination SolverTermination
	}{
		{name: "timeout", termination: SolverTimeLimit},
		{name: "infeasible", termination: SolverInfeasible},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			recommendation := testDiscreteRecommendation()
			request, err := NewDiscreteSolverRequest(recommendation, SolverSelection{PreferredBackend: "corpus-" + testCase.name}, SolverDeterminism{Seed: 0, Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{discreteCorpusBackend{name: "corpus-" + testCase.name, termination: testCase.termination}}, func() time.Time { return time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC) })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != testCase.termination {
				t.Fatalf("termination = %s, want %s", receipt.Termination, testCase.termination)
			}
			if err := VerifyDiscreteSolverReceipt(request, receipt); err != nil {
				t.Fatal(err)
			}
			covered[testCase.name] = true
		})
	}
	for _, required := range []string{"equality", "architecture", "failure-domain", "maintenance", "pdb", "disk-pressure", "boundary", "timeout", "infeasible"} {
		if !covered[required] {
			t.Errorf("discrete conformance corpus lacks %s", required)
		}
	}
}

type discreteCorpusBackend struct {
	name        string
	termination SolverTermination
}

func (backend discreteCorpusBackend) Descriptor() SolverBackendDescriptor {
	return SolverBackendDescriptor{Name: backend.name, Version: "1.0.0", ExecutableDigest: testExecutableDigest, Architecture: "test", Capabilities: []string{DiscreteScheduleCapability}}
}

func (backend discreteCorpusBackend) SolveDiscrete(context.Context, DiscreteSolverRequest) (DiscreteBackendOutcome, error) {
	return DiscreteBackendOutcome{Termination: backend.termination, Explanation: "explicit conformance corpus outcome"}, nil
}

func TestDiscreteSolverRequestBindsExactIntegerDomainAndOwnsInputs(t *testing.T) {
	recommendation := testDiscreteRecommendation()
	request, err := NewDiscreteSolverRequest(recommendation, SolverSelection{PreferredBackend: "cp-sat"}, SolverDeterminism{Seed: 0, Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if request.APIVersion != DiscreteSolverAPIVersion || request.Model.Revision != DiscreteScheduleModelRevision || request.Capability != DiscreteScheduleCapability || !validContentID(request.ID, "dsolreq_") {
		t.Fatalf("request envelope = %#v", request)
	}
	if err := VerifyDiscreteSolverRequest(request); err != nil {
		t.Fatal(err)
	}

	// Construction canonicalizes and detaches every mutable caller field.
	recommendation.Problem.Tasks[0].EligibleLanes[0] = "mutated"
	recommendation.Problem.Tasks[0].Demand["slots"] = 99
	recommendation.Problem.Lanes[0].Topology["zone"] = "mutated"
	if err := VerifyDiscreteSolverRequest(request); err != nil {
		t.Fatalf("caller mutation changed request: %v", err)
	}

	mutations := []struct {
		name   string
		mutate func(*DiscreteSolverRequest)
	}{
		{"snapshot", func(value *DiscreteSolverRequest) { value.Recommendation.Snapshot.Revision = "foreign" }},
		{"model", func(value *DiscreteSolverRequest) { value.Model.Revision = "foreign" }},
		{"objective", func(value *DiscreteSolverRequest) { value.Recommendation.Objective.Secondary = nil }},
		{"seed", func(value *DiscreteSolverRequest) { value.Determinism.Seed++ }},
		{"limit", func(value *DiscreteSolverRequest) { value.Limits.MemoryLimitBytes++ }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := cloneSolverJSON(t, request)
			mutation.mutate(&candidate)
			if err := VerifyDiscreteSolverRequest(candidate); err == nil {
				t.Fatal("accepted request whose content no longer matches its identity")
			}
		})
	}
}

func TestDiscreteDigestPartitionBindsHardTimeAndObjectiveCostInputs(t *testing.T) {
	baseRecommendation := testDiscreteRecommendation()
	base, err := NewDiscreteSolverRequest(baseRecommendation, SolverSelection{PreferredBackend: "cp-sat"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}

	hardRecommendation := cloneSolverJSON(t, baseRecommendation)
	hardRecommendation.Problem.Origin = "2026-08-12T12:00:01Z"
	hardRecommendation.Problem.HorizonSeconds++
	hard, err := NewDiscreteSolverRequest(hardRecommendation, base.Selection, base.Determinism, base.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if hard.Model.HardConstraintsDigest == base.Model.HardConstraintsDigest {
		t.Fatal("hard digest omitted origin or horizon")
	}

	costRecommendation := cloneSolverJSON(t, baseRecommendation)
	costRecommendation.Problem.Tasks[0].EstimatedCostMicros["lane-a"]++
	cost, err := NewDiscreteSolverRequest(costRecommendation, base.Selection, base.Determinism, base.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if cost.Model.HardConstraintsDigest != base.Model.HardConstraintsDigest || cost.Model.ObjectiveDigest == base.Model.ObjectiveDigest {
		t.Fatalf("cost mutation crossed digest partitions: base=%#v cost=%#v", base.Model, cost.Model)
	}
}

func TestDiscreteMaintenanceCanonicalizationUsesEndTieBreaker(t *testing.T) {
	left := testDiscreteRecommendation()
	left.Problem.MaintenanceWindows = append(left.Problem.MaintenanceWindows, DiscreteMaintenanceWindow{Lane: "lane-a", Start: 2, End: 5})
	right := cloneSolverJSON(t, left)
	right.Problem.MaintenanceWindows[0], right.Problem.MaintenanceWindows[1] = right.Problem.MaintenanceWindows[1], right.Problem.MaintenanceWindows[0]
	leftRequest, err := NewDiscreteSolverRequest(left, SolverSelection{PreferredBackend: "cp-sat"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	rightRequest, err := NewDiscreteSolverRequest(right, leftRequest.Selection, leftRequest.Determinism, leftRequest.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if leftRequest.ID != rightRequest.ID {
		t.Fatalf("equal-start maintenance order changed request ID: %s != %s", leftRequest.ID, rightRequest.ID)
	}
}

func TestDiscreteResultDerivesAllNonChoiceFieldsAndVerifiesConstraints(t *testing.T) {
	problem := testDiscreteRecommendation().Problem
	choices := []DiscreteChoice{
		{Task: "prepare", Lane: "lane-a", Start: 0},
		{Task: "rollout-a", Lane: "lane-a", Start: 4},
		{Task: "rollout-b", Lane: "lane-b", Start: 7},
	}
	result, err := NewDiscreteScheduleResult(problem, choices)
	if err != nil {
		t.Fatal(err)
	}
	if result.MakespanSeconds != 10 || result.EstimatedTotalCostMicros == nil || *result.EstimatedTotalCostMicros != 24 || result.Assignments[1].Finish != 7 || result.Assignments[1].EstimatedCostMicros == nil || *result.Assignments[1].EstimatedCostMicros != 10 {
		t.Fatalf("derived result = %#v", result)
	}
	if err := VerifyDiscreteScheduleResult(problem, result); err != nil {
		t.Fatal(err)
	}

	mutations := []struct {
		name   string
		mutate func(*DiscreteScheduleResult)
	}{
		{"finish", func(value *DiscreteScheduleResult) { value.Assignments[1].Finish++ }},
		{"cost", func(value *DiscreteScheduleResult) { (*value.Assignments[1].EstimatedCostMicros)++ }},
		{"makespan", func(value *DiscreteScheduleResult) { value.MakespanSeconds-- }},
		{"total cost", func(value *DiscreteScheduleResult) { (*value.EstimatedTotalCostMicros)++ }},
		{"ineligible architecture", func(value *DiscreteScheduleResult) { value.Assignments[0].Lane = "lane-b" }},
		{"precedence", func(value *DiscreteScheduleResult) { value.Assignments[1].Start = 1; value.Assignments[1].Finish = 4 }},
		{"maintenance", func(value *DiscreteScheduleResult) { value.Assignments[1].Start = 2; value.Assignments[1].Finish = 5 }},
		{"topology", func(value *DiscreteScheduleResult) {
			value.Assignments[2].Lane = "lane-a"
			value.Assignments[2].EstimatedCostMicros = pointerToInt64(11)
		}},
		{"pdb concurrency", func(value *DiscreteScheduleResult) { value.Assignments[2].Start = 4; value.Assignments[2].Finish = 7 }},
		{"missing task", func(value *DiscreteScheduleResult) { value.Assignments = value.Assignments[:2] }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := cloneSolverJSON(t, result)
			mutation.mutate(&candidate)
			if err := VerifyDiscreteScheduleResult(problem, candidate); err == nil {
				t.Fatal("accepted constraint or derived-field mutation")
			}
		})
	}
	badChoices := cloneSolverJSON(t, choices)
	badChoices[0].Start = int64(^uint64(0) >> 1)
	if _, err := NewDiscreteScheduleResult(problem, badChoices); err == nil {
		t.Fatal("accepted overflowing provider-authored start")
	}
	capacityProblem := cloneSolverJSON(t, problem)
	capacityProblem.TopologyRules = nil
	capacityProblem.ConcurrencyRules = nil
	capacityChoices := []DiscreteChoice{
		{Task: "prepare", Lane: "lane-a", Start: 0},
		{Task: "rollout-a", Lane: "lane-b", Start: 4},
		{Task: "rollout-b", Lane: "lane-b", Start: 4},
	}
	if _, err := NewDiscreteScheduleResult(capacityProblem, capacityChoices); err == nil || !strings.Contains(err.Error(), "capacity") {
		t.Fatalf("capacity mutation error = %v", err)
	}
}

func TestDiscreteProblemFailsClosedBeforeBackendInvocation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DiscreteRecommendationRequest)
		want   string
	}{
		{"precedence cycle", func(value *DiscreteRecommendationRequest) {
			value.Problem.Precedences = append(value.Problem.Precedences, DiscretePrecedence{Before: "rollout-a", After: "prepare"})
		}, "cycle"},
		{"missing topology", func(value *DiscreteRecommendationRequest) { delete(value.Problem.Lanes[1].Topology, "zone") }, "topology evidence"},
		{"partial price", func(value *DiscreteRecommendationRequest) {
			delete(value.Problem.Tasks[2].EstimatedCostMicros, "lane-b")
		}, "every eligible"},
		{"impossible window", func(value *DiscreteRecommendationRequest) { value.Problem.Tasks[0].LatestFinish = 1 }, "window"},
		{"unknown dimension", func(value *DiscreteRecommendationRequest) { value.Problem.Tasks[0].Demand["memory"] = 1 }, "demand dimensions"},
		{"no-op pdb", func(value *DiscreteRecommendationRequest) {
			value.Problem.ConcurrencyRules[0].Maximum = len(value.Problem.ConcurrencyRules[0].Tasks)
		}, "concurrency rule"},
		{"topology cardinality", func(value *DiscreteRecommendationRequest) {
			for index := 0; index <= maximumDiscreteTopologyKeys; index++ {
				value.Problem.Lanes[0].Topology[fmt.Sprintf("key-%d", index)] = "value"
			}
		}, "topology"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			request := testDiscreteRecommendation()
			testCase.mutate(&request)
			_, err := NewDiscreteSolverRequest(request, SolverSelection{PreferredBackend: "cp-sat"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want %q", err, testCase.want)
			}
		})
	}
}

func testDiscreteRecommendation() DiscreteRecommendationRequest {
	return DiscreteRecommendationRequest{
		APIVersion: EvidenceAPIVersion, Kind: "DiscreteRecommendationRequest",
		Snapshot:    Snapshot{ID: testSnapshotID, Source: "test", Revision: "r1", CapturedAt: "2026-08-12T11:59:00Z"},
		Objective:   Objective{Primary: "minimize_makespan", Secondary: []string{"minimize_estimated_cost"}},
		Constraints: []ConstraintEvidence{{ID: "rollout-policy", Source: "compiler", Revision: "r1", ObservedAt: "2026-08-12T11:58:00Z"}},
		Problem: DiscreteScheduleProblem{
			Origin: "2026-08-12T12:00:00Z", TimeUnit: "seconds", HorizonSeconds: 20,
			Lanes: []DiscreteLane{
				{ID: "lane-b", Capacity: map[string]int64{"cpu": 2, "slots": 1}, Topology: map[string]string{"architecture": "arm64", "zone": "zone-b"}},
				{ID: "lane-a", Capacity: map[string]int64{"cpu": 2, "slots": 1}, Topology: map[string]string{"architecture": "amd64", "zone": "zone-a"}},
			},
			Tasks: []DiscreteTask{
				{ID: "rollout-b", DurationSeconds: 3, EarliestStart: 2, LatestFinish: 15, EligibleLanes: []string{"lane-b", "lane-a"}, Demand: map[string]int64{"cpu": 1, "slots": 1}, EstimatedCostMicros: map[string]int64{"lane-a": 11, "lane-b": 9}},
				{ID: "prepare", DurationSeconds: 2, EarliestStart: 0, LatestFinish: 5, EligibleLanes: []string{"lane-a"}, Demand: map[string]int64{"cpu": 1, "slots": 1}, EstimatedCostMicros: map[string]int64{"lane-a": 5}},
				{ID: "rollout-a", DurationSeconds: 3, EarliestStart: 2, LatestFinish: 15, EligibleLanes: []string{"lane-b", "lane-a"}, Demand: map[string]int64{"cpu": 1, "slots": 1}, EstimatedCostMicros: map[string]int64{"lane-a": 10, "lane-b": 12}},
			},
			Precedences: []DiscretePrecedence{{Before: "prepare", After: "rollout-a"}, {Before: "prepare", After: "rollout-b"}},
			TopologyRules: []DiscreteTopologyRule{
				{ID: "architecture", Tasks: []string{"prepare", "rollout-a"}, Key: "architecture", Relation: "SAME"},
				{ID: "failure-domain", Tasks: []string{"rollout-a", "rollout-b"}, Key: "zone", Relation: "DIFFERENT"},
			},
			ConcurrencyRules:   []DiscreteConcurrencyRule{{ID: "availability", Kind: "PDB_DISRUPTION", Tasks: []string{"rollout-a", "rollout-b"}, Maximum: 1}},
			MaintenanceWindows: []DiscreteMaintenanceWindow{{Lane: "lane-a", Start: 2, End: 4}},
			CostModel:          &CostModel{Currency: "USD", Source: "price-list", AsOf: "2026-08-12T11:55:00Z"},
		},
	}
}

func pointerToInt64(value int64) *int64 { return &value }
