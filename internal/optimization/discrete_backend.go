package optimization

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"time"
)

const NativeDiscreteBackendVersion = "1.0.0"

var errConfiguredDiscreteSolverTimeLimit = errors.New("configured discrete solver time limit elapsed")

type discreteConfiguredDeadlineContextKey struct{}

type DiscreteBackendReportedEvidence struct {
	Status    string                   `json:"status"`
	ProofKind string                   `json:"proofKind,omitempty"`
	Trace     *DiscreteCpSatProofTrace `json:"trace,omitempty"`
	// Deprecated scalar proof fields remain wire-readable for pre-cutover
	// receipts, but the exact CP-SAT verifier accepts only the ordered trace.
	ModelDigest        string                           `json:"modelDigest,omitempty"`
	ParametersDigest   string                           `json:"parametersDigest,omitempty"`
	ResponseDigest     string                           `json:"responseDigest,omitempty"`
	CompositeObjective *DiscreteCompositeObjectiveProof `json:"compositeObjective,omitempty"`
	Objectives         []SolverObjectiveProof           `json:"objectives,omitempty"`
}

type DiscreteCpSatProofTrace struct {
	Revision              string                    `json:"revision"`
	Complete              bool                      `json:"complete"`
	StopReason            string                    `json:"stopReason"`
	PlanDigest            string                    `json:"planDigest"`
	CanonicalResultDigest string                    `json:"canonicalResultDigest,omitempty"`
	Phases                []DiscreteCpSatPhaseProof `json:"phases"`
}

type DiscreteCpSatPhaseProof struct {
	Index            int    `json:"index"`
	Kind             string `json:"kind"`
	Key              string `json:"key"`
	Unit             string `json:"unit"`
	ModelDigest      string `json:"modelDigest"`
	ParametersDigest string `json:"parametersDigest"`
	ResponseDigest   string `json:"responseDigest"`
	Status           string `json:"status"`
	Incumbent        string `json:"incumbent,omitempty"`
	BestBound        string `json:"bestBound,omitempty"`
	Gap              string `json:"gap,omitempty"`
}

type DiscreteCompositeObjectiveProof struct {
	Unit                 string `json:"unit"`
	Incumbent            string `json:"incumbent"`
	BestBound            string `json:"bestBound"`
	Gap                  string `json:"gap"`
	MakespanScale        string `json:"makespanScale"`
	MaximumSecondaryCost string `json:"maximumSecondaryCost"`
}

type DiscreteSolverReceipt struct {
	APIVersion              string                           `json:"apiVersion"`
	Kind                    string                           `json:"kind"`
	ID                      string                           `json:"id"`
	RequestID               string                           `json:"requestId"`
	RequestedBackend        string                           `json:"requestedBackend"`
	AttemptedBackends       []string                         `json:"attemptedBackends"`
	SelectedBackend         *SolverBackendDescriptor         `json:"selectedBackend,omitempty"`
	FallbackFrom            string                           `json:"fallbackFrom,omitempty"`
	Termination             SolverTermination                `json:"termination"`
	BackendReportedEvidence *DiscreteBackendReportedEvidence `json:"backendReportedEvidence,omitempty"`
	Result                  *DiscreteScheduleResult          `json:"result,omitempty"`
	Explanation             string                           `json:"explanation,omitempty"`
	StartedAt               string                           `json:"startedAt"`
	FinishedAt              string                           `json:"finishedAt"`
}

type DiscreteBackendOutcome struct {
	Termination             SolverTermination
	BackendReportedEvidence *DiscreteBackendReportedEvidence
	Choices                 []DiscreteChoice
	Explanation             string
}

type DiscreteSolverBackend interface {
	Descriptor() SolverBackendDescriptor
	SolveDiscrete(context.Context, DiscreteSolverRequest) (DiscreteBackendOutcome, error)
}

type NativeDiscreteSolverBackend struct {
	ExecutableDigest string
	Architecture     string
}

func (backend NativeDiscreteSolverBackend) Descriptor() SolverBackendDescriptor {
	return SolverBackendDescriptor{
		Name: NativeBackendName, Version: NativeDiscreteBackendVersion,
		ExecutableDigest: backend.ExecutableDigest, Architecture: backend.Architecture,
		Capabilities: []string{DiscreteScheduleCapability},
	}
}

func (backend NativeDiscreteSolverBackend) SolveDiscrete(ctx context.Context, request DiscreteSolverRequest) (DiscreteBackendOutcome, error) {
	if request.Determinism.Seed != 0 || request.Determinism.Workers != 1 {
		return DiscreteBackendOutcome{}, ErrSolverBackendUnavailable
	}
	choices, err := solveDiscreteGreedy(ctx, request.Recommendation.Problem, hasObjective(request.Recommendation.Objective.Secondary, "minimize_estimated_cost"))
	if errors.Is(err, context.DeadlineExceeded) && errors.Is(context.Cause(ctx), errConfiguredDiscreteSolverTimeLimit) {
		return DiscreteBackendOutcome{Termination: SolverTimeLimit, Explanation: "native discrete search reached its execution deadline"}, nil
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return DiscreteBackendOutcome{Termination: SolverUnknown, Explanation: "native discrete search was stopped by its caller context"}, nil
	}
	if err != nil {
		return DiscreteBackendOutcome{Termination: SolverUnknown, Explanation: boundedSolverExplanation(err.Error())}, nil
	}
	return DiscreteBackendOutcome{Termination: SolverFeasible, Choices: choices}, nil
}

