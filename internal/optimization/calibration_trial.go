package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

const CalibrationTrialAPIVersion = "optimization.platform.example/calibration-trial/v1alpha1"

const (
	CalibrationTrialInsufficient = "INSUFFICIENT"
	CalibrationTrialPass         = "PASS"
	CalibrationTrialFail         = "FAIL"
)

// CalibrationTrialAssessment is an aggregate, immutable gate over already
// verified shadow records. It is evidence only: it cannot alter lifecycle
// state, authorize a backend, or claim a realized business outcome.
type CalibrationTrialAssessment struct {
	APIVersion                   string   `json:"apiVersion"`
	Kind                         string   `json:"kind"`
	ID                           string   `json:"id"`
	Backend                      string   `json:"backend"`
	BackendVersion               string   `json:"backendVersion,omitempty"`
	BackendArtifactDigest        string   `json:"backendArtifactDigest,omitempty"`
	BackendArchitecture          string   `json:"backendArchitecture,omitempty"`
	MinimumRuns                  int      `json:"minimumRuns"`
	ObservedRuns                 int      `json:"observedRuns"`
	DistinctInputDigests         int      `json:"distinctInputDigests"`
	CalibrationIDs               []string `json:"calibrationIds"`
	ReferenceOperationalFailures int      `json:"referenceOperationalFailures"`
	CandidateOperationalFailures int      `json:"candidateOperationalFailures"`
	FeasibilityDisagreements     int      `json:"feasibilityDisagreements"`
	AssignmentDisagreements      int      `json:"assignmentDisagreements"`
	ReferenceLatencyNanoseconds  int64    `json:"referenceLatencyNanoseconds"`
	CandidateLatencyNanoseconds  int64    `json:"candidateLatencyNanoseconds"`
	RealizedOutcomeStatus        string   `json:"realizedOutcomeStatus"`
	Status                       string   `json:"status"`
	Reason                       string   `json:"reason"`
}

type calibrationTrialSample struct {
	ID, InputDigest, Backend, BackendVersion, BackendArtifactDigest, BackendArchitecture, Feasibility string
	ReferenceFailure, CandidateFailure                                                                bool
	AssignmentDisagreements                                                                           int
	ReferenceLatency, CandidateLatency                                                                int64
}

// AssessCalibrationTrial applies a deliberately conservative P9 criterion.
// A candidate must have enough runs, never disagree on feasibility, and be no
// worse than the native reference on aggregate failures or latency. Shadow
// records with operationally unknown outcomes count as failures and remain
// non-selectable evidence.
func AssessCalibrationTrial(records []SolverShadowCalibration, minimumRuns int) (CalibrationTrialAssessment, error) {
	samples := make([]calibrationTrialSample, 0, len(records))
	for _, record := range records {
		if err := VerifySolverShadowCalibration(record); err != nil {
			return CalibrationTrialAssessment{}, fmt.Errorf("calibration trial record: %w", err)
		}
		disagreements := 0
		if record.Metrics.AssignmentDisagreementCount != nil {
			disagreements = *record.Metrics.AssignmentDisagreementCount
		}
		version, artifact, architecture := "", "", ""
		if record.Candidate.Receipt.SelectedBackend != nil {
			version, artifact, architecture = record.Candidate.Receipt.SelectedBackend.Version, record.Candidate.Receipt.SelectedBackend.ExecutableDigest, record.Candidate.Receipt.SelectedBackend.Architecture
		}
		samples = append(samples, calibrationTrialSample{ID: record.ID, InputDigest: record.InputDigest, Backend: record.Candidate.Request.Selection.PreferredBackend, BackendVersion: version, BackendArtifactDigest: artifact, BackendArchitecture: architecture, Feasibility: string(record.Feasibility), ReferenceFailure: record.Metrics.ReferenceHadOperationalFailure, CandidateFailure: record.Metrics.CandidateHadOperationalFailure, AssignmentDisagreements: disagreements, ReferenceLatency: record.Metrics.ReferenceLatencyNanoseconds, CandidateLatency: record.Metrics.CandidateLatencyNanoseconds})
	}
	return assessCalibrationTrialSamples(samples, minimumRuns)
}

