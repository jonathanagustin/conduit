package optimization

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jonathanagustin/conduit/internal/optimization/solverprotocol"
)

const (
	SolverAPIVersion            = solverprotocol.SolverAPIVersion
	NativeBackendName           = "native"
	NativeBackendVersion        = "1.0.0"
	AllocationBackendCapability = solverprotocol.AllocationBackendCapability
	AllocationModelRevision     = solverprotocol.AllocationModelRevision
	maximumSolverExplanation    = 2_048
	maximumSolverReportedStatus = 128
	maximumSolverReportedNumber = 128
	maximumSolverMemoryBytes    = int64(1 << 50)
)

type SolverTermination = solverprotocol.SolverTermination

const (
	SolverOptimal    = solverprotocol.SolverOptimal
	SolverFeasible   = solverprotocol.SolverFeasible
	SolverInfeasible = solverprotocol.SolverInfeasible
	SolverUnbounded  = solverprotocol.SolverUnbounded
	SolverTimeLimit  = solverprotocol.SolverTimeLimit
	SolverError      = solverprotocol.SolverError
	SolverUnknown    = solverprotocol.SolverUnknown
)

type SolverSelection struct {
	PreferredBackend    string   `json:"preferredBackend"`
	AuthorizedFallbacks []string `json:"authorizedFallbacks,omitempty"`
}

type SolverDeterminism struct {
	Seed    uint64 `json:"seed"`
	Workers int    `json:"workers"`
}

type SolverLimits struct {
	TimeLimitMilliseconds int64 `json:"timeLimitMilliseconds"`
	MemoryLimitBytes      int64 `json:"memoryLimitBytes"`
}

type SolverModel struct {
	Name                  string `json:"name"`
	Revision              string `json:"revision"`
	Digest                string `json:"digest"`
	HardConstraintsDigest string `json:"hardConstraintsDigest"`
	ObjectiveDigest       string `json:"objectiveDigest"`
}

type SolverRequest struct {
	APIVersion     string                `json:"apiVersion"`
	Kind           string                `json:"kind"`
	ID             string                `json:"id"`
	Recommendation RecommendationRequest `json:"recommendation"`
	Model          SolverModel           `json:"model"`
	Capability     string                `json:"capability"`
	Selection      SolverSelection       `json:"selection"`
	Determinism    SolverDeterminism     `json:"determinism"`
	Limits         SolverLimits          `json:"limits"`
}

type SolverBackendDescriptor struct {
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	ExecutableDigest string   `json:"executableDigest"`
	Architecture     string   `json:"architecture"`
	Capabilities     []string `json:"capabilities"`
}

type SolverObjectiveEvidence struct {
	Unit       string `json:"unit"`
	Incumbent  string `json:"incumbent"`
	LowerBound string `json:"lowerBound"`
	Gap        string `json:"gap"`
}

// SolverBackendReportedEvidence preserves the solver's exact, untrusted status
// and optional proof values. Verification checks its syntax and consistency;
// the backend executable remains the source of these claims.
type SolverBackendReportedEvidence = solverprotocol.SolverBackendReportedEvidence

// SolverObjectiveProof is an untrusted, objective-scoped optimizer proof. All
// numeric values are strings so the host can reject lossy or noncanonical
// representations before binding them into receipt identity.
type SolverObjectiveProof = solverprotocol.SolverObjectiveProof

type SolverReceipt struct {
	APIVersion              string                         `json:"apiVersion"`
	Kind                    string                         `json:"kind"`
	ID                      string                         `json:"id"`
	RequestID               string                         `json:"requestId"`
	RequestedBackend        string                         `json:"requestedBackend"`
	AttemptedBackends       []string                       `json:"attemptedBackends"`
	SelectedBackend         *SolverBackendDescriptor       `json:"selectedBackend,omitempty"`
	FallbackFrom            string                         `json:"fallbackFrom,omitempty"`
	Termination             SolverTermination              `json:"termination"`
	BackendReportedEvidence *SolverBackendReportedEvidence `json:"backendReportedEvidence,omitempty"`
	Objective               *SolverObjectiveEvidence       `json:"objective,omitempty"`
	Result                  *Result                        `json:"result,omitempty"`
	Explanation             string                         `json:"explanation,omitempty"`
	StartedAt               string                         `json:"startedAt"`
	FinishedAt              string                         `json:"finishedAt"`
}

type SolverBackendOutcome struct {
	Termination             SolverTermination
	BackendReportedEvidence *SolverBackendReportedEvidence
	Result                  *Result
	Explanation             string
}

type SolverBackend interface {
	Descriptor() SolverBackendDescriptor
	Solve(context.Context, SolverRequest) (SolverBackendOutcome, error)
}

var ErrSolverBackendUnavailable = errors.New("solver backend unavailable")

