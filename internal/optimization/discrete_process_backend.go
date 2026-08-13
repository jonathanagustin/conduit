package optimization

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	ExternalDiscreteSolverProcessAPIVersion = "optimization.platform.example/external-discrete-solver-process/v1alpha1"
	CpSatBackendName                        = "cp-sat"
	maximumCpSatResponseBytes               = 4 << 20
	cpSatShutdownGrace                      = 250 * time.Millisecond
)

type ExternalDiscreteSolverProcessRequest struct {
	APIVersion           string `json:"apiVersion"`
	Kind                 string `json:"kind"`
	RequestID            string `json:"requestId"`
	ModelDigest          string `json:"modelDigest"`
	ParametersDigest     string `json:"parametersDigest"`
	ModelBase64          string `json:"modelBase64"`
	ParametersBase64     string `json:"parametersBase64"`
	WatchdogMilliseconds int64  `json:"watchdogMilliseconds"`
	MaximumResponseBytes int    `json:"maximumResponseBytes"`
}

type ExternalDiscreteSolverProcessResponse struct {
	APIVersion        string `json:"apiVersion"`
	Kind              string `json:"kind"`
	RequestID         string `json:"requestId"`
	ModelDigest       string `json:"modelDigest"`
	ParametersDigest  string `json:"parametersDigest"`
	ResponseBase64    string `json:"responseBase64"`
	DeadlineTriggered bool   `json:"deadlineTriggered"`
}

// ExternalProcessDiscreteSolverBackend is the hexagonal adapter for a
// digest-pinned CP-SAT process. The process receives compiler-owned protobuf
// bytes and returns only the raw CpSolverResponse. Platform maps variables back
// to task/lane/start choices and independently derives every other result.
type ExternalProcessDiscreteSolverBackend struct {
	BackendDescriptor  SolverBackendDescriptor
	ExecutablePath     string
	StagingDirectory   string
	MaximumOutputBytes int
}

func (backend ExternalProcessDiscreteSolverBackend) Descriptor() SolverBackendDescriptor {
	return canonicalBackendDescriptor(backend.BackendDescriptor)
}

