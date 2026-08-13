package optimization

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestExternalProcessDiscreteBackendRealCpSat(t *testing.T) {
	path := os.Getenv("PLATFORM_CPSAT_ADAPTER")
	if path == "" {
		t.Skip("PLATFORM_CPSAT_ADAPTER is not set")
	}
	binary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(binary))
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{testExternalDiscreteProcessBackend(path, digest)}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverOptimal || receipt.Result == nil || receipt.Result.MakespanSeconds != 8 || receipt.Result.EstimatedTotalCostMicros == nil || *receipt.Result.EstimatedTotalCostMicros != 24 {
		t.Fatalf("real CP-SAT receipt = %#v; result = %#v", receipt, receipt.Result)
	}
	if receipt.BackendReportedEvidence == nil || receipt.BackendReportedEvidence.Status != "OPTIMAL" || receipt.BackendReportedEvidence.Trace == nil || !receipt.BackendReportedEvidence.Trace.Complete {
		t.Fatalf("real CP-SAT evidence = %#v", receipt.BackendReportedEvidence)
	}
	if err := VerifyDiscreteSolverReceipt(request, receipt); err != nil {
		t.Fatal(err)
	}
	if evidencePath := os.Getenv("PLATFORM_CPSAT_SEMANTIC_EVIDENCE"); evidencePath != "" {
		semantic, err := json.MarshalIndent(map[string]any{
			"schemaVersion":           1,
			"requestId":               receipt.RequestID,
			"termination":             receipt.Termination,
			"result":                  receipt.Result,
			"backendReportedEvidence": receipt.BackendReportedEvidence,
		}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		semantic = append(semantic, '\n')
		if existing, readErr := os.ReadFile(evidencePath); readErr == nil {
			if string(existing) != string(semantic) {
				t.Fatal("real CP-SAT semantic evidence changed across repeated executions")
			}
		} else if !os.IsNotExist(readErr) {
			t.Fatal(readErr)
		} else if err := os.WriteFile(evidencePath, semantic, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExternalProcessDiscreteBackendRunsPinnedCpSatEnvelope(t *testing.T) {
	t.Setenv("PLATFORM_EXTERNAL_SOLVER_TEST_SECRET", "must-not-cross-process-boundary")
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	processResponses := testCpSatProcessResponses(t, request, testDiscreteChoices())
	path, digest := writeDiscreteProcessFixtures(t, processResponses)
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{testExternalDiscreteProcessBackend(path, digest)}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverOptimal || receipt.Result == nil || receipt.Result.MakespanSeconds != 10 || receipt.SelectedBackend == nil || receipt.SelectedBackend.ExecutableDigest != digest {
		t.Fatalf("CP-SAT receipt = %#v", receipt)
	}
	if receipt.BackendReportedEvidence == nil || receipt.BackendReportedEvidence.Status != "OPTIMAL" || receipt.BackendReportedEvidence.Trace == nil || !receipt.BackendReportedEvidence.Trace.Complete || len(receipt.BackendReportedEvidence.Trace.Phases) != 5 || !validDigest(receipt.BackendReportedEvidence.Trace.PlanDigest) {
		t.Fatalf("CP-SAT evidence = %#v", receipt.BackendReportedEvidence)
	}
	if err := VerifyDiscreteSolverReceipt(request, receipt); err != nil {
		t.Fatal(err)
	}
}

func TestExternalProcessDiscreteBackendPreservesCpSatTerminationStates(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	tests := []struct {
		name        string
		status      int32
		choices     []DiscreteChoice
		deadline    bool
		termination SolverTermination
		wantResult  bool
	}{
		{"feasible incumbent", cpSatStatusFeasible, testDiscreteChoices(), true, SolverTimeLimit, true},
		{"feasible without deadline", cpSatStatusFeasible, testDiscreteChoices(), false, SolverFeasible, true},
		{"infeasible", cpSatStatusInfeasible, nil, false, SolverInfeasible, false},
		{"unknown", cpSatStatusUnknown, nil, false, SolverUnknown, false},
		{"deadline unknown", cpSatStatusUnknown, nil, true, SolverTimeLimit, false},
		{"model invalid", cpSatStatusModelInvalid, nil, false, SolverError, false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			response := testCpSatProcessResponse(t, request, testCase.status, testCase.choices, testCase.deadline)
			path, digest := writeDiscreteProcessFixture(t, response)
			receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{testExternalDiscreteProcessBackend(path, digest)}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != testCase.termination || (receipt.Result != nil) != testCase.wantResult {
				t.Fatalf("CP-SAT termination receipt = %#v", receipt)
			}
			if receipt.BackendReportedEvidence == nil || receipt.BackendReportedEvidence.Trace == nil {
				t.Fatalf("CP-SAT terminal state lacks an ordered trace: %#v", receipt)
			}
			forged := cloneSolverJSON(t, receipt)
			forged.BackendReportedEvidence.Trace = nil
			forged.BackendReportedEvidence.ProofKind = ""
			forged.BackendReportedEvidence.ModelDigest = testExecutableDigest
			forged.BackendReportedEvidence.ParametersDigest = testExecutableDigest
			forged.BackendReportedEvidence.ResponseDigest = testExecutableDigest
			forged.ID, _ = discreteSolverReceiptID(forged)
			if err := VerifyDiscreteSolverReceipt(request, forged); err == nil {
				t.Fatal("accepted a CP-SAT terminal receipt with its ordered trace omitted")
			}
		})
	}
}

func TestExternalProcessDiscreteBackendBindsLaterCpSatTerminalStates(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat"})
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	base := testCpSatProcessResponses(t, request, testDiscreteChoices())
	tests := []struct {
		name        string
		status      int32
		deadline    bool
		termination SolverTermination
		wantResult  bool
	}{
		{"unknown", cpSatStatusUnknown, false, SolverUnknown, false},
		{"deadline unknown preserves prefix", cpSatStatusUnknown, true, SolverTimeLimit, true},
		{"model invalid", cpSatStatusModelInvalid, false, SolverError, false},
		{"infeasible contradiction", cpSatStatusInfeasible, false, SolverError, false},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			responses := append([]ExternalDiscreteSolverProcessResponse(nil), base[:2]...)
			responses[1].ResponseBase64 = base64.StdEncoding.EncodeToString(encodeTestCpSatResponse(testCase.status, nil, nil))
			responses[1].DeadlineTriggered = testCase.deadline
			path, digest := writeDiscreteProcessFixtures(t, responses)
			receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{testExternalDiscreteProcessBackend(path, digest)}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != testCase.termination || (receipt.Result != nil) != testCase.wantResult || receipt.BackendReportedEvidence == nil || receipt.BackendReportedEvidence.Trace == nil || len(receipt.BackendReportedEvidence.Trace.Phases) != 2 {
				t.Fatalf("later terminal receipt = %#v", receipt)
			}
			forged := cloneSolverJSON(t, receipt)
			forged.BackendReportedEvidence.Trace.Phases = forged.BackendReportedEvidence.Trace.Phases[:1]
			forged.BackendReportedEvidence.Trace.PlanDigest = cpSatPlanDigest(request.ID, *forged.BackendReportedEvidence.Trace)
			forged.ID, _ = discreteSolverReceiptID(forged)
			if err := VerifyDiscreteSolverReceipt(request, forged); err == nil {
				t.Fatal("accepted omission of a later CP-SAT terminal phase")
			}
		})
	}
}