type NativeSolverBackend struct {
	ExecutableDigest string
	Architecture     string
}

func (backend NativeSolverBackend) Descriptor() SolverBackendDescriptor {
	return SolverBackendDescriptor{
		Name: NativeBackendName, Version: NativeBackendVersion,
		ExecutableDigest: backend.ExecutableDigest, Architecture: backend.Architecture,
		Capabilities: []string{AllocationBackendCapability},
	}
}

func (backend NativeSolverBackend) Solve(ctx context.Context, request SolverRequest) (SolverBackendOutcome, error) {
	if err := ctx.Err(); err != nil {
		return SolverBackendOutcome{Termination: SolverTimeLimit, Explanation: err.Error()}, nil
	}
	if request.Determinism.Workers != 1 || request.Determinism.Seed != 0 {
		return SolverBackendOutcome{}, ErrSolverBackendUnavailable
	}
	result, err := solve(request.Recommendation.Problem, hasObjective(request.Recommendation.Objective.Secondary, "minimize_estimated_cost"))
	if err != nil {
		return SolverBackendOutcome{Termination: SolverError, Explanation: err.Error()}, nil
	}
	termination := SolverFeasible
	if result.OptimalityGap <= tolerance && len(request.Recommendation.Objective.Secondary) == 0 {
		termination = SolverOptimal
	}
	outcome := SolverBackendOutcome{Termination: termination, Result: &result}
	if termination == SolverOptimal {
		outcome.BackendReportedEvidence = optimalBackendReportedEvidence("OPTIMAL", result)
	}
	return outcome, nil
}

func NewSolverRequest(
	recommendation RecommendationRequest,
	selection SolverSelection,
	determinism SolverDeterminism,
	limits SolverLimits,
) (SolverRequest, error) {
	recommendation = canonicalRequest(recommendation)
	if err := validateRecommendationRequest(recommendation); err != nil {
		return SolverRequest{}, err
	}
	if _, _, _, err := validate(recommendation.Problem); err != nil {
		return SolverRequest{}, err
	}
	modelDigest, err := digestJSON(recommendation.Problem)
	if err != nil {
		return SolverRequest{}, err
	}
	hardConstraints := struct {
		Evidence  []ConstraintEvidence `json:"evidence"`
		Items     []WorkItem           `json:"items"`
		Resources []Resource           `json:"resources"`
	}{recommendation.Constraints, recommendation.Problem.Items, recommendation.Problem.Resources}
	constraintsDigest, err := digestJSON(hardConstraints)
	if err != nil {
		return SolverRequest{}, err
	}
	objectiveDigest, err := digestJSON(recommendation.Objective)
	if err != nil {
		return SolverRequest{}, err
	}
	request := SolverRequest{
		APIVersion: SolverAPIVersion, Kind: "SolverRequest", Recommendation: recommendation,
		Model: SolverModel{
			Name: "allocation.makespan", Revision: AllocationModelRevision, Digest: modelDigest,
			HardConstraintsDigest: constraintsDigest, ObjectiveDigest: objectiveDigest,
		},
		Capability: AllocationBackendCapability, Selection: selection,
		Determinism: determinism, Limits: limits,
	}
	if err := validateSolverRequestFields(request); err != nil {
		return SolverRequest{}, err
	}
	request.ID, err = solverRequestID(request)
	if err != nil {
		return SolverRequest{}, err
	}
	return request, nil
}

func VerifySolverRequest(request SolverRequest) error {
	protocolRequest, err := solverProtocolRequest(request)
	if err != nil {
		return err
	}
	return solverprotocol.VerifySolverRequest(protocolRequest)
}

