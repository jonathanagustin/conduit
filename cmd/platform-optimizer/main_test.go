package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func TestCommandProducesMeasuredSchedule(t *testing.T) {
	input := bytes.NewBufferString(`{"items":[{"id":"mutation-1","demand":8},{"id":"mutation-2","demand":7},{"id":"mutation-3","demand":6}],"resources":[{"id":"shard-1","capacity":1},{"id":"shard-2","capacity":1}]}`)
	var output bytes.Buffer
	if err := run(input, &output); err != nil {
		t.Fatal(err)
	}
	var result optimization.Result
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.RelaxedMakespan <= 0 || result.RoundedMakespan <= 0 || len(result.Assignments) != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestCommandRejectsTrailingInput(t *testing.T) {
	if err := run(bytes.NewBufferString(`{} {}`), &bytes.Buffer{}); err == nil {
		t.Fatal("accepted trailing JSON")
	}
}

func TestCompileAndAssessSubstratePreflightCommandsIncludeDiskEvidence(t *testing.T) {
	encodedObservation, err := os.ReadFile("../../internal/optimization/testdata/substrate-observation.json")
	if err != nil {
		t.Fatal(err)
	}
	var observation optimization.SubstrateObservationSnapshot
	if err := json.Unmarshal(encodedObservation, &observation); err != nil {
		t.Fatal(err)
	}
	demand := optimization.RolloutCapacityDemand{
		Strategy:            optimization.RolloutBlueGreen,
		Additional:          optimization.CapacityVector{PodSlots: 2, MilliCPU: 2000, MemoryBytes: 2_000_000_000, IPAddresses: 2, EphemeralStorageBytes: 2_000_000_000},
		DaemonSetPerNewNode: optimization.CapacityVector{PodSlots: 1, MilliCPU: 100, MemoryBytes: 100_000_000, IPAddresses: 1, EphemeralStorageBytes: 100_000_000},
		NewNodes:            1, MinimumReadyNodes: 3, ConcurrentEvictions: 1, DisruptionsAllowed: 1, AcceptanceWindowSecs: 300,
		Storage: optimization.StorageDemand{
			Images: []optimization.ImageStorageDemand{{Digest: testDigest('1'), CompressedBytes: 200_000_000, UnpackedBytes: 500_000_000, UnpackedInodes: 20_000, Copies: 1, Source: "registry/manifest", SourceRevision: "release-r7", ObservedAt: "2026-08-09T11:59:30Z", RequestDigest: testDigest('3'), EvidenceDigest: testDigest('2'), CollectorIdentity: "collector:registry", Fidelity: optimization.ObservationFidelityExact}},
			Placements: []optimization.NodeStorageDemand{{
				NodeID: "node-a", WorkloadBytes: 2_100_000_000, ImageDigests: []string{testDigest('1')},
				WritableLayerReserveBytes: 500_000_000, WritableLayerReserveInodes: 50_000,
				LogReserveBytes: 500_000_000, LogReserveInodes: 50_000,
				MinimumRemainingBytes: 5_000_000_000, MinimumRemainingInodes: 500_000,
			}},
		},
	}
	compileInput, err := json.Marshal(substratePreflightCompileRequest{Observation: observation, Demand: demand, RequireNetworkPolicyCoverage: true, RequireNetworkTelemetryCoverage: true})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runCommand([]string{"compile-substrate-preflight"}, bytes.NewReader(compileInput), &output, time.Now()); err != nil {
		t.Fatal(err)
	}
	var request optimization.SubstratePreflightRequest
	if err := json.Unmarshal(output.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	if request.Capacity.Storage.Nodes[0].FreeInodes != 1_000_000 || len(request.Capacity.Storage.Nodes) != 3 {
		t.Fatalf("storage evidence = %#v", request.Capacity.Storage)
	}
	assessmentInput, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runCommand([]string{"assess-substrate-preflight"}, bytes.NewReader(assessmentInput), &output, time.Now()); err != nil {
		t.Fatal(err)
	}
	var assessment optimization.SubstratePreflightAssessment
	if err := json.Unmarshal(output.Bytes(), &assessment); err != nil {
		t.Fatal(err)
	}
	if !assessment.Eligible || len(assessment.CapacityChecks) != 7 {
		t.Fatalf("assessment = %#v", assessment)
	}
}

func testDigest(character byte) string {
	return "sha256:" + string(bytes.Repeat([]byte{character}, 64))
}

func TestRecommendCommandProducesEvidenceEnvelope(t *testing.T) {
	input := bytes.NewBufferString(`{"apiVersion":"optimization.platform.example/v1alpha1","kind":"RecommendationRequest","snapshot":{"id":"snap_0000000000000000000000000000000000000000000000000000000000000000","source":"test","revision":"r","capturedAt":"2026-08-08T12:00:00Z"},"objective":{"primary":"minimize_makespan"},"constraints":[],"problem":{"items":[{"id":"i","demand":1}],"resources":[{"id":"r","capacity":1}]}}`)
	var output bytes.Buffer
	if err := runCommand([]string{"recommend"}, input, &output, time.Date(2026, 8, 8, 12, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var recommendation optimization.Recommendation
	if err := json.Unmarshal(output.Bytes(), &recommendation); err != nil {
		t.Fatal(err)
	}
	if recommendation.ID == "" || recommendation.Request.Snapshot.ID != "snap_0000000000000000000000000000000000000000000000000000000000000000" || len(recommendation.Result.Assignments) != 1 {
		t.Fatalf("recommendation = %#v", recommendation)
	}
}

func TestSolveAndVerifyBackendReceiptCommands(t *testing.T) {
	recommendation := optimization.RecommendationRequest{
		APIVersion: optimization.EvidenceAPIVersion, Kind: "RecommendationRequest",
		Snapshot:    optimization.Snapshot{ID: "snap_0000000000000000000000000000000000000000000000000000000000000000", Source: "test", Revision: "r1", CapturedAt: "2026-08-09T17:59:00Z"},
		Objective:   optimization.Objective{Primary: "minimize_makespan"},
		Constraints: []optimization.ConstraintEvidence{{ID: "architecture", Source: "compiler", Revision: "r1", ObservedAt: "2026-08-09T17:58:00Z"}},
		Problem: optimization.Problem{
			Items:     []optimization.WorkItem{{ID: "native", Demand: 1, Eligible: []string{runtime.GOARCH}}},
			Resources: []optimization.Resource{{ID: runtime.GOARCH, Capacity: 1}},
		},
	}
	compileInput, err := json.Marshal(solverCompileRequest{
		Recommendation: recommendation,
		Selection:      optimization.SolverSelection{PreferredBackend: optimization.NativeBackendName},
		Determinism:    optimization.SolverDeterminism{Workers: 1},
		Limits:         optimization.SolverLimits{TimeLimitMilliseconds: 1_000, MemoryLimitBytes: 64 << 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runCommand([]string{"compile-backend-request"}, bytes.NewReader(compileInput), &output, time.Now()); err != nil {
		t.Fatal(err)
	}
	var request optimization.SolverRequest
	if err := json.Unmarshal(output.Bytes(), &request); err != nil {
		t.Fatal(err)
	}
	encoded := append([]byte(nil), output.Bytes()...)
	started := time.Date(2026, 8, 9, 18, 0, 0, 0, time.UTC)
	finished := started.Add(time.Millisecond)
	times := []time.Time{started, finished}
	index := 0
	output.Reset()
	if err := runCommandWithClock([]string{"solve-backend"}, bytes.NewReader(encoded), &output, func() time.Time {
		value := times[min(index, len(times)-1)]
		index++
		return value
	}); err != nil {
		t.Fatal(err)
	}
	var receipt optimization.SolverReceipt
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.SelectedBackend == nil || receipt.SelectedBackend.Architecture != runtime.GOARCH || receipt.StartedAt == receipt.FinishedAt {
		t.Fatalf("native CLI receipt = %#v", receipt)
	}
	verification, err := json.Marshal(solverReceiptVerification{Request: request, Receipt: receipt})
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runCommand([]string{"verify-backend-receipt"}, bytes.NewReader(verification), &output, finished); err != nil {
		t.Fatal(err)
	}
	var result map[string]bool
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result["valid"] {
		t.Fatalf("verification = %v / %#v", err, result)
	}
}

func TestCommandsPersistValueAndBitemporalTwinEvidence(t *testing.T) {
	root := t.TempDir()
	valueRequest := valueRecordRequest{StoreDirectory: filepath.Join(root, "value"), SnapshotID: "snap_0000000000000000000000000000000000000000000000000000000000000000", Criteria: []optimization.ValueCriterion{{ID: "benefit", Stakeholder: "users", UserSegment: "active users", Function: "complete work", Criticality: "important", Measure: "completion", Unit: "ratio", Scale: "CARDINAL", Baseline: "1/2", Target: "3/4", Direction: optimization.ValueMaximize}}, Alternatives: []optimization.ValueAlternative{{ID: "candidate", Estimates: map[string]optimization.ValueEstimate{"benefit": {Value: "3/4", Source: "experiment", SourceRevision: "r1", AsOf: "2026-08-08T12:00:00Z", HorizonSeconds: 3600, Distribution: "confidence-95"}}, LifecycleCost: &optimization.LifecycleCost{Currency: "USD", PriceSource: "price-list", PriceSourceRevision: "r1", AsOf: "2026-08-08T12:00:00Z", HorizonSeconds: 3600}, TimeToValueSeconds: 60, DiscountRate: "0", DiscountBasis: "none", Assumptions: []string{"stable demand"}}}}
	encoded, err := json.Marshal(valueRequest)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runCommand([]string{"record-value"}, bytes.NewReader(encoded), &output, time.Date(2026, 8, 8, 12, 1, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var valueRecord optimization.ValueAssessmentRecord
	if err := json.Unmarshal(output.Bytes(), &valueRecord); err != nil {
		t.Fatal(err)
	}
	if _, err := (optimization.FileValueAssessmentStore{Directory: valueRequest.StoreDirectory}).Get(valueRecord.ID); err != nil {
		t.Fatal(err)
	}

	fact, err := optimization.NewTemporalFact(optimization.TemporalFact{EntityID: "system", Attribute: "users", Value: json.RawMessage(`42`), Unit: "count", Plane: optimization.StateExperienced, ValidFrom: "2026-08-08T12:00:00Z", RecordedAt: "2026-08-08T12:00:01Z", Source: "analytics", SourceRevision: "r1", RequestDigest: "sha256:req", ResponseDigest: "sha256:res", CollectorIdentity: "collector", Fidelity: optimization.FidelityExact})
	if err != nil {
		t.Fatal(err)
	}
	twinRequest := twinSnapshotRequest{StoreDirectory: filepath.Join(root, "twin"), ValidAt: "2026-08-08T12:00:30Z", KnownAt: "2026-08-08T12:00:30Z", Requirements: []optimization.TwinRequirement{{ID: "users", EntityID: "system", Attribute: "users", Unit: "count", Plane: optimization.StateExperienced, MinimumFidelity: optimization.FidelityExact, MaximumStalenessSeconds: 60}}, Facts: []optimization.TemporalFact{fact}}
	encoded, err = json.Marshal(twinRequest)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runCommand([]string{"record-twin-snapshot"}, bytes.NewReader(encoded), &output, time.Now()); err != nil {
		t.Fatal(err)
	}
	var snapshot optimization.TwinSnapshot
	if err := json.Unmarshal(output.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := (optimization.FileTwinStore{Directory: twinRequest.StoreDirectory}).GetSnapshot(snapshot.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRecordEvaluatorsPersistsCalibratedBatch(t *testing.T) {
	finding := func(id string) optimization.EvaluatorFinding {
		return optimization.EvaluatorFinding{ID: id, ClusterKey: "same-defect", Category: "correctness", Target: "optimizer", Summary: "boundary fails", Confidence: "3/4", Evidence: []string{"go test ./internal/optimization -run TestBoundary"}, Hypothesis: true}
	}
	run := func(family, model, findingID string) optimization.EvaluatorRun {
		return optimization.EvaluatorRun{Provider: "cursor", ModelFamily: family, Model: model, ModelVersion: "2026-08-08", PromptSHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", InputSnapshotID: "snap_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Snapshot: optimization.EvaluatorSnapshotReceipt{APIVersion: "optimization.platform.example/evaluator-snapshot/v1alpha1", Kind: "EvaluatorSnapshot", Repository: "owner/repo", Path: "platform", Commit: "1111111111111111111111111111111111111111", Tree: "2222222222222222222222222222222222222222", ArchiveSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SnapshotID: "snap_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SecretScanRevision: "r1"}, ReadOnly: true, SecretScanRevision: "r1", InvocationID: family + "-request", SessionID: family + "-session", ReceiptSHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Usage: optimization.EvaluatorUsage{InputTokens: 100, OutputTokens: 10}, InputAccessStatus: optimization.EvaluatorAccessComplete, StartedAt: "2026-08-08T12:00:00Z", FinishedAt: "2026-08-08T12:00:01Z", CostStatus: optimization.EvaluatorCostMeasured, CostMicros: 10, CostCurrency: "USD", PriceSource: "usage", PriceRevision: "r1", Findings: []optimization.EvaluatorFinding{finding(findingID)}}
	}
	request := evaluatorBatchRequest{StoreDirectory: filepath.Join(t.TempDir(), "evaluators"), Runs: []optimization.EvaluatorRun{run("grok", "grok-4.5", "grok-1"), run("kimi", "kimi-k3", "kimi-1")}, Dispositions: []optimization.FindingDisposition{{FindingID: "grok-1", CanonicalClusterKey: "same-defect", Status: optimization.DispositionVerifiedDefect, Verifier: "go-test", Source: "repo", SourceRevision: "r1", ExecutableGate: "go test ./internal/optimization -run TestBoundary", VerifiedAt: "2026-08-08T12:05:00Z", Notes: "reproduced"}, {FindingID: "kimi-1", CanonicalClusterKey: "same-defect", Status: optimization.DispositionUnresolved, Verifier: "go-test", Source: "repo", SourceRevision: "r1", VerifiedAt: "2026-08-08T12:05:00Z", Notes: "not yet reproduced"}}}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := runCommand([]string{"record-evaluators"}, bytes.NewReader(encoded), &output, time.Now()); err != nil {
		t.Fatal(err)
	}
	var result evaluatorBatchResult
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Metrics) != 2 || (result.Metrics[0].Precision != "1" && result.Metrics[1].Precision != "1") {
		t.Fatalf("evaluator result = %#v", result)
	}
	if _, err := (optimization.FileEvaluatorStore{Directory: request.StoreDirectory}).GetBatch(result.Batch.ID); err != nil {
		t.Fatal(err)
	}
	resolutionRequest := evaluatorResolutionRequest{StoreDirectory: request.StoreDirectory, BatchID: result.Batch.ID, Dispositions: []optimization.FindingDisposition{{FindingID: "kimi-1", CanonicalClusterKey: "same-defect", Status: optimization.DispositionVerifiedDefect, Verifier: "go-test", Source: "repo", SourceRevision: "r2", ExecutableGate: "go test ./internal/optimization -run TestBoundary", VerifiedAt: "2026-08-08T12:10:00Z", Notes: "reproduced later"}}}
	encoded, err = json.Marshal(resolutionRequest)
	if err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := runCommand([]string{"resolve-evaluators"}, bytes.NewReader(encoded), &output, time.Now()); err != nil {
		t.Fatal(err)
	}
	var resolved evaluatorResolutionResult
	if err := json.Unmarshal(output.Bytes(), &resolved); err != nil {
		t.Fatal(err)
	}
	if len(resolved.Metrics) != 2 || resolved.Metrics[0].VerifiedDefects+resolved.Metrics[1].VerifiedDefects != 2 {
		t.Fatalf("resolved metrics = %#v", resolved.Metrics)
	}
	if _, err := (optimization.FileEvaluatorStore{Directory: request.StoreDirectory}).GetResolution(result.Batch, resolved.Resolution.ID); err != nil {
		t.Fatal(err)
	}
}
