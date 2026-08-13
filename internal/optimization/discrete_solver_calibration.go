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

const DiscreteSolverCalibrationAPIVersion = "optimization.platform.example/discrete-solver-calibration/v1alpha1"

// DiscreteSolverCalibrationRun retains complete evidence so a calibration can
// still be replayed after either backend is disabled or retired.
type DiscreteSolverCalibrationRun struct {
	Request DiscreteSolverRequest `json:"request"`
	Receipt DiscreteSolverReceipt `json:"receipt"`
}

// DiscreteSolverShadowMetrics are derived exclusively from independently
// verified receipts. Optional result metrics are absent when either solve did
// not return a complete, compiler-verifiable schedule.
type DiscreteSolverShadowMetrics struct {
	ReferenceLatencyNanoseconds       int64  `json:"referenceLatencyNanoseconds"`
	CandidateLatencyNanoseconds       int64  `json:"candidateLatencyNanoseconds"`
	ReferenceMakespanSeconds          *int64 `json:"referenceMakespanSeconds,omitempty"`
	CandidateMakespanSeconds          *int64 `json:"candidateMakespanSeconds,omitempty"`
	CandidateMinusReferenceMakespan   *int64 `json:"candidateMinusReferenceMakespan,omitempty"`
	ReferenceEstimatedCostMicros      *int64 `json:"referenceEstimatedCostMicros,omitempty"`
	CandidateEstimatedCostMicros      *int64 `json:"candidateEstimatedCostMicros,omitempty"`
	CandidateMinusReferenceCostMicros *int64 `json:"candidateMinusReferenceCostMicros,omitempty"`
	AssignmentDisagreementCount       *int   `json:"assignmentDisagreementCount,omitempty"`
	ReferenceHadOperationalFailure    bool   `json:"referenceHadOperationalFailure"`
	CandidateHadOperationalFailure    bool   `json:"candidateHadOperationalFailure"`
}

// DiscreteSolverShadowCalibration compares a native reference with a distinct
// discrete candidate without granting either solve proposal or mutation
// authority. Candidate-specific realized outcomes remain unknown because a
// shadow receipt was not selected for governed execution.
type DiscreteSolverShadowCalibration struct {
	APIVersion         string                       `json:"apiVersion"`
	Kind               string                       `json:"kind"`
	ID                 string                       `json:"id"`
	Mode               string                       `json:"mode"`
	InfluencedProposal bool                         `json:"influencedProposal"`
	MutatedState       bool                         `json:"mutatedState"`
	InputDigest        string                       `json:"inputDigest"`
	Reference          DiscreteSolverCalibrationRun `json:"reference"`
	Candidate          DiscreteSolverCalibrationRun `json:"candidate"`
	Feasibility        SolverFeasibilityComparison  `json:"feasibility"`
	Metrics            DiscreteSolverShadowMetrics  `json:"metrics"`
	RealizedOutcome    SolverShadowRealizedOutcome  `json:"realizedOutcome"`
	Unknowns           []string                     `json:"unknowns"`
	ComparedAt         string                       `json:"comparedAt"`
}

func NewDiscreteSolverShadowCalibration(
	referenceRequest DiscreteSolverRequest,
	referenceReceipt DiscreteSolverReceipt,
	candidateRequest DiscreteSolverRequest,
	candidateReceipt DiscreteSolverReceipt,
	comparedAt time.Time,
) (DiscreteSolverShadowCalibration, error) {
	record, err := buildDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		return DiscreteSolverShadowCalibration{}, err
	}
	record.ID, err = discreteSolverShadowCalibrationID(record)
	if err != nil {
		return DiscreteSolverShadowCalibration{}, err
	}
	return record, nil
}