func RunSolverRequest(
	ctx context.Context,
	request SolverRequest,
	backends []SolverBackend,
	now func() time.Time,
) (SolverReceipt, error) {
	if err := VerifySolverRequest(request); err != nil {
		return SolverReceipt{}, err
	}
	if now == nil {
		return SolverReceipt{}, errors.New("solver clock is required")
	}
	available := make(map[string]SolverBackend, len(backends))
	for _, backend := range backends {
		if backend == nil {
			return SolverReceipt{}, errors.New("nil solver backend")
		}
		descriptor := canonicalBackendDescriptor(backend.Descriptor())
		if err := validateBackendDescriptor(descriptor); err != nil {
			return SolverReceipt{}, err
		}
		if _, exists := available[descriptor.Name]; exists {
			return SolverReceipt{}, fmt.Errorf("duplicate solver backend %q", descriptor.Name)
		}
		available[descriptor.Name] = backend
	}

	started := now().UTC()
	candidates := append([]string{request.Selection.PreferredBackend}, request.Selection.AuthorizedFallbacks...)
	attempted := make([]string, 0, len(candidates))
	for _, name := range candidates {
		attempted = append(attempted, name)
		backend, exists := available[name]
		if !exists {
			continue
		}
		descriptor := canonicalBackendDescriptor(backend.Descriptor())
		if !containsString(descriptor.Capabilities, request.Capability) {
			continue
		}
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(request.Limits.TimeLimitMilliseconds)*time.Millisecond)
		outcome, runErr := backend.Solve(runCtx, request)
		runContextErr := runCtx.Err()
		cancel()
		finished := now().UTC()
		if errors.Is(runErr, ErrSolverBackendUnavailable) {
			continue
		}
		if runErr != nil {
			outcome = SolverBackendOutcome{Termination: SolverError, Explanation: runErr.Error()}
		}
		if runContextErr != nil && outcome.Termination != SolverTimeLimit {
			outcome = SolverBackendOutcome{
				Termination: SolverTimeLimit, BackendReportedEvidence: cloneBackendReportedEvidence(outcome.BackendReportedEvidence),
				Result: outcome.Result, Explanation: runContextErr.Error(),
			}
		}
		receipt := SolverReceipt{
			APIVersion: SolverAPIVersion, Kind: "SolverReceipt", RequestID: request.ID,
			RequestedBackend: request.Selection.PreferredBackend, AttemptedBackends: attempted,
			SelectedBackend: &descriptor, Termination: outcome.Termination,
			BackendReportedEvidence: cloneBackendReportedEvidence(outcome.BackendReportedEvidence), Result: outcome.Result,
			Explanation: boundedSolverExplanation(outcome.Explanation),
			StartedAt:   started.Format(time.RFC3339Nano), FinishedAt: finished.Format(time.RFC3339Nano),
		}
		if name != request.Selection.PreferredBackend {
			receipt.FallbackFrom = request.Selection.PreferredBackend
		}
		if outcome.Result != nil {
			receipt.Objective = solverObjectiveEvidence(*outcome.Result)
		}
		return finalizeSolverReceipt(request, receipt)
	}

	finished := now().UTC()
	receipt := SolverReceipt{
		APIVersion: SolverAPIVersion, Kind: "SolverReceipt", RequestID: request.ID,
		RequestedBackend: request.Selection.PreferredBackend, AttemptedBackends: attempted,
		Termination: SolverUnknown, Explanation: "no authorized backend was available for the requested capability",
		StartedAt: started.Format(time.RFC3339Nano), FinishedAt: finished.Format(time.RFC3339Nano),
	}
	return finalizeSolverReceipt(request, receipt)
}

func VerifySolverReceipt(request SolverRequest, receipt SolverReceipt) error {
	if err := VerifySolverRequest(request); err != nil {
		return err
	}
	if receipt.APIVersion != SolverAPIVersion || receipt.Kind != "SolverReceipt" || receipt.RequestID != request.ID {
		return errors.New("unsupported or mismatched solver receipt")
	}
	started, err := time.Parse(time.RFC3339Nano, receipt.StartedAt)
	if err != nil {
		return fmt.Errorf("solver receipt startedAt: %w", err)
	}
	finished, err := time.Parse(time.RFC3339Nano, receipt.FinishedAt)
	if err != nil || finished.Before(started) {
		return errors.New("solver receipt requires ordered timestamps")
	}
	if receipt.RequestedBackend != request.Selection.PreferredBackend || len(receipt.AttemptedBackends) == 0 || receipt.AttemptedBackends[0] != request.Selection.PreferredBackend {
		return errors.New("solver receipt does not preserve requested backend order")
	}
	allowed := append([]string{request.Selection.PreferredBackend}, request.Selection.AuthorizedFallbacks...)
	if len(receipt.AttemptedBackends) > len(allowed) {
		return errors.New("solver receipt attempted an unauthorized backend")
	}
	for index, name := range receipt.AttemptedBackends {
		if name != allowed[index] {
			return errors.New("solver receipt attempted an unauthorized backend or order")
		}
	}
	if receipt.SelectedBackend != nil {
		descriptor := canonicalBackendDescriptor(*receipt.SelectedBackend)
		if err := validateBackendDescriptor(descriptor); err != nil || !reflect.DeepEqual(descriptor, *receipt.SelectedBackend) {
			return errors.New("solver receipt has an invalid backend descriptor")
		}
		if !containsString(descriptor.Capabilities, request.Capability) || !containsString(receipt.AttemptedBackends, descriptor.Name) {
			return errors.New("selected backend lacks the requested capability or was not attempted")
		}
		if receipt.AttemptedBackends[len(receipt.AttemptedBackends)-1] != descriptor.Name {
			return errors.New("selected backend must be the final attempted backend")
		}
		if descriptor.Name != request.Selection.PreferredBackend {
			if receipt.FallbackFrom != request.Selection.PreferredBackend || !containsString(request.Selection.AuthorizedFallbacks, descriptor.Name) {
				return errors.New("solver fallback was not explicitly authorized")
			}
		} else if receipt.FallbackFrom != "" {
			return errors.New("preferred backend cannot report a fallback")
		}
	} else if receipt.FallbackFrom != "" {
		return errors.New("unselected solver receipt cannot report a fallback")
	}
	if receipt.SelectedBackend == nil && receipt.Termination == SolverUnknown && !reflect.DeepEqual(receipt.AttemptedBackends, allowed) {
		return errors.New("unknown solver receipt must retain every authorized unavailable candidate")
	}
	captured, _ := time.Parse(time.RFC3339, request.Recommendation.Snapshot.CapturedAt)
	if started.Before(captured) {
		return errors.New("solver receipt predates its input snapshot")
	}
	if receipt.Explanation != boundedSolverExplanation(receipt.Explanation) {
		return errors.New("solver explanation is not bounded and redacted")
	}
	if err := verifyTerminationEvidence(request, receipt); err != nil {
		return err
	}
	wantID, err := solverReceiptID(receipt)
	if err != nil {
		return err
	}
	if receipt.ID != wantID {
		return errors.New("solver receipt identity does not match content")
	}
	return nil
}

