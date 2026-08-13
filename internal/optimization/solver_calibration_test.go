package optimization

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCommittedLocalHiGHSShadowCalibrationReplaysWithoutAuthority(t *testing.T) {
	encoded, err := os.ReadFile("../../docs/evidence/highs-1.15.1-shadow-amd64.json")
	if err != nil {
		t.Fatal(err)
	}
	var record SolverShadowCalibration
	if err := json.Unmarshal(encoded, &record); err != nil {
		t.Fatal(err)
	}
	if err := VerifySolverShadowCalibration(record); err != nil {
		t.Fatalf("verify committed HiGHS shadow calibration: %v", err)
	}
	candidateEvidence, err := os.ReadFile("../../docs/evidence/highs-1.15.1-shim-candidate-amd64.json")
	if err != nil {
		t.Fatal(err)
	}
	var candidate struct {
		Admission string `json:"admission"`
		Candidate struct {
			BinarySHA256 string `json:"binarySha256"`
		} `json:"candidate"`
	}
	if err := json.Unmarshal(candidateEvidence, &candidate); err != nil {
		t.Fatal(err)
	}
	if candidate.Admission != "UNADMITTED" || record.Mode != "SHADOW" || record.InfluencedProposal || record.MutatedState {
		t.Fatalf("committed HiGHS evidence crossed authority boundary: candidate=%#v calibration=%#v", candidate, record)
	}
	if record.Reference.Request.Recommendation.Snapshot.Source != "candidate-verifier" || record.Reference.Request.Selection.PreferredBackend != NativeBackendName || record.Candidate.Request.Selection.PreferredBackend != HiGHSBackendName || len(record.Candidate.Request.Selection.AuthorizedFallbacks) != 0 {
		t.Fatalf("committed HiGHS shadow selection/input = %#v / %#v", record.Reference.Request, record.Candidate.Request)
	}
	if record.Candidate.Receipt.SelectedBackend == nil || record.Candidate.Receipt.SelectedBackend.ExecutableDigest != "sha256:"+candidate.Candidate.BinarySHA256 || record.Candidate.Receipt.Termination != SolverOptimal {
		t.Fatalf("committed HiGHS candidate binding = %#v", record.Candidate.Receipt)
	}
	if record.Feasibility != SolverFeasibilityAgreeFeasible || record.Metrics.CandidateMinusReferenceGap != "0" || record.Metrics.CandidateMinusReferenceMakespan != "0" || record.Metrics.AssignmentDisagreementCount == nil || *record.Metrics.AssignmentDisagreementCount != 2 {
		t.Fatalf("committed HiGHS comparison = %#v / %#v", record.Feasibility, record.Metrics)
	}
	if record.Metrics.ReferenceLatencyNanoseconds <= 0 || record.Metrics.CandidateLatencyNanoseconds <= record.Metrics.ReferenceLatencyNanoseconds || record.RealizedOutcome.ValueError != SolverShadowRealizedUnknown {
		t.Fatalf("committed HiGHS measured/unknown outcomes = %#v / %#v", record.Metrics, record.RealizedOutcome)
	}
}

func TestSolverShadowCalibrationIsReplayableImmutableAndPersisted(t *testing.T) {
	referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt := solverCalibrationFixture(t, SolverBackendOutcome{})
	record, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	if record.Mode != "SHADOW" || record.InfluencedProposal || record.MutatedState {
		t.Fatalf("shadow authority flags = %#v", record)
	}
	if record.Feasibility != SolverFeasibilityAgreeFeasible || record.Metrics.AssignmentDisagreementCount == nil || *record.Metrics.AssignmentDisagreementCount != 0 {
		t.Fatalf("comparison = %#v / %#v", record.Feasibility, record.Metrics)
	}
	if record.Metrics.ReferenceLatencyNanoseconds != int64(2*time.Millisecond) || record.Metrics.CandidateLatencyNanoseconds != int64(7*time.Millisecond) {
		t.Fatalf("latencies = %#v", record.Metrics)
	}
	if record.RealizedOutcome.ScheduleError != SolverShadowRealizedUnknown || len(record.Unknowns) != 3 {
		t.Fatalf("realized outcome = %#v / %#v", record.RealizedOutcome, record.Unknowns)
	}
	if err := VerifySolverShadowCalibration(record); err != nil {
		t.Fatalf("verify calibration: %v", err)
	}

	// The record owns its request/receipt bytes rather than aliasing mutable
	// caller input.
	referenceRequest.Recommendation.Problem.Items[0].ID = "mutated-after-construction"
	referenceReceipt.AttemptedBackends[0] = "mutated-after-construction"
	if err := VerifySolverShadowCalibration(record); err != nil {
		t.Fatalf("caller mutation changed calibration: %v", err)
	}

	store := FileSolverCalibrationStore{Directory: t.TempDir()}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, record) {
		t.Fatalf("stored calibration changed: %#v", got)
	}
	if _, err := store.Get("../../unsafe"); err == nil {
		t.Fatal("accepted unsafe calibration id")
	}
}