func RunDiscreteSolverRequest(ctx context.Context, request DiscreteSolverRequest, backends []DiscreteSolverBackend, now func() time.Time) (DiscreteSolverReceipt, error) {
	if err := VerifyDiscreteSolverRequest(request); err != nil {
		return DiscreteSolverReceipt{}, err
	}
	if now == nil {
		return DiscreteSolverReceipt{}, errors.New("discrete solver clock is required")
	}
	available := make(map[string]DiscreteSolverBackend, len(backends))
	for _, backend := range backends {
		if backend == nil {
			return DiscreteSolverReceipt{}, errors.New("nil discrete solver backend")
		}
		descriptor := canonicalBackendDescriptor(backend.Descriptor())
		if err := validateBackendDescriptor(descriptor); err != nil {
			return DiscreteSolverReceipt{}, err
		}
		if available[descriptor.Name] != nil {
			return DiscreteSolverReceipt{}, fmt.Errorf("duplicate discrete solver backend %q", descriptor.Name)
		}
		available[descriptor.Name] = backend
	}
	started := now().UTC()
	candidates := append([]string{request.Selection.PreferredBackend}, request.Selection.AuthorizedFallbacks...)
	attempted := make([]string, 0, len(candidates))
	for _, name := range candidates {
		attempted = append(attempted, name)
		backend := available[name]
		if backend == nil {
			continue
		}
		descriptor := canonicalBackendDescriptor(backend.Descriptor())
		if !containsString(descriptor.Capabilities, request.Capability) {
			continue
		}
		configuredDeadline := time.Now().Add(time.Duration(request.Limits.TimeLimitMilliseconds) * time.Millisecond)
		deadlineCtx := context.WithValue(ctx, discreteConfiguredDeadlineContextKey{}, configuredDeadline)
		runCtx, cancel := context.WithDeadlineCause(deadlineCtx, configuredDeadline, errConfiguredDiscreteSolverTimeLimit)
		outcome, runErr := backend.SolveDiscrete(runCtx, request)
		contextErr := runCtx.Err()
		contextCause := context.Cause(runCtx)
		cancel()
		finished := now().UTC()
		if errors.Is(runErr, ErrSolverBackendUnavailable) {
			continue
		}
		if runErr != nil {
			outcome = DiscreteBackendOutcome{Termination: SolverError, Explanation: runErr.Error()}
		}
		terminalCpSatTrace := outcome.BackendReportedEvidence != nil && outcome.BackendReportedEvidence.Trace != nil && outcome.BackendReportedEvidence.Trace.StopReason == "SOLVER_TERMINAL"
		if errors.Is(contextCause, errConfiguredDiscreteSolverTimeLimit) && outcome.Termination != SolverTimeLimit && !terminalCpSatTrace {
			outcome = DiscreteBackendOutcome{Termination: SolverTimeLimit, Choices: append([]DiscreteChoice(nil), outcome.Choices...), BackendReportedEvidence: cloneDiscreteReportedEvidence(outcome.BackendReportedEvidence), Explanation: contextErr.Error()}
			if outcome.BackendReportedEvidence != nil {
				outcome.BackendReportedEvidence.Status = "UNKNOWN"
				if outcome.BackendReportedEvidence.Trace != nil {
					if outcome.BackendReportedEvidence.Trace.Complete && len(outcome.BackendReportedEvidence.Trace.Phases) > 0 {
						removed := outcome.BackendReportedEvidence.Trace.Phases[len(outcome.BackendReportedEvidence.Trace.Phases)-1]
						outcome.BackendReportedEvidence.Trace.Phases = outcome.BackendReportedEvidence.Trace.Phases[:len(outcome.BackendReportedEvidence.Trace.Phases)-1]
						if removed.Kind == "OBJECTIVE" && len(outcome.BackendReportedEvidence.Objectives) > 0 {
							outcome.BackendReportedEvidence.Objectives = outcome.BackendReportedEvidence.Objectives[:len(outcome.BackendReportedEvidence.Objectives)-1]
						}
					}
					outcome.BackendReportedEvidence.Trace.Complete = false
					outcome.BackendReportedEvidence.Trace.StopReason = "HOST_DEADLINE"
					outcome.BackendReportedEvidence.Trace.CanonicalResultDigest = ""
					outcome.BackendReportedEvidence.Trace.PlanDigest = cpSatPlanDigest(request.ID, *outcome.BackendReportedEvidence.Trace)
				}
			}
		} else if contextErr != nil && !errors.Is(contextCause, errConfiguredDiscreteSolverTimeLimit) {
			outcome = DiscreteBackendOutcome{Termination: SolverUnknown, Explanation: "discrete solver request was stopped by its caller context"}
		}
		receipt := DiscreteSolverReceipt{
			APIVersion: DiscreteSolverAPIVersion, Kind: "DiscreteSolverReceipt", RequestID: request.ID,
			RequestedBackend: request.Selection.PreferredBackend, AttemptedBackends: attempted, SelectedBackend: &descriptor,
			Termination: outcome.Termination, BackendReportedEvidence: cloneDiscreteReportedEvidence(outcome.BackendReportedEvidence),
			Explanation: boundedSolverExplanation(outcome.Explanation), StartedAt: started.Format(time.RFC3339Nano), FinishedAt: finished.Format(time.RFC3339Nano),
		}
		if name != request.Selection.PreferredBackend {
			receipt.FallbackFrom = request.Selection.PreferredBackend
		}
		if len(outcome.Choices) > 0 {
			result, err := NewDiscreteScheduleResult(request.Recommendation.Problem, outcome.Choices)
			if err != nil {
				receipt.Termination = SolverError
				receipt.BackendReportedEvidence = nil
				receipt.Explanation = boundedSolverExplanation("backend returned choices that failed independent verification")
			} else {
				receipt.Result = &result
			}
		}
		return finalizeDiscreteSolverReceipt(request, receipt)
	}
	finished := now().UTC()
	receipt := DiscreteSolverReceipt{
		APIVersion: DiscreteSolverAPIVersion, Kind: "DiscreteSolverReceipt", RequestID: request.ID,
		RequestedBackend: request.Selection.PreferredBackend, AttemptedBackends: attempted,
		Termination: SolverUnknown, Explanation: "no authorized backend was available for the requested discrete capability",
		StartedAt: started.Format(time.RFC3339Nano), FinishedAt: finished.Format(time.RFC3339Nano),
	}
	return finalizeDiscreteSolverReceipt(request, receipt)
}