func VerifyNativeSolverReceipt(ctx context.Context, request SolverRequest, receipt SolverReceipt) error {
	if err := VerifySolverReceipt(request, receipt); err != nil {
		return err
	}
	if receipt.SelectedBackend == nil || receipt.SelectedBackend.Name != NativeBackendName {
		return errors.New("receipt was not produced by the native backend")
	}
	started, _ := time.Parse(time.RFC3339Nano, receipt.StartedAt)
	finished, _ := time.Parse(time.RFC3339Nano, receipt.FinishedAt)
	times := []time.Time{started, finished}
	index := 0
	replayed, err := RunSolverRequest(ctx, request, []SolverBackend{NativeSolverBackend{
		ExecutableDigest: receipt.SelectedBackend.ExecutableDigest,
		Architecture:     receipt.SelectedBackend.Architecture,
	}}, func() time.Time {
		value := times[min(index, len(times)-1)]
		index++
		return value
	})
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replayed, receipt) {
		return errors.New("native solver receipt does not match deterministic replay")
	}
	return nil
}

func validateSolverRequestFields(request SolverRequest) error {
	if request.APIVersion != SolverAPIVersion || request.Kind != "SolverRequest" {
		return errors.New("unsupported solver request type")
	}
	if request.Model.Name != "allocation.makespan" || request.Model.Revision != AllocationModelRevision || request.Capability != AllocationBackendCapability {
		return errors.New("unsupported solver model or capability")
	}
	if !validDigest(request.Model.Digest) || !validDigest(request.Model.HardConstraintsDigest) || !validDigest(request.Model.ObjectiveDigest) {
		return errors.New("solver model requires exact SHA-256 evidence digests")
	}
	if !validSolverToken(request.Selection.PreferredBackend) || request.Determinism.Workers < 1 || request.Determinism.Workers > 256 {
		return errors.New("preferred backend and bounded positive worker count are required")
	}
	if request.Limits.TimeLimitMilliseconds < 1 || request.Limits.TimeLimitMilliseconds > int64((24*time.Hour)/time.Millisecond) || request.Limits.MemoryLimitBytes < 1 || request.Limits.MemoryLimitBytes > maximumSolverMemoryBytes {
		return errors.New("positive bounded solver time and memory limits are required")
	}
	seen := map[string]bool{request.Selection.PreferredBackend: true}
	for _, fallback := range request.Selection.AuthorizedFallbacks {
		if !validSolverToken(fallback) || seen[fallback] {
			return errors.New("solver fallback list contains an empty or duplicate backend")
		}
		seen[fallback] = true
	}
	return nil
}

func validateBackendDescriptor(descriptor SolverBackendDescriptor) error {
	if !validSolverToken(descriptor.Name) || !validSolverToken(descriptor.Version) || !validSolverToken(descriptor.Architecture) || !validDigest(descriptor.ExecutableDigest) || len(descriptor.Capabilities) == 0 {
		return errors.New("solver backend descriptor is incomplete")
	}
	for index, capability := range descriptor.Capabilities {
		if !validSolverToken(capability) || (index > 0 && descriptor.Capabilities[index-1] == capability) {
			return errors.New("solver backend capabilities must be unique and non-empty")
		}
	}
	return nil
}