func VerifyDiscreteSolverShadowCalibration(record DiscreteSolverShadowCalibration) error {
	if record.APIVersion != DiscreteSolverCalibrationAPIVersion || record.Kind != "DiscreteSolverShadowCalibration" || !validContentID(record.ID, "dsolcal_") {
		return errors.New("unsupported or incomplete discrete solver shadow calibration")
	}
	comparedAt, err := time.Parse(time.RFC3339Nano, record.ComparedAt)
	if err != nil {
		return fmt.Errorf("discrete solver calibration comparedAt: %w", err)
	}
	replayed, err := buildDiscreteSolverShadowCalibration(record.Reference.Request, record.Reference.Receipt, record.Candidate.Request, record.Candidate.Receipt, comparedAt)
	if err != nil {
		return err
	}
	replayed.ID, err = discreteSolverShadowCalibrationID(replayed)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(replayed, record) {
		return errors.New("discrete solver shadow calibration does not match independent replay")
	}
	return nil
}

func buildDiscreteSolverShadowCalibration(
	referenceRequest DiscreteSolverRequest,
	referenceReceipt DiscreteSolverReceipt,
	candidateRequest DiscreteSolverRequest,
	candidateReceipt DiscreteSolverReceipt,
	comparedAt time.Time,
) (DiscreteSolverShadowCalibration, error) {
	if err := VerifyDiscreteSolverReceipt(referenceRequest, referenceReceipt); err != nil {
		return DiscreteSolverShadowCalibration{}, fmt.Errorf("reference discrete solver evidence: %w", err)
	}
	if err := VerifyDiscreteSolverReceipt(candidateRequest, candidateReceipt); err != nil {
		return DiscreteSolverShadowCalibration{}, fmt.Errorf("candidate discrete solver evidence: %w", err)
	}
	if referenceRequest.Selection.PreferredBackend != NativeBackendName || candidateRequest.Selection.PreferredBackend == NativeBackendName || referenceRequest.Selection.PreferredBackend == candidateRequest.Selection.PreferredBackend {
		return DiscreteSolverShadowCalibration{}, errors.New("discrete shadow calibration requires distinct native-reference and candidate backend selections")
	}
	if len(referenceRequest.Selection.AuthorizedFallbacks) != 0 || len(candidateRequest.Selection.AuthorizedFallbacks) != 0 {
		return DiscreteSolverShadowCalibration{}, errors.New("discrete shadow calibration forbids fallback ambiguity")
	}
	referenceInput, referenceDigest, err := discreteSolverCalibrationInput(referenceRequest)
	if err != nil {
		return DiscreteSolverShadowCalibration{}, err
	}
	candidateInput, candidateDigest, err := discreteSolverCalibrationInput(candidateRequest)
	if err != nil {
		return DiscreteSolverShadowCalibration{}, err
	}
	if referenceDigest != candidateDigest || !reflect.DeepEqual(referenceInput, candidateInput) {
		return DiscreteSolverShadowCalibration{}, errors.New("discrete shadow calibration requires the exact same non-selection input")
	}
	latestFinish, err := laterDiscreteSolverFinish(referenceReceipt, candidateReceipt)
	if err != nil {
		return DiscreteSolverShadowCalibration{}, err
	}
	comparedAt = comparedAt.UTC()
	if comparedAt.Before(latestFinish) {
		return DiscreteSolverShadowCalibration{}, errors.New("discrete solver calibration predates a compared receipt")
	}
	metrics, err := deriveDiscreteSolverShadowMetrics(referenceReceipt, candidateReceipt)
	if err != nil {
		return DiscreteSolverShadowCalibration{}, err
	}
	referenceRun, err := cloneDiscreteSolverCalibrationRun(DiscreteSolverCalibrationRun{Request: referenceRequest, Receipt: referenceReceipt})
	if err != nil {
		return DiscreteSolverShadowCalibration{}, err
	}
	candidateRun, err := cloneDiscreteSolverCalibrationRun(DiscreteSolverCalibrationRun{Request: candidateRequest, Receipt: candidateReceipt})
	if err != nil {
		return DiscreteSolverShadowCalibration{}, err
	}
	unknowns := []string{
		"candidate realized cost error: shadow candidate was not executed",
		"candidate realized schedule error: shadow candidate was not executed",
		"candidate realized value error: shadow candidate was not executed",
	}
	sort.Strings(unknowns)
	return DiscreteSolverShadowCalibration{
		APIVersion:  DiscreteSolverCalibrationAPIVersion,
		Kind:        "DiscreteSolverShadowCalibration",
		Mode:        "SHADOW",
		InputDigest: referenceDigest,
		Reference:   referenceRun,
		Candidate:   candidateRun,
		Feasibility: deriveDiscreteSolverFeasibilityComparison(referenceReceipt, candidateReceipt),
		Metrics:     metrics,
		RealizedOutcome: SolverShadowRealizedOutcome{
			ScheduleError: SolverShadowRealizedUnknown,
			CostError:     SolverShadowRealizedUnknown,
			ValueError:    SolverShadowRealizedUnknown,
		},
		Unknowns:   unknowns,
		ComparedAt: comparedAt.Format(time.RFC3339Nano),
	}, nil
}