func VerifyDiscreteSolverReceipt(request DiscreteSolverRequest, receipt DiscreteSolverReceipt) error {
	if err := VerifyDiscreteSolverRequest(request); err != nil {
		return err
	}
	if receipt.APIVersion != DiscreteSolverAPIVersion || receipt.Kind != "DiscreteSolverReceipt" || receipt.RequestID != request.ID {
		return errors.New("unsupported or mismatched discrete solver receipt")
	}
	started, err := time.Parse(time.RFC3339Nano, receipt.StartedAt)
	if err != nil {
		return errors.New("discrete solver receipt has invalid startedAt")
	}
	finished, err := time.Parse(time.RFC3339Nano, receipt.FinishedAt)
	if err != nil || finished.Before(started) {
		return errors.New("discrete solver receipt requires ordered timestamps")
	}
	captured, _ := time.Parse(time.RFC3339, request.Recommendation.Snapshot.CapturedAt)
	if started.Before(captured) {
		return errors.New("discrete solver receipt predates its snapshot")
	}
	allowed := append([]string{request.Selection.PreferredBackend}, request.Selection.AuthorizedFallbacks...)
	if receipt.RequestedBackend != request.Selection.PreferredBackend || len(receipt.AttemptedBackends) == 0 || len(receipt.AttemptedBackends) > len(allowed) {
		return errors.New("discrete solver receipt loses backend selection order")
	}
	for index, name := range receipt.AttemptedBackends {
		if name != allowed[index] {
			return errors.New("discrete solver receipt attempted an unauthorized backend")
		}
	}
	if receipt.SelectedBackend != nil {
		descriptor := canonicalBackendDescriptor(*receipt.SelectedBackend)
		if err := validateBackendDescriptor(descriptor); err != nil || !reflect.DeepEqual(descriptor, *receipt.SelectedBackend) || !containsString(descriptor.Capabilities, request.Capability) || receipt.AttemptedBackends[len(receipt.AttemptedBackends)-1] != descriptor.Name {
			return errors.New("discrete solver receipt has an invalid selected backend")
		}
		if descriptor.Name == request.Selection.PreferredBackend {
			if receipt.FallbackFrom != "" {
				return errors.New("preferred discrete backend cannot report fallback")
			}
		} else if receipt.FallbackFrom != request.Selection.PreferredBackend || !containsString(request.Selection.AuthorizedFallbacks, descriptor.Name) {
			return errors.New("discrete solver fallback was not explicitly authorized")
		}
	} else {
		if receipt.FallbackFrom != "" || receipt.Termination != SolverUnknown || !reflect.DeepEqual(receipt.AttemptedBackends, allowed) {
			return errors.New("unselected discrete receipt must preserve complete unavailable policy")
		}
	}
	if receipt.Explanation != boundedSolverExplanation(receipt.Explanation) {
		return errors.New("discrete solver explanation is not bounded and redacted")
	}
	if err := verifyDiscreteTerminationEvidence(request, receipt); err != nil {
		return err
	}
	wantID, err := discreteSolverReceiptID(receipt)
	if err != nil {
		return err
	}
	if receipt.ID != wantID {
		return errors.New("discrete solver receipt identity does not match content")
	}
	return nil
}