// AssessDiscreteCalibrationTrial applies the same P9 criterion to the
// discrete CP-SAT shadow contract; it is intentionally separate at the API
// boundary because discrete and continuous receipts are not interchangeable.
func AssessDiscreteCalibrationTrial(records []DiscreteSolverShadowCalibration, minimumRuns int) (CalibrationTrialAssessment, error) {
	samples := make([]calibrationTrialSample, 0, len(records))
	for _, record := range records {
		if err := VerifyDiscreteSolverShadowCalibration(record); err != nil {
			return CalibrationTrialAssessment{}, fmt.Errorf("discrete calibration trial record: %w", err)
		}
		disagreements := 0
		if record.Metrics.AssignmentDisagreementCount != nil {
			disagreements = *record.Metrics.AssignmentDisagreementCount
		}
		version, artifact, architecture := "", "", ""
		if record.Candidate.Receipt.SelectedBackend != nil {
			version, artifact, architecture = record.Candidate.Receipt.SelectedBackend.Version, record.Candidate.Receipt.SelectedBackend.ExecutableDigest, record.Candidate.Receipt.SelectedBackend.Architecture
		}
		samples = append(samples, calibrationTrialSample{ID: record.ID, InputDigest: record.InputDigest, Backend: record.Candidate.Request.Selection.PreferredBackend, BackendVersion: version, BackendArtifactDigest: artifact, BackendArchitecture: architecture, Feasibility: string(record.Feasibility), ReferenceFailure: record.Metrics.ReferenceHadOperationalFailure, CandidateFailure: record.Metrics.CandidateHadOperationalFailure, AssignmentDisagreements: disagreements, ReferenceLatency: record.Metrics.ReferenceLatencyNanoseconds, CandidateLatency: record.Metrics.CandidateLatencyNanoseconds})
	}
	return assessCalibrationTrialSamples(samples, minimumRuns)
}

func assessCalibrationTrialSamples(samples []calibrationTrialSample, minimumRuns int) (CalibrationTrialAssessment, error) {
	if minimumRuns < 1 {
		return CalibrationTrialAssessment{}, errors.New("calibration trial minimum runs must be positive")
	}
	if len(samples) == 0 {
		return CalibrationTrialAssessment{}, errors.New("calibration trial requires shadow records")
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i].ID < samples[j].ID })
	assessment := CalibrationTrialAssessment{APIVersion: CalibrationTrialAPIVersion, Kind: "CalibrationTrialAssessment", MinimumRuns: minimumRuns, ObservedRuns: len(samples), RealizedOutcomeStatus: SolverShadowRealizedUnknown}
	seen := map[string]bool{}
	inputDigests := map[string]bool{}
	for _, sample := range samples {
		if seen[sample.ID] {
			return CalibrationTrialAssessment{}, fmt.Errorf("calibration trial repeats record %q", sample.ID)
		}
		seen[sample.ID] = true
		if sample.InputDigest == "" {
			return CalibrationTrialAssessment{}, errors.New("calibration trial sample is missing an input digest")
		}
		inputDigests[sample.InputDigest] = true
		if assessment.Backend == "" {
			assessment.Backend, assessment.BackendVersion, assessment.BackendArtifactDigest, assessment.BackendArchitecture = sample.Backend, sample.BackendVersion, sample.BackendArtifactDigest, sample.BackendArchitecture
		} else if assessment.Backend != sample.Backend || assessment.BackendVersion != sample.BackendVersion || assessment.BackendArtifactDigest != sample.BackendArtifactDigest || assessment.BackendArchitecture != sample.BackendArchitecture {
			return CalibrationTrialAssessment{}, errors.New("calibration trial mixes candidate backend coordinates")
		}
		assessment.CalibrationIDs = append(assessment.CalibrationIDs, sample.ID)
		if sample.ReferenceFailure {
			assessment.ReferenceOperationalFailures++
		}
		if sample.CandidateFailure {
			assessment.CandidateOperationalFailures++
		}
		if sample.Feasibility == string(SolverFeasibilityDisagree) {
			assessment.FeasibilityDisagreements++
		}
		assessment.AssignmentDisagreements += sample.AssignmentDisagreements
		assessment.ReferenceLatencyNanoseconds += sample.ReferenceLatency
		assessment.CandidateLatencyNanoseconds += sample.CandidateLatency
	}
	assessment.DistinctInputDigests = len(inputDigests)
	minimumDistinctInputs := minimumRuns
	if minimumDistinctInputs > 2 {
		minimumDistinctInputs = 2
	}
	if len(assessment.CalibrationIDs) < minimumRuns || assessment.DistinctInputDigests < minimumDistinctInputs {
		assessment.Status = CalibrationTrialInsufficient
		assessment.Reason = "minimum shadow corpus or distinct input coverage has not been reached"
	} else if assessment.FeasibilityDisagreements > 0 || assessment.CandidateOperationalFailures > assessment.ReferenceOperationalFailures || assessment.CandidateLatencyNanoseconds > assessment.ReferenceLatencyNanoseconds {
		assessment.Status = CalibrationTrialFail
		assessment.Reason = "candidate is infeasible, less reliable, or slower than the native reference"
	} else {
		assessment.Status = CalibrationTrialPass
		assessment.Reason = "candidate meets the bounded shadow trial criterion; realized outcomes remain unknown"
	}
	assessment.ID = calibrationTrialAssessmentID(assessment)
	return assessment, VerifyCalibrationTrialAssessment(assessment)
}