func (backend ExternalProcessDiscreteSolverBackend) SolveDiscrete(ctx context.Context, request DiscreteSolverRequest) (DiscreteBackendOutcome, error) {
	if err := VerifyDiscreteSolverRequest(request); err != nil {
		return DiscreteBackendOutcome{}, err
	}
	descriptor := backend.Descriptor()
	if err := validateBackendDescriptor(descriptor); err != nil {
		return DiscreteBackendOutcome{}, err
	}
	if !containsString(descriptor.Capabilities, DiscreteScheduleCapability) {
		return DiscreteBackendOutcome{}, ErrSolverBackendUnavailable
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(context.Cause(ctx), errConfiguredDiscreteSolverTimeLimit) {
			return DiscreteBackendOutcome{Termination: SolverTimeLimit, Explanation: "CP-SAT time limit elapsed before execution"}, nil
		}
		return DiscreteBackendOutcome{Termination: SolverUnknown, Explanation: "CP-SAT execution was stopped by its caller context"}, nil
	}
	planner, err := newCpSatPhasePlanner(request)
	if err != nil {
		return DiscreteBackendOutcome{}, ErrSolverBackendUnavailable
	}
	process := ExternalProcessSolverBackend{
		BackendDescriptor: descriptor, ExecutablePath: backend.ExecutablePath,
		StagingDirectory: backend.StagingDirectory, MaximumOutputBytes: backend.MaximumOutputBytes,
	}
	if err := validateExternalSolverProcessConfiguration(process); err != nil {
		return DiscreteBackendOutcome{}, err
	}
	staged, cleanup, stageErr := process.stageVerifiedExecutable()
	if stageErr != nil {
		if errors.Is(stageErr, os.ErrNotExist) {
			return DiscreteBackendOutcome{}, ErrSolverBackendUnavailable
		}
		return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT executable failed integrity admission"}, nil
	}
	defer cleanup()

	maximumOutput := backend.MaximumOutputBytes
	if maximumOutput == 0 {
		maximumOutput = defaultMaximumSolverOutput
	}
	rawResponseMaximum, err := cpSatRawResponseLimit(maximumOutput)
	if err != nil {
		return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT output bounds cannot contain a response envelope"}, nil
	}
	phases := make([]DiscreteCpSatPhaseProof, 0, len(planner.phases))
	objectives := make([]SolverObjectiveProof, 0, 2)
	var lastChoices []DiscreteChoice
	for !planner.complete() {
		if err := ctx.Err(); err != nil {
			if errors.Is(context.Cause(ctx), errConfiguredDiscreteSolverTimeLimit) {
				return cpSatDeadlineBetweenPhases(request, phases, objectives, lastChoices), nil
			}
			return DiscreteBackendOutcome{Termination: SolverUnknown, Explanation: "CP-SAT execution was stopped by its caller context"}, nil
		}
		compiled, err := planner.compileNextPhase()
		if err != nil {
			return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT phase compilation failed"}, nil
		}
		execution, err := backend.executeCpSatPhase(ctx, staged, request, compiled, maximumOutput, rawResponseMaximum)
		if err != nil {
			if errors.Is(err, errCpSatConfiguredDeadline) {
				return cpSatDeadlineBetweenPhases(request, phases, objectives, lastChoices), nil
			}
			if errors.Is(err, errCpSatCallerCanceled) {
				return DiscreteBackendOutcome{Termination: SolverUnknown, Explanation: "CP-SAT process was stopped by its caller context"}, nil
			}
			return DiscreteBackendOutcome{Termination: SolverError, Explanation: boundedSolverExplanation(err.Error())}, nil
		}
		status, err := cpSatStatusName(execution.Response.Status)
		if err != nil {
			return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT returned an unknown status"}, nil
		}
		proof := DiscreteCpSatPhaseProof{
			Index: compiled.Phase.Index, Kind: compiled.Phase.Kind, Key: compiled.Phase.Key, Unit: compiled.Phase.Unit,
			ModelDigest: execution.ModelDigest, ParametersDigest: execution.ParametersDigest, Status: status,
		}
		switch execution.Response.Status {
		case cpSatStatusOptimal, cpSatStatusFeasible:
			choices, _, value, err := cpSatVerifiedPhaseResult(request, planner, compiled, execution.Response)
			if err != nil {
				return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT solution failed independent phase verification"}, nil
			}
			proof.Incumbent = strconv.FormatInt(value, 10)
			// CpSolverResponse.inner_objective_lower_bound is a proto3 scalar.
			// The official serializer omits an exact zero even though the field's
			// semantic value is zero. A zero incumbent under OPTIMAL therefore
			// proves the same exact equality without an on-wire field.
			zeroOptimalBound := execution.Response.Status == cpSatStatusOptimal && value == 0 && execution.Response.InnerObjectiveLowerBound == nil
			if execution.Response.InnerObjectiveLowerBound != nil {
				bound := *execution.Response.InnerObjectiveLowerBound
				if bound < 0 || bound > value {
					return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT returned an inconsistent exact phase bound"}, nil
				}
				proof.BestBound = strconv.FormatInt(bound, 10)
				proof.Gap = strconv.FormatInt(value-bound, 10)
			} else if zeroOptimalBound {
				proof.BestBound = "0"
				proof.Gap = "0"
			}
			proof.ResponseDigest = cpSatPhaseSemanticDigest(request.ID, proof)
			phases = append(phases, proof)
			lastChoices = choices
			if execution.Response.Status == cpSatStatusFeasible {
				evidence := cpSatTraceEvidence(request, status, phases, objectives, false, nil)
				if execution.DeadlineTriggered {
					return DiscreteBackendOutcome{Termination: SolverTimeLimit, BackendReportedEvidence: evidence, Choices: choices, Explanation: "CP-SAT stopped with a feasible incumbent at its watchdog deadline"}, nil
				}
				return DiscreteBackendOutcome{Termination: SolverFeasible, BackendReportedEvidence: evidence, Choices: choices}, nil
			}
			if !zeroOptimalBound && (execution.Response.InnerObjectiveLowerBound == nil || *execution.Response.InnerObjectiveLowerBound != value) {
				return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT optimal phase lacks an equal exact integer bound"}, nil
			}
			if compiled.Phase.Kind == "OBJECTIVE" {
				encoded := strconv.FormatInt(value, 10)
				objectives = append(objectives, SolverObjectiveProof{Objective: compiled.Phase.Key, Unit: compiled.Phase.Unit, Incumbent: encoded, BestBound: encoded, Gap: "0"})
			}
			if err := planner.acceptExactPhase(compiled, value); err != nil {
				return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT exact phase fix failed"}, nil
			}
		case cpSatStatusInfeasible, cpSatStatusModelInvalid, cpSatStatusUnknown:
			if len(execution.Response.Solution) != 0 {
				return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT terminal response contradicts its non-solution status"}, nil
			}
			proof.ResponseDigest = cpSatPhaseSemanticDigest(request.ID, proof)
			phases = append(phases, proof)
			evidence := cpSatTraceEvidence(request, status, phases, objectives, false, nil)
			switch execution.Response.Status {
			case cpSatStatusInfeasible:
				if compiled.Phase.Index == 0 {
					return DiscreteBackendOutcome{Termination: SolverInfeasible, BackendReportedEvidence: evidence, Explanation: "CP-SAT proved the bounded schedule infeasible"}, nil
				}
				return DiscreteBackendOutcome{Termination: SolverError, BackendReportedEvidence: evidence, Explanation: "CP-SAT contradicted an exact earlier phase fix"}, nil
			case cpSatStatusModelInvalid:
				return DiscreteBackendOutcome{Termination: SolverError, BackendReportedEvidence: evidence, Explanation: "CP-SAT rejected the compiler-owned model"}, nil
			default:
				if execution.DeadlineTriggered {
					explanation := "CP-SAT watchdog stopped search without an incumbent"
					if len(lastChoices) > 0 {
						explanation = "CP-SAT watchdog stopped search with its last exact incumbent"
					}
					return DiscreteBackendOutcome{Termination: SolverTimeLimit, BackendReportedEvidence: evidence, Choices: append([]DiscreteChoice(nil), lastChoices...), Explanation: explanation}, nil
				}
				return DiscreteBackendOutcome{Termination: SolverUnknown, BackendReportedEvidence: evidence, Explanation: "CP-SAT could not classify the bounded schedule"}, nil
			}
		default:
			return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT returned an unsupported status"}, nil
		}
	}
	if len(lastChoices) == 0 {
		return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT canonical phase plan completed without a result"}, nil
	}
	lastResult, err := NewDiscreteScheduleResult(request.Recommendation.Problem, lastChoices)
	if err != nil {
		return DiscreteBackendOutcome{Termination: SolverError, Explanation: "CP-SAT canonical phase plan completed with an invalid result"}, nil
	}
	evidence := cpSatTraceEvidence(request, "OPTIMAL", phases, objectives, true, &lastResult)
	return DiscreteBackendOutcome{Termination: SolverOptimal, BackendReportedEvidence: evidence, Choices: lastChoices}, nil
}

