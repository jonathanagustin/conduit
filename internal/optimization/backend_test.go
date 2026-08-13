package optimization

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testExecutableDigest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

func TestNativeSolverBackendProducesReplayableVersionedReceipt(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: NativeBackendName})
	if request.APIVersion != "optimization.platform.example/v1alpha2" {
		t.Fatalf("solver api version = %q", request.APIVersion)
	}
	started := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	finished := started.Add(25 * time.Millisecond)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{NativeSolverBackend{
		ExecutableDigest: testExecutableDigest, Architecture: "amd64",
	}}, sequenceClock(started, finished))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ID == "" || receipt.RequestID != request.ID || receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != NativeBackendName {
		t.Fatalf("receipt identity/backend = %#v", receipt)
	}
	if receipt.Termination != SolverFeasible || receipt.Objective == nil || receipt.Objective.Unit != "normalized_load_ratio" {
		t.Fatalf("termination/objective = %#v", receipt)
	}
	if err := VerifyNativeSolverReceipt(context.Background(), request, receipt); err != nil {
		t.Fatalf("replay native receipt: %v", err)
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	var decoded SolverReceipt
	if err := json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(decoded, receipt) {
		t.Fatalf("receipt round trip = %v / %#v", err, decoded)
	}
}

func TestSolverRequestBindsModelConstraintsObjectivesAndLimits(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: NativeBackendName})
	mutations := []struct {
		name   string
		mutate func(*SolverRequest)
	}{
		{"model digest", func(value *SolverRequest) { value.Model.Digest = strings.Replace(value.Model.Digest, "a", "b", 1) }},
		{"constraint digest", func(value *SolverRequest) { value.Model.HardConstraintsDigest = testExecutableDigest }},
		{"objective digest", func(value *SolverRequest) { value.Model.ObjectiveDigest = testExecutableDigest }},
		{"snapshot", func(value *SolverRequest) { value.Recommendation.Snapshot.Revision = "changed" }},
		{"seed", func(value *SolverRequest) { value.Determinism.Seed++ }},
		{"worker count", func(value *SolverRequest) { value.Determinism.Workers++ }},
		{"time limit", func(value *SolverRequest) { value.Limits.TimeLimitMilliseconds++ }},
		{"memory limit", func(value *SolverRequest) { value.Limits.MemoryLimitBytes++ }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := cloneSolverJSON(t, request)
			mutation.mutate(&candidate)
			if err := VerifySolverRequest(candidate); err == nil {
				t.Fatal("accepted request whose derived identity no longer matches")
			}
		})
	}
}

