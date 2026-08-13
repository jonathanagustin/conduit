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

func TestCommittedLocalCpSatShadowCalibrationReplaysWithoutAuthority(t *testing.T) {
	encoded, err := os.ReadFile("../../docs/evidence/or-tools-9.15-cpsat-shadow-amd64.json")
	if err != nil {
		t.Fatal(err)
	}
	var record DiscreteSolverShadowCalibration
	if err := json.Unmarshal(encoded, &record); err != nil {
		t.Fatal(err)
	}
	if err := VerifyDiscreteSolverShadowCalibration(record); err != nil {
		t.Fatalf("verify committed CP-SAT shadow calibration: %v", err)
	}
	candidateEvidence, err := os.ReadFile("../../docs/evidence/or-tools-9.15-cpsat-candidate-amd64.json")
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
		t.Fatalf("committed CP-SAT evidence crossed authority boundary: candidate=%#v calibration=%#v", candidate, record)
	}
	if record.Reference.Request.Recommendation.Snapshot.Source != "synthetic-shadow" || record.Reference.Request.Selection.PreferredBackend != NativeBackendName || record.Candidate.Request.Selection.PreferredBackend != CpSatBackendName || len(record.Candidate.Request.Selection.AuthorizedFallbacks) != 0 {
		t.Fatalf("committed CP-SAT shadow selection/input = %#v / %#v", record.Reference.Request, record.Candidate.Request)
	}
	if record.Candidate.Receipt.SelectedBackend == nil || record.Candidate.Receipt.SelectedBackend.ExecutableDigest != "sha256:"+candidate.Candidate.BinarySHA256 || record.Candidate.Receipt.Termination != SolverOptimal {
		t.Fatalf("committed CP-SAT candidate binding = %#v", record.Candidate.Receipt)
	}
	if record.Feasibility != SolverFeasibilityAgreeFeasible || record.Metrics.CandidateMinusReferenceMakespan == nil || *record.Metrics.CandidateMinusReferenceMakespan != 0 || record.Metrics.CandidateMinusReferenceCostMicros == nil || *record.Metrics.CandidateMinusReferenceCostMicros != 0 || record.Metrics.AssignmentDisagreementCount == nil || *record.Metrics.AssignmentDisagreementCount != 0 {
		t.Fatalf("committed CP-SAT comparison = %#v / %#v", record.Feasibility, record.Metrics)
	}
	if record.Metrics.ReferenceLatencyNanoseconds <= 0 || record.Metrics.CandidateLatencyNanoseconds <= record.Metrics.ReferenceLatencyNanoseconds || record.RealizedOutcome.ValueError != SolverShadowRealizedUnknown {
		t.Fatalf("committed CP-SAT measured/unknown outcomes = %#v / %#v", record.Metrics, record.RealizedOutcome)
	}
}

func TestDiscreteSolverShadowCalibrationIsReplayableImmutableAndPersisted(t *testing.T) {
	referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt := discreteSolverCalibrationFixture(t, DiscreteBackendOutcome{})
	record, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	if record.Mode != "SHADOW" || record.InfluencedProposal || record.MutatedState || !validContentID(record.ID, "dsolcal_") {
		t.Fatalf("discrete shadow identity/authority = %#v", record)
	}
	if record.Feasibility != SolverFeasibilityAgreeFeasible || record.Metrics.AssignmentDisagreementCount == nil || *record.Metrics.AssignmentDisagreementCount != 0 {
		t.Fatalf("discrete comparison = %#v / %#v", record.Feasibility, record.Metrics)
	}
	if record.Metrics.ReferenceLatencyNanoseconds != int64(2*time.Millisecond) || record.Metrics.CandidateLatencyNanoseconds != int64(7*time.Millisecond) {
		t.Fatalf("discrete latencies = %#v", record.Metrics)
	}
	assertDiscreteMetric(t, record.Metrics.ReferenceMakespanSeconds, 10, "reference makespan")
	assertDiscreteMetric(t, record.Metrics.CandidateMakespanSeconds, 10, "candidate makespan")
	assertDiscreteMetric(t, record.Metrics.CandidateMinusReferenceMakespan, 0, "makespan delta")
	assertDiscreteMetric(t, record.Metrics.ReferenceEstimatedCostMicros, 24, "reference cost")
	assertDiscreteMetric(t, record.Metrics.CandidateEstimatedCostMicros, 24, "candidate cost")
	assertDiscreteMetric(t, record.Metrics.CandidateMinusReferenceCostMicros, 0, "cost delta")
	if record.RealizedOutcome.ScheduleError != SolverShadowRealizedUnknown || record.RealizedOutcome.CostError != SolverShadowRealizedUnknown || record.RealizedOutcome.ValueError != SolverShadowRealizedUnknown || len(record.Unknowns) != 3 {
		t.Fatalf("discrete realized outcome = %#v / %#v", record.RealizedOutcome, record.Unknowns)
	}
	if err := VerifyDiscreteSolverShadowCalibration(record); err != nil {
		t.Fatalf("verify discrete calibration: %v", err)
	}

	referenceRequest.Recommendation.Problem.Tasks[0].ID = "mutated-after-construction"
	referenceReceipt.AttemptedBackends[0] = "mutated-after-construction"
	candidateRequest.Recommendation.Problem.Lanes[0].ID = "mutated-after-construction"
	candidateReceipt.Result.Assignments[0].Lane = "mutated-after-construction"
	if err := VerifyDiscreteSolverShadowCalibration(record); err != nil {
		t.Fatalf("caller mutation changed discrete calibration: %v", err)
	}

	store := FileDiscreteSolverCalibrationStore{Directory: t.TempDir()}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, record) {
		t.Fatalf("stored discrete calibration changed: %#v", got)
	}
	if _, err := store.Get("../../unsafe"); err == nil {
		t.Fatal("accepted unsafe discrete calibration id")
	}
}