var (
	errCpSatConfiguredDeadline = errors.New("configured CP-SAT deadline elapsed")
	errCpSatCallerCanceled     = errors.New("CP-SAT caller canceled")
)

type cpSatPhaseExecution struct {
	Response          cpSatResponse
	DeadlineTriggered bool
	ModelDigest       string
	ParametersDigest  string
}

func (backend ExternalProcessDiscreteSolverBackend) executeCpSatPhase(ctx context.Context, staged string, request DiscreteSolverRequest, compiled cpSatCompiledModel, maximumOutput, rawResponseMaximum int) (cpSatPhaseExecution, error) {
	modelDigest := digestBytes(compiled.Model)
	parametersDigest := digestBytes(compiled.Parameters)
	processRequest := ExternalDiscreteSolverProcessRequest{
		APIVersion: ExternalDiscreteSolverProcessAPIVersion, Kind: "ExternalDiscreteSolverProcessRequest",
		RequestID: request.ID, ModelDigest: modelDigest, ParametersDigest: parametersDigest,
		ModelBase64: base64.StdEncoding.EncodeToString(compiled.Model), ParametersBase64: base64.StdEncoding.EncodeToString(compiled.Parameters),
		WatchdogMilliseconds: cpSatWatchdogMilliseconds(ctx, request.Limits.TimeLimitMilliseconds), MaximumResponseBytes: rawResponseMaximum,
	}
	input, err := json.Marshal(processRequest)
	if err != nil {
		return cpSatPhaseExecution{}, fmt.Errorf("encode external CP-SAT request: %w", err)
	}
	stdout := &boundedProcessBuffer{maximum: maximumOutput}
	stderr := &boundedProcessBuffer{maximum: maximumSolverExplanation}
	command := exec.CommandContext(ctx, staged)
	command.Dir = filepath.Dir(staged)
	command.Env = []string{"LANG=C", "LC_ALL=C", "TZ=UTC"}
	command.Stdin = bytes.NewReader(input)
	command.Stdout = stdout
	command.Stderr = stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = 250 * time.Millisecond
	runErr := command.Run()
	if err := ctx.Err(); err != nil {
		if errors.Is(context.Cause(ctx), errConfiguredDiscreteSolverTimeLimit) {
			return cpSatPhaseExecution{}, errCpSatConfiguredDeadline
		}
		return cpSatPhaseExecution{}, errCpSatCallerCanceled
	}
	if integrityErr := verifyFileDigest(staged, backend.Descriptor().ExecutableDigest); integrityErr != nil {
		return cpSatPhaseExecution{}, errors.New("CP-SAT staged executable changed during execution")
	}
	if errors.Is(stdout.err, errBoundedProcessOutput) || errors.Is(stderr.err, errBoundedProcessOutput) {
		return cpSatPhaseExecution{}, errors.New("CP-SAT process exceeded its bounded output contract")
	}
	if runErr != nil {
		return cpSatPhaseExecution{}, errors.New("CP-SAT process failed")
	}
	response, err := decodeExternalDiscreteSolverProcessResponse(stdout.Bytes())
	if err != nil {
		return cpSatPhaseExecution{}, errors.New("CP-SAT process emitted an invalid response envelope")
	}
	if response.RequestID != request.ID || response.ModelDigest != modelDigest || response.ParametersDigest != parametersDigest {
		return cpSatPhaseExecution{}, errors.New("CP-SAT process response does not bind request, model, and parameters")
	}
	rawResponse, err := decodeBoundedBase64(response.ResponseBase64, rawResponseMaximum)
	if err != nil {
		return cpSatPhaseExecution{}, errors.New("CP-SAT process emitted an invalid bounded protobuf response")
	}
	parsed, err := parseCpSatResponse(rawResponse, compiled.VariableCount)
	if err != nil {
		return cpSatPhaseExecution{}, errors.New("CP-SAT process emitted a malformed protobuf response")
	}
	return cpSatPhaseExecution{Response: parsed, DeadlineTriggered: response.DeadlineTriggered, ModelDigest: modelDigest, ParametersDigest: parametersDigest}, nil
}

