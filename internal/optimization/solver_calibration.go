package optimization

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"time"
)

const SolverCalibrationAPIVersion = "optimization.platform.example/solver-calibration/v1alpha1"

type SolverFeasibilityComparison string

const (
	SolverFeasibilityAgreeFeasible   SolverFeasibilityComparison = "AGREE_FEASIBLE"
	SolverFeasibilityAgreeInfeasible SolverFeasibilityComparison = "AGREE_INFEASIBLE"
	SolverFeasibilityAgreeUnbounded  SolverFeasibilityComparison = "AGREE_UNBOUNDED"
	SolverFeasibilityDisagree        SolverFeasibilityComparison = "DISAGREE"
	SolverFeasibilityUnknown         SolverFeasibilityComparison = "UNKNOWN"
	SolverShadowRealizedUnknown                                  = "UNKNOWN_NOT_EXECUTED"
)

// SolverCalibrationRun retains the complete request and receipt so calibration
// evidence remains independently replayable after a backend is disabled.
type SolverCalibrationRun struct {
	Request SolverRequest `json:"request"`
	Receipt SolverReceipt `json:"receipt"`
}

type SolverShadowMetrics struct {
	ReferenceLatencyNanoseconds       int64  `json:"referenceLatencyNanoseconds"`
	CandidateLatencyNanoseconds       int64  `json:"candidateLatencyNanoseconds"`
	ReferenceOptimalityGap            string `json:"referenceOptimalityGap,omitempty"`
	CandidateOptimalityGap            string `json:"candidateOptimalityGap,omitempty"`
	CandidateMinusReferenceGap        string `json:"candidateMinusReferenceGap,omitempty"`
	CandidateMinusReferenceMakespan   string `json:"candidateMinusReferenceMakespan,omitempty"`
	CandidateMinusReferenceCostMicros *int64 `json:"candidateMinusReferenceCostMicros,omitempty"`
	AssignmentDisagreementCount       *int   `json:"assignmentDisagreementCount,omitempty"`
	ReferenceHadOperationalFailure    bool   `json:"referenceHadOperationalFailure"`
	CandidateHadOperationalFailure    bool   `json:"candidateHadOperationalFailure"`
}

type SolverShadowRealizedOutcome struct {
	ScheduleError string `json:"scheduleError"`
	CostError     string `json:"costError"`
	ValueError    string `json:"valueError"`
}

// SolverShadowCalibration is immutable advisory evidence. A shadow candidate
// cannot have candidate-specific realized error because it did not influence a
// proposal; those outcomes stay explicitly unknown until a separately governed
// experiment executes an exact receipt.
type SolverShadowCalibration struct {
	APIVersion         string                      `json:"apiVersion"`
	Kind               string                      `json:"kind"`
	ID                 string                      `json:"id"`
	Mode               string                      `json:"mode"`
	InfluencedProposal bool                        `json:"influencedProposal"`
	MutatedState       bool                        `json:"mutatedState"`
	InputDigest        string                      `json:"inputDigest"`
	Reference          SolverCalibrationRun        `json:"reference"`
	Candidate          SolverCalibrationRun        `json:"candidate"`
	Feasibility        SolverFeasibilityComparison `json:"feasibility"`
	Metrics            SolverShadowMetrics         `json:"metrics"`
	RealizedOutcome    SolverShadowRealizedOutcome `json:"realizedOutcome"`
	Unknowns           []string                    `json:"unknowns"`
	ComparedAt         string                      `json:"comparedAt"`
}

func NewSolverShadowCalibration(referenceRequest SolverRequest, referenceReceipt SolverReceipt, candidateRequest SolverRequest, candidateReceipt SolverReceipt, comparedAt time.Time) (SolverShadowCalibration, error) {
	record, err := buildSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		return SolverShadowCalibration{}, err
	}
	record.ID, err = solverShadowCalibrationID(record)
	if err != nil {
		return SolverShadowCalibration{}, err
	}
	return record, nil
}

func VerifySolverShadowCalibration(record SolverShadowCalibration) error {
	if record.APIVersion != SolverCalibrationAPIVersion || record.Kind != "SolverShadowCalibration" || !validContentID(record.ID, "solcal_") {
		return errors.New("unsupported or incomplete solver shadow calibration")
	}
	comparedAt, err := time.Parse(time.RFC3339Nano, record.ComparedAt)
	if err != nil {
		return fmt.Errorf("solver calibration comparedAt: %w", err)
	}
	replayed, err := buildSolverShadowCalibration(record.Reference.Request, record.Reference.Receipt, record.Candidate.Request, record.Candidate.Receipt, comparedAt)
	if err != nil {
		return err
	}
	replayed.ID, err = solverShadowCalibrationID(replayed)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replayed, record) {
		return errors.New("solver shadow calibration does not match independent replay")
	}
	return nil
}