func VerifyNativeDiscreteSolverReceipt(ctx context.Context, request DiscreteSolverRequest, receipt DiscreteSolverReceipt) error {
	if err := VerifyDiscreteSolverReceipt(request, receipt); err != nil {
		return err
	}
	if receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != NativeBackendName {
		return errors.New("discrete receipt was not produced by native backend")
	}
	started, _ := time.Parse(time.RFC3339Nano, receipt.StartedAt)
	finished, _ := time.Parse(time.RFC3339Nano, receipt.FinishedAt)
	replayed, err := RunDiscreteSolverRequest(ctx, request, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: receipt.SelectedBackend.ExecutableDigest, Architecture: receipt.SelectedBackend.Architecture}}, sequenceDiscreteClock(started, finished))
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replayed, receipt) {
		return errors.New("native discrete solver receipt does not match deterministic replay")
	}
	return nil
}

func verifyDiscreteTerminationEvidence(request DiscreteSolverRequest, receipt DiscreteSolverReceipt) error {
	valid := map[SolverTermination]bool{SolverOptimal: true, SolverFeasible: true, SolverInfeasible: true, SolverUnbounded: true, SolverTimeLimit: true, SolverError: true, SolverUnknown: true}
	if !valid[receipt.Termination] {
		return errors.New("unknown discrete solver termination")
	}
	hasResult := receipt.Result != nil
	switch receipt.Termination {
	case SolverOptimal, SolverFeasible:
		if receipt.SelectedBackend == nil || !hasResult || receipt.Explanation != "" {
			return errors.New("successful discrete termination requires a selected backend, result, and no explanation")
		}
	case SolverInfeasible:
		if receipt.SelectedBackend == nil || hasResult || receipt.Explanation == "" {
			return errors.New("discrete infeasible requires selected backend, explanation, and no result")
		}
	case SolverUnbounded:
		return errors.New("bounded discrete schedule cannot be unbounded")
	case SolverError:
		if receipt.SelectedBackend == nil || hasResult || receipt.Explanation == "" {
			return errors.New("discrete solver error requires selected backend, explanation, and no result")
		}
	case SolverUnknown:
		if hasResult || receipt.Explanation == "" {
			return errors.New("discrete unknown requires explanation and no result")
		}
	case SolverTimeLimit:
		if receipt.SelectedBackend == nil || receipt.Explanation == "" {
			return errors.New("discrete time limit requires selected backend and explanation")
		}
	}
	if hasResult {
		if err := VerifyDiscreteScheduleResult(request.Recommendation.Problem, *receipt.Result); err != nil {
			return err
		}
	}
	cpSatSelected := receipt.SelectedBackend != nil && receipt.SelectedBackend.Name == "cp-sat"
	if err := verifyDiscreteReportedEvidence(request, receipt.BackendReportedEvidence, receipt.SelectedBackend != nil, cpSatSelected, receipt.Termination, receipt.Result); err != nil {
		return err
	}
	if receipt.Termination == SolverOptimal {
		if receipt.BackendReportedEvidence == nil || receipt.Result == nil || receipt.BackendReportedEvidence.ProofKind != "CP_SAT_EXACT_LEXICOGRAPHIC_V1" || receipt.BackendReportedEvidence.Trace == nil || !receipt.BackendReportedEvidence.Trace.Complete || len(receipt.BackendReportedEvidence.Objectives) != len(requestedSolverObjectives(request.Recommendation.Objective)) {
			return errors.New("optimal discrete solver termination requires every objective proof")
		}
		for _, proof := range receipt.BackendReportedEvidence.Objectives {
			if proof.Gap != "0" || proof.BestBound != proof.Incumbent {
				return errors.New("optimal discrete proof requires zero gap and equal bound")
			}
		}
	}
	return nil
}