func cpSatVerifiedPhaseResult(request DiscreteSolverRequest, planner *cpSatPhasePlanner, compiled cpSatCompiledModel, response cpSatResponse) ([]DiscreteChoice, DiscreteScheduleResult, int64, error) {
	choices, err := choicesFromCpSatResponse(compiled, response)
	if err != nil {
		return nil, DiscreteScheduleResult{}, 0, err
	}
	result, err := NewDiscreteScheduleResult(request.Recommendation.Problem, choices)
	if err != nil || validateCpSatSolutionProjection(compiled, response, result) != nil || planner.validateFixedPrefix(result) != nil {
		return nil, DiscreteScheduleResult{}, 0, errors.New("CP-SAT result projection failed")
	}
	value, err := cpSatPhaseValue(compiled, response, request, result)
	return choices, result, value, err
}

func cpSatPhaseSemanticDigest(requestID string, proof DiscreteCpSatPhaseProof) string {
	proof.ResponseDigest = ""
	digest, _ := digestJSON(struct {
		RequestID string                  `json:"requestId"`
		Revision  string                  `json:"revision"`
		Phase     DiscreteCpSatPhaseProof `json:"phase"`
	}{requestID, cpSatCanonicalRevision, proof})
	return digest
}

func cpSatPlanDigest(requestID string, trace DiscreteCpSatProofTrace) string {
	digest, _ := digestJSON(struct {
		RequestID  string                    `json:"requestId"`
		Revision   string                    `json:"revision"`
		Complete   bool                      `json:"complete"`
		StopReason string                    `json:"stopReason"`
		Phases     []DiscreteCpSatPhaseProof `json:"phases"`
	}{requestID, trace.Revision, trace.Complete, trace.StopReason, append([]DiscreteCpSatPhaseProof(nil), trace.Phases...)})
	return digest
}

func cpSatCanonicalResultDigest(request DiscreteSolverRequest, result DiscreteScheduleResult) string {
	digest, _ := digestJSON(struct {
		RequestID string                 `json:"requestId"`
		Revision  string                 `json:"revision"`
		Result    DiscreteScheduleResult `json:"result"`
	}{request.ID, cpSatCanonicalRevision, result})
	return digest
}

