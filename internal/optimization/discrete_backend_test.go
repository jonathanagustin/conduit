package optimization

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNativeDiscreteBackendProducesReplayableReceipt(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: NativeBackendName})
	started := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceDiscreteClock(started, started.Add(3*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverFeasible || receipt.Result == nil || receipt.Result.MakespanSeconds != 10 || receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != NativeBackendName {
		t.Fatalf("native discrete receipt = %#v", receipt)
	}
	if err := VerifyNativeDiscreteSolverReceipt(context.Background(), request, receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Result.EstimatedTotalCostMicros == nil || *receipt.Result.EstimatedTotalCostMicros != 24 {
		t.Fatalf("native cost result = %#v", receipt.Result)
	}
}

func TestDiscreteBackendFallbackAndTerminationStatesRemainDistinct(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat", AuthorizedFallbacks: []string{NativeBackendName}})
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "arm64"}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != NativeBackendName || receipt.FallbackFrom != "cp-sat" || !reflect.DeepEqual(receipt.AttemptedBackends, []string{"cp-sat", NativeBackendName}) {
		t.Fatalf("discrete fallback receipt = %#v", receipt)
	}

	request = testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	receipt, err = RunDiscreteSolverRequest(context.Background(), request, nil, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverUnknown || receipt.SelectedBackend != nil || receipt.Result != nil {
		t.Fatalf("unavailable discrete receipt = %#v", receipt)
	}

	choices := testDiscreteChoices()
	result, err := NewDiscreteScheduleResult(request.Recommendation.Problem, choices)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name        string
		outcome     DiscreteBackendOutcome
		termination SolverTermination
		wantResult  bool
	}{
		{"optimal", DiscreteBackendOutcome{Termination: SolverOptimal, Choices: choices, BackendReportedEvidence: testDiscreteOptimalEvidence(t, request, result)}, SolverOptimal, true},
		{"time limit incumbent", DiscreteBackendOutcome{Termination: SolverTimeLimit, Choices: choices, Explanation: "limit after incumbent"}, SolverTimeLimit, true},
		{"time limit unknown", DiscreteBackendOutcome{Termination: SolverTimeLimit, Explanation: "limit before incumbent"}, SolverTimeLimit, false},
		{"infeasible", DiscreteBackendOutcome{Termination: SolverInfeasible, Explanation: "bounded model proven infeasible"}, SolverInfeasible, false},
		{"unknown", DiscreteBackendOutcome{Termination: SolverUnknown, Explanation: "search stopped without classification"}, SolverUnknown, false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{fixtureDiscreteBackend{outcome: testCase.outcome}}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != testCase.termination || (receipt.Result != nil) != testCase.wantResult {
				t.Fatalf("termination receipt = %#v", receipt)
			}
		})
	}
}