func verifyDiscreteReportedEvidence(request DiscreteSolverRequest, evidence *DiscreteBackendReportedEvidence, selected, cpSatSelected bool, termination SolverTermination, result *DiscreteScheduleResult) error {
	if evidence == nil {
		return nil
	}
	if !selected || !validDiscreteSolverStatus(evidence.Status) {
		return errors.New("discrete backend evidence requires a selected backend and bounded status")
	}
	if cpSatSelected && evidence.Trace == nil {
		return errors.New("CP-SAT backend evidence requires an ordered compiler-owned proof trace")
	}
	if evidence.ResponseDigest != "" && !validDigest(evidence.ResponseDigest) {
		return errors.New("discrete backend response digest must be exact SHA-256")
	}
	if (evidence.ModelDigest != "" && !validDigest(evidence.ModelDigest)) || (evidence.ParametersDigest != "" && !validDigest(evidence.ParametersDigest)) {
		return errors.New("discrete backend model and parameter digests must be exact SHA-256")
	}
	if !discreteStatusMatchesTermination(evidence.Status, termination) && !(evidence.Trace != nil && termination == SolverError && evidence.Status == "INFEASIBLE") {
		return errors.New("discrete backend status contradicts termination")
	}
	if evidence.Trace != nil {
		if evidence.ProofKind != "CP_SAT_EXACT_LEXICOGRAPHIC_V1" || evidence.ModelDigest != "" || evidence.ParametersDigest != "" || evidence.ResponseDigest != "" || evidence.CompositeObjective != nil {
			return errors.New("CP-SAT trace cannot mix ordered and legacy proof fields")
		}
		if err := verifyDiscreteCpSatTrace(request, evidence, termination, result); err != nil {
			return err
		}
	}
	if len(evidence.Objectives) > 0 && result == nil && evidence.Trace == nil {
		return errors.New("discrete objective evidence requires a verified result")
	}
	if err := verifyDiscreteCompositeProof(request, evidence.CompositeObjective, result); err != nil {
		return err
	}
	requested := requestedSolverObjectives(request.Recommendation.Objective)
	if len(evidence.Objectives) > len(requested) {
		return errors.New("discrete proofs exceed requested objectives")
	}
	for index, proof := range evidence.Objectives {
		if proof.Objective != requested[index] {
			return errors.New("discrete proofs must be an ordered requested-objective prefix")
		}
		incumbent, err := parseSolverReportedInt64("discrete incumbent", proof.Incumbent)
		if err != nil {
			return err
		}
		bound, err := parseSolverReportedInt64("discrete bound", proof.BestBound)
		if err != nil {
			return err
		}
		gap, err := parseSolverReportedInt64("discrete gap", proof.Gap)
		if err != nil || bound < 0 || gap < 0 || gap != incumbent-bound {
			return errors.New("discrete proof has an invalid bound or gap")
		}
		switch proof.Objective {
		case "minimize_makespan":
			if proof.Unit != "seconds" || (result != nil && incumbent != result.MakespanSeconds) {
				return errors.New("discrete makespan proof does not match result")
			}
		case "minimize_estimated_cost":
			if proof.Unit != "micros" || (result != nil && (result.EstimatedTotalCostMicros == nil || incumbent != *result.EstimatedTotalCostMicros)) {
				return errors.New("discrete cost proof does not match result")
			}
		default:
			return errors.New("discrete proof names unsupported objective")
		}
	}
	return nil
}