func VerifyCalibrationTrialAssessment(assessment CalibrationTrialAssessment) error {
	if assessment.APIVersion != CalibrationTrialAPIVersion || assessment.Kind != "CalibrationTrialAssessment" || !validContentID(assessment.ID, "caltrial_") || assessment.MinimumRuns < 1 || assessment.ObservedRuns != len(assessment.CalibrationIDs) || assessment.DistinctInputDigests < 1 || assessment.DistinctInputDigests > assessment.ObservedRuns || assessment.RealizedOutcomeStatus != SolverShadowRealizedUnknown {
		return errors.New("invalid calibration trial assessment")
	}
	if assessment.Status != CalibrationTrialInsufficient && assessment.Status != CalibrationTrialPass && assessment.Status != CalibrationTrialFail {
		return errors.New("invalid calibration trial status")
	}
	ids := append([]string(nil), assessment.CalibrationIDs...)
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, assessment.CalibrationIDs) {
		return errors.New("calibration trial IDs are not canonical")
	}
	for _, id := range assessment.CalibrationIDs {
		if !validContentID(id, "solcal_") && !validContentID(id, "dsolcal_") {
			return fmt.Errorf("calibration trial references invalid shadow record %q", id)
		}
	}
	if assessment.CandidateOperationalFailures < 0 || assessment.ReferenceOperationalFailures < 0 || assessment.FeasibilityDisagreements < 0 || assessment.AssignmentDisagreements < 0 || assessment.ReferenceLatencyNanoseconds < 0 || assessment.CandidateLatencyNanoseconds < 0 {
		return errors.New("calibration trial metrics cannot be negative")
	}
	minimumDistinctInputs := assessment.MinimumRuns
	if minimumDistinctInputs > 2 {
		minimumDistinctInputs = 2
	}
	if assessment.Status == CalibrationTrialPass && (assessment.ObservedRuns < assessment.MinimumRuns || assessment.DistinctInputDigests < minimumDistinctInputs || assessment.FeasibilityDisagreements != 0 || assessment.CandidateOperationalFailures > assessment.ReferenceOperationalFailures || assessment.CandidateLatencyNanoseconds > assessment.ReferenceLatencyNanoseconds) {
		return errors.New("calibration trial claims pass without satisfying criterion")
	}
	if calibrationTrialAssessmentID(assessment) != assessment.ID {
		return errors.New("calibration trial ID does not match content")
	}
	return nil
}

func calibrationTrialAssessmentID(assessment CalibrationTrialAssessment) string {
	assessment.ID = ""
	encoded, _ := json.Marshal(assessment)
	digest := sha256.Sum256(encoded)
	return "caltrial_" + hex.EncodeToString(digest[:])
}