func TestDiscreteSolverShadowCalibrationPreservesExactDifferencesAndOperationalUnknown(t *testing.T) {
	t.Run("exact result differences", func(t *testing.T) {
		referenceRequest, _, candidateRequest, _, comparedAt := discreteSolverCalibrationFixture(t, DiscreteBackendOutcome{})
		choices := []DiscreteChoice{
			{Task: "prepare", Lane: "lane-a", Start: 0},
			{Task: "rollout-a", Lane: "lane-b", Start: 4},
			{Task: "rollout-b", Lane: "lane-a", Start: 8},
		}
		candidateRecommendation := cloneSolverJSON(t, candidateRequest.Recommendation)
		candidateRecommendation.Problem.TopologyRules = nil
		referenceRecommendation := cloneSolverJSON(t, referenceRequest.Recommendation)
		referenceRecommendation.Problem.TopologyRules = nil
		referenceRequest, candidateRequest = discreteCalibrationRequests(t, referenceRecommendation, candidateRecommendation)
		started := comparedAt.Add(-10 * time.Millisecond)
		referenceReceipt, err := RunDiscreteSolverRequest(context.Background(), referenceRequest, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceDiscreteClock(started, started.Add(2*time.Millisecond)))
		if err != nil {
			t.Fatal(err)
		}
		candidateReceipt, err := RunDiscreteSolverRequest(context.Background(), candidateRequest, []DiscreteSolverBackend{calibrationDiscreteBackend{outcome: DiscreteBackendOutcome{Termination: SolverFeasible, Choices: choices}}}, sequenceDiscreteClock(started.Add(time.Millisecond), started.Add(8*time.Millisecond)))
		if err != nil {
			t.Fatal(err)
		}
		record, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
		if err != nil {
			t.Fatal(err)
		}
		assertDiscreteMetric(t, record.Metrics.ReferenceMakespanSeconds, 8, "reference makespan")
		assertDiscreteMetric(t, record.Metrics.CandidateMakespanSeconds, 11, "candidate makespan")
		assertDiscreteMetric(t, record.Metrics.CandidateMinusReferenceMakespan, 3, "makespan delta")
		assertDiscreteMetric(t, record.Metrics.ReferenceEstimatedCostMicros, 26, "reference cost")
		assertDiscreteMetric(t, record.Metrics.CandidateEstimatedCostMicros, 28, "candidate cost")
		assertDiscreteMetric(t, record.Metrics.CandidateMinusReferenceCostMicros, 2, "cost delta")
		if record.Metrics.AssignmentDisagreementCount == nil || *record.Metrics.AssignmentDisagreementCount != 2 {
			t.Fatalf("assignment disagreements = %#v", record.Metrics.AssignmentDisagreementCount)
		}
	})

	t.Run("semantic disagreement", func(t *testing.T) {
		referenceRequest, referenceReceipt, candidateRequest, _, comparedAt := discreteSolverCalibrationFixture(t, DiscreteBackendOutcome{})
		candidateReceipt, err := RunDiscreteSolverRequest(context.Background(), candidateRequest, []DiscreteSolverBackend{calibrationDiscreteBackend{outcome: DiscreteBackendOutcome{
			Termination:             SolverInfeasible,
			Explanation:             "candidate classified the bounded discrete model as infeasible",
			BackendReportedEvidence: &DiscreteBackendReportedEvidence{Status: "INFEASIBLE"},
		}}}, sequenceDiscreteClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
		if err != nil {
			t.Fatal(err)
		}
		record, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
		if err != nil {
			t.Fatal(err)
		}
		if record.Feasibility != SolverFeasibilityDisagree || record.Metrics.AssignmentDisagreementCount != nil || record.Metrics.CandidateMakespanSeconds != nil {
			t.Fatalf("discrete semantic disagreement = %#v", record)
		}
	})

	t.Run("operational unknown", func(t *testing.T) {
		referenceRequest, referenceReceipt, candidateRequest, _, comparedAt := discreteSolverCalibrationFixture(t, DiscreteBackendOutcome{})
		candidateReceipt, err := RunDiscreteSolverRequest(context.Background(), candidateRequest, nil, sequenceDiscreteClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
		if err != nil {
			t.Fatal(err)
		}
		record, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
		if err != nil {
			t.Fatal(err)
		}
		if record.Feasibility != SolverFeasibilityUnknown || !record.Metrics.CandidateHadOperationalFailure || record.Metrics.AssignmentDisagreementCount != nil || record.Metrics.CandidateEstimatedCostMicros != nil {
			t.Fatalf("discrete operational unknown = %#v", record)
		}
	})
}

func TestDiscreteSolverShadowCalibrationFailsClosedOnAuthorityInputAndMetricTampering(t *testing.T) {
	referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt := discreteSolverCalibrationFixture(t, DiscreteBackendOutcome{})
	record, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt)
	if err != nil {
		t.Fatal(err)
	}
	mutations := []struct {
		name   string
		mutate func(*DiscreteSolverShadowCalibration)
	}{
		{"proposal influence", func(value *DiscreteSolverShadowCalibration) { value.InfluencedProposal = true }},
		{"state mutation", func(value *DiscreteSolverShadowCalibration) { value.MutatedState = true }},
		{"mode", func(value *DiscreteSolverShadowCalibration) { value.Mode = "ACTIVE" }},
		{"input digest", func(value *DiscreteSolverShadowCalibration) { value.InputDigest = testExecutableDigest }},
		{"latency", func(value *DiscreteSolverShadowCalibration) { value.Metrics.CandidateLatencyNanoseconds++ }},
		{"reference makespan", func(value *DiscreteSolverShadowCalibration) { (*value.Metrics.ReferenceMakespanSeconds)++ }},
		{"candidate cost", func(value *DiscreteSolverShadowCalibration) { (*value.Metrics.CandidateEstimatedCostMicros)++ }},
		{"assignment disagreement", func(value *DiscreteSolverShadowCalibration) { (*value.Metrics.AssignmentDisagreementCount)++ }},
		{"feasibility", func(value *DiscreteSolverShadowCalibration) { value.Feasibility = SolverFeasibilityDisagree }},
		{"realized claim", func(value *DiscreteSolverShadowCalibration) { value.RealizedOutcome.CostError = "KNOWN" }},
		{"unknown removal", func(value *DiscreteSolverShadowCalibration) { value.Unknowns = value.Unknowns[:2] }},
		{"foreign receipt", func(value *DiscreteSolverShadowCalibration) {
			value.Candidate.Receipt.RequestID = value.Reference.Request.ID
		}},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := cloneSolverJSON(t, record)
			mutation.mutate(&candidate)
			candidate.ID, _ = discreteSolverShadowCalibrationID(candidate)
			if err := VerifyDiscreteSolverShadowCalibration(candidate); err == nil {
				t.Fatal("accepted tampered discrete calibration")
			}
		})
	}

	fallbackRequest := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "shadow-candidate", AuthorizedFallbacks: []string{NativeBackendName}})
	fallbackReceipt, err := RunDiscreteSolverRequest(context.Background(), fallbackRequest, []DiscreteSolverBackend{calibrationDiscreteBackend{outcome: DiscreteBackendOutcome{Termination: SolverFeasible, Choices: testDiscreteChoices()}}}, sequenceDiscreteClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, fallbackRequest, fallbackReceipt, comparedAt); err == nil || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("discrete fallback ambiguity error = %v", err)
	}

	changed := cloneSolverJSON(t, candidateRequest.Recommendation)
	changed.Snapshot.Revision = "foreign-revision"
	foreignRequest, err := NewDiscreteSolverRequest(changed, SolverSelection{PreferredBackend: "shadow-candidate"}, candidateRequest.Determinism, candidateRequest.Limits)
	if err != nil {
		t.Fatal(err)
	}
	foreignReceipt, err := RunDiscreteSolverRequest(context.Background(), foreignRequest, []DiscreteSolverBackend{calibrationDiscreteBackend{outcome: DiscreteBackendOutcome{Termination: SolverFeasible, Choices: testDiscreteChoices()}}}, sequenceDiscreteClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, foreignRequest, foreignReceipt, comparedAt); err == nil || !strings.Contains(err.Error(), "same non-selection input") {
		t.Fatalf("discrete foreign input error = %v", err)
	}
	if _, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, comparedAt.Add(-time.Hour)); err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("early discrete comparison error = %v", err)
	}

	nativeCandidateRequest := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: NativeBackendName})
	nativeCandidateReceipt, err := RunDiscreteSolverRequest(context.Background(), nativeCandidateRequest, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceDiscreteClock(comparedAt.Add(-8*time.Millisecond), comparedAt.Add(-time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDiscreteSolverShadowCalibration(referenceRequest, referenceReceipt, nativeCandidateRequest, nativeCandidateReceipt, comparedAt); err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("native candidate selection error = %v", err)
	}
}

func discreteSolverCalibrationFixture(t *testing.T, candidateOutcome DiscreteBackendOutcome) (DiscreteSolverRequest, DiscreteSolverReceipt, DiscreteSolverRequest, DiscreteSolverReceipt, time.Time) {
	t.Helper()
	referenceRequest := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: NativeBackendName})
	candidateRequest := testDiscreteSolverRequest(t, SolverSelection{PreferredBackend: "shadow-candidate"})
	started := time.Date(2026, 8, 12, 12, 1, 0, 0, time.UTC)
	referenceReceipt, err := RunDiscreteSolverRequest(context.Background(), referenceRequest, []DiscreteSolverBackend{NativeDiscreteSolverBackend{ExecutableDigest: testExecutableDigest, Architecture: "amd64"}}, sequenceDiscreteClock(started, started.Add(2*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	if candidateOutcome.Termination == "" {
		candidateOutcome = DiscreteBackendOutcome{Termination: SolverFeasible, Choices: testDiscreteChoices()}
	}
	candidateReceipt, err := RunDiscreteSolverRequest(context.Background(), candidateRequest, []DiscreteSolverBackend{calibrationDiscreteBackend{outcome: candidateOutcome}}, sequenceDiscreteClock(started.Add(time.Millisecond), started.Add(8*time.Millisecond)))
	if err != nil {
		t.Fatal(err)
	}
	return referenceRequest, referenceReceipt, candidateRequest, candidateReceipt, started.Add(10 * time.Millisecond)
}

func discreteCalibrationRequests(t *testing.T, reference, candidate DiscreteRecommendationRequest) (DiscreteSolverRequest, DiscreteSolverRequest) {
	t.Helper()
	referenceRequest, err := NewDiscreteSolverRequest(reference, SolverSelection{PreferredBackend: NativeBackendName}, SolverDeterminism{Workers: 1}, SolverLimits{TimeLimitMilliseconds: 5_000, MemoryLimitBytes: 256 << 20})
	if err != nil {
		t.Fatal(err)
	}
	candidateRequest, err := NewDiscreteSolverRequest(candidate, SolverSelection{PreferredBackend: "shadow-candidate"}, referenceRequest.Determinism, referenceRequest.Limits)
	if err != nil {
		t.Fatal(err)
	}
	return referenceRequest, candidateRequest
}

func assertDiscreteMetric(t *testing.T, value *int64, want int64, name string) {
	t.Helper()
	if value == nil {
		t.Fatalf("%s is absent, want %d", name, want)
	}
	if *value != want {
		t.Fatalf("%s = %d, want %d", name, *value, want)
	}
}

type calibrationDiscreteBackend struct {
	outcome DiscreteBackendOutcome
}

func (backend calibrationDiscreteBackend) Descriptor() SolverBackendDescriptor {
	return SolverBackendDescriptor{
		Name: "shadow-candidate", Version: "1.0.0", ExecutableDigest: testExecutableDigest,
		Architecture: "test", Capabilities: []string{DiscreteScheduleCapability},
	}
}

func (backend calibrationDiscreteBackend) SolveDiscrete(context.Context, DiscreteSolverRequest) (DiscreteBackendOutcome, error) {
	return backend.outcome, nil
}
