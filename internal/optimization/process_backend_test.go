package optimization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExternalProcessSolverBackendRunsDigestPinnedEnvelope(t *testing.T) {
	t.Setenv("PLATFORM_EXTERNAL_SOLVER_TEST_SECRET", "must-not-cross-process-boundary")
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs"})
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	choices := externalSolverAssignmentChoices(result.Assignments)
	for left, right := 0, len(choices)-1; left < right; left, right = left+1, right-1 {
		choices[left], choices[right] = choices[right], choices[left]
	}
	response := ExternalSolverProcessResponse{
		APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
		RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverFeasible,
		BackendReportedEvidence: &SolverBackendReportedEvidence{Status: "kFeasible"}, Assignments: &choices,
	}
	path, digest := writeExternalSolverFixture(t, response, "", 0)
	backend := testExternalProcessBackend(path, digest)
	now := time.Date(2026, 8, 11, 19, 0, 0, 0, time.UTC)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverFeasible || receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != "highs" || receipt.SelectedBackend.ExecutableDigest != digest {
		t.Fatalf("receipt = %#v", receipt)
	}
	if !reflect.DeepEqual(receipt.BackendReportedEvidence, response.BackendReportedEvidence) {
		t.Fatalf("external backend evidence was not copied exactly: %#v", receipt.BackendReportedEvidence)
	}
	expected := cloneSolverJSON(t, result)
	sort.Slice(expected.Assignments, func(i, j int) bool { return expected.Assignments[i].Item < expected.Assignments[j].Item })
	if receipt.Result == nil || !reflect.DeepEqual(*receipt.Result, expected) {
		t.Fatalf("host-reconstructed result = %#v, want %#v", receipt.Result, expected)
	}
	if err := VerifySolverReceipt(request, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestExternalProcessSolverBackendFailsClosedOnIntegrityAndForeignEvidence(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs", AuthorizedFallbacks: []string{NativeBackendName}})
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	response := ExternalSolverProcessResponse{
		APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
		RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverFeasible,
		Assignments: pointerToExternalSolverChoices(externalSolverAssignmentChoices(result.Assignments)),
	}
	path, digest := writeExternalSolverFixture(t, response, "", 0)
	now := time.Date(2026, 8, 11, 19, 0, 0, 0, time.UTC)

	t.Run("digest mismatch is an error, not fallback", func(t *testing.T) {
		backend := testExternalProcessBackend(path, "sha256:"+strings.Repeat("0", 64))
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != "highs" || receipt.FallbackFrom != "" {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("symlink executable is rejected", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "solver-link")
		if err := os.Symlink(path, link); err != nil {
			t.Fatal(err)
		}
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(link, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != "highs" {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("staged executable mutation is rejected", func(t *testing.T) {
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		script := "#!/bin/sh\nIFS= read -r _request || true\n/bin/chmod u+w \"$0\"\nprintf '\\n# changed\\n' >> \"$0\"\nprintf '%s\\n' '" + shellSingleQuote(string(encoded)) + "'\n"
		mutatingPath, mutatingDigest := writeExternalSolverScript(t, script)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(mutatingPath, mutatingDigest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || !strings.Contains(receipt.Explanation, "changed during execution") {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("foreign request binding", func(t *testing.T) {
		foreign := response
		foreign.RequestID = "solreq_" + strings.Repeat("a", 64)
		foreignPath, foreignDigest := writeExternalSolverFixture(t, foreign, "", 0)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(foreignPath, foreignDigest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || !strings.Contains(receipt.Explanation, "does not bind") {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("foreign model binding", func(t *testing.T) {
		foreign := response
		foreign.ModelDigest = "sha256:" + strings.Repeat("a", 64)
		foreignPath, foreignDigest := writeExternalSolverFixture(t, foreign, "", 0)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(foreignPath, foreignDigest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || !strings.Contains(receipt.Explanation, "does not bind") {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("independent verifier rejects invalid assignment", func(t *testing.T) {
		invalid := response
		choices := append([]ExternalSolverAssignmentChoice(nil), (*response.Assignments)...)
		choices[0].Resource = "not-a-resource"
		invalid.Assignments = &choices
		invalidPath, invalidDigest := writeExternalSolverFixture(t, invalid, "", 0)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(invalidPath, invalidDigest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || receipt.Result != nil || receipt.Explanation != "external solver emitted invalid assignment choices" {
			t.Fatalf("invalid assignment receipt = %#v", receipt)
		}
	})

	if digest == "" {
		t.Fatal("fixture digest is empty")
	}
}

func TestExternalProcessSolverBackendPreservesTerminationSemantics(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs"})
	now := time.Date(2026, 8, 11, 19, 0, 0, 0, time.UTC)

	t.Run("unknown remains selected backend evidence", func(t *testing.T) {
		response := ExternalSolverProcessResponse{
			APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
			RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverUnknown,
			Explanation: "backend could not classify the model",
		}
		path, digest := writeExternalSolverFixture(t, response, "", 0)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverUnknown || receipt.SelectedBackend == nil || receipt.Result != nil {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("time limited incumbent remains time limited", func(t *testing.T) {
		result, err := solve(request.Recommendation.Problem, true)
		if err != nil {
			t.Fatal(err)
		}
		response := ExternalSolverProcessResponse{
			APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
			RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverTimeLimit,
			BackendReportedEvidence: &SolverBackendReportedEvidence{Status: "kTimeLimit"},
			Assignments:             pointerToExternalSolverChoices(externalSolverAssignmentChoices(result.Assignments)),
			Explanation:             "time expired with an incumbent",
		}
		path, digest := writeExternalSolverFixture(t, response, "", 0)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverTimeLimit || receipt.Result == nil || receipt.Objective == nil || !reflect.DeepEqual(receipt.BackendReportedEvidence, response.BackendReportedEvidence) {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("unbounded remains distinct", func(t *testing.T) {
		response := ExternalSolverProcessResponse{
			APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
			RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverUnbounded,
			BackendReportedEvidence: &SolverBackendReportedEvidence{Status: "kUnbounded"},
			Explanation:             "objective is unbounded",
		}
		path, digest := writeExternalSolverFixture(t, response, "", 0)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverUnbounded || receipt.SelectedBackend == nil || receipt.Result != nil || !reflect.DeepEqual(receipt.BackendReportedEvidence, response.BackendReportedEvidence) {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("malformed reported proof fails shape validation", func(t *testing.T) {
		result, err := solve(request.Recommendation.Problem, true)
		if err != nil {
			t.Fatal(err)
		}
		response := ExternalSolverProcessResponse{
			APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
			RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverFeasible,
			BackendReportedEvidence: &SolverBackendReportedEvidence{Status: "kFeasible", Objectives: []SolverObjectiveProof{{
				Objective: "minimize_makespan", Unit: "normalized_load_ratio", Incumbent: exactFloat(result.RoundedMakespan), BestBound: exactFloat(result.RoundedMakespan), Gap: "NaN",
			}}},
			Assignments: pointerToExternalSolverChoices(externalSolverAssignmentChoices(result.Assignments)),
		}
		path, digest := writeExternalSolverFixture(t, response, "", 0)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || !strings.Contains(receipt.Explanation, "inconsistent termination evidence") {
			t.Fatalf("receipt = %#v", receipt)
		}
	})
}

func TestExternalProcessSolverBackendOwnsResultReconstruction(t *testing.T) {
	request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs"})
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 11, 19, 0, 0, 0, time.UTC)

	t.Run("provider-authored result is rejected", func(t *testing.T) {
		encoded, err := json.Marshal(map[string]any{
			"apiVersion":  ExternalSolverProcessAPIVersion,
			"kind":        "ExternalSolverProcessResponse",
			"requestId":   request.ID,
			"modelDigest": request.Model.Digest,
			"termination": SolverFeasible,
			"result":      result,
		})
		if err != nil {
			t.Fatal(err)
		}
		path, digest := writeExternalSolverRawFixture(t, string(encoded))
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || receipt.Result != nil || receipt.Explanation != "external solver emitted an invalid response envelope" {
			t.Fatalf("provider-authored result receipt = %#v", receipt)
		}
	})

	for _, legacyVersion := range []string{
		"optimization.platform.example/external-solver-process/v1alpha1",
		"optimization.platform.example/external-solver-process/v1alpha2",
	} {
		t.Run("legacy envelope "+legacyVersion, func(t *testing.T) {
			choices := externalSolverAssignmentChoices(result.Assignments)
			response := ExternalSolverProcessResponse{
				APIVersion: legacyVersion,
				Kind:       "ExternalSolverProcessResponse", RequestID: request.ID, ModelDigest: request.Model.Digest,
				Termination: SolverFeasible, Assignments: &choices,
			}
			path, digest := writeExternalSolverFixture(t, response, "", 0)
			receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != SolverError || receipt.Result != nil {
				t.Fatalf("legacy envelope receipt = %#v", receipt)
			}
		})
	}

	valid := externalSolverAssignmentChoices(result.Assignments)
	invalidCases := []struct {
		name        string
		assignments *[]ExternalSolverAssignmentChoice
	}{
		{name: "missing"},
		{name: "explicit empty", assignments: pointerToExternalSolverChoices([]ExternalSolverAssignmentChoice{})},
		{name: "partial", assignments: pointerToExternalSolverChoices(append([]ExternalSolverAssignmentChoice(nil), valid[:2]...))},
		{name: "duplicate item", assignments: pointerToExternalSolverChoices([]ExternalSolverAssignmentChoice{valid[0], valid[0], valid[2]})},
		{name: "unknown item", assignments: pointerToExternalSolverChoices([]ExternalSolverAssignmentChoice{{Item: "foreign", Resource: valid[0].Resource}, valid[1], valid[2]})},
		{name: "unknown resource", assignments: pointerToExternalSolverChoices([]ExternalSolverAssignmentChoice{{Item: valid[0].Item, Resource: "foreign"}, valid[1], valid[2]})},
		{name: "ineligible resource", assignments: pointerToExternalSolverChoices([]ExternalSolverAssignmentChoice{{Item: "native", Resource: "arm64"}, {Item: "portable", Resource: "arm64"}, {Item: "small", Resource: "amd64"}})},
	}
	for _, testCase := range invalidCases {
		t.Run(testCase.name, func(t *testing.T) {
			response := ExternalSolverProcessResponse{
				APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
				RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverFeasible,
				Assignments: testCase.assignments,
			}
			path, digest := writeExternalSolverFixture(t, response, "", 0)
			receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != SolverError || receipt.Result != nil {
				t.Fatalf("invalid choices receipt = %#v", receipt)
			}
		})
	}

	t.Run("host derives assignment demand and exact cost", func(t *testing.T) {
		recommendation := testRecommendationRequest()
		recommendation.Objective.Secondary = []string{"minimize_estimated_cost"}
		recommendation.Problem = Problem{
			CostModel: &CostModel{Currency: "USD", Source: "fixture", AsOf: "2026-08-11T18:00:00Z"},
			Resources: []Resource{{ID: "a", Capacity: 1}, {ID: "b", Capacity: 1}},
			Items: []WorkItem{{ID: "job", Demand: 3, EstimatedCostMicros: map[string]int64{
				"a": 500_001, "b": 200_003,
			}}},
		}
		costRequest, err := NewSolverRequest(
			recommendation, SolverSelection{PreferredBackend: "highs"}, SolverDeterminism{Workers: 1},
			SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20},
		)
		if err != nil {
			t.Fatal(err)
		}
		choices := []ExternalSolverAssignmentChoice{{Item: "job", Resource: "b"}}
		response := ExternalSolverProcessResponse{
			APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
			RequestID: costRequest.ID, ModelDigest: costRequest.Model.Digest, Termination: SolverFeasible,
			Assignments: &choices,
		}
		path, digest := writeExternalSolverFixture(t, response, "", 0)
		receipt, err := RunSolverRequest(context.Background(), costRequest, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Result == nil || len(receipt.Result.Assignments) != 1 || receipt.Result.Assignments[0].Demand != 3 || receipt.Result.Assignments[0].EstimatedCostMicros == nil || *receipt.Result.Assignments[0].EstimatedCostMicros != 200_003 || receipt.Result.RoundedEstimatedCostMicros == nil || *receipt.Result.RoundedEstimatedCostMicros != 200_003 || receipt.Result.EstimatedSavingsMicros == nil || *receipt.Result.EstimatedSavingsMicros != 299_998 {
			t.Fatalf("host-derived assignment evidence = %#v", receipt.Result)
		}
	})

	t.Run("explicit empty assignments reconstruct empty problem", func(t *testing.T) {
		recommendation := testRecommendationRequest()
		recommendation.Problem = Problem{}
		emptyRequest, err := NewSolverRequest(
			recommendation, SolverSelection{PreferredBackend: "highs"}, SolverDeterminism{Workers: 1},
			SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20},
		)
		if err != nil {
			t.Fatal(err)
		}
		empty := []ExternalSolverAssignmentChoice{}
		response := ExternalSolverProcessResponse{
			APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
			RequestID: emptyRequest.ID, ModelDigest: emptyRequest.Model.Digest, Termination: SolverFeasible,
			Assignments: &empty,
		}
		path, digest := writeExternalSolverFixture(t, response, "", 0)
		receipt, err := RunSolverRequest(context.Background(), emptyRequest, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverFeasible || receipt.Result == nil || len(receipt.Result.Assignments) != 0 || len(receipt.Result.Fractions) != 0 {
			t.Fatalf("empty incumbent receipt = %#v", receipt)
		}
	})
}

func TestExternalProcessSolverBackendAcceptsExactMultiObjectiveOptimalProof(t *testing.T) {
	recommendation := testRecommendationRequest()
	recommendation.Objective.Secondary = []string{"minimize_estimated_cost"}
	recommendation.Problem.CostModel = &CostModel{Currency: "USD", Source: "fixture", AsOf: "2026-08-11T18:00:00Z"}
	for index := range recommendation.Problem.Items {
		recommendation.Problem.Items[index].EstimatedCostMicros = map[string]int64{"amd64": int64(100 + index), "arm64": int64(50 + index)}
	}
	request, err := NewSolverRequest(recommendation, SolverSelection{PreferredBackend: "highs"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	result, err := solve(request.Recommendation.Problem, true)
	if err != nil {
		t.Fatal(err)
	}
	choices := externalSolverAssignmentChoices(result.Assignments)
	response := ExternalSolverProcessResponse{
		APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse",
		RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverOptimal,
		BackendReportedEvidence: exactOptimalObjectiveProofs(result, true), Assignments: &choices,
	}
	path, digest := writeExternalSolverFixture(t, response, "", 0)
	receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time {
		return time.Date(2026, 8, 11, 20, 0, 0, 0, time.UTC)
	})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverOptimal || receipt.Result == nil || !reflect.DeepEqual(receipt.BackendReportedEvidence, response.BackendReportedEvidence) {
		t.Fatalf("multi-objective process receipt = %#v", receipt)
	}
}

func TestExternalProcessSolverBackendBoundsRuntimeAndOutput(t *testing.T) {
	now := time.Date(2026, 8, 11, 19, 0, 0, 0, time.UTC)

	t.Run("missing executable may use authorized fallback", func(t *testing.T) {
		request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs", AuthorizedFallbacks: []string{NativeBackendName}})
		backend := testExternalProcessBackend(filepath.Join(t.TempDir(), "missing"), "sha256:"+strings.Repeat("0", 64))
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend, NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: runtime.GOARCH}}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != NativeBackendName || receipt.FallbackFrom != "highs" {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("deadline remains time limit", func(t *testing.T) {
		request, err := NewSolverRequest(testRecommendationRequest(), SolverSelection{PreferredBackend: "highs"}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 20, MemoryLimitBytes: 64 << 20})
		if err != nil {
			t.Fatal(err)
		}
		response := ExternalSolverProcessResponse{APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse", RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverInfeasible, Explanation: "unused"}
		marker := filepath.Join(t.TempDir(), "escaped-descendant")
		path, digest := writeDelayedExternalSolverFixture(t, response, marker)
		wallStarted := time.Now()
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverTimeLimit {
			t.Fatalf("receipt = %#v", receipt)
		}
		if elapsed := time.Since(wallStarted); elapsed > 500*time.Millisecond {
			t.Fatalf("process group survived 20 ms deadline for %s", elapsed)
		}
		time.Sleep(350 * time.Millisecond)
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("timed-out descendant survived long enough to write its marker")
		} else if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	})

	t.Run("stdout is bounded", func(t *testing.T) {
		request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs"})
		response := ExternalSolverProcessResponse{APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse", RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverInfeasible, Explanation: strings.Repeat("x", 512)}
		path, digest := writeExternalSolverFixture(t, response, "", 0)
		backend := testExternalProcessBackend(path, digest)
		backend.MaximumOutputBytes = 128
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{backend}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || receipt.Explanation != "external solver exceeded its bounded output contract" {
			t.Fatalf("receipt = %#v", receipt)
		}
	})

	t.Run("stderr is discarded", func(t *testing.T) {
		request := testSolverRequest(t, SolverSelection{PreferredBackend: "highs"})
		response := ExternalSolverProcessResponse{APIVersion: ExternalSolverProcessAPIVersion, Kind: "ExternalSolverProcessResponse", RequestID: request.ID, ModelDigest: request.Model.Digest, Termination: SolverInfeasible, Explanation: "unused"}
		path, digest := writeExternalSolverFixture(t, response, "authorization=super-secret-token", 9)
		receipt, err := RunSolverRequest(context.Background(), request, []SolverBackend{testExternalProcessBackend(path, digest)}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Termination != SolverError || receipt.Explanation != "external solver process failed" || strings.Contains(receipt.Explanation, "super-secret-token") {
			t.Fatalf("receipt = %#v", receipt)
		}
	})
}

func testExternalProcessBackend(path, digest string) ExternalProcessSolverBackend {
	return ExternalProcessSolverBackend{
		BackendDescriptor: SolverBackendDescriptor{
			Name: "highs", Version: "1.15.1", ExecutableDigest: digest, Architecture: runtime.GOARCH,
			Capabilities: []string{AllocationBackendCapability},
		},
		ExecutablePath: path,
	}
}

func writeExternalSolverFixture(t *testing.T, response ExternalSolverProcessResponse, stderr string, exitCode int) (string, string) {
	t.Helper()
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nIFS= read -r _request || true\n"
	script += "if [ -n \"${PLATFORM_EXTERNAL_SOLVER_TEST_SECRET+x}\" ]; then printf '%s\\n' 'inherited environment' >&2; exit 7; fi\n"
	if stderr != "" {
		script += "printf '%s\\n' '" + shellSingleQuote(stderr) + "' >&2\n"
	}
	script += "printf '%s\\n' '" + shellSingleQuote(string(encoded)) + "'\n"
	script += "exit " + strconv.Itoa(exitCode) + "\n"
	return writeExternalSolverScript(t, script)
}

func writeExternalSolverRawFixture(t *testing.T, encoded string) (string, string) {
	t.Helper()
	script := "#!/bin/sh\nIFS= read -r _request || true\nprintf '%s\\n' '" + shellSingleQuote(encoded) + "'\n"
	return writeExternalSolverScript(t, script)
}

func writeDelayedExternalSolverFixture(t *testing.T, response ExternalSolverProcessResponse, marker string) (string, string) {
	t.Helper()
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	child := "/bin/sleep 0.2; printf escaped > '" + shellSingleQuote(marker) + "'"
	script := "#!/bin/sh\nIFS= read -r _request || true\n/bin/sh -c '" + shellSingleQuote(child) + "' &\n/bin/sleep 1\nprintf '%s\\n' '" + shellSingleQuote(string(encoded)) + "'\n"
	return writeExternalSolverScript(t, script)
}

func writeExternalSolverScript(t *testing.T, script string) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "solver-fixture")
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(script))
	return path, "sha256:" + hex.EncodeToString(digest[:])
}

func shellSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "'\"'\"'")
}

func externalSolverAssignmentChoices(assignments []Assignment) []ExternalSolverAssignmentChoice {
	choices := make([]ExternalSolverAssignmentChoice, 0, len(assignments))
	for _, assignment := range assignments {
		choices = append(choices, ExternalSolverAssignmentChoice{Item: assignment.Item, Resource: assignment.Resource})
	}
	return choices
}

func pointerToExternalSolverChoices(choices []ExternalSolverAssignmentChoice) *[]ExternalSolverAssignmentChoice {
	return &choices
}