func verifyTerminationEvidence(request SolverRequest, receipt SolverReceipt) error {
	valid := map[SolverTermination]bool{
		SolverOptimal: true, SolverFeasible: true, SolverInfeasible: true,
		SolverUnbounded: true, SolverTimeLimit: true, SolverError: true, SolverUnknown: true,
	}
	if !valid[receipt.Termination] {
		return errors.New("unknown solver termination state")
	}
	hasResult := receipt.Result != nil
	switch receipt.Termination {
	case SolverOptimal, SolverFeasible:
		if !hasResult || receipt.SelectedBackend == nil {
			return errors.New("successful solver termination requires a selected backend and result")
		}
	case SolverInfeasible, SolverUnbounded:
		if hasResult || receipt.SelectedBackend == nil || receipt.Explanation == "" {
			return errors.New("infeasible or unbounded solver termination requires a selected backend, explanation, and no result")
		}
	case SolverError:
		if hasResult || receipt.SelectedBackend == nil || receipt.Explanation == "" {
			return errors.New("solver error requires a selected backend, explanation, and no result")
		}
	case SolverUnknown:
		if hasResult || receipt.Explanation == "" {
			return errors.New("unknown solver termination requires an explanation and no result")
		}
	case SolverTimeLimit:
		if receipt.SelectedBackend == nil || receipt.Explanation == "" {
			return errors.New("time-limited solver termination requires a selected backend and explanation")
		}
	}
	if hasResult {
		if receipt.Objective == nil {
			return errors.New("solver result requires objective evidence")
		}
		if err := verifySolverResult(request.Recommendation.Problem, *receipt.Result); err != nil {
			return err
		}
		want := solverObjectiveEvidence(*receipt.Result)
		if !reflect.DeepEqual(receipt.Objective, want) {
			return errors.New("solver objective evidence does not match result")
		}
	} else if receipt.Objective != nil {
		return errors.New("solver receipt without result cannot claim objective evidence")
	}
	if err := verifyBackendReportedEvidence(request, receipt.BackendReportedEvidence, receipt.SelectedBackend != nil, receipt.Result); err != nil {
		return err
	}
	if receipt.Termination == SolverOptimal {
		if err := verifyReportedOptimalProof(request, receipt.BackendReportedEvidence, receipt.Result); err != nil {
			return err
		}
	}
	return nil
}

func verifyBackendReportedEvidence(request SolverRequest, evidence *SolverBackendReportedEvidence, selected bool, result *Result) error {
	if evidence == nil {
		return nil
	}
	if !selected {
		return errors.New("unselected solver receipt cannot contain backend-reported evidence")
	}
	if !validSolverReportedStatus(evidence.Status) {
		return errors.New("backend-reported status must be a bounded enum-like token")
	}
	requested := requestedSolverObjectives(request.Recommendation.Objective)
	if len(evidence.Objectives) > len(requested) {
		return errors.New("backend-reported objective proofs exceed the requested objective set")
	}
	if len(evidence.Objectives) > 0 && result == nil {
		return errors.New("backend-reported objective proofs require an incumbent result")
	}
	for index, proof := range evidence.Objectives {
		if proof.Objective != requested[index] {
			return errors.New("backend-reported objective proofs must be an ordered prefix of requested objectives")
		}
		if err := verifySolverObjectiveProof(proof, *result); err != nil {
			return err
		}
	}
	return nil
}

func verifyReportedOptimalProof(request SolverRequest, evidence *SolverBackendReportedEvidence, result *Result) error {
	requested := requestedSolverObjectives(request.Recommendation.Objective)
	if evidence == nil || result == nil || len(evidence.Objectives) != len(requested) {
		return errors.New("optimal solver termination requires zero-gap proof evidence for every requested objective")
	}
	for _, proof := range evidence.Objectives {
		if proof.Gap != "0" || proof.BestBound != proof.Incumbent {
			return errors.New("optimal solver termination requires zero reported proof gap and bound equal to incumbent for every objective")
		}
	}
	return nil
}

func parseSolverReportedFloat(name, encoded string) (float64, error) {
	if len(encoded) == 0 || len(encoded) > maximumSolverReportedNumber {
		return 0, fmt.Errorf("backend-reported %s must be a bounded finite number", name)
	}
	value, err := strconv.ParseFloat(encoded, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("backend-reported %s must be a bounded finite number", name)
	}
	if encoded != exactFloat(value) {
		return 0, fmt.Errorf("backend-reported %s must use canonical binary64 syntax", name)
	}
	return value, nil
}

func parseSolverReportedInt64(name, encoded string) (int64, error) {
	if len(encoded) == 0 || len(encoded) > maximumSolverReportedNumber {
		return 0, fmt.Errorf("backend-reported %s must be a bounded exact integer", name)
	}
	value, err := strconv.ParseInt(encoded, 10, 64)
	if err != nil || encoded != strconv.FormatInt(value, 10) {
		return 0, fmt.Errorf("backend-reported %s must use canonical int64 syntax", name)
	}
	return value, nil
}