func buildSolverShadowCalibration(referenceRequest SolverRequest, referenceReceipt SolverReceipt, candidateRequest SolverRequest, candidateReceipt SolverReceipt, comparedAt time.Time) (SolverShadowCalibration, error) {
	if err := VerifySolverReceipt(referenceRequest, referenceReceipt); err != nil {
		return SolverShadowCalibration{}, fmt.Errorf("reference solver evidence: %w", err)
	}
	if err := VerifySolverReceipt(candidateRequest, candidateReceipt); err != nil {
		return SolverShadowCalibration{}, fmt.Errorf("candidate solver evidence: %w", err)
	}
	if referenceRequest.Selection.PreferredBackend != NativeBackendName || candidateRequest.Selection.PreferredBackend == NativeBackendName || referenceRequest.Selection.PreferredBackend == candidateRequest.Selection.PreferredBackend {
		return SolverShadowCalibration{}, errors.New("shadow calibration requires distinct native-reference and candidate backend selections")
	}
	if len(referenceRequest.Selection.AuthorizedFallbacks) != 0 || len(candidateRequest.Selection.AuthorizedFallbacks) != 0 {
		return SolverShadowCalibration{}, errors.New("shadow calibration forbids fallback ambiguity")
	}
	referenceInput, referenceDigest, err := solverCalibrationInput(referenceRequest)
	if err != nil {
		return SolverShadowCalibration{}, err
	}
	candidateInput, candidateDigest, err := solverCalibrationInput(candidateRequest)
	if err != nil {
		return SolverShadowCalibration{}, err
	}
	if referenceDigest != candidateDigest || !reflect.DeepEqual(referenceInput, candidateInput) {
		return SolverShadowCalibration{}, errors.New("shadow calibration runs must use the exact same non-selection input")
	}
	latestFinish, err := laterSolverFinish(referenceReceipt, candidateReceipt)
	if err != nil {
		return SolverShadowCalibration{}, err
	}
	comparedAt = comparedAt.UTC()
	if comparedAt.Before(latestFinish) {
		return SolverShadowCalibration{}, errors.New("solver calibration predates a compared receipt")
	}
	metrics, err := deriveSolverShadowMetrics(referenceReceipt, candidateReceipt)
	if err != nil {
		return SolverShadowCalibration{}, err
	}
	referenceRun, err := cloneSolverCalibrationRun(SolverCalibrationRun{Request: referenceRequest, Receipt: referenceReceipt})
	if err != nil {
		return SolverShadowCalibration{}, err
	}
	candidateRun, err := cloneSolverCalibrationRun(SolverCalibrationRun{Request: candidateRequest, Receipt: candidateReceipt})
	if err != nil {
		return SolverShadowCalibration{}, err
	}
	unknowns := []string{
		"candidate realized cost error: shadow candidate was not executed",
		"candidate realized schedule error: shadow candidate was not executed",
		"candidate realized value error: shadow candidate was not executed",
	}
	sort.Strings(unknowns)
	return SolverShadowCalibration{
		APIVersion: SolverCalibrationAPIVersion, Kind: "SolverShadowCalibration", Mode: "SHADOW",
		InfluencedProposal: false, MutatedState: false, InputDigest: referenceDigest,
		Reference: referenceRun, Candidate: candidateRun,
		Feasibility: deriveSolverFeasibilityComparison(referenceReceipt, candidateReceipt), Metrics: metrics,
		RealizedOutcome: SolverShadowRealizedOutcome{ScheduleError: SolverShadowRealizedUnknown, CostError: SolverShadowRealizedUnknown, ValueError: SolverShadowRealizedUnknown},
		Unknowns:        unknowns, ComparedAt: comparedAt.Format(time.RFC3339Nano),
	}, nil
}

type solverCalibrationComparableInput struct {
	APIVersion     string                `json:"apiVersion"`
	Kind           string                `json:"kind"`
	Recommendation RecommendationRequest `json:"recommendation"`
	Model          SolverModel           `json:"model"`
	Capability     string                `json:"capability"`
	Determinism    SolverDeterminism     `json:"determinism"`
	Limits         SolverLimits          `json:"limits"`
}

func solverCalibrationInput(request SolverRequest) (solverCalibrationComparableInput, string, error) {
	input := solverCalibrationComparableInput{
		APIVersion: request.APIVersion, Kind: request.Kind, Recommendation: request.Recommendation,
		Model: request.Model, Capability: request.Capability, Determinism: request.Determinism, Limits: request.Limits,
	}
	digest, err := digestJSON(input)
	return input, digest, err
}

func laterSolverFinish(reference, candidate SolverReceipt) (time.Time, error) {
	referenceFinished, err := time.Parse(time.RFC3339Nano, reference.FinishedAt)
	if err != nil {
		return time.Time{}, err
	}
	candidateFinished, err := time.Parse(time.RFC3339Nano, candidate.FinishedAt)
	if err != nil {
		return time.Time{}, err
	}
	if candidateFinished.After(referenceFinished) {
		return candidateFinished, nil
	}
	return referenceFinished, nil
}