func TestSolverShadowCalibrationPreservesDisagreementAndOperationalUnknown(t *testing.T) {
	t.Run("semantic disagreement", func(t *testing.T) {
		referenceRequest, referenceReceipt, candidateRequest, _, comparedAt := solverCalibrationFixture(t, SolverBackendOutcome{})
		candidateReceipt, err := RunSolverRequest(context.Background(), candidateRequest, []SolverBackend{fixtureSolverBackend{outcome: SolverBackendOutcome{
			Termination: SolverInfeasible, Explanation: "candidate classified the bounded model as infeasible",
		}}}, sequenceClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
		if err != nil {
			t.Fatal(err)
		}
		record, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
		if err != nil {
			t.Fatal(err)
		}
		if record.Feasibility != SolverFeasibilityDisagree || record.Metrics.AssignmentDisagreementCount != nil {
			t.Fatalf("semantic disagreement = %#v", record)
		}
	})

	t.Run("operational unknown", func(t *testing.T) {
		referenceRequest, referenceReceipt, candidateRequest, _, comparedAt := solverCalibrationFixture(t, SolverBackendOutcome{})
		candidateReceipt, err := RunSolverRequest(context.Background(), candidateRequest, nil, sequenceClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
		if err != nil {
			t.Fatal(err)
		}
		record, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
		if err != nil {
			t.Fatal(err)
		}
		if record.Feasibility != SolverFeasibilityUnknown || !record.Metrics.CandidateHadOperationalFailure || record.Metrics.AssignmentDisagreementCount != nil {
			t.Fatalf("operational unknown = %#v", record)
		}
	})
}

func TestSolverShadowCalibrationFailsClosedOnAuthorityInputAndMetricTampering(t *testing.T) {
	referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt := solverCalibrationFixture(t, SolverBackendOutcome{})
	record, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*SolverShadowCalibration)
	}{
		{"proposal influence", func(value *SolverShadowCalibration) { value.InfluencedProposal = true }},
		{"state mutation", func(value *SolverShadowCalibration) { value.MutatedState = true }},
		{"mode", func(value *SolverShadowCalibration) { value.Mode = "ACTIVE" }},
		{"latency", func(value *SolverShadowCalibration) { value.Metrics.CandidateLatencyNanoseconds++ }},
		{"feasibility", func(value *SolverShadowCalibration) { value.Feasibility = SolverFeasibilityDisagree }},
		{"realized claim", func(value *SolverShadowCalibration) { value.RealizedOutcome.CostError = "KNOWN" }},
		{"unknown removal", func(value *SolverShadowCalibration) { value.Unknowns = value.Unknowns[:2] }},
		{"foreign receipt", func(value *SolverShadowCalibration) { value.Candidate.Receipt.RequestID = value.Reference.Request.ID }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := cloneSolverJSON(t, record)
			mutation.mutate(&candidate)
			candidate.ID, _ = solverShadowCalibrationID(candidate)
			if err := VerifySolverShadowCalibration(candidate); err == nil {
				t.Fatal("accepted tampered calibration")
			}
		})
	}

	fallbackRequest := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture", AuthorizedFallbacks: []string{NativeBackendName}})
	fallbackReceipt, err := RunSolverRequest(context.Background(), fallbackRequest, []SolverBackend{fixtureSolverBackend{outcome: SolverBackendOutcome{Termination: SolverFeasible, Result: candidateReceipt.Result}}}, sequenceClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, fallbackRequest, fallbackReceipt, comparedAt); err == nil || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("fallback ambiguity error = %v", err)
	}

	changed := testRecommendationRequest()
	changed.Snapshot.Revision = "foreign-revision"
	foreignRequest, err := NewSolverRequest(changed, SolverSelection{PreferredBackend: "fixture"}, SolverDeterminism{Workers: 1}, referenceRequest.Limits)
	if err != nil {
		t.Fatal(err)
	}
	foreignResult, err := Solve(foreignRequest.Recommendation.Problem)
	if err != nil {
		t.Fatal(err)
	}
	foreignReceipt, err := RunSolverRequest(context.Background(), foreignRequest, []SolverBackend{fixtureSolverBackend{outcome: SolverBackendOutcome{Termination: SolverFeasible, Result: &foreignResult}}}, sequenceClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, foreignRequest, foreignReceipt, comparedAt); err == nil || !strings.Contains(err.Error(), "same non-selection input") {
		t.Fatalf("foreign input error = %v", err)
	}
	if _, err := NewSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt.Add(-time.Hour)); err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("early comparison error = %v", err)
	}
}

func solverCalibrationFixture(t *testing.T, candidateOutcome SolverBackendOutcome) (SolverRequest, SolverReceipt, SolverRequest, SolverReceipt, time.Time) {
	t.Helper()
	referenceRequest := testSolverRequest(t, SolverSelection{PreferredBackend: NativeBackendName})
	candidateRequest := testSolverRequest(t, SolverSelection{PreferredBackend: "fixture"})
	started := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	referenceReceipt, err := RunSolverRequest(context.Background(), referenceRequest, []SolverBackend{NativeSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceClock(started, started.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if candidateOutcome.Termination == "" {
		result, solveErr := Solve(candidateRequest.Recommendation.Problem)
		if solveErr != nil {
			t.Fatal(solveErr)
		}
		candidateOutcome = SolverBackendOutcome{Termination: SolverFeasible, Result: &result}
	}
	candidateReceipt, err := RunSolverRequest(context.Background(), candidateRequest, []SolverBackend{fixtureSolverBackend{outcome: candidateOutcome}}, sequenceClock(started.Add(time.Millisecond), started.Add(8*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	return referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, started.Add(10 * time.Millisecond)
}