func verifyDiscreteCpSatTrace(request DiscreteSolverRequest, evidence *DiscreteBackendReportedEvidence, termination SolverTermination, result *DiscreteScheduleResult) error {
	trace := evidence.Trace
	if trace == nil || trace.Revision != cpSatCanonicalRevision || len(trace.Phases) == 0 || len(trace.Phases) > maximumCpSatPhases || (trace.StopReason != "COMPLETE" && trace.StopReason != "SOLVER_TERMINAL" && trace.StopReason != "HOST_DEADLINE") {
		return errors.New("CP-SAT proof trace has an invalid revision or phase count")
	}
	planner, err := newCpSatPhasePlanner(request)
	if err != nil {
		return errors.New("CP-SAT proof trace cannot rebuild its compiler-owned phase plan")
	}
	exactObjectives := make([]SolverObjectiveProof, 0, 2)
	for index, proof := range trace.Phases {
		compiled, err := planner.compileNextPhase()
		if err != nil {
			return errors.New("CP-SAT proof trace exceeds its compiler-owned phase plan")
		}
		descriptor := compiled.Phase
		if proof.Index != descriptor.Index || proof.Kind != descriptor.Kind || proof.Key != descriptor.Key || proof.Unit != descriptor.Unit || proof.ModelDigest != digestBytes(compiled.Model) || proof.ParametersDigest != digestBytes(compiled.Parameters) {
			return errors.New("CP-SAT proof phase does not bind its compiler-owned descriptor, model, and parameters")
		}
		if !validDigest(proof.ResponseDigest) || proof.ResponseDigest != cpSatPhaseSemanticDigest(request.ID, proof) || !validDiscreteSolverStatus(proof.Status) {
			return errors.New("CP-SAT proof phase has an invalid semantic response binding")
		}
		if proof.Status != "OPTIMAL" && index != len(trace.Phases)-1 {
			return errors.New("CP-SAT proof trace continues after a terminal non-optimal phase")
		}
		if proof.Status == "OPTIMAL" {
			value, err := parseSolverReportedInt64("exact CP-SAT phase incumbent", proof.Incumbent)
			if err != nil || value < 0 || proof.BestBound != proof.Incumbent || proof.Gap != "0" {
				return errors.New("exact CP-SAT proof phase lacks an equal integer incumbent and bound")
			}
			if result != nil {
				want, resultErr := cpSatPhaseValueFromResult(request, descriptor, *result)
				if resultErr != nil || want != value {
					return errors.New("exact CP-SAT proof phase contradicts its verified result")
				}
			} else if index == len(trace.Phases)-1 {
				return errors.New("unprojected exact CP-SAT phase is not bound by a following compiler model")
			}
			encoded := strconv.FormatInt(value, 10)
			if descriptor.Kind == "OBJECTIVE" {
				exactObjectives = append(exactObjectives, SolverObjectiveProof{Objective: descriptor.Key, Unit: descriptor.Unit, Incumbent: encoded, BestBound: encoded, Gap: "0"})
			}
			if err := planner.acceptExactPhase(compiled, value); err != nil {
				return err
			}
			continue
		}
		if proof.Status == "FEASIBLE" {
			if result == nil || proof.Incumbent == "" {
				return errors.New("feasible CP-SAT proof phase requires a verified incumbent")
			}
			value, err := cpSatPhaseValueFromResult(request, descriptor, *result)
			if err != nil || proof.Incumbent != strconv.FormatInt(value, 10) {
				return errors.New("feasible CP-SAT proof phase contradicts its result")
			}
			if proof.BestBound != "" {
				bound, parseErr := parseSolverReportedInt64("CP-SAT phase bound", proof.BestBound)
				gap, gapErr := parseSolverReportedInt64("CP-SAT phase gap", proof.Gap)
				if parseErr != nil || gapErr != nil || bound < 0 || bound > value || gap != value-bound {
					return errors.New("feasible CP-SAT proof phase has an invalid integer bound")
				}
			} else if proof.Gap != "" {
				return errors.New("feasible CP-SAT proof phase cannot report a gap without a bound")
			}
		} else if proof.Incumbent != "" || proof.BestBound != "" || proof.Gap != "" {
			return errors.New("CP-SAT terminal phase without a solution cannot report objective values")
		}
	}
	if trace.Complete != planner.complete() {
		return errors.New("CP-SAT proof completeness contradicts its compiler-owned phase plan")
	}
	if trace.Complete {
		if termination != SolverOptimal || result == nil || trace.CanonicalResultDigest != cpSatCanonicalResultDigest(request, *result) {
			return errors.New("complete CP-SAT proof trace does not bind its canonical result")
		}
	} else if trace.CanonicalResultDigest != "" || termination == SolverOptimal {
		return errors.New("incomplete CP-SAT proof trace cannot claim a canonical result")
	}
	terminal := trace.Phases[len(trace.Phases)-1]
	if trace.Complete {
		if trace.StopReason != "COMPLETE" || terminal.Status != "OPTIMAL" || evidence.Status != "OPTIMAL" {
			return errors.New("complete CP-SAT proof trace requires an optimal terminal phase")
		}
	} else {
		switch terminal.Status {
		case "OPTIMAL":
			if trace.StopReason != "HOST_DEADLINE" || termination != SolverTimeLimit || evidence.Status != "UNKNOWN" || result == nil {
				return errors.New("exact CP-SAT prefix without a terminal response requires a timed-out incumbent")
			}
		case "FEASIBLE":
			if trace.StopReason != "SOLVER_TERMINAL" || (termination != SolverFeasible && termination != SolverTimeLimit) || evidence.Status != "FEASIBLE" {
				return errors.New("feasible CP-SAT terminal phase contradicts receipt termination")
			}
		case "INFEASIBLE":
			want := SolverError
			if terminal.Index == 0 {
				want = SolverInfeasible
			}
			if trace.StopReason != "SOLVER_TERMINAL" || termination != want || evidence.Status != "INFEASIBLE" {
				return errors.New("infeasible CP-SAT terminal phase contradicts its phase position")
			}
		case "MODEL_INVALID":
			if trace.StopReason != "SOLVER_TERMINAL" || termination != SolverError || evidence.Status != "MODEL_INVALID" {
				return errors.New("invalid CP-SAT terminal phase contradicts receipt termination")
			}
		case "UNKNOWN":
			if trace.StopReason != "SOLVER_TERMINAL" || (termination != SolverUnknown && termination != SolverTimeLimit) || evidence.Status != "UNKNOWN" {
				return errors.New("unknown CP-SAT terminal phase contradicts receipt termination")
			}
		}
	}
	if trace.PlanDigest != cpSatPlanDigest(request.ID, *trace) {
		return errors.New("CP-SAT proof trace plan digest does not match its ordered phases")
	}
	if len(evidence.Objectives) != len(exactObjectives) {
		return errors.New("CP-SAT objective proofs do not equal the exact ordered objective prefix")
	}
	for index := range exactObjectives {
		if evidence.Objectives[index] != exactObjectives[index] {
			return errors.New("CP-SAT objective proofs do not equal the exact ordered objective prefix")
		}
	}
	return nil
}

func verifyDiscreteCompositeProof(request DiscreteSolverRequest, proof *DiscreteCompositeObjectiveProof, result *DiscreteScheduleResult) error {
	if proof == nil {
		return nil
	}
	if result == nil || proof.Unit != "lexicographic_integer" {
		return errors.New("discrete composite proof requires a verified result and exact unit")
	}
	incumbent, err := parseSolverReportedInt64("discrete composite incumbent", proof.Incumbent)
	if err != nil {
		return err
	}
	bound, err := parseSolverReportedInt64("discrete composite bound", proof.BestBound)
	if err != nil {
		return err
	}
	gap, err := parseSolverReportedInt64("discrete composite gap", proof.Gap)
	if err != nil {
		return err
	}
	scale, err := parseSolverReportedInt64("discrete composite makespan scale", proof.MakespanScale)
	if err != nil {
		return err
	}
	maximumCost, err := parseSolverReportedInt64("discrete maximum secondary cost", proof.MaximumSecondaryCost)
	if err != nil {
		return err
	}
	wantMaximum, err := maximumDiscreteSecondaryCost(request.Recommendation.Problem)
	if err != nil {
		return err
	}
	wantScale := int64(1)
	wantCost := int64(0)
	if hasObjective(request.Recommendation.Objective.Secondary, "minimize_estimated_cost") {
		wantScale = wantMaximum + 1
		if result.EstimatedTotalCostMicros == nil {
			return errors.New("discrete composite proof requires complete cost evidence")
		}
		wantCost = *result.EstimatedTotalCostMicros
	}
	if result.MakespanSeconds > (math.MaxInt64-wantCost)/wantScale {
		return errors.New("discrete composite proof overflows its exact objective")
	}
	wantIncumbent := result.MakespanSeconds*wantScale + wantCost
	if scale != wantScale || maximumCost != wantMaximum || incumbent != wantIncumbent || bound < 0 || bound > incumbent || gap != incumbent-bound {
		return errors.New("discrete composite proof contradicts the compiled objective")
	}
	return nil
}