func TestSolverFallbackMustBeExplicitAndIsRetained(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs", AuthorizedFallbacks: []string{NativeBackendName}})
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{NativeSolverBackend{
		ExecutableDigest: testExecutableDigest, Architecture: "arm64",
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != NativeBackendName || receipt.FallbackFrom != "highs" || !reflect.DeepEqual(receipt.AttemptedBackends, []string{"highs", NativeBackendName}) {
		t.Fatalf("explicit fallback receipt = %#v", receipt)
	}

	request = testSolverRequest(t, SolverSelection{PreferredBackend: "highs"})
	receipt, err = RunSolverRequest(context.Background(), request, []SolverBackend{NativeSolverBackend{
		ExecutableDigest: testExecutableDigest, Architecture: "arm64",
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverUnknown || receipt.SelectedBackend != nil || receipt.FallbackFrom != "" || receipt.Result != nil {
		t.Fatalf("unauthorized implicit fallback = %#v", receipt)
	}
}

func TestSolverTerminationStatesRemainDistinct(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture"})
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	cases := []struct {
		name        string
		termination SolverTermination
		result      bool
		explanation string
	}{
		{"infeasible", SolverInfeasible, false, "constraint set has no feasible assignment"},
		{"time limit without incumbent", SolverTimeLimit, false, "time budget exhausted"},
		{"time limit with incumbent", SolverTimeLimit, true, "time budget exhausted after finding an incumbent"},
		{"unbounded", SolverUnbounded, false, "objective is unbounded"},
		{"error", SolverError, false, "backend failed"},
		{"unknown", SolverUnknown, false, "backend could not classify the model"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var result *Result
			if testCase.result {
				solved, err := Solve(request.Recommendation.Problem)
				if err != nil {
					t.Fatal(err)
				}
				result = &solved
			}
			backend := fixtureSolverBackend{outcome: SolverBackendOutcome{
				Termination: testCase.termination, Result: result, Explanation: testCase.explanation,
			}}
			receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != testCase.termination || receipt.SelectedBackend == nil {
				t.Fatalf("termination collapsed = %#v", receipt)
			}
		})
	}
}

func TestSolverOptimalUsesBackendReportedProofNotLPRelaxationGap(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture"})
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.OptimalityGap <= tolerance {
		t.Fatalf("fixture needs a positive LP-relaxation gap, got %g", result.OptimalityGap)
	}
	proof := optimalBackendReportedEvidence("fixture.model_status:optimal", result)
	backend := fixtureSolverBackend{outcome: SolverBackendOutcome{
		Termination: SolverOptimal, BackendReportedEvidence: proof, Result: &result,
	}}
	now := time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverOptimal || receipt.Result == nil || receipt.Result.OptimalityGap <= tolerance || !reflect.DeepEqual(receipt.BackendReportedEvidence, proof) {
		t.Fatalf("receipt did not preserve distinct backend proof and LP gap: %#v", receipt)
	}
	if err := VerifySolverReceipt(request, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestSolverOptimalRejectsMissingOrFalseBackendProof(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture"})
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		evidence *SolverBackendReportedEvidence
	}{
		{name: "missing"},
		{name: "nonzero gap", evidence: &SolverBackendReportedEvidence{
			Status: "fixture.model_status:optimal", Objectives: []SolverObjectiveProof{{
				Objective: "minimize_makespan", Unit: "normalized_load_ratio", Incumbent: exactFloat(result.RoundedMakespan), BestBound: exactFloat(result.RoundedMakespan), Gap: "0.01",
			}},
		}},
		{name: "bound differs from incumbent", evidence: &SolverBackendReportedEvidence{
			Status: "fixture.model_status:optimal", Objectives: []SolverObjectiveProof{{
				Objective: "minimize_makespan", Unit: "normalized_load_ratio", Incumbent: exactFloat(result.RoundedMakespan), BestBound: exactFloat(result.RoundedMakespan - 1), Gap: "0",
			}},
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			backend := fixtureSolverBackend{outcome: SolverBackendOutcome{
				Termination: SolverOptimal, BackendReportedEvidence: testCase.evidence, Result: &result,
			}}
			if _, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now }); err == nil || !strings.Contains(err.Error(), "proof") {
				t.Fatalf("false proof error = %v", err)
			}
		})
	}
}

func TestSolverBackendReportedEvidenceRejectsMalformedValues(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture"})
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		evidence SolverBackendReportedEvidence
	}{
		{name: "control character in status", evidence: SolverBackendReportedEvidence{Status: "optimal\nforged"}},
		{name: "printable status leakage", evidence: SolverBackendReportedEvidence{Status: "optimal secret=value"}},
		{name: "oversized status", evidence: SolverBackendReportedEvidence{Status: strings.Repeat("a", maximumSolverReportedStatus+1)}},
		{name: "non-finite bound", evidence: malformedMakespanProof(result, exactFloat(result.RoundedMakespan), "NaN", "0")},
		{name: "non-finite gap", evidence: malformedMakespanProof(result, exactFloat(result.RoundedMakespan), exactFloat(result.RoundedMakespan), "+Inf")},
		{name: "negative gap", evidence: malformedMakespanProof(result, exactFloat(result.RoundedMakespan), exactFloat(result.RoundedMakespan), "-0.01")},
		{name: "noncanonical gap", evidence: malformedMakespanProof(result, exactFloat(result.RoundedMakespan), exactFloat(result.RoundedMakespan), "0.0")},
		{name: "bound above minimization incumbent", evidence: malformedMakespanProof(result, exactFloat(result.RoundedMakespan), exactFloat(result.RoundedMakespan+1), "0")},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			backend := fixtureSolverBackend{outcome: SolverBackendOutcome{
				Termination: SolverFeasible, BackendReportedEvidence: &testCase.evidence, Result: &result,
			}}
			if _, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now }); err == nil || !strings.Contains(err.Error(), "backend-reported") {
				t.Fatalf("malformed evidence error = %v", err)
			}
		})
	}
}

