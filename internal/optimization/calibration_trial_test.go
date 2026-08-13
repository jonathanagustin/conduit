package optimization

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCalibrationTrialRequiresCorpusAndPreservesUnknownRealizedOutcome(t *testing.T) {
	referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt := solverCalibrationFixture(t, SolverBackendOutcome{})
	record, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := AssessCalibrationTrial([]SolverShadowCalibration{record}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != CalibrationTrialInsufficient || assessment.RealizedOutcomeStatus != SolverShadowRealizedUnknown || assessment.ObservedRuns != 1 {
		t.Fatalf("assessment = %#v", assessment)
	}
	if err := VerifyCalibrationTrialAssessment(assessment); err != nil {
		t.Fatal(err)
	}
}

func TestCalibrationTrialRejectsRepeatedInputCorpusAndTampering(t *testing.T) {
	referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt := solverCalibrationFixture(t, SolverBackendOutcome{})
	first, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = ""
	// A second independently identified record is needed for corpus cardinality;
	// retain the same verified semantics while changing only the compared time.
	second.ComparedAt = comparedAt.Add(time.Second).Format(time.RFC3339Nano)
	second.ID, _ = solverShadowCalibrationID(second)
	if err := VerifySolverShadowCalibration(second); err != nil {
		t.Fatal(err)
	}
	assessment, err := AssessCalibrationTrial([]SolverShadowCalibration{first, second}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != CalibrationTrialInsufficient || assessment.DistinctInputDigests != 1 || assessment.CandidateOperationalFailures != 0 || assessment.FeasibilityDisagreements != 0 {
		t.Fatalf("repeated-input assessment = %#v", assessment)
	}
	assessment.Status = CalibrationTrialPass
	assessment.Reason = "forged pass"
	assessment.ID = calibrationTrialAssessmentID(assessment)
	if err := VerifyCalibrationTrialAssessment(assessment); err == nil {
		t.Fatal("accepted slower trial as a passing assessment")
	}
}

func TestCalibrationTrialVerifierAcceptsOnlyAValidPassShape(t *testing.T) {
	assessment := CalibrationTrialAssessment{
		APIVersion: CalibrationTrialAPIVersion, Kind: "CalibrationTrialAssessment", Backend: "highs", MinimumRuns: 2,
		ObservedRuns: 2, DistinctInputDigests: 2, CalibrationIDs: []string{"solcal_" + strings.Repeat("a", 64), "solcal_" + strings.Repeat("b", 64)}, RealizedOutcomeStatus: SolverShadowRealizedUnknown,
		Status: CalibrationTrialPass, Reason: "criterion met", ReferenceLatencyNanoseconds: 10, CandidateLatencyNanoseconds: 10,
	}
	assessment.ID = calibrationTrialAssessmentID(assessment)
	if err := VerifyCalibrationTrialAssessment(assessment); err != nil {
		t.Fatal(err)
	}
}

func TestDiscreteCalibrationTrialUsesTheSameConservativeCriterion(t *testing.T) {
	referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt := discreteSolverCalibrationFixture(t, DiscreteBackendOutcome{})
	record, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := AssessDiscreteCalibrationTrial([]DiscreteSolverShadowCalibration{record}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != CalibrationTrialInsufficient || assessment.Backend == "" || assessment.RealizedOutcomeStatus != SolverShadowRealizedUnknown {
		t.Fatalf("discrete assessment = %#v", assessment)
	}
}

func TestDiscreteCalibrationTrialPassRequiresAndAcceptsDistinctInputs(t *testing.T) {
	refOne, _, candOne, _, comparedAt := discreteSolverCalibrationFixture(t, DiscreteBackendOutcome{})
	startOne := comparedAt.Add(-10 * time.Millisecond)
	refReceiptOne, err := RunDiscreteSolverRequest(context.Background(), refOne, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceDiscreteClock(startOne, startOne.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	candReceiptOne, err := RunDiscreteSolverRequest(context.Background(), candOne, []DiscreteSolverBackend{calibrationDiscreteBackend{outcome: DiscreteBackendOutcome{Termination: SolverFeasible, Choices: testDiscreteChoices()}}}, sequenceDiscreteClock(startOne.Add(time.Millisecond), startOne.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	refRecommendation := cloneSolverJSON(t, refOne.Recommendation)
	refRecommendation.Problem.Tasks[0].Demand["cpu"]++
	refTwo, err := NewDiscreteSolverRequest(refRecommendation, refOne.Selection, refOne.Determinism, refOne.Limits)
	if err != nil {
		t.Fatal(err)
	}
	candRecommendation := cloneSolverJSON(t, candOne.Recommendation)
	candRecommendation.Problem.Tasks[0].Demand["cpu"]++
	candTwo, err := NewDiscreteSolverRequest(candRecommendation, candOne.Selection, candOne.Determinism, candOne.Limits)
	if err != nil {
		t.Fatal(err)
	}
	startTwo := startOne.Add(time.Second)
	refReceiptTwo, err := RunDiscreteSolverRequest(context.Background(), refTwo, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceDiscreteClock(startTwo, startTwo.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	candReceiptTwo, err := RunDiscreteSolverRequest(context.Background(), candTwo, []DiscreteSolverBackend{calibrationDiscreteBackend{outcome: DiscreteBackendOutcome{Termination: SolverFeasible, Choices: testDiscreteChoices()}}}, sequenceDiscreteClock(startTwo.Add(time.Millisecond), startTwo.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	one, err := NewDiscreteSolverShadowCalibration(refOne, refReceiptOne, candOne, candReceiptOne, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	two, err := NewDiscreteSolverShadowCalibration(refTwo, refReceiptTwo, candTwo, candReceiptTwo, startTwo.Add(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := AssessDiscreteCalibrationTrial([]DiscreteSolverShadowCalibration{one, two}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != CalibrationTrialPass || assessment.DistinctInputDigests != 2 {
		t.Fatalf("distinct-input discrete calibration assessment = %#v", assessment)
	}
}

func TestCalibrationTrialPassRequiresAndAcceptsDistinctInputs(t *testing.T) {
	refOne, _, candOne, _, comparedAt := solverCalibrationFixture(t, SolverBackendOutcome{})
	startOne := comparedAt.Add(-10 * time.Millisecond)
	refReceiptOne, err := RunSolverRequest(context.Background(), refOne, []SolverBackend{NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceClock(startOne, startOne.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	resultOne, err := Solve(candOne.Recommendation.Problem)
	if err != nil {
		t.Fatal(err)
	}
	candReceiptOne, err := RunSolverRequest(context.Background(), candOne, []SolverBackend{fixtureSolverBackend{outcome: SolverBackendOutcome{Termination: SolverFeasible, Result: &resultOne}}}, sequenceClock(startOne.Add(time.Millisecond), startOne.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	refRecommendation := cloneSolverJSON(t, refOne.Recommendation)
	refRecommendation.Problem.Items[0].Demand++
	refTwo, err := NewSolverRequest(refRecommendation, refOne.Selection, refOne.Determinism, refOne.Limits)
	if err != nil {
		t.Fatal(err)
	}
	candRecommendation := cloneSolverJSON(t, candOne.Recommendation)
	candRecommendation.Problem.Items[0].Demand++
	candTwo, err := NewSolverRequest(candRecommendation, candOne.Selection, candOne.Determinism, candOne.Limits)
	if err != nil {
		t.Fatal(err)
	}
	started := comparedAt.Add(time.Second)
	refReceiptTwo, err := RunSolverRequest(context.Background(), refTwo, []SolverBackend{NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceClock(started, started.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Solve(candTwo.Recommendation.Problem)
	if err != nil {
		t.Fatal(err)
	}
	candReceiptTwo, err := RunSolverRequest(context.Background(), candTwo, []SolverBackend{fixtureSolverBackend{outcome: SolverBackendOutcome{Termination: SolverFeasible, Result: &result}}}, sequenceClock(started.Add(time.Millisecond), started.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	one, err := NewSolverShadowCalibration(refOne, refReceiptOne, candOne, candReceiptOne, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	two, err := NewSolverShadowCalibration(refTwo, refReceiptTwo, candTwo, candReceiptTwo, started.Add(10*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	assessment, err := AssessCalibrationTrial([]SolverShadowCalibration{one, two}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Status != CalibrationTrialPass || assessment.DistinctInputDigests != 2 {
		t.Fatalf("distinct-input calibration assessment = %#v", assessment)
	}
}

func TestCalibrationTrialRejectsForeignCorpusIdentity(t *testing.T) {
	assessment := CalibrationTrialAssessment{
		APIVersion: CalibrationTrialAPIVersion, Kind: "CalibrationTrialAssessment", Backend: "highs", MinimumRuns: 1,
		ObservedRuns: 1, CalibrationIDs: []string{"foreign-record"}, RealizedOutcomeStatus: SolverShadowRealizedUnknown,
		Status: CalibrationTrialInsufficient, Reason: "not enough runs",
	}
	assessment.ID = calibrationTrialAssessmentID(assessment)
	if err := VerifyCalibrationTrialAssessment(assessment); err == nil {
		t.Fatal("accepted foreign calibration record identity")
	}
}