func TestDiscreteReceiptRejectsProofResultAndAuthorityTampering(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	result, err := NewDiscreteScheduleResult(request.Recommendation.Problem, testDiscreteChoices())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{fixtureDiscreteBackend{outcome: DiscreteBackendOutcome{Termination: SolverOptimal, Choices: testDiscreteChoices(), BackendReportedEvidence: testDiscreteOptimalEvidence(t, request, result)}}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*DiscreteSolverReceipt)
	}{
		{"request", func(value *DiscreteSolverReceipt) { value.RequestID = "dsolreq_" + strings.Repeat("0", 64) }},
		{"attempt", func(value *DiscreteSolverReceipt) { value.AttemptedBackends[0] = "unauthorized" }},
		{"backend capability", func(value *DiscreteSolverReceipt) {
			value.SelectedBackend.Capabilities = []string{AllocationBackendCapability}
		}},
		{"assignment", func(value *DiscreteSolverReceipt) { value.Result.Assignments[1].Start++ }},
		{"proof objective", func(value *DiscreteSolverReceipt) {
			value.BackendReportedEvidence.Objectives[0].Objective = "minimize_estimated_cost"
		}},
		{"proof bound", func(value *DiscreteSolverReceipt) { value.BackendReportedEvidence.Objectives[0].BestBound = "0" }},
		{"proof kind", func(value *DiscreteSolverReceipt) { value.BackendReportedEvidence.ProofKind = "" }},
		{"phase model digest", func(value *DiscreteSolverReceipt) {
			value.BackendReportedEvidence.Trace.Phases[0].ModelDigest = "sha256:" + strings.Repeat("f", 64)
			value.BackendReportedEvidence.Trace.PlanDigest = cpSatPlanDigest(request.ID, *value.BackendReportedEvidence.Trace)
		}},
		{"phase parameter digest", func(value *DiscreteSolverReceipt) {
			value.BackendReportedEvidence.Trace.Phases[0].ParametersDigest = "sha256:" + strings.Repeat("e", 64)
			value.BackendReportedEvidence.Trace.PlanDigest = cpSatPlanDigest(request.ID, *value.BackendReportedEvidence.Trace)
		}},
		{"phase omission", func(value *DiscreteSolverReceipt) {
			value.BackendReportedEvidence.Trace.Phases = value.BackendReportedEvidence.Trace.Phases[1:]
			value.BackendReportedEvidence.Trace.PlanDigest = cpSatPlanDigest(request.ID, *value.BackendReportedEvidence.Trace)
		}},
		{"phase descriptor", func(value *DiscreteSolverReceipt) {
			value.BackendReportedEvidence.Trace.Phases[2].Key = "forged-task"
			value.BackendReportedEvidence.Trace.Phases[2].ResponseDigest = cpSatPhaseSemanticDigest(request.ID, value.BackendReportedEvidence.Trace.Phases[2])
			value.BackendReportedEvidence.Trace.PlanDigest = cpSatPlanDigest(request.ID, *value.BackendReportedEvidence.Trace)
		}},
		{"phase response digest", func(value *DiscreteSolverReceipt) {
			value.BackendReportedEvidence.Trace.Phases[0].ResponseDigest = "sha256:" + strings.Repeat("d", 64)
			value.BackendReportedEvidence.Trace.PlanDigest = cpSatPlanDigest(request.ID, *value.BackendReportedEvidence.Trace)
		}},
		{"optimality", func(value *DiscreteSolverReceipt) {
			value.BackendReportedEvidence.Objectives = value.BackendReportedEvidence.Objectives[:1]
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := cloneSolverJSON(t, receipt)
			mutation.mutate(&candidate)
			candidate.ID, _ = discreteSolverReceiptID(candidate)
			if err := VerifyDiscreteSolverReceipt(request, candidate); err == nil {
				t.Fatal("accepted tampered discrete receipt")
			}
		})
	}

	badChoices := testDiscreteChoices()
	badChoices[1].Start = 2
	receipt, err = RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{fixtureDiscreteBackend{outcome: DiscreteBackendOutcome{Termination: SolverFeasible, Choices: badChoices}}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverError || receipt.Result != nil || !strings.Contains(receipt.Explanation, "independent verification") {
		t.Fatalf("unverified choices were not converted to fail-closed error: %#v", receipt)
	}
}