func cpSatTraceEvidence(request DiscreteSolverRequest, status string, phases []DiscreteCpSatPhaseProof, objectives []SolverObjectiveProof, complete bool, result *DiscreteScheduleResult) *DiscreteBackendReportedEvidence {
	stopReason := "SOLVER_TERMINAL"
	if complete {
		stopReason = "COMPLETE"
	}
	trace := DiscreteCpSatProofTrace{Revision: cpSatCanonicalRevision, Complete: complete, StopReason: stopReason, Phases: append([]DiscreteCpSatPhaseProof(nil), phases...)}
	if complete && result != nil {
		trace.CanonicalResultDigest = cpSatCanonicalResultDigest(request, *result)
	}
	trace.PlanDigest = cpSatPlanDigest(request.ID, trace)
	return &DiscreteBackendReportedEvidence{Status: status, ProofKind: "CP_SAT_EXACT_LEXICOGRAPHIC_V1", Trace: &trace, Objectives: append([]SolverObjectiveProof(nil), objectives...)}
}

func cpSatDeadlineBetweenPhases(request DiscreteSolverRequest, phases []DiscreteCpSatPhaseProof, objectives []SolverObjectiveProof, choices []DiscreteChoice) DiscreteBackendOutcome {
	outcome := DiscreteBackendOutcome{Termination: SolverTimeLimit, Explanation: "CP-SAT cumulative phase deadline elapsed"}
	if len(phases) > 0 {
		outcome.BackendReportedEvidence = cpSatTraceEvidence(request, "UNKNOWN", phases, objectives, false, nil)
		outcome.BackendReportedEvidence.Trace.StopReason = "HOST_DEADLINE"
		outcome.BackendReportedEvidence.Trace.PlanDigest = cpSatPlanDigest(request.ID, *outcome.BackendReportedEvidence.Trace)
		outcome.Choices = append([]DiscreteChoice(nil), choices...)
	}
	return outcome
}

func validateCpSatSolutionProjection(compiled cpSatCompiledModel, response cpSatResponse, result DiscreteScheduleResult) error {
	assignments := make(map[string]DiscreteAssignment, len(result.Assignments))
	for _, assignment := range result.Assignments {
		assignments[assignment.Task] = assignment
	}
	for _, binding := range compiled.Bindings {
		assignment := assignments[binding.Task]
		if response.Solution[binding.Start] != assignment.Start || response.Solution[binding.End] != assignment.Finish {
			return errors.New("CP-SAT task variables do not match reconstructed assignment")
		}
	}
	if response.Solution[compiled.MakespanVariable] != result.MakespanSeconds {
		return errors.New("CP-SAT makespan variable does not match reconstructed makespan")
	}
	return nil
}

func cpSatWatchdogMilliseconds(ctx context.Context, requested int64) int64 {
	configuredDeadline, exists := ctx.Value(discreteConfiguredDeadlineContextKey{}).(time.Time)
	if !exists {
		return requested
	}
	remaining := time.Until(configuredDeadline) - cpSatShutdownGrace
	if remaining < time.Millisecond {
		return 1
	}
	milliseconds := remaining.Milliseconds()
	return min(requested, milliseconds)
}

func cpSatRawResponseLimit(maximumEnvelope int) (int, error) {
	const conservativeJSONEnvelopeBytes = 2 << 10
	if maximumEnvelope <= conservativeJSONEnvelopeBytes+4 {
		return 0, errors.New("CP-SAT response envelope bound is too small")
	}
	raw := ((maximumEnvelope - conservativeJSONEnvelopeBytes) / 4) * 3
	return min(raw, maximumCpSatResponseBytes), nil
}

func decodeExternalDiscreteSolverProcessResponse(encoded []byte) (ExternalDiscreteSolverProcessResponse, error) {
	var response ExternalDiscreteSolverProcessResponse
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&response); err != nil {
		return response, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return response, errors.New("external CP-SAT response contains trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return response, errors.New("external CP-SAT response has invalid trailing data")
	}
	if response.APIVersion != ExternalDiscreteSolverProcessAPIVersion || response.Kind != "ExternalDiscreteSolverProcessResponse" || !validContentID(response.RequestID, "dsolreq_") || !validDigest(response.ModelDigest) || !validDigest(response.ParametersDigest) || response.ResponseBase64 == "" {
		return response, errors.New("unsupported external CP-SAT response")
	}
	return response, nil
}

func decodeBoundedBase64(encoded string, maximum int) ([]byte, error) {
	if encoded == "" || len(encoded) > base64.StdEncoding.EncodedLen(maximum) {
		return nil, errors.New("base64 value exceeds its bound")
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) == 0 || len(decoded) > maximum {
		return nil, errors.New("invalid bounded base64 value")
	}
	return decoded, nil
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(digest[:])
}