func TestSolverBackendReportedEvidenceIsReceiptIdentityBound(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture"})
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	backend := fixtureSolverBackend{outcome: SolverBackendOutcome{
		Termination:             SolverOptimal,
		BackendReportedEvidence: optimalBackendReportedEvidence("fixture.model_status:optimal", result),
		Result:                  &result,
	}}
	now := time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	candidate := cloneSolverJSON(t, receipt)
	candidate.BackendReportedEvidence.Status = "fixture.model_status:optimal-tampered"
	if err := VerifySolverReceipt(request, candidate); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("proof tamper error = %v", err)
	}
}

func TestSolverObjectiveProofsBindRequestedOrderUnitsAndEveryOptimalObjective(t *testing.T) {
	recommendation := testRecommendationRequest()
	recommendation.Objective.Secondary = []string{"minimize_estimated_cost"}
	recommendation.Problem.CostModel = &CostModel{Currency: "USD", Source: "fixture", AsOf: "2026-08-11T19:00:00Z"}
	for index := range recommendation.Problem.Items {
		recommendation.Problem.Items[index].EstimatedCostMicros = map[string]int64{"amd64": int64(20 + index), "arm64": int64(10 + index)}
	}
	request, err := NewSolverRequest(recommendation, SolverSelection{PreferredBackend: "fixture"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	proof := exactOptimalObjectiveProofs(result, true)
	now := time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{fixtureSolverBackend{outcome: SolverBackendOutcome{
		Termination: SolverOptimal, BackendReportedEvidence: proof, Result: &result,
	}}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverOptimal || !reflect.DeepEqual(receipt.BackendReportedEvidence, proof) {
		t.Fatalf("multi-objective optimal receipt = %#v", receipt)
	}
	prefix := cloneSolverJSON(t, proof)
	prefix.Objectives = prefix.Objectives[:1]
	prefixReceipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{fixtureSolverBackend{outcome: SolverBackendOutcome{
		Termination: SolverFeasible, BackendReportedEvidence: prefix, Result: &result,
	}}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if prefixReceipt.Termination != SolverFeasible || len(prefixReceipt.BackendReportedEvidence.Objectives) != 1 {
		t.Fatalf("valid objective proof prefix = %#v", prefixReceipt)
	}

	cases := []struct {
		name   string
		mutate func(*SolverBackendReportedEvidence)
	}{
		{name: "missing secondary proof", mutate: func(value *SolverBackendReportedEvidence) { value.Objectives = value.Objectives[:1] }},
		{name: "wrong objective order", mutate: func(value *SolverBackendReportedEvidence) {
			value.Objectives[0], value.Objectives[1] = value.Objectives[1], value.Objectives[0]
		}},
		{name: "wrong primary unit", mutate: func(value *SolverBackendReportedEvidence) { value.Objectives[0].Unit = "seconds" }},
		{name: "primary incumbent tamper", mutate: func(value *SolverBackendReportedEvidence) {
			value.Objectives[0].Incumbent = exactFloat(result.RoundedMakespan + 1)
		}},
		{name: "noncanonical cost", mutate: func(value *SolverBackendReportedEvidence) {
			value.Objectives[1].Incumbent = "0" + value.Objectives[1].Incumbent
		}},
		{name: "cost gap tamper", mutate: func(value *SolverBackendReportedEvidence) { value.Objectives[1].Gap = "1" }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			candidate := cloneSolverJSON(t, proof)
			testCase.mutate(candidate)
			_, err := RunSolverRequest(context.Background(), request, []SolverBackend{fixtureSolverBackend{outcome: SolverBackendOutcome{
				Termination: SolverOptimal, BackendReportedEvidence: candidate, Result: &result,
			}}}, func() time.Time { return now })
			if err == nil {
				t.Fatal("accepted tampered objective proof")
			}
		})
	}
}

func exactOptimalObjectiveProofs(result Result, includeCost bool) *SolverBackendReportedEvidence {
	makespan := exactFloat(result.RoundedMakespan)
	proof := &SolverBackendReportedEvidence{Status: "fixture.model_status:optimal", Objectives: []SolverObjectiveProof{{
		Objective: "minimize_makespan", Unit: "normalized_load_ratio", Incumbent: makespan, BestBound: makespan, Gap: "0",
	}}}
	if includeCost {
		cost := strconv.FormatInt(*result.RoundedEstimatedCostMicros, 10)
		proof.Objectives = append(proof.Objectives, SolverObjectiveProof{
			Objective: "minimize_estimated_cost", Unit: "micros", Incumbent: cost, BestBound: cost, Gap: "0",
		})
	}
	return proof
}

func TestSolverReceiptIndependentVerifierRejectsConstraintAndOptimalityTampering(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: NativeBackendName})
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{NativeSolverBackend{
		ExecutableDigest: testExecutableDigest, Architecture: "amd64",
	}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}

	t.Run("hard eligibility", func(t *testing.T) {
		candidate := cloneSolverJSON(t, receipt)
		for index := range candidate.Result.Assignments {
			if candidate.Result.Assignments[index].Item == "native" {
				candidate.Result.Assignments[index].Resource = "arm64"
			}
		}
		candidate.ID, _ = solverReceiptID(candidate)
		if err := VerifySolverReceipt(request, candidate); err == nil || !strings.Contains(err.Error(), "ineligible") {
			t.Fatalf("eligibility tamper error = %v", err)
		}
	})
	t.Run("work demand", func(t *testing.T) {
		candidate := cloneSolverJSON(t, receipt)
		candidate.Result.Assignments[0].Demand++
		candidate.ID, _ = solverReceiptID(candidate)
		if err := VerifySolverReceipt(request, candidate); err == nil || !strings.Contains(err.Error(), "demand") {
			t.Fatalf("demand tamper error = %v", err)
		}
	})
	t.Run("optimality", func(t *testing.T) {
		candidate := cloneSolverJSON(t, receipt)
		candidate.Termination = SolverOptimal
		candidate.ID, _ = solverReceiptID(candidate)
		if err := VerifySolverReceipt(request, candidate); err == nil || !strings.Contains(err.Error(), "optimal") {
			t.Fatalf("optimality tamper error = %v", err)
		}
	})
	t.Run("lower bound", func(t *testing.T) {
		candidate := cloneSolverJSON(t, receipt)
		candidate.Result.RelaxedMakespan++
		candidate.Result.OptimalityGap = candidate.Result.RoundedMakespan/candidate.Result.RelaxedMakespan - 1
		candidate.Objective = solverObjectiveEvidence(*candidate.Result)
		candidate.ID, _ = solverReceiptID(candidate)
		if err := VerifySolverReceipt(request, candidate); err == nil || !strings.Contains(err.Error(), "lower bound") {
			t.Fatalf("lower-bound tamper error = %v", err)
		}
	})
	t.Run("resource metric", func(t *testing.T) {
		candidate := cloneSolverJSON(t, receipt)
		candidate.Result.Resources[0].Load++
		candidate.ID, _ = solverReceiptID(candidate)
		if err := VerifySolverReceipt(request, candidate); err == nil || !strings.Contains(err.Error(), "resource metric") {
			t.Fatalf("resource-metric tamper error = %v", err)
		}
	})
}

func TestNativeBackendDoesNotClaimSecondaryObjectiveOptimality(t *testing.T) {
	recommendation := testRecommendationRequest()
	recommendation.Objective.Secondary = []string{"minimize_estimated_cost"}
	recommendation.Problem.CostModel = &CostModel{Currency: "USD", Source: "fixture", AsOf: "2026-08-09T17:00:00Z"}
	for index := range recommendation.Problem.Items {
		recommendation.Problem.Items[index].EstimatedCostMicros = map[string]int64{"amd64": 2, "arm64": 1}
		if recommendation.Problem.Items[index].ID == "native" {
			delete(recommendation.Problem.Items[index].EstimatedCostMicros, "arm64")
		}
	}
	request, err := NewSolverRequest(recommendation, SolverSelection{PreferredBackend: NativeBackendName}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverFeasible {
		t.Fatalf("secondary objective was falsely declared globally optimal: %#v", receipt)
	}
}

func TestNativeBackendHandlesEmptyAdvisoryProblemWithoutInventingEvidence(t *testing.T) {
	recommendation := testRecommendationRequest()
	recommendation.Problem = Problem{}
	request, err := NewSolverRequest(recommendation, SolverSelection{PreferredBackend: NativeBackendName}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverOptimal || receipt.Result == nil || len(receipt.Result.Assignments) != 0 {
		t.Fatalf("empty receipt = %#v", receipt)
	}
	if receipt.BackendReportedEvidence == nil || len(receipt.BackendReportedEvidence.Objectives) != 1 || receipt.BackendReportedEvidence.Objectives[0].BestBound != "0" || receipt.BackendReportedEvidence.Objectives[0].Gap != "0" {
		t.Fatalf("native optimal receipt lacks valid backend proof: %#v", receipt.BackendReportedEvidence)
	}
}

func malformedMakespanProof(result Result, incumbent, bestBound, gap string) SolverBackendReportedEvidence {
	return SolverBackendReportedEvidence{Status: "optimal", Objectives: []SolverObjectiveProof{{
		Objective: "minimize_makespan", Unit: "normalized_load_ratio",
		Incumbent: incumbent, BestBound: bestBound, Gap: gap,
	}}}
}

func TestSolverExplanationIsBoundedAndCredentialRedacted(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture"})
	backend := fixtureSolverBackend{outcome: SolverBackendOutcome{
		Termination: SolverError,
		Explanation: "authorization=top-secret bearer abc.def " + strings.Repeat("x", maximumSolverExplanation*2),
	}}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(receipt.Explanation, "top-secret") || strings.Contains(receipt.Explanation, "abc.def") || len(receipt.Explanation) > maximumSolverExplanation+len("…") {
		t.Fatalf("unbounded or unredacted explanation = %q", receipt.Explanation)
	}
}

func TestSolverConformanceCorpus(t *testing.T) {
	encoded, err := os.ReadFile("testdata/solver-conformance.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		APIVersion string `json:"apiVersion"`
		Cases      []struct {
			Name                string            `json:"name"`
			ConstraintClasses   []string          `json:"constraintClasses"`
			Mode                string            `json:"mode"`
			Problem             Problem           `json:"problem"`
			ExpectedTermination SolverTermination `json:"expectedTermination"`
			ExpectedAssignments map[string]string `json:"expectedAssignments"`
			Rejected            bool              `json:"rejected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(encoded, &corpus); err != nil {
		t.Fatal(err)
	}
	if corpus.APIVersion != "optimization.platform.example/conformance-v1" {
		t.Fatalf("unsupported conformance corpus %q", corpus.APIVersion)
	}
	if len(corpus.Cases) < 8 {
		t.Fatalf("conformance corpus has only %d cases", len(corpus.Cases))
	}
	covered := map[string]bool{}
	for _, testCase := range corpus.Cases {
		for _, class := range testCase.ConstraintClasses {
			covered[class] = true
		}
		t.Run(testCase.Name, func(t *testing.T) {
			recommendation := testRecommendationRequest()
			recommendation.Problem = testCase.Problem
			request, err := NewSolverRequest(recommendation, SolverSelection{PreferredBackend: NativeBackendName}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
			if testCase.Rejected {
				if err == nil {
					t.Fatal("invalid corpus case was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
			var backend SolverBackend = NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}
			if testCase.Mode == "timeout" {
				request, err = NewSolverRequest(recommendation, SolverSelection{PreferredBackend: "fixture"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
				backend = fixtureSolverBackend{outcome: SolverBackendOutcome{Termination: SolverTimeLimit, Explanation: "time budget exhausted"}}
			} else if testCase.Mode == "infeasible" {
				request, err = NewSolverRequest(recommendation, SolverSelection{PreferredBackend: "fixture"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
				backend = fixtureSolverBackend{outcome: SolverBackendOutcome{Termination: SolverInfeasible, Explanation: "constraint set is infeasible"}}
			}
			if err != nil {
				t.Fatal(err)
			}
			receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != testCase.ExpectedTermination {
				t.Fatalf("termination = %s, want %s", receipt.Termination, testCase.ExpectedTermination)
			}
			if receipt.Result == nil {
				return
			}
			for _, assignment := range receipt.Result.Assignments {
				if want, exists := testCase.ExpectedAssignments[assignment.Item]; exists && assignment.Resource != want {
					t.Fatalf("assignment %s = %s, want %s", assignment.Item, assignment.Resource, want)
				}
			}
		})
	}
	for _, required := range []string{"equality", "overflow", "architecture", "failure-domain", "maintenance", "pdb", "disk-pressure", "timeout"} {
		if !covered[required] {
			t.Errorf("conformance corpus lacks %s", required)
		}
	}
}

type fixtureSolverBackend struct {
	outcome SolverBackendOutcome
	err     error
}

func (backend fixtureSolverBackend) Descriptor() SolverBackendDescriptor {
	return SolverBackendDescriptor{
		Name: "fixture", Version: "1.0.0", ExecutableDigest: testExecutableDigest,
		Architecture: "test", Capabilities: []string{AllocationBackendCapability},
	}
}

func (backend fixtureSolverBackend) Solve(context.Context, SolverRequest) (SolverBackendOutcome, error) {
	return backend.outcome, backend.err
}

func testSolverRequest(t *testing.T, selection SolverSelection) SolverRequest {
	t.Helper()
	request, err := NewSolverRequest(
		testRecommendationRequest(), selection, SolverDeterminism{Workers: 1},
		SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 128 << 20},
	)
	if err != nil {
		t.Fatal(err)
	}
	return request
}

func testRecommendationRequest() RecommendationRequest {
	return RecommendationRequest{
		APIVersion: EvidenceAPIVersion, Kind: "RecommendationRequest",
		Snapshot:    Snapshot{ID: testSnapshotID, Source: "conformance", Revision: "fixture-v1", CapturedAt: "2026-08-09T17:59:00Z"},
		Objective:   Objective{Primary: "minimize_makespan"},
		Constraints: []ConstraintEvidence{{ID: "admission", Source: "compiler", Revision: "v1", ObservedAt: "2026-08-09T17:58:00Z"}},
		Problem: Problem{
			Resources: []Resource{{ID: "amd64", Capacity: 1}, {ID: "arm64", Capacity: 1}},
			Items:     []WorkItem{{ID: "portable", Demand: 8}, {ID: "native", Demand: 7, Eligible: []string{"amd64"}}, {ID: "small", Demand: 6}},
		},
	}
}

func sequenceClock(values ...time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		value := values[min(index, len(values)-1)]
		index++
		return value
	}
}

func TestUnavailableBackendErrorFallsThroughOnlyToAuthorizedFallback(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture", AuthorizedFallbacks: []string{NativeBackendName}})
	backend := fixtureSolverBackend{err: ErrSolverBackendUnavailable}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend, NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != NativeBackendName || receipt.FallbackFrom != "fixture" {
		t.Fatalf("fallback receipt = %#v", receipt)
	}

	candidate := cloneSolverJSON(t, receipt)
	candidate.SelectedBackend.Name = "unapproved"
	candidate.ID, _ = solverReceiptID(candidate)
	if err := VerifySolverReceipt(request, candidate); err == nil {
		t.Fatal("accepted fallback to an unauthorized backend")
	}
}

func TestBackendRuntimeFailureIsErrorEvidenceNotImplicitFallback(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture", AuthorizedFallbacks: []string{NativeBackendName}})
	backend := fixtureSolverBackend{err: errors.New("runtime crash")}
	now := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend, NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverError || receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != "fixture" || receipt.FallbackFrom != "" {
		t.Fatalf("runtime error silently fell back = %#v", receipt)
	}
}

func cloneSolverJSON[T any](t *testing.T, value T) T {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var cloned T
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		t.Fatal(err)
	}
	return cloned
}
