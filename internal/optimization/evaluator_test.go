package optimization

import (
	"path/filepath"
	"reflect"
	"testing"
)

const evaluatorPromptHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const evaluatorSnapshotID = "snap_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func testEvaluatorSnapshot() EvaluatorSnapshotReceipt {
	return EvaluatorSnapshotReceipt{APIVersion: "optimization.platform.example/evaluator-snapshot/v1alpha1", Kind: "EvaluatorSnapshot", Repository: "owner/repo", Path: "platform", Commit: "1111111111111111111111111111111111111111", Tree: "2222222222222222222222222222222222222222", ArchiveSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", SnapshotID: evaluatorSnapshotID, SecretScanRevision: "secret-patterns-r1"}
}

func testEvaluatorRun(t *testing.T, family, model, started, finished string, cost int64, findings []EvaluatorFinding) EvaluatorRun {
	t.Helper()
	run, err := NewEvaluatorRun(EvaluatorRun{
		Provider: "cursor", ModelFamily: family, Model: model, ModelVersion: "2026-08-08",
		PromptSHA256: evaluatorPromptHash, InputSnapshotID: evaluatorSnapshotID, Snapshot: testEvaluatorSnapshot(), ReadOnly: true,
		SecretScanRevision: "secret-patterns-r1", StartedAt: started, FinishedAt: finished,
		InvocationID: family + "-request", SessionID: family + "-session", ReceiptSHA256: evaluatorPromptHash,
		Usage: EvaluatorUsage{InputTokens: 100, OutputTokens: 10}, InputAccessStatus: EvaluatorAccessComplete, CostStatus: EvaluatorCostMeasured,
		CostMicros: cost, CostCurrency: "USD", PriceSource: "cursor-usage", PriceRevision: "2026-08-08",
		Findings: findings,
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func TestEvaluatorRunRepresentsUnavailableCostWithoutClaimingFree(t *testing.T) {
	run := testEvaluatorRun(t, "kimi", "kimi-k3", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, nil)
	run.ID, run.CostStatus, run.CostCurrency = "", EvaluatorCostUnavailable, ""
	run.CostUnavailableReason = "personal integration key cannot read the team billing API"
	run.PriceSource, run.PriceRevision = "cursor-admin-api", "HTTP-401@2026-08-08"
	unknown, err := NewEvaluatorRun(run)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*EvaluatorRun)
	}{
		{"unavailable without reason", func(candidate *EvaluatorRun) { candidate.CostUnavailableReason = "" }},
		{"unavailable with currency", func(candidate *EvaluatorRun) { candidate.CostCurrency = "USD" }},
		{"missing invocation", func(candidate *EvaluatorRun) { candidate.InvocationID = "" }},
		{"missing receipt digest", func(candidate *EvaluatorRun) { candidate.ReceiptSHA256 = "" }},
		{"empty usage", func(candidate *EvaluatorRun) { candidate.Usage = EvaluatorUsage{} }},
		{"negative usage", func(candidate *EvaluatorRun) { candidate.Usage.InputTokens = -1 }},
		{"missing access status", func(candidate *EvaluatorRun) { candidate.InputAccessStatus = "" }},
		{"incomplete access without unknown", func(candidate *EvaluatorRun) { candidate.InputAccessStatus = EvaluatorAccessIncomplete }},
		{"snapshot digest mismatch", func(candidate *EvaluatorRun) { candidate.Snapshot.ArchiveSHA256 = evaluatorPromptHash }},
		{"snapshot id mismatch", func(candidate *EvaluatorRun) {
			candidate.InputSnapshotID = "snap_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}},
		{"snapshot scan mismatch", func(candidate *EvaluatorRun) { candidate.Snapshot.SecretScanRevision = "other" }},
		{"snapshot secret match", func(candidate *EvaluatorRun) { candidate.Snapshot.SecretMatches = 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := run
			test.mutate(&candidate)
			if _, err := NewEvaluatorRun(candidate); err == nil {
				t.Fatalf("accepted invalid run: %#v", candidate)
			}
		})
	}
	overflowing := run
	overflowing.ID = ""
	overflowing.Usage = EvaluatorUsage{InputTokens: 1 << 62, OutputTokens: 1 << 62, CacheReadTokens: 1 << 62, CacheWriteTokens: 1 << 62}
	if _, err := NewEvaluatorRun(overflowing); err != nil {
		t.Fatalf("valid non-empty counters were added with overflow: %v", err)
	}

	measured := testEvaluatorRun(t, "grok", "grok-4.5", "2026-08-08T12:00:00Z", "2026-08-08T12:00:02Z", 25, nil)
	batch, err := NewEvaluatorBatch([]EvaluatorRun{unknown, measured}, nil)
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := MeasureEvaluatorBatch(batch)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range metrics {
		if metric.ModelFamily == "kimi" && (metric.CostStatus != EvaluatorCostUnavailable || metric.CostUnavailableReason == "" || metric.CostCurrency != "") {
			t.Fatalf("unknown cost lost in metric: %#v", metric)
		}
	}
	incomplete := run
	incomplete.ID, incomplete.InputAccessStatus = "", EvaluatorAccessIncomplete
	incomplete.ResidualUnknowns = []string{"snapshot could not be read"}
	incomplete, err = NewEvaluatorRun(incomplete)
	if err != nil {
		t.Fatal(err)
	}
	incompleteBatch, err := NewEvaluatorBatch([]EvaluatorRun{incomplete, measured}, nil)
	if err != nil {
		t.Fatal(err)
	}
	incompleteMetrics, err := MeasureEvaluatorBatch(incompleteBatch)
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range incompleteMetrics {
		if metric.ModelFamily == "kimi" && (metric.RecallStatus != EvaluatorMetricUnavailable || metric.RecallAgainstBatchTruth != "" || metric.RecallUnavailableReason == "") {
			t.Fatalf("incomplete access was reported as measured recall: %#v", metric)
		}
	}
}

func testFinding(id, cluster, confidence string) EvaluatorFinding {
	return EvaluatorFinding{ID: id, ClusterKey: cluster, Category: "correctness", Target: "internal/optimization", Summary: id, Confidence: confidence, Evidence: []string{"go test ./internal/optimization"}, Hypothesis: true}
}

func TestEvaluatorBatchClustersDisagreementAndMeasuresCalibration(t *testing.T) {
	grok := testEvaluatorRun(t, "grok", "grok-4.5", "2026-08-08T12:00:00Z", "2026-08-08T12:00:02Z", 1500, []EvaluatorFinding{testFinding("grok-shared", "grok-wording", "9/10"), testFinding("grok-false", "grok-only", "4/5")})
	kimi := testEvaluatorRun(t, "kimi", "kimi-k3", "2026-08-08T12:00:00Z", "2026-08-08T12:00:03Z", 1000, []EvaluatorFinding{testFinding("kimi-shared", "kimi-wording", "3/5"), testFinding("kimi-unique", "kimi-only", "7/10")})
	gemini := testEvaluatorRun(t, "gemini", "gemini-3.1-pro", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 500, []EvaluatorFinding{testFinding("gemini-false", "gemini-only", "1/2")})
	disposition := func(id string, status FindingDispositionStatus, gate string) FindingDisposition {
		cluster := "canonical-" + id
		if id == "grok-shared" || id == "kimi-shared" {
			cluster = "shared-defect"
		}
		return FindingDisposition{FindingID: id, CanonicalClusterKey: cluster, Status: status, Verifier: "independent-go-gate", Source: "repository", SourceRevision: "commit-1", ExecutableGate: gate, VerifiedAt: "2026-08-08T12:05:00Z", Notes: "reproduced against quiescent tree"}
	}
	batch, err := NewEvaluatorBatch([]EvaluatorRun{grok, kimi, gemini}, []FindingDisposition{
		disposition("grok-shared", DispositionVerifiedDefect, "go test ./internal/optimization -run TestShared"),
		disposition("grok-false", DispositionFalsePositive, ""),
		disposition("kimi-shared", DispositionVerifiedDefect, "go test ./internal/optimization -run TestShared"),
		disposition("kimi-unique", DispositionVerifiedDefect, "go test ./internal/optimization -run TestKimiUnique"),
		disposition("gemini-false", DispositionUnresolved, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Clusters) != 4 || len(batch.Clusters[3].ConfidenceValues) != 2 {
		t.Fatalf("clusters = %#v", batch.Clusters)
	}
	metrics, err := MeasureEvaluatorBatch(batch)
	if err != nil {
		t.Fatal(err)
	}
	byFamily := map[string]EvaluatorMetric{}
	for _, metric := range metrics {
		byFamily[metric.ModelFamily] = metric
	}
	if byFamily["grok"].Precision != "1/2" || byFamily["grok"].FalseDiscoveryRate != "1/2" || byFamily["grok"].VerifiedDefects != 1 || byFamily["grok"].VerifiedClusters != 1 || byFamily["grok"].RecallAgainstBatchTruth != "1/2" {
		t.Fatalf("grok metrics = %#v", byFamily["grok"])
	}
	if byFamily["kimi"].Precision != "1" || byFamily["kimi"].VerifiedDefects != 2 || byFamily["kimi"].VerifiedClusters != 2 || byFamily["kimi"].UniqueVerifiedDefects != 1 || byFamily["kimi"].RecallAgainstBatchTruth != "1" {
		t.Fatalf("kimi metrics = %#v", byFamily["kimi"])
	}
	if byFamily["gemini"].Precision != "" || byFamily["gemini"].LatencyNanoseconds != 1_000_000_000 {
		t.Fatalf("gemini metrics = %#v", byFamily["gemini"])
	}

	store := FileEvaluatorStore{Directory: filepath.Join(t.TempDir(), "durable")}
	for _, run := range batch.Runs {
		if err := store.SaveRun(run); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveBatch(batch); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetBatch(batch.ID)
	if err != nil || loaded.ID != batch.ID {
		t.Fatalf("load batch = %#v, %v", loaded, err)
	}
	tampered := batch
	tampered.Clusters = append([]EvaluatorCluster(nil), batch.Clusters...)
	tampered.Clusters[0].ConfidenceValues = []string{"1"}
	if _, err := MeasureEvaluatorBatch(tampered); err == nil {
		t.Fatal("accepted tampered derived cluster evidence")
	}
}

func TestEvaluatorBatchRequiresSameReadOnlySanitizedContract(t *testing.T) {
	valid := testEvaluatorRun(t, "grok", "grok-4.5", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("one", "one", "1/2")})
	changed := valid
	changed.ModelFamily, changed.Model, changed.ID = "kimi", "kimi-k3", ""
	changed.PromptSHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	changed, err := NewEvaluatorRun(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewEvaluatorBatch([]EvaluatorRun{valid, changed}, nil); err == nil {
		t.Fatal("expected prompt mismatch to fail closed")
	}
	unsafe := valid
	unsafe.ReadOnly, unsafe.ID = false, ""
	if _, err := NewEvaluatorRun(unsafe); err == nil {
		t.Fatal("expected mutation-capable evaluator to be rejected")
	}
	notSanitized := valid
	notSanitized.SecretScanRevision, notSanitized.ID = "", ""
	if _, err := NewEvaluatorRun(notSanitized); err == nil {
		t.Fatal("expected missing secret scan contract to be rejected")
	}
}

func TestVerifiedEvaluatorFindingRequiresExecutableGroundTruth(t *testing.T) {
	one := testEvaluatorRun(t, "grok", "grok-4.5", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("one", "one", "1/2")})
	two := testEvaluatorRun(t, "kimi", "kimi-k3", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("two", "two", "1/2")})
	_, err := NewEvaluatorBatch([]EvaluatorRun{one, two}, []FindingDisposition{{FindingID: "one", CanonicalClusterKey: "one", Status: DispositionVerifiedDefect, Verifier: "human", Source: "repo", SourceRevision: "r1", VerifiedAt: "2026-08-08T12:05:00Z", Notes: "looks right"}})
	if err == nil {
		t.Fatal("expected unexecuted verified claim to fail closed")
	}
}

func TestEvaluatorResolutionOnlyAmendsUnresolvedFindings(t *testing.T) {
	one := testEvaluatorRun(t, "grok", "grok-4.5", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("one", "one", "1/2")})
	two := testEvaluatorRun(t, "kimi", "kimi-k3", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("two", "two", "1/2")})
	disposition := func(id string, status FindingDispositionStatus, gate string) FindingDisposition {
		return FindingDisposition{FindingID: id, CanonicalClusterKey: id, Status: status, Verifier: "go-test", Source: "repo", SourceRevision: "r1", ExecutableGate: gate, VerifiedAt: "2026-08-08T12:05:00Z", Notes: "independently reproduced"}
	}
	batch, err := NewEvaluatorBatch([]EvaluatorRun{one, two}, []FindingDisposition{
		disposition("one", DispositionVerifiedDefect, "go test ./internal/optimization -run TestOne"),
		disposition("two", DispositionUnresolved, ""),
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := NewEvaluatorResolution(batch, []FindingDisposition{disposition("two", DispositionVerifiedDefect, "go test ./internal/optimization -run TestTwo")})
	if err != nil {
		t.Fatal(err)
	}
	store := FileEvaluatorStore{Directory: filepath.Join(t.TempDir(), "durable")}
	if err := store.SaveResolution(batch, resolution); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetResolution(batch, resolution.ID)
	if err != nil || !reflect.DeepEqual(loaded, resolution) {
		t.Fatalf("loaded resolution = %#v, %v", loaded, err)
	}
	if _, err := NewEvaluatorResolution(batch, []FindingDisposition{disposition("one", DispositionFalsePositive, "")}); err == nil {
		t.Fatal("accepted an amendment to an already resolved finding")
	}
	tampered := resolution
	tampered.Dispositions = append([]FindingDisposition(nil), resolution.Dispositions...)
	tampered.Dispositions[0].Notes = "changed"
	if err := VerifyEvaluatorResolution(batch, tampered); err == nil {
		t.Fatal("accepted a tampered resolution")
	}
}

func TestEvaluatorResolutionAmendsOmittedUnknownAndRemeasures(t *testing.T) {
	one := testEvaluatorRun(t, "same", "model-a", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("one", "shared", "1/2")})
	two := testEvaluatorRun(t, "same", "model-b", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("two", "shared", "1/2")})
	batch, err := NewEvaluatorBatch([]EvaluatorRun{one, two}, nil)
	if err != nil {
		t.Fatal(err)
	}
	disposition := func(id string) FindingDisposition {
		return FindingDisposition{FindingID: id, CanonicalClusterKey: "shared", Status: DispositionVerifiedDefect, Verifier: "go-test", Source: "repo", SourceRevision: "r2", ExecutableGate: "go test ./internal/optimization -run TestShared", VerifiedAt: "2026-08-08T12:05:00Z", Notes: "reproduced"}
	}
	resolution, err := NewEvaluatorResolution(batch, []FindingDisposition{disposition("one"), disposition("two")})
	if err != nil {
		t.Fatal(err)
	}
	metrics, err := MeasureEvaluatorBatchAfterResolutions(batch, []EvaluatorResolution{resolution})
	if err != nil {
		t.Fatal(err)
	}
	for _, metric := range metrics {
		if metric.VerifiedDefects != 1 || metric.VerifiedClusters != 1 || metric.UniqueVerifiedDefects != 0 {
			t.Fatalf("resolved metric = %#v", metric)
		}
	}
}

func TestEvaluatorResolutionRejectsConflictingTerminalDispositions(t *testing.T) {
	run := testEvaluatorRun(t, "grok", "grok-4.5", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("one", "one", "1/2")})
	peer := testEvaluatorRun(t, "kimi", "kimi-k3", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, nil)
	batch, err := NewEvaluatorBatch([]EvaluatorRun{run, peer}, nil)
	if err != nil {
		t.Fatal(err)
	}
	disposition := func(status FindingDispositionStatus, gate string) FindingDisposition {
		return FindingDisposition{FindingID: "one", CanonicalClusterKey: "one", Status: status, Verifier: "go-test", Source: "repo", SourceRevision: "r3", ExecutableGate: gate, VerifiedAt: "2026-08-08T12:05:00Z", Notes: "independently reproduced"}
	}
	verified, err := NewEvaluatorResolution(batch, []FindingDisposition{disposition(DispositionVerifiedDefect, "go test ./internal/optimization -run TestOne")})
	if err != nil {
		t.Fatal(err)
	}
	falsePositive, err := NewEvaluatorResolution(batch, []FindingDisposition{disposition(DispositionFalsePositive, "")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := MeasureEvaluatorBatchAfterResolutions(batch, []EvaluatorResolution{verified, falsePositive}); err == nil {
		t.Fatal("accepted conflicting terminal resolutions for one finding")
	}
}

func TestListEvaluatorResolutionsScopesRecordsToRequestedBatch(t *testing.T) {
	store := FileEvaluatorStore{Directory: filepath.Join(t.TempDir(), "durable")}
	makeBatchAndResolution := func(family, findingID string) (EvaluatorBatch, EvaluatorResolution) {
		t.Helper()
		run := testEvaluatorRun(t, family, family+"-model", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding(findingID, findingID, "1/2")})
		peer := testEvaluatorRun(t, family+"-peer", family+"-peer-model", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, nil)
		batch, err := NewEvaluatorBatch([]EvaluatorRun{run, peer}, nil)
		if err != nil {
			t.Fatal(err)
		}
		resolution, err := NewEvaluatorResolution(batch, []FindingDisposition{{FindingID: findingID, CanonicalClusterKey: findingID, Status: DispositionFalsePositive, Verifier: "go-test", Source: "repo", SourceRevision: "r4", VerifiedAt: "2026-08-08T12:05:00Z", Notes: "independently reproduced"}})
		if err != nil {
			t.Fatal(err)
		}
		return batch, resolution
	}
	batchA, resolutionA := makeBatchAndResolution("grok", "grok-one")
	batchB, resolutionB := makeBatchAndResolution("kimi", "kimi-one")
	if err := store.SaveResolution(batchA, resolutionA); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveResolution(batchB, resolutionB); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListResolutions(batchA)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != resolutionA.ID {
		t.Fatalf("batch A resolutions = %#v", got)
	}
}

func TestVerifyEvaluatorRunRejectsNonCanonicalFindingOrder(t *testing.T) {
	run := testEvaluatorRun(t, "grok", "grok", "2026-08-08T12:00:00Z", "2026-08-08T12:00:01Z", 0, []EvaluatorFinding{testFinding("a", "a", "1/2"), testFinding("b", "b", "1/2")})
	run.Findings[0], run.Findings[1] = run.Findings[1], run.Findings[0]
	id, err := evaluatorContentID("eval_", evaluatorRunIdentity(run))
	if err != nil {
		t.Fatal(err)
	}
	run.ID = id
	if err := VerifyEvaluatorRun(run); err == nil {
		t.Fatal("accepted a self-consistent non-canonical evaluator run")
	}
}