func TestDiscreteBackendRuntimeFailureDoesNotImplicitlyFallback(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat", AuthorizedFallbacks: []string{NativeBackendName}})
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{fixtureDiscreteBackend{err: errors.New("runtime crash")}, NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverError || receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != "cp-sat" || receipt.FallbackFrom != "" {
		t.Fatalf("runtime failure silently fell back = %#v", receipt)
	}
}

func TestDiscreteCallerCancellationIsNotReportedAsTimeLimit(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(ctx, request, []DiscreteSolverBackend{fixtureDiscreteBackend{outcome: DiscreteBackendOutcome{
		Termination: SolverFeasible, Choices: testDiscreteChoices(), BackendReportedEvidence: &DiscreteBackendReportedEvidence{Status: "FEASIBLE"},
	}}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverUnknown || receipt.Result != nil || receipt.BackendReportedEvidence != nil || strings.Contains(receipt.Explanation, "time limit") {
		t.Fatalf("caller cancellation was misclassified = %#v", receipt)
	}

	deadlineCtx, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer deadlineCancel()
	receipt, err = RunDiscreteSolverRequest(deadlineCtx, request, []DiscreteSolverBackend{fixtureDiscreteBackend{outcome: DiscreteBackendOutcome{
		Termination: SolverFeasible, Choices: testDiscreteChoices(), BackendReportedEvidence: &DiscreteBackendReportedEvidence{Status: "FEASIBLE"},
	}}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverUnknown || receipt.Result != nil || receipt.BackendReportedEvidence != nil || strings.Contains(receipt.Explanation, "time limit") {
		t.Fatalf("caller deadline was misclassified = %#v", receipt)
	}
}

func TestDiscreteConfiguredDeadlineRemainsTimeLimit(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	request.Limits.TimeLimitMilliseconds = 1
	request.ID, _ = discreteSolverRequestID(request)
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{waitingDiscreteBackend{}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverTimeLimit || receipt.Result != nil || !strings.Contains(receipt.Explanation, "deadline") {
		t.Fatalf("configured deadline was not retained = %#v", receipt)
	}
}

func TestDiscreteConfiguredDeadlineDoesNotOverwriteBoundCpSatTerminalResponse(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	request.Limits.TimeLimitMilliseconds = 1
	request.ID, _ = discreteSolverRequestID(request)
	result, err := NewDiscreteScheduleResult(request.Recommendation.Problem, testDiscreteChoices())
	if err != nil {
		t.Fatal(err)
	}
	evidence := testDiscreteOptimalEvidence(t, request, result)
	terminal := evidence.Trace.Phases[0]
	terminal.Status, terminal.Incumbent, terminal.BestBound, terminal.Gap = "UNKNOWN", "", "", ""
	terminal.ResponseDigest = cpSatPhaseSemanticDigest(request.ID, terminal)
	evidence = cpSatTraceEvidence(request, "UNKNOWN", []DiscreteCpSatPhaseProof{terminal}, nil, false, nil)
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	backend := deadlineBoundaryDiscreteBackend{outcome: DiscreteBackendOutcome{Termination: SolverUnknown, BackendReportedEvidence: evidence, Explanation: "solver terminal response won the deadline race"}}
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{backend}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverUnknown || receipt.BackendReportedEvidence == nil || receipt.BackendReportedEvidence.Trace == nil || receipt.BackendReportedEvidence.Trace.StopReason != "SOLVER_TERMINAL" {
		t.Fatalf("deadline boundary overwrote a bound terminal response: %#v", receipt)
	}
}

type fixtureDiscreteBackend struct {
	outcome DiscreteBackendOutcome
	err     error
}

type waitingDiscreteBackend struct{}

type deadlineBoundaryDiscreteBackend struct{ outcome DiscreteBackendOutcome }

func (backend deadlineBoundaryDiscreteBackend) Descriptor() SolverBackendDescriptor {
	return fixtureDiscreteBackend{}.Descriptor()
}

func (backend deadlineBoundaryDiscreteBackend) SolveDiscrete(ctx context.Context, _ DiscreteSolverRequest) (DiscreteBackendOutcome, error) {
	<-ctx.Done()
	return backend.outcome, nil
}

func (waitingDiscreteBackend) Descriptor() SolverBackendDescriptor {
	return fixtureDiscreteBackend{}.Descriptor()
}

func (waitingDiscreteBackend) SolveDiscrete(ctx context.Context, _ DiscreteSolverRequest) (DiscreteBackendOutcome, error) {
	<-ctx.Done()
	return DiscreteBackendOutcome{Termination: SolverUnknown}, nil
}

func (backend fixtureDiscreteBackend) Descriptor() SolverBackendDescriptor {
	return SolverBackendDescriptor{Name: "cp-sat", Version: "9.15.6755", ExecutableDigest: testExecutableDigest, Architecture: "test", Capabilities: []string{DiscreteScheduleCapability}}
}

func (backend fixtureDiscreteBackend) SolveDiscrete(context.Context, DiscreteSolverRequest) (DiscreteBackendOutcome, error) {
	return backend.outcome, backend.err
}

func testDiscreteSolverRequest(t *testing.T, selection SolverSelection) DiscreteSolverRequest {
	t.Helper()
	request, err := NewDiscreteSolverRequest(testDiscreteRecommendation(), selection, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func testDiscreteChoices() []DiscreteChoice {
	return []DiscreteChoice{{Task: "prepare", Lane: "lane-a", Start: 0}, {Task: "rollout-a", Lane: "lane-a", Start: 4}, {Task: "rollout-b", Lane: "lane-b", Start: 7}}
}

func testDiscreteOptimalEvidence(t *testing.T, request DiscreteSolverRequest, result DiscreteScheduleResult) *DiscreteBackendReportedEvidence {
	t.Helper()
	planner, err := newCpSatPhasePlanner(request)
	if err != nil {
		t.Fatal(err)
	}
	phases := make([]DiscreteCpSatPhaseProof, 0, len(planner.phases))
	objectives := make([]SolverObjectiveProof, 0, 2)
	for !planner.complete() {
		compiled, err := planner.compileNextPhase()
		if err != nil {
			t.Fatal(err)
		}
		phaseValue, err := cpSatPhaseValueFromResult(request, compiled.Phase, result)
		if err != nil {
			t.Fatal(err)
		}
		value := strconv.FormatInt(phaseValue, 10)
		proof := DiscreteCpSatPhaseProof{Index: compiled.Phase.Index, Kind: compiled.Phase.Kind, Key: compiled.Phase.Key, Unit: compiled.Phase.Unit, ModelDigest: digestBytes(compiled.Model), ParametersDigest: digestBytes(compiled.Parameters), Status: "OPTIMAL", Incumbent: value, BestBound: value, Gap: "0"}
		proof.ResponseDigest = cpSatPhaseSemanticDigest(request.ID, proof)
		phases = append(phases, proof)
		if compiled.Phase.Kind == "OBJECTIVE" {
			objectives = append(objectives, SolverObjectiveProof{Objective: compiled.Phase.Key, Unit: compiled.Phase.Unit, Incumbent: value, BestBound: value, Gap: "0"})
		}
		if err := planner.acceptExactPhase(compiled, phaseValue); err != nil {
			t.Fatal(err)
		}
	}
	return cpSatTraceEvidence(request, "OPTIMAL", phases, objectives, true, &result)
}