func TestExternalProcessDiscreteBackendFailsClosedOnForeignOrMalformedEvidence(t *testing.T) {
	request := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "cp-sat", AuthorizedFallbacks: []string{NativeBackendName}})
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	valid := testCpSatProcessResponse(t, request, cpSatStatusOptimal, testDiscreteChoices(), false)
	inconsistentVector := mutateCpSatProcessSolution(t, request, valid, func(compiled cpSatCompiledModel, response *cpSatResponse) {
		response.Solution[compiled.MakespanVariable]++
	})
	modelInvalidSolution := mutateCpSatProcessSolution(t, request, valid, func(_ cpSatCompiledModel, response *cpSatResponse) {
		response.Status = cpSatStatusModelInvalid
		response.InnerObjectiveLowerBound = nil
	})

	tests := []struct {
		name     string
		response ExternalDiscreteSolverProcessResponse
		want     string
	}{
		{"foreign request", mutateDiscreteProcessResponse(valid, func(value *ExternalDiscreteSolverProcessResponse) {
			value.RequestID = "dsolreq_" + strings.Repeat("a", 64)
		}), "does not bind"},
		{"foreign model", mutateDiscreteProcessResponse(valid, func(value *ExternalDiscreteSolverProcessResponse) {
			value.ModelDigest = "sha256:" + strings.Repeat("a", 64)
		}), "does not bind"},
		{"foreign parameters", mutateDiscreteProcessResponse(valid, func(value *ExternalDiscreteSolverProcessResponse) {
			value.ParametersDigest = "sha256:" + strings.Repeat("a", 64)
		}), "does not bind"},
		{"malformed protobuf", mutateDiscreteProcessResponse(valid, func(value *ExternalDiscreteSolverProcessResponse) {
			value.ResponseBase64 = base64.StdEncoding.EncodeToString([]byte{0x12, 0x02, 0x01})
		}), "malformed protobuf"},
		{"malformed base64", mutateDiscreteProcessResponse(valid, func(value *ExternalDiscreteSolverProcessResponse) { value.ResponseBase64 = "***" }), "invalid bounded protobuf"},
		{"inconsistent solution vector", inconsistentVector, "independent phase verification"},
		{"model invalid with solution", modelInvalidSolution, "contradicts its non-solution status"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			path, digest := writeDiscreteProcessFixture(t, testCase.response)
			receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{testExternalDiscreteProcessBackend(path, digest), NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: runtime.GOARCH}}, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Termination != SolverError || receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != "cp-sat" || receipt.FallbackFrom != "" || !strings.Contains(receipt.Explanation, testCase.want) {
				t.Fatalf("fail-closed process receipt = %#v", receipt)
			}
		})
	}

	encoded, err := json.Marshal(map[string]any{
		"apiVersion": valid.APIVersion, "kind": valid.Kind, "requestId": valid.RequestID,
		"modelDigest": valid.ModelDigest, "parametersDigest": valid.ParametersDigest,
		"responseBase64": valid.ResponseBase64, "deadlineTriggered": false,
		"providerAuthoredResult": map[string]any{"makespanSeconds": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	path, digest := writeExternalSolverRawFixture(t, string(encoded))
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{testExternalDiscreteProcessBackend(path, digest)}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Termination != SolverError || receipt.Explanation != "CP-SAT process emitted an invalid response envelope" {
		t.Fatalf("provider-authored result was not rejected: %#v", receipt)
	}
}

func TestCpSatWatchdogAndRawBoundsReserveHostControl(t *testing.T) {
	callerCtx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Second))
	defer cancel()
	if got := cpSatWatchdogMilliseconds(callerCtx, 5_000); got != 5_000 {
		t.Fatalf("caller deadline was converted into solver watchdog: %d", got)
	}
	configuredDeadline := time.Now().Add(2 * time.Second)
	configuredCtx := context.WithValue(context.Background(), discreteConfiguredDeadlineContextKey{}, configuredDeadline)
	if got := cpSatWatchdogMilliseconds(configuredCtx, 5_000); got < 1 || got >= 2_000 {
		t.Fatalf("configured watchdog did not reserve host grace: %d", got)
	}
	raw, err := cpSatRawResponseLimit(defaultMaximumSolverOutput)
	if err != nil || raw <= 0 || base64.StdEncoding.EncodedLen(raw)+(2<<10) > defaultMaximumSolverOutput {
		t.Fatalf("raw/envelope bounds are inconsistent: raw=%d err=%v", raw, err)
	}
	if _, err := cpSatRawResponseLimit(2 << 10); err == nil {
		t.Fatal("accepted output bound too small for response envelope")
	}
}