func verifySolverObjectiveProof(proof SolverObjectiveProof, result Result) error {
	switch proof.Objective {
	case "minimize_makespan":
		if proof.Unit != "normalized_load_ratio" {
			return errors.New("backend-reported makespan proof has the wrong unit")
		}
		incumbent, err := parseSolverReportedFloat("makespan incumbent", proof.Incumbent)
		if err != nil {
			return err
		}
		bestBound, err := parseSolverReportedFloat("makespan best bound", proof.BestBound)
		if err != nil {
			return err
		}
		gap, err := parseSolverReportedFloat("makespan gap", proof.Gap)
		if err != nil {
			return err
		}
		if incumbent != result.RoundedMakespan || bestBound < 0 || bestBound > incumbent || gap < 0 || gap != incumbent-bestBound {
			return errors.New("backend-reported makespan proof is inconsistent with its incumbent, bound, or gap")
		}
	case "minimize_estimated_cost":
		if proof.Unit != "micros" || result.RoundedEstimatedCostMicros == nil {
			return errors.New("backend-reported cost proof requires an exact micros incumbent")
		}
		incumbent, err := parseSolverReportedInt64("cost incumbent", proof.Incumbent)
		if err != nil {
			return err
		}
		bestBound, err := parseSolverReportedInt64("cost best bound", proof.BestBound)
		if err != nil {
			return err
		}
		gap, err := parseSolverReportedInt64("cost gap", proof.Gap)
		if err != nil {
			return err
		}
		if incumbent != *result.RoundedEstimatedCostMicros || bestBound < 0 || bestBound > incumbent || gap < 0 || gap != incumbent-bestBound {
			return errors.New("backend-reported cost proof is inconsistent with its incumbent, bound, or gap")
		}
	default:
		return fmt.Errorf("backend-reported proof names unsupported objective %q", proof.Objective)
	}
	return nil
}

func requestedSolverObjectives(objective Objective) []string {
	requested := make([]string, 0, 1+len(objective.Secondary))
	requested = append(requested, objective.Primary)
	requested = append(requested, objective.Secondary...)
	return requested
}