type discreteSolverCalibrationComparableInput struct {
	APIVersion     string                        `json:"apiVersion"`
	Kind           string                        `json:"kind"`
	Recommendation DiscreteRecommendationRequest `json:"recommendation"`
	Model          SolverModel                   `json:"model"`
	Capability     string                        `json:"capability"`
	Determinism    SolverDeterminism             `json:"determinism"`
	Limits         SolverLimits                  `json:"limits"`
}

func discreteSolverCalibrationInput(request DiscreteSolverRequest) (discreteSolverCalibrationComparableInput, string, error) {
	input := discreteSolverCalibrationComparableInput{
		APIVersion:     request.APIVersion,
		Kind:           request.Kind,
		Recommendation: request.Recommendation,
		Model:          request.Model,
		Capability:     request.Capability,
		Determinism:    request.Determinism,
		Limits:         request.Limits,
	}
	digest, err := digestJSON(input)
	return input, digest, err
}

func laterDiscreteSolverFinish(reference, candidate DiscreteSolverReceipt) (time.Time, error) {
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

func deriveDiscreteSolverFeasibilityComparison(reference, candidate DiscreteSolverReceipt) SolverFeasibilityComparison {
	if reference.Result != nil && candidate.Result != nil {
		return SolverFeasibilityAgreeFeasible
	}
	if discreteSolverOperationallyUnknown(reference) || discreteSolverOperationallyUnknown(candidate) {
		return SolverFeasibilityUnknown
	}
	if reference.Termination == SolverInfeasible && candidate.Termination == SolverInfeasible {
		return SolverFeasibilityAgreeInfeasible
	}
	return SolverFeasibilityDisagree
}

func discreteSolverOperationallyUnknown(receipt DiscreteSolverReceipt) bool {
	if receipt.Result != nil {
		return false
	}
	return receipt.Termination == SolverUnknown || receipt.Termination == SolverError || receipt.Termination == SolverTimeLimit
}

func deriveDiscreteSolverShadowMetrics(reference, candidate DiscreteSolverReceipt) (DiscreteSolverShadowMetrics, error) {
	referenceStarted, _ := time.Parse(time.RFC3339Nano, reference.StartedAt)
	referenceFinished, _ := time.Parse(time.RFC3339Nano, reference.FinishedAt)
	candidateStarted, _ := time.Parse(time.RFC3339Nano, candidate.StartedAt)
	candidateFinished, _ := time.Parse(time.RFC3339Nano, candidate.FinishedAt)
	referenceLatency, err := exactElapsedNanoseconds(referenceStarted, referenceFinished)
	if err != nil {
		return DiscreteSolverShadowMetrics{}, fmt.Errorf("reference discrete solver latency: %w", err)
	}
	candidateLatency, err := exactElapsedNanoseconds(candidateStarted, candidateFinished)
	if err != nil {
		return DiscreteSolverShadowMetrics{}, fmt.Errorf("candidate discrete solver latency: %w", err)
	}
	metrics := DiscreteSolverShadowMetrics{
		ReferenceLatencyNanoseconds:    referenceLatency,
		CandidateLatencyNanoseconds:    candidateLatency,
		ReferenceHadOperationalFailure: discreteSolverOperationallyUnknown(reference),
		CandidateHadOperationalFailure: discreteSolverOperationallyUnknown(candidate),
	}
	if reference.Result == nil || candidate.Result == nil {
		return metrics, nil
	}
	referenceMakespan := reference.Result.MakespanSeconds
	candidateMakespan := candidate.Result.MakespanSeconds
	makespanDelta := exactInt64Difference(candidateMakespan, referenceMakespan)
	disagreements := discreteAssignmentDisagreements(*reference.Result, *candidate.Result)
	metrics.ReferenceMakespanSeconds = &referenceMakespan
	metrics.CandidateMakespanSeconds = &candidateMakespan
	metrics.CandidateMinusReferenceMakespan = &makespanDelta
	metrics.AssignmentDisagreementCount = &disagreements
	if reference.Result.EstimatedTotalCostMicros != nil && candidate.Result.EstimatedTotalCostMicros != nil {
		referenceCost := *reference.Result.EstimatedTotalCostMicros
		candidateCost := *candidate.Result.EstimatedTotalCostMicros
		costDelta := exactInt64Difference(candidateCost, referenceCost)
		metrics.ReferenceEstimatedCostMicros = &referenceCost
		metrics.CandidateEstimatedCostMicros = &candidateCost
		metrics.CandidateMinusReferenceCostMicros = &costDelta
	}
	return metrics, nil
}

func exactElapsedNanoseconds(started, finished time.Time) (int64, error) {
	seconds := new(big.Int).Sub(big.NewInt(finished.Unix()), big.NewInt(started.Unix()))
	result := new(big.Int).Mul(seconds, big.NewInt(int64(time.Second)))
	result.Add(result, big.NewInt(int64(finished.Nanosecond()-started.Nanosecond())))
	if result.Sign() < 0 || !result.IsInt64() {
		return 0, errors.New("duration is negative or exceeds exact int64 nanoseconds")
	}
	return result.Int64(), nil
}

type discreteAssignmentIdentity struct {
	Lane  string
	Start int64
}

func discreteAssignmentDisagreements(reference, candidate DiscreteScheduleResult) int {
	want := make(map[string]discreteAssignmentIdentity, len(reference.Assignments))
	for _, assignment := range reference.Assignments {
		want[assignment.Task] = discreteAssignmentIdentity{Lane: assignment.Lane, Start: assignment.Start}
	}
	disagreements := 0
	for _, assignment := range candidate.Assignments {
		if want[assignment.Task] != (discreteAssignmentIdentity{Lane: assignment.Lane, Start: assignment.Start}) {
			disagreements++
		}
	}
	return disagreements
}

func cloneDiscreteSolverCalibrationRun(run DiscreteSolverCalibrationRun) (DiscreteSolverCalibrationRun, error) {
	encoded, err := json.Marshal(run)
	if err != nil {
		return DiscreteSolverCalibrationRun{}, err
	}
	var cloned DiscreteSolverCalibrationRun
	if err := json.Unmarshal(encoded, &cloned); err != nil {
		return DiscreteSolverCalibrationRun{}, err
	}
	return cloned, nil
}

func discreteSolverShadowCalibrationID(record DiscreteSolverShadowCalibration) (string, error) {
	record.ID = ""
	digest, err := digestJSON(record)
	if err != nil {
		return "", err
	}
	return "dsolcal_" + digest[len("sha256:"):], nil
}

type FileDiscreteSolverCalibrationStore struct{ Directory string }

func (store FileDiscreteSolverCalibrationStore) Save(record DiscreteSolverShadowCalibration) error {
	if err := VerifyDiscreteSolverShadowCalibration(record); err != nil {
		return err
	}
	return FileTwinStore(store).save("discrete-solver-calibrations", record.ID, record)
}

func (store FileDiscreteSolverCalibrationStore) Get(id string) (DiscreteSolverShadowCalibration, error) {
	var record DiscreteSolverShadowCalibration
	if !validContentID(id, "dsolcal_") {
		return record, errors.New("invalid discrete solver calibration id")
	}
	if err := FileTwinStore(store).get("discrete-solver-calibrations", id, &record); err != nil {
		return record, err
	}
	return record, VerifyDiscreteSolverShadowCalibration(record)
}