func TestExternalCpSatCompilePreflightAcceptsLargeIterativeObjective(t *testing.T) {
	recommendation := testDiscreteRecommendation()
	recommendation.Problem.HorizonSeconds = maximumDiscreteHorizonSeconds
	for index := range recommendation.Problem.Tasks {
		for lane := range recommendation.Problem.Tasks[index].EstimatedCostMicros {
			recommendation.Problem.Tasks[index].EstimatedCostMicros[lane] = maximumDiscreteQuantity
		}
	}
	request, err := NewDiscreteSolverRequest(recommendation, SolverSelection{PreferredBackend: "cp-sat", AuthorizedFallbacks: []string{NativeBackendName}}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	response := testCpSatProcessResponse(t, request, cpSatStatusInfeasible, nil, false)
	path, digest := writeDiscreteProcessFixture(t, response)
	now := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	receipt, err := RunDiscreteSolverRequest(context.Background(), request, []DiscreteSolverBackend{
		testExternalDiscreteProcessBackend(path, digest),
		NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: runtime.GOARCH},
	}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != "cp-sat" || receipt.FallbackFrom != "" || receipt.Termination != SolverInfeasible {
		t.Fatalf("CP-SAT iterative preflight rejected a bounded request = %#v", receipt)
	}
}

func testExternalDiscreteProcessBackend(path, digest string) ExternalProcessDiscreteSolverBackend {
	return ExternalProcessDiscreteSolverBackend{
		BackendDescriptor: SolverBackendDescriptor{
			Name: "cp-sat", Version: "9.15.6755-unadmitted.1", ExecutableDigest: digest,
			Architecture: runtime.GOARCH, Capabilities: []string{DiscreteScheduleCapability},
		},
		ExecutablePath: path,
	}
}

func testCpSatProcessResponse(t *testing.T, request DiscreteSolverRequest, status int32, choices []DiscreteChoice, deadline bool) ExternalDiscreteSolverProcessResponse {
	t.Helper()
	compiled, err := compileCpSatModel(request)
	if err != nil {
		t.Fatal(err)
	}
	var solution []int64
	var lowerBound *int64
	if status == cpSatStatusOptimal || status == cpSatStatusFeasible {
		solution = make([]int64, compiled.VariableCount)
		choiceByTask := make(map[string]DiscreteChoice, len(choices))
		for _, choice := range choices {
			choiceByTask[choice.Task] = choice
		}
		for _, binding := range compiled.Bindings {
			choice := choiceByTask[binding.Task]
			task := discreteTaskByID(request.Recommendation.Problem.Tasks, binding.Task)
			solution[binding.Start], solution[binding.End] = choice.Start, choice.Start+task.DurationSeconds
			solution[binding.LaneBooleans[choice.Lane]] = 1
		}
		result, resultErr := NewDiscreteScheduleResult(request.Recommendation.Problem, choices)
		if resultErr != nil {
			t.Fatal(resultErr)
		}
		solution[compiled.MakespanVariable] = result.MakespanSeconds
		incumbent, incumbentErr := cpSatPhaseValueFromResult(request, compiled.Phase, result)
		if incumbentErr != nil {
			t.Fatal(incumbentErr)
		}
		if status == cpSatStatusOptimal {
			lowerBound = &incumbent
		} else {
			bound := int64(0)
			lowerBound = &bound
		}
	}
	raw := encodeTestCpSatResponse(status, solution, lowerBound)
	return ExternalDiscreteSolverProcessResponse{
		APIVersion: ExternalDiscreteSolverProcessAPIVersion, Kind: "ExternalDiscreteSolverProcessResponse",
		RequestID: request.ID, ModelDigest: digestBytes(compiled.Model), ParametersDigest: digestBytes(compiled.Parameters),
		ResponseBase64: base64.StdEncoding.EncodeToString(raw), DeadlineTriggered: deadline,
	}
}

func testCpSatProcessResponses(t *testing.T, request DiscreteSolverRequest, choices []DiscreteChoice) []ExternalDiscreteSolverProcessResponse {
	t.Helper()
	planner, err := newCpSatPhasePlanner(request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := NewDiscreteScheduleResult(request.Recommendation.Problem, choices)
	if err != nil {
		t.Fatal(err)
	}
	responses := make([]ExternalDiscreteSolverProcessResponse, 0, len(planner.phases))
	for !planner.complete() {
		compiled, err := planner.compileNextPhase()
		if err != nil {
			t.Fatal(err)
		}
		solution := make([]int64, compiled.VariableCount)
		choiceByTask := make(map[string]DiscreteChoice, len(choices))
		for _, choice := range choices {
			choiceByTask[choice.Task] = choice
		}
		for _, binding := range compiled.Bindings {
			choice := choiceByTask[binding.Task]
			task := discreteTaskByID(request.Recommendation.Problem.Tasks, binding.Task)
			solution[binding.Start], solution[binding.End] = choice.Start, choice.Start+task.DurationSeconds
			solution[binding.LaneBooleans[choice.Lane]] = 1
		}
		solution[compiled.MakespanVariable] = result.MakespanSeconds
		value, err := cpSatPhaseValueFromResult(request, compiled.Phase, result)
		if err != nil {
			t.Fatal(err)
		}
		var serializedBound *int64
		if value != 0 {
			serializedBound = &value
		}
		raw := encodeTestCpSatResponse(cpSatStatusOptimal, solution, serializedBound)
		responses = append(responses, ExternalDiscreteSolverProcessResponse{
			APIVersion: ExternalDiscreteSolverProcessAPIVersion, Kind: "ExternalDiscreteSolverProcessResponse",
			RequestID: request.ID, ModelDigest: digestBytes(compiled.Model), ParametersDigest: digestBytes(compiled.Parameters),
			ResponseBase64: base64.StdEncoding.EncodeToString(raw),
		})
		if err := planner.acceptExactPhase(compiled, value); err != nil {
			t.Fatal(err)
		}
	}
	return responses
}

func writeDiscreteProcessFixture(t *testing.T, response ExternalDiscreteSolverProcessResponse) (string, string) {
	return writeDiscreteProcessFixtures(t, []ExternalDiscreteSolverProcessResponse{response})
}

func writeDiscreteProcessFixtures(t *testing.T, responses []ExternalDiscreteSolverProcessResponse) (string, string) {
	t.Helper()
	counter := t.TempDir() + "/phase"
	script := "#!/bin/sh\nIFS= read -r _request || true\n"
	script += "if [ -n \"${PLATFORM_EXTERNAL_SOLVER_TEST_SECRET+x}\" ]; then printf '%s\\n' 'inherited environment' >&2; exit 7; fi\n"
	script += "phase=0; if [ -f '" + shellSingleQuote(counter) + "' ]; then phase=$(sed -n '1p' '" + shellSingleQuote(counter) + "'); fi\n"
	script += "next=$((phase + 1)); printf '%s\\n' \"$next\" > '" + shellSingleQuote(counter) + "'\ncase \"$phase\" in\n"
	for index, response := range responses {
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		script += fmt.Sprintf("%d) printf '%%s\\n' '%s' ;;\n", index, shellSingleQuote(string(encoded)))
	}
	script += "*) exit 9 ;;\nesac\n"
	return writeExternalSolverScript(t, script)
}

func mutateDiscreteProcessResponse(value ExternalDiscreteSolverProcessResponse, mutate func(*ExternalDiscreteSolverProcessResponse)) ExternalDiscreteSolverProcessResponse {
	mutate(&value)
	return value
}

func mutateCpSatProcessSolution(t *testing.T, request DiscreteSolverRequest, value ExternalDiscreteSolverProcessResponse, mutate func(cpSatCompiledModel, *cpSatResponse)) ExternalDiscreteSolverProcessResponse {
	t.Helper()
	compiled, err := compileCpSatModel(request)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(value.ResponseBase64)
	if err != nil {
		t.Fatal(err)
	}
	response, err := parseCpSatResponse(raw, compiled.VariableCount)
	if err != nil {
		t.Fatal(err)
	}
	mutate(compiled, &response)
	value.ResponseBase64 = base64.StdEncoding.EncodeToString(encodeTestCpSatResponse(response.Status, response.Solution, response.InnerObjectiveLowerBound))
	return value
}