func validSolverReportedStatus(value string) bool {
	if value == "" || len(value) > maximumSolverReportedStatus {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func optimalBackendReportedEvidence(status string, result Result) *SolverBackendReportedEvidence {
	incumbent := exactFloat(result.RoundedMakespan)
	return &SolverBackendReportedEvidence{Status: status, Objectives: []SolverObjectiveProof{{
		Objective: "minimize_makespan", Unit: "normalized_load_ratio",
		Incumbent: incumbent, BestBound: incumbent, Gap: "0",
	}}}
}

func cloneBackendReportedEvidence(evidence *SolverBackendReportedEvidence) *SolverBackendReportedEvidence {
	if evidence == nil {
		return nil
	}
	cloned := *evidence
	cloned.Objectives = append([]SolverObjectiveProof(nil), evidence.Objectives...)
	return &cloned
}

func verifySolverResult(problem Problem, result Result) error {
	items, resources, eligible, err := validate(problem)
	if err != nil {
		return err
	}
	reference, err := solve(problem, true)
	if err != nil {
		return err
	}
	if !nearlyEqual(result.RelaxedMakespan, reference.RelaxedMakespan) || !nearlyEqual(result.BaselineMakespan, reference.BaselineMakespan) {
		return errors.New("solver result lower bound or incumbent baseline does not match the independent reference")
	}
	if !reflect.DeepEqual(result.CostModel, problem.CostModel) || !equalOptionalInt64(result.BaselineEstimatedCostMicros, reference.BaselineEstimatedCostMicros) {
		return errors.New("solver result changes cost-model or baseline-cost evidence")
	}
	if len(result.Assignments) != len(items) {
		return errors.New("solver assignment does not cover every work item exactly once")
	}
	if len(items) == 0 {
		if len(result.Fractions) != 0 || len(result.Resources) != 0 || result.RelaxedMakespan != 0 || result.BaselineMakespan != 0 || result.RoundedMakespan != 0 || result.OptimalityGap != 0 || result.Improvement != 0 || result.MeanUtilization != 0 || result.Imbalance != 0 || result.RoundedEstimatedCostMicros != nil || result.EstimatedSavingsMicros != nil {
			return errors.New("empty solver problem contains invented result evidence")
		}
		return nil
	}
	itemByID := make(map[string]WorkItem, len(items))
	resourceByID := make(map[string]Resource, len(resources))
	eligibleByItem := make(map[string]map[string]bool, len(items))
	for _, item := range items {
		itemByID[item.ID] = item
		eligibleByItem[item.ID] = map[string]bool{}
		for _, resourceIndex := range eligible[item.ID] {
			eligibleByItem[item.ID][resources[resourceIndex].ID] = true
		}
	}
	for _, resource := range resources {
		resourceByID[resource.ID] = resource
	}
	seen := make(map[string]bool, len(items))
	loads := make(map[string]float64, len(resources))
	fractionalLoads := make(map[string]float64, len(resources))
	allCostsKnown := problem.CostModel != nil
	totalCost := int64(0)
	for _, assignment := range result.Assignments {
		item, exists := itemByID[assignment.Item]
		if !exists || seen[assignment.Item] || !eligibleByItem[assignment.Item][assignment.Resource] {
			return fmt.Errorf("solver assignment %q is unknown, duplicated, or ineligible", assignment.Item)
		}
		seen[assignment.Item] = true
		if !nearlyEqual(assignment.Demand, item.Demand) {
			return fmt.Errorf("solver assignment %q changes work demand", assignment.Item)
		}
		cost, costKnown := item.EstimatedCostMicros[assignment.Resource]
		if costKnown {
			if assignment.EstimatedCostMicros == nil || *assignment.EstimatedCostMicros != cost {
				return fmt.Errorf("solver assignment %q changes estimated cost", assignment.Item)
			}
			if cost > math.MaxInt64-totalCost {
				return errors.New("solver assignment cost overflows micro-units")
			}
			totalCost += cost
		} else {
			allCostsKnown = false
			if assignment.EstimatedCostMicros != nil {
				return fmt.Errorf("solver assignment %q invents estimated cost", assignment.Item)
			}
		}
		loads[assignment.Resource] += assignment.Demand
	}
	if len(result.Fractions) != len(items) {
		return errors.New("solver relaxation does not cover every work item")
	}
	for _, item := range items {
		fractions, exists := result.Fractions[item.ID]
		if !exists || len(fractions) == 0 {
			return fmt.Errorf("solver relaxation omits work item %q", item.ID)
		}
		shareTotal := 0.0
		seenResources := map[string]bool{}
		for _, fraction := range fractions {
			if seenResources[fraction.Resource] || !eligibleByItem[item.ID][fraction.Resource] || fraction.Share <= 0 || math.IsNaN(fraction.Share) || math.IsInf(fraction.Share, 0) {
				return fmt.Errorf("solver relaxation for %q is duplicated, ineligible, or invalid", item.ID)
			}
			seenResources[fraction.Resource] = true
			shareTotal += fraction.Share
			fractionalLoads[fraction.Resource] += fraction.Share * item.Demand
		}
		if !nearlyEqual(shareTotal, 1) {
			return fmt.Errorf("solver relaxation for %q does not total one", item.ID)
		}
	}
	for _, resource := range resources {
		if fractionalLoads[resource.ID] > result.RelaxedMakespan*resource.Capacity+tolerance {
			return fmt.Errorf("solver relaxation overloads resource %q at its claimed bound", resource.ID)
		}
	}
	rounded := 0.0
	for resourceID, load := range loads {
		rounded = math.Max(rounded, load/resourceByID[resourceID].Capacity)
	}
	if !nearlyEqual(rounded, result.RoundedMakespan) || result.RelaxedMakespan < 0 || result.RelaxedMakespan > result.RoundedMakespan+tolerance {
		return errors.New("solver result has invalid incumbent or lower bound")
	}
	wantGap := 0.0
	if result.RelaxedMakespan > tolerance {
		wantGap = result.RoundedMakespan/result.RelaxedMakespan - 1
	}
	if !nearlyEqual(wantGap, result.OptimalityGap) {
		return errors.New("solver result has invalid LP-relaxation gap")
	}
	wantImprovement := 0.0
	if result.BaselineMakespan > tolerance {
		wantImprovement = 1 - result.RoundedMakespan/result.BaselineMakespan
	}
	if !nearlyEqual(result.Improvement, wantImprovement) {
		return errors.New("solver result has invalid improvement evidence")
	}
	metricsByResource := make(map[string]ResourceMetric, len(result.Resources))
	for _, metric := range result.Resources {
		if _, exists := metricsByResource[metric.Resource]; exists {
			return errors.New("solver result duplicates resource metrics")
		}
		metricsByResource[metric.Resource] = metric
	}
	if len(metricsByResource) != len(resources) {
		return errors.New("solver result does not cover every resource metric")
	}
	minimumUtilization := math.Inf(1)
	meanUtilization := 0.0
	if len(resources) == 0 {
		minimumUtilization = 0
	}
	for _, resource := range resources {
		metric, exists := metricsByResource[resource.ID]
		if !exists {
			return fmt.Errorf("solver result omits resource metric %q", resource.ID)
		}
		normalizedLoad := loads[resource.ID] / resource.Capacity
		utilization := 0.0
		if result.RoundedMakespan > tolerance {
			utilization = normalizedLoad / result.RoundedMakespan
		}
		if !nearlyEqual(metric.Load, loads[resource.ID]) || !nearlyEqual(metric.NormalizedLoad, normalizedLoad) || !nearlyEqual(metric.Utilization, utilization) {
			return fmt.Errorf("solver result changes resource metric %q", resource.ID)
		}
		minimumUtilization = math.Min(minimumUtilization, normalizedLoad)
		meanUtilization += utilization / float64(len(resources))
	}
	if !nearlyEqual(result.MeanUtilization, meanUtilization) || !nearlyEqual(result.Imbalance, result.RoundedMakespan-minimumUtilization) {
		return errors.New("solver result has invalid utilization or imbalance evidence")
	}
	if allCostsKnown {
		if result.RoundedEstimatedCostMicros == nil || *result.RoundedEstimatedCostMicros != totalCost {
			return errors.New("solver result has invalid aggregate estimated cost")
		}
	} else if result.RoundedEstimatedCostMicros != nil {
		return errors.New("solver result turns unknown cost into an aggregate estimate")
	}
	if result.BaselineEstimatedCostMicros != nil && result.RoundedEstimatedCostMicros != nil {
		wantSavings := *result.BaselineEstimatedCostMicros - *result.RoundedEstimatedCostMicros
		if result.EstimatedSavingsMicros == nil || *result.EstimatedSavingsMicros != wantSavings {
			return errors.New("solver result has invalid estimated savings")
		}
	} else if result.EstimatedSavingsMicros != nil {
		return errors.New("solver result invents estimated savings")
	}
	return nil
}

func equalOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validSolverToken(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("._-+/:", character) {
			continue
		}
		return false
	}
	return true
}

func finalizeSolverReceipt(request SolverRequest, receipt SolverReceipt) (SolverReceipt, error) {
	receipt.AttemptedBackends = append([]string(nil), receipt.AttemptedBackends...)
	receipt.BackendReportedEvidence = cloneBackendReportedEvidence(receipt.BackendReportedEvidence)
	if receipt.SelectedBackend != nil {
		descriptor := canonicalBackendDescriptor(*receipt.SelectedBackend)
		receipt.SelectedBackend = &descriptor
	}
	var err error
	receipt.ID, err = solverReceiptID(receipt)
	if err != nil {
		return SolverReceipt{}, err
	}
	if err := VerifySolverReceipt(request, receipt); err != nil {
		return SolverReceipt{}, err
	}
	return receipt, nil
}

func solverRequestID(request SolverRequest) (string, error) {
	request.ID = ""
	digest, err := digestJSON(request)
	if err != nil {
		return "", err
	}
	return "solreq_" + strings.TrimPrefix(digest, "sha256:"), nil
}

func solverReceiptID(receipt SolverReceipt) (string, error) {
	receipt.ID = ""
	digest, err := digestJSON(receipt)
	if err != nil {
		return "", err
	}
	return "solrcpt_" + strings.TrimPrefix(digest, "sha256:"), nil
}

func digestJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode solver evidence: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func canonicalBackendDescriptor(descriptor SolverBackendDescriptor) SolverBackendDescriptor {
	descriptor.Capabilities = append([]string(nil), descriptor.Capabilities...)
	sort.Strings(descriptor.Capabilities)
	return descriptor
}

func solverObjectiveEvidence(result Result) *SolverObjectiveEvidence {
	return &SolverObjectiveEvidence{
		Unit:       "normalized_load_ratio",
		Incumbent:  exactFloat(result.RoundedMakespan),
		LowerBound: exactFloat(result.RelaxedMakespan),
		Gap:        exactFloat(result.OptimalityGap),
	}
}

func exactFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}

func nearlyEqual(left, right float64) bool {
	return math.Abs(left-right) <= tolerance*math.Max(1, math.Max(math.Abs(left), math.Abs(right)))
}

var (
	solverBearerPattern = regexp.MustCompile(`(?i)\b(bearer)\s+[^\s,;]+`)
	solverSecretPattern = regexp.MustCompile(`(?i)\b(authorization|password|passwd|token|secret|api[_-]?key)\s*[:=]\s*[^\s,;]+`)
)

func boundedSolverExplanation(value string) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "�")
	value = solverBearerPattern.ReplaceAllString(value, "$1 [REDACTED]")
	value = solverSecretPattern.ReplaceAllStringFunc(value, func(match string) string {
		separator := strings.IndexAny(match, ":=")
		if separator < 0 {
			return "[REDACTED]"
		}
		return strings.TrimSpace(match[:separator]) + "=[REDACTED]"
	})
	if len(value) <= maximumSolverExplanation {
		return value
	}
	value = value[:maximumSolverExplanation]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value) + "…"
}