func deriveSolverFeasibilityComparison(reference, candidate SolverReceipt) SolverFeasibilityComparison {
	if reference.Result != nil && candidate.Result != nil {
		return SolverFeasibilityAgreeFeasible
	}
	if solverOperationallyUnknown(reference) || solverOperationallyUnknown(candidate) {
		return SolverFeasibilityUnknown
	}
	if reference.Termination == SolverInfeasible && candidate.Termination == SolverInfeasible {
		return SolverFeasibilityAgreeInfeasible
	}
	if reference.Termination == SolverUnbounded && candidate.Termination == SolverUnbounded {
		return SolverFeasibilityAgreeUnbounded
	}
	return SolverFeasibilityDisagree
}

func solverOperationallyUnknown(receipt SolverReceipt) bool {
	if receipt.Result != nil {
		return false
	}
	return receipt.Termination == SolverUnknown || receipt.Termination == SolverError || receipt.Termination == SolverTimeLimit
}

func deriveSolverShadowMetrics(reference, candidate SolverReceipt) (SolverShadowMetrics, error) {
	referenceStarted, _ := time.Parse(time.RFC3339Nano, reference.StartedAt)
	referenceFinished, _ := time.Parse(time.RFC3339Nano, reference.FinishedAt)
	candidateStarted, _ := time.Parse(time.RFC3339Nano, candidate.StartedAt)
	candidateFinished, _ := time.Parse(time.RFC3339Nano, candidate.FinishedAt)
	metrics := SolverShadowMetrics{
		ReferenceLatencyNanoseconds:    referenceFinished.Sub(referenceStarted).Nanoseconds(),
		CandidateLatencyNanoseconds:    candidateFinished.Sub(candidateStarted).Nanoseconds(),
		ReferenceHadOperationalFailure: solverOperationallyUnknown(reference),
		CandidateHadOperationalFailure: solverOperationallyUnknown(candidate),
	}
	if reference.Result == nil || candidate.Result == nil {
		return metrics, nil
	}
	referenceGap := exactRationalFloat(reference.Result.OptimalityGap)
	candidateGap := exactRationalFloat(candidate.Result.OptimalityGap)
	metrics.ReferenceOptimalityGap = referenceGap.RatString()
	metrics.CandidateOptimalityGap = candidateGap.RatString()
	metrics.CandidateMinusReferenceGap = new(big.Rat).Sub(candidateGap, referenceGap).RatString()
	metrics.CandidateMinusReferenceMakespan = new(big.Rat).Sub(exactRationalFloat(candidate.Result.RoundedMakespan), exactRationalFloat(reference.Result.RoundedMakespan)).RatString()
	disagreements := assignmentDisagreements(*reference.Result, *candidate.Result)
	metrics.AssignmentDisagreementCount = &disagreements
	if reference.Result.RoundedEstimatedCostMicros != nil && candidate.Result.RoundedEstimatedCostMicros != nil {
		delta := exactInt64Difference(*candidate.Result.RoundedEstimatedCostMicros, *reference.Result.RoundedEstimatedCostMicros)
		metrics.CandidateMinusReferenceCostMicros = &delta
	}
	return metrics, nil
}

func exactRationalFloat(value float64) *big.Rat {
	return new(big.Rat).SetFloat64(value)
}

func exactInt64Difference(candidate, reference int64) int64 {
	if candidate >= reference {
		return candidate - reference
	}
	return -(reference - candidate)
}

func assignmentDisagreements(reference, candidate Result) int {
	want := make(map[string]string, len(reference.Assignments))
	for _, assignment := range reference.Assignments {
		want[assignment.Item] = assignment.Resource
	}
	disagreements := 0
	for _, assignment := range candidate.Assignments {
		if want[assignment.Item] != assignment.Resource {
			disagreements++
		}
	}
	return disagreements
}

func cloneSolverCalibrationRun(run SolverCalibrationRun) (SolverCalibrationRun, error) {
	encoded, err := json.Marshal(run)
	if err != nil {
		return SolverCalibrationRun{}, err
	}
	var cloned SolverCalibrationRun
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return SolverCalibrationRun{}, err
	}
	return cloned, nil
}

func solverShadowCalibrationID(record SolverShadowCalibration) (string, error) {
	record.ID = ""
	digest, err := digestJSON(record)
	if err != nil {
		return "", err
	}
	return "solcal_" + digest[len("sha256:"):], nil
}

type FileSolverCalibrationStore struct{ Directory string }

func (store FileSolverCalibrationStore) Save(record SolverShadowCalibration) error {
	if err := VerifySolverShadowCalibration(record); err != nil {
		return err
	}
	return FileTwinStore(store).save("solver-calibrations", record.ID, record)
}

func (store FileSolverCalibrationStore) Get(id string) (SolverShadowCalibration, error) {
	var record SolverShadowCalibration
	if !validContentID(id, "solcal_") {
		return record, errors.New("invalid solver calibration id")
	}
	if err := FileTwinStore(store).get("solver-calibrations", id, &record); err != nil {
		return record, err
	}
	return record, VerifySolverShadowCalibration(record)
}