func validDiscreteSolverStatus(status string) bool {
	switch status {
	case "UNKNOWN", "MODEL_INVALID", "FEASIBLE", "INFEASIBLE", "OPTIMAL":
		return true
	default:
		return false
	}
}

func discreteStatusMatchesTermination(status string, termination SolverTermination) bool {
	switch status {
	case "OPTIMAL":
		return termination == SolverOptimal
	case "FEASIBLE":
		return termination == SolverFeasible || termination == SolverTimeLimit
	case "INFEASIBLE":
		return termination == SolverInfeasible
	case "MODEL_INVALID":
		return termination == SolverError
	case "UNKNOWN":
		return termination == SolverUnknown || termination == SolverTimeLimit
	default:
		return false
	}
}

func solveDiscreteGreedy(ctx context.Context, problem DiscreteScheduleProblem, optimizeCost bool) ([]DiscreteChoice, error) {
	order, err := discreteTopologicalOrder(problem)
	if err != nil {
		return nil, err
	}
	taskByID := make(map[string]DiscreteTask, len(problem.Tasks))
	laneByID := make(map[string]DiscreteLane, len(problem.Lanes))
	for _, task := range problem.Tasks {
		taskByID[task.ID] = task
	}
	for _, lane := range problem.Lanes {
		laneByID[lane.ID] = lane
	}
	assigned := make(map[string]DiscreteChoice, len(problem.Tasks))
	choices := make([]DiscreteChoice, 0, len(problem.Tasks))
	for _, taskID := range order {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		task := taskByID[taskID]
		starts := discreteCandidateStarts(problem, task, assigned, taskByID)
		var selected *DiscreteChoice
		for _, start := range starts {
			valid := make([]DiscreteChoice, 0, len(task.EligibleLanes))
			for _, laneID := range task.EligibleLanes {
				choice := DiscreteChoice{Task: task.ID, Lane: laneID, Start: start}
				if discretePartialChoiceValid(problem, choice, assigned, taskByID, laneByID) {
					valid = append(valid, choice)
				}
			}
			if len(valid) == 0 {
				continue
			}
			sort.Slice(valid, func(i, j int) bool {
				if optimizeCost {
					left, leftOK := task.EstimatedCostMicros[valid[i].Lane]
					right, rightOK := task.EstimatedCostMicros[valid[j].Lane]
					if leftOK && rightOK && left != right {
						return left < right
					}
				}
				return valid[i].Lane < valid[j].Lane
			})
			candidate := valid[0]
			selected = &candidate
			break
		}
		if selected == nil {
			return nil, fmt.Errorf("native discrete heuristic found no schedule for task %s", task.ID)
		}
		assigned[task.ID] = *selected
		choices = append(choices, *selected)
	}
	if _, err := NewDiscreteScheduleResult(problem, choices); err != nil {
		return nil, fmt.Errorf("native discrete heuristic failed independent verification: %w", err)
	}
	return choices, nil
}

func discreteTopologicalOrder(problem DiscreteScheduleProblem) ([]string, error) {
	indegree := make(map[string]int, len(problem.Tasks))
	edges := make(map[string][]string, len(problem.Tasks))
	for _, task := range problem.Tasks {
		indegree[task.ID] = 0
	}
	for _, edge := range problem.Precedences {
		edges[edge.Before] = append(edges[edge.Before], edge.After)
		indegree[edge.After]++
	}
	ready := make([]string, 0, len(problem.Tasks))
	for task, degree := range indegree {
		if degree == 0 {
			ready = append(ready, task)
		}
	}
	sort.Strings(ready)
	order := make([]string, 0, len(problem.Tasks))
	for len(ready) > 0 {
		task := ready[0]
		ready = ready[1:]
		order = append(order, task)
		for _, after := range edges[task] {
			indegree[after]--
			if indegree[after] == 0 {
				ready = append(ready, after)
				sort.Strings(ready)
			}
		}
	}
	if len(order) != len(problem.Tasks) {
		return nil, errors.New("discrete precedence graph is cyclic")
	}
	return order, nil
}

func discreteCandidateStarts(problem DiscreteScheduleProblem, task DiscreteTask, assigned map[string]DiscreteChoice, tasks map[string]DiscreteTask) []int64 {
	candidates := map[int64]bool{task.EarliestStart: true}
	for _, edge := range problem.Precedences {
		if edge.After == task.ID {
			if before, exists := assigned[edge.Before]; exists {
				candidates[before.Start+tasks[edge.Before].DurationSeconds+edge.LagSeconds] = true
			}
		}
	}
	for taskID, choice := range assigned {
		candidates[choice.Start+tasks[taskID].DurationSeconds] = true
	}
	for _, window := range problem.MaintenanceWindows {
		if containsString(task.EligibleLanes, window.Lane) {
			candidates[window.End] = true
		}
	}
	starts := make([]int64, 0, len(candidates))
	for candidate := range candidates {
		if candidate >= task.EarliestStart && candidate <= task.LatestFinish-task.DurationSeconds {
			starts = append(starts, candidate)
		}
	}
	sort.Slice(starts, func(i, j int) bool { return starts[i] < starts[j] })
	return starts
}

func discretePartialChoiceValid(problem DiscreteScheduleProblem, candidate DiscreteChoice, assigned map[string]DiscreteChoice, tasks map[string]DiscreteTask, lanes map[string]DiscreteLane) bool {
	task := tasks[candidate.Task]
	finish := candidate.Start + task.DurationSeconds
	for _, window := range problem.MaintenanceWindows {
		if window.Lane == candidate.Lane && intervalsOverlap(candidate.Start, finish, window.Start, window.End) {
			return false
		}
	}
	for _, edge := range problem.Precedences {
		if edge.After == candidate.Task {
			before, exists := assigned[edge.Before]
			if !exists || candidate.Start < before.Start+tasks[edge.Before].DurationSeconds+edge.LagSeconds {
				return false
			}
		}
	}
	for _, rule := range problem.TopologyRules {
		if !containsString(rule.Tasks, candidate.Task) {
			continue
		}
		value := lanes[candidate.Lane].Topology[rule.Key]
		for _, taskID := range rule.Tasks {
			other, exists := assigned[taskID]
			if !exists {
				continue
			}
			otherValue := lanes[other.Lane].Topology[rule.Key]
			if (rule.Relation == "SAME" && value != otherValue) || (rule.Relation == "DIFFERENT" && value == otherValue) {
				return false
			}
		}
	}
	for dimension, capacity := range lanes[candidate.Lane].Capacity {
		events := map[int64]int64{candidate.Start: task.Demand[dimension], finish: -task.Demand[dimension]}
		for taskID, other := range assigned {
			if other.Lane == candidate.Lane {
				events[other.Start] += tasks[taskID].Demand[dimension]
				events[other.Start+tasks[taskID].DurationSeconds] -= tasks[taskID].Demand[dimension]
			}
		}
		if maximumSweepLoad(events) > capacity {
			return false
		}
	}
	for _, rule := range problem.ConcurrencyRules {
		if !containsString(rule.Tasks, candidate.Task) {
			continue
		}
		events := map[int64]int64{candidate.Start: 1, finish: -1}
		for _, taskID := range rule.Tasks {
			if other, exists := assigned[taskID]; exists {
				events[other.Start]++
				events[other.Start+tasks[taskID].DurationSeconds]--
			}
		}
		if maximumSweepLoad(events) > int64(rule.Maximum) {
			return false
		}
	}
	return true
}

func finalizeDiscreteSolverReceipt(request DiscreteSolverRequest, receipt DiscreteSolverReceipt) (DiscreteSolverReceipt, error) {
	receipt.AttemptedBackends = append([]string(nil), receipt.AttemptedBackends...)
	receipt.BackendReportedEvidence = cloneDiscreteReportedEvidence(receipt.BackendReportedEvidence)
	if receipt.SelectedBackend != nil {
		descriptor := canonicalBackendDescriptor(*receipt.SelectedBackend)
		receipt.SelectedBackend = &descriptor
	}
	var err error
	receipt.ID, err = discreteSolverReceiptID(receipt)
	if err != nil {
		return DiscreteSolverReceipt{}, err
	}
	if err := VerifyDiscreteSolverReceipt(request, receipt); err != nil {
		return DiscreteSolverReceipt{}, err
	}
	return receipt, nil
}

func cloneDiscreteReportedEvidence(evidence *DiscreteBackendReportedEvidence) *DiscreteBackendReportedEvidence {
	if evidence == nil {
		return nil
	}
	result := *evidence
	result.Objectives = append([]SolverObjectiveProof(nil), evidence.Objectives...)
	if evidence.Trace != nil {
		trace := *evidence.Trace
		trace.Phases = append([]DiscreteCpSatPhaseProof(nil), evidence.Trace.Phases...)
		result.Trace = &trace
	}
	if evidence.CompositeObjective != nil {
		proof := *evidence.CompositeObjective
		result.CompositeObjective = &proof
	}
	return &result
}

func discreteSolverReceiptID(receipt DiscreteSolverReceipt) (string, error) {
	receipt.ID = ""
	digest, err := digestJSON(receipt)
	if err != nil {
		return "", err
	}
	return "dsolrcpt_" + digest[len("sha256:"):], nil
}

func sequenceDiscreteClock(values ...time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		value := values[min(index, len(values)-1)]
		index++
		return value
	}
}
