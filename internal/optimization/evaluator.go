package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"time"
)

const EvaluatorEvidenceAPIVersion = "optimization.platform.example/evaluator/v1alpha1"

type EvaluatorCostStatus string
type EvaluatorAccessStatus string
type EvaluatorMetricStatus string

const (
	EvaluatorCostMeasured    EvaluatorCostStatus = "MEASURED"
	EvaluatorCostUnavailable EvaluatorCostStatus = "UNAVAILABLE"
)

const (
	EvaluatorAccessComplete    EvaluatorAccessStatus = "COMPLETE"
	EvaluatorAccessIncomplete  EvaluatorAccessStatus = "INCOMPLETE"
	EvaluatorMetricMeasured    EvaluatorMetricStatus = "MEASURED"
	EvaluatorMetricUnavailable EvaluatorMetricStatus = "UNAVAILABLE"
)

type EvaluatorUsage struct {
	InputTokens      int64 `json:"inputTokens"`
	OutputTokens     int64 `json:"outputTokens"`
	CacheReadTokens  int64 `json:"cacheReadTokens"`
	CacheWriteTokens int64 `json:"cacheWriteTokens"`
}

type EvaluatorSnapshotReceipt struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	Repository         string `json:"repository"`
	Path               string `json:"path"`
	Commit             string `json:"commit"`
	Tree               string `json:"tree"`
	ArchiveSHA256      string `json:"archiveSha256"`
	SnapshotID         string `json:"snapshotId"`
	SecretScanRevision string `json:"secretScanRevision"`
	SecretMatches      int64  `json:"secretMatches"`
}

type EvaluatorFinding struct {
	ID         string   `json:"id"`
	ClusterKey string   `json:"clusterKey"`
	Category   string   `json:"category"`
	Target     string   `json:"target"`
	Summary    string   `json:"summary"`
	Confidence string   `json:"confidence"`
	Evidence   []string `json:"evidence"`
	Hypothesis bool     `json:"hypothesis"`
}

type EvaluatorRun struct {
	APIVersion            string                   `json:"apiVersion"`
	Kind                  string                   `json:"kind"`
	ID                    string                   `json:"id"`
	Provider              string                   `json:"provider"`
	ModelFamily           string                   `json:"modelFamily"`
	Model                 string                   `json:"model"`
	ModelVersion          string                   `json:"modelVersion"`
	PromptSHA256          string                   `json:"promptSha256"`
	InputSnapshotID       string                   `json:"inputSnapshotId"`
	Snapshot              EvaluatorSnapshotReceipt `json:"snapshot"`
	ReadOnly              bool                     `json:"readOnly"`
	SecretScanRevision    string                   `json:"secretScanRevision"`
	InvocationID          string                   `json:"invocationId"`
	SessionID             string                   `json:"sessionId"`
	ReceiptSHA256         string                   `json:"receiptSha256"`
	Usage                 EvaluatorUsage           `json:"usage"`
	InputAccessStatus     EvaluatorAccessStatus    `json:"inputAccessStatus"`
	ResidualUnknowns      []string                 `json:"residualUnknowns"`
	StartedAt             string                   `json:"startedAt"`
	FinishedAt            string                   `json:"finishedAt"`
	CostStatus            EvaluatorCostStatus      `json:"costStatus"`
	CostMicros            int64                    `json:"costMicros"`
	CostCurrency          string                   `json:"costCurrency,omitempty"`
	PriceSource           string                   `json:"priceSource"`
	PriceRevision         string                   `json:"priceRevision"`
	CostUnavailableReason string                   `json:"costUnavailableReason,omitempty"`
	Findings              []EvaluatorFinding       `json:"findings"`
}

type FindingDispositionStatus string

const (
	DispositionVerifiedDefect FindingDispositionStatus = "VERIFIED_DEFECT"
	DispositionFalsePositive  FindingDispositionStatus = "FALSE_POSITIVE"
	DispositionUnresolved     FindingDispositionStatus = "UNRESOLVED"
)

type FindingDisposition struct {
	FindingID           string                   `json:"findingId"`
	CanonicalClusterKey string                   `json:"canonicalClusterKey"`
	Status              FindingDispositionStatus `json:"status"`
	Verifier            string                   `json:"verifier"`
	Source              string                   `json:"source"`
	SourceRevision      string                   `json:"sourceRevision"`
	ExecutableGate      string                   `json:"executableGate,omitempty"`
	VerifiedAt          string                   `json:"verifiedAt"`
	Notes               string                   `json:"notes"`
}

type EvaluatorCluster struct {
	Key              string   `json:"key"`
	FindingIDs       []string `json:"findingIds"`
	RunIDs           []string `json:"runIds"`
	ModelFamilies    []string `json:"modelFamilies"`
	ConfidenceValues []string `json:"confidenceValues"`
}

type EvaluatorBatch struct {
	APIVersion      string               `json:"apiVersion"`
	Kind            string               `json:"kind"`
	ID              string               `json:"id"`
	PromptSHA256    string               `json:"promptSha256"`
	InputSnapshotID string               `json:"inputSnapshotId"`
	Runs            []EvaluatorRun       `json:"runs"`
	Dispositions    []FindingDisposition `json:"dispositions"`
	Clusters        []EvaluatorCluster   `json:"clusters"`
}

type EvaluatorResolution struct {
	APIVersion   string               `json:"apiVersion"`
	Kind         string               `json:"kind"`
	ID           string               `json:"id"`
	BatchID      string               `json:"batchId"`
	Dispositions []FindingDisposition `json:"dispositions"`
}

type EvaluatorMetric struct {
	RunID                   string                `json:"runId"`
	Provider                string                `json:"provider"`
	ModelFamily             string                `json:"modelFamily"`
	Model                   string                `json:"model"`
	LatencyNanoseconds      int64                 `json:"latencyNanoseconds"`
	Usage                   EvaluatorUsage        `json:"usage"`
	CostStatus              EvaluatorCostStatus   `json:"costStatus"`
	CostMicros              int64                 `json:"costMicros"`
	CostCurrency            string                `json:"costCurrency,omitempty"`
	CostUnavailableReason   string                `json:"costUnavailableReason,omitempty"`
	DisposedFindings        int64                 `json:"disposedFindings"`
	VerifiedDefects         int64                 `json:"verifiedDefects"`
	VerifiedClusters        int64                 `json:"verifiedClusters"`
	FalsePositives          int64                 `json:"falsePositives"`
	UniqueVerifiedDefects   int64                 `json:"uniqueVerifiedDefects"`
	Precision               string                `json:"precision,omitempty"`
	FalseDiscoveryRate      string                `json:"falseDiscoveryRate,omitempty"`
	RecallAgainstBatchTruth string                `json:"recallAgainstBatchTruth,omitempty"`
	RecallStatus            EvaluatorMetricStatus `json:"recallStatus"`
	RecallUnavailableReason string                `json:"recallUnavailableReason,omitempty"`
}

func NewEvaluatorRun(run EvaluatorRun) (EvaluatorRun, error) {
	run.APIVersion, run.Kind, run.ID = EvaluatorEvidenceAPIVersion, "EvaluatorRun", ""
	run.Findings = append([]EvaluatorFinding(nil), run.Findings...)
	run.ResidualUnknowns = append([]string(nil), run.ResidualUnknowns...)
	sort.Strings(run.ResidualUnknowns)
	for i := range run.Findings {
		run.Findings[i].Evidence = append([]string(nil), run.Findings[i].Evidence...)
		sort.Strings(run.Findings[i].Evidence)
	}
	sort.Slice(run.Findings, func(i, j int) bool { return run.Findings[i].ID < run.Findings[j].ID })
	if err := validateEvaluatorRun(run); err != nil {
		return EvaluatorRun{}, err
	}
	id, err := evaluatorContentID("eval_", evaluatorRunIdentity(run))
	if err != nil {
		return EvaluatorRun{}, err
	}
	run.ID = id
	return run, nil
}

func VerifyEvaluatorRun(run EvaluatorRun) error {
	if run.APIVersion != EvaluatorEvidenceAPIVersion || run.Kind != "EvaluatorRun" || !validContentID(run.ID, "eval_") {
		return errors.New("unsupported or incomplete evaluator run")
	}
	replayed, err := NewEvaluatorRun(run)
	if err != nil || !reflect.DeepEqual(replayed, run) {
		return errors.New("evaluator run does not match canonical replay")
	}
	return nil
}

func validateEvaluatorRun(run EvaluatorRun) error {
	if run.Provider == "" || run.ModelFamily == "" || run.Model == "" || run.ModelVersion == "" || run.SecretScanRevision == "" {
		return errors.New("evaluator identity, version, and secret-scan revision are required")
	}
	if run.InvocationID == "" || run.SessionID == "" || !validSHA256(run.ReceiptSHA256) {
		return errors.New("evaluator run requires invocation, session, and receipt identity")
	}
	if run.Usage.InputTokens < 0 || run.Usage.OutputTokens < 0 || run.Usage.CacheReadTokens < 0 || run.Usage.CacheWriteTokens < 0 ||
		(run.Usage.InputTokens == 0 && run.Usage.OutputTokens == 0 && run.Usage.CacheReadTokens == 0 && run.Usage.CacheWriteTokens == 0) {
		return errors.New("evaluator run requires non-negative, non-empty measured token usage")
	}
	if run.InputAccessStatus != EvaluatorAccessComplete && run.InputAccessStatus != EvaluatorAccessIncomplete {
		return errors.New("evaluator input access status must be COMPLETE or INCOMPLETE")
	}
	if run.InputAccessStatus == EvaluatorAccessIncomplete && len(run.ResidualUnknowns) == 0 {
		return errors.New("incomplete evaluator access requires a residual unknown")
	}
	for _, unknown := range run.ResidualUnknowns {
		if unknown == "" {
			return errors.New("evaluator residual unknowns must be non-empty")
		}
	}
	if !run.ReadOnly {
		return errors.New("evaluator run must be declared read-only")
	}
	if !validSHA256(run.PromptSHA256) || !validContentID(run.InputSnapshotID, "snap_") {
		return errors.New("evaluator run requires prompt digest and content-addressed input snapshot")
	}
	if err := validateEvaluatorSnapshotReceipt(run.Snapshot); err != nil {
		return err
	}
	if run.InputSnapshotID != run.Snapshot.SnapshotID || run.SecretScanRevision != run.Snapshot.SecretScanRevision {
		return errors.New("evaluator run does not match its snapshot receipt")
	}
	started, err := time.Parse(time.RFC3339Nano, run.StartedAt)
	if err != nil {
		return fmt.Errorf("evaluator startedAt: %w", err)
	}
	finished, err := time.Parse(time.RFC3339Nano, run.FinishedAt)
	if err != nil || finished.Before(started) {
		return errors.New("evaluator finishedAt must be valid and not precede startedAt")
	}
	if run.PriceSource == "" || run.PriceRevision == "" {
		return errors.New("evaluator cost requires pinned price provenance")
	}
	switch run.CostStatus {
	case EvaluatorCostMeasured:
		if run.CostMicros < 0 || !validCurrency(run.CostCurrency) || run.CostUnavailableReason != "" {
			return errors.New("measured evaluator cost requires non-negative micros, currency, and no unavailable reason")
		}
	case EvaluatorCostUnavailable:
		if run.CostMicros != 0 || run.CostCurrency != "" || run.CostUnavailableReason == "" {
			return errors.New("unavailable evaluator cost requires zero placeholder, no currency, and an explicit reason")
		}
	default:
		return errors.New("evaluator cost status must be MEASURED or UNAVAILABLE")
	}
	seen := map[string]bool{}
	for _, finding := range run.Findings {
		if finding.ID == "" || finding.ClusterKey == "" || finding.Category == "" || finding.Target == "" || finding.Summary == "" || len(finding.Evidence) == 0 || seen[finding.ID] {
			return fmt.Errorf("finding %q requires unique identity, cluster key, target, summary, and evidence", finding.ID)
		}
		seen[finding.ID] = true
		confidence, ok := new(big.Rat).SetString(finding.Confidence)
		if !ok || confidence.Sign() < 0 || confidence.Cmp(big.NewRat(1, 1)) > 0 {
			return fmt.Errorf("finding %q confidence must be an exact rational in [0,1]", finding.ID)
		}
		if !finding.Hypothesis {
			return fmt.Errorf("finding %q must enter as a hypothesis", finding.ID)
		}
	}
	return nil
}

func validateEvaluatorSnapshotReceipt(receipt EvaluatorSnapshotReceipt) error {
	if receipt.APIVersion != "optimization.platform.example/evaluator-snapshot/v1alpha1" || receipt.Kind != "EvaluatorSnapshot" {
		return errors.New("unsupported evaluator snapshot receipt")
	}
	if receipt.Repository == "" || receipt.Path == "" || !validGitObjectID(receipt.Commit) || !validGitObjectID(receipt.Tree) {
		return errors.New("evaluator snapshot requires repository, path, commit, and tree identity")
	}
	if !validSHA256(receipt.ArchiveSHA256) || receipt.SnapshotID != "snap_"+receipt.ArchiveSHA256 {
		return errors.New("evaluator snapshot id must match its archive digest")
	}
	if receipt.SecretScanRevision == "" || receipt.SecretMatches != 0 {
		return errors.New("evaluator snapshot must carry a passing pinned secret scan")
	}
	return nil
}

func NewEvaluatorBatch(runs []EvaluatorRun, dispositions []FindingDisposition) (EvaluatorBatch, error) {
	if len(runs) < 2 {
		return EvaluatorBatch{}, errors.New("at least two evaluator runs are required")
	}
	runs = append([]EvaluatorRun(nil), runs...)
	dispositions = append([]FindingDisposition(nil), dispositions...)
	sort.Slice(runs, func(i, j int) bool { return runs[i].ID < runs[j].ID })
	sort.Slice(dispositions, func(i, j int) bool { return dispositions[i].FindingID < dispositions[j].FindingID })
	batch := EvaluatorBatch{APIVersion: EvaluatorEvidenceAPIVersion, Kind: "EvaluatorBatch", Runs: runs, Dispositions: dispositions}
	findingRun, findingFamily := map[string]string{}, map[string]string{}
	findingByID := map[string]EvaluatorFinding{}
	seenRuns := map[string]bool{}
	for _, run := range runs {
		if err := VerifyEvaluatorRun(run); err != nil {
			return EvaluatorBatch{}, err
		}
		if seenRuns[run.ID] {
			return EvaluatorBatch{}, fmt.Errorf("duplicate evaluator run %q", run.ID)
		}
		seenRuns[run.ID] = true
		if batch.PromptSHA256 == "" {
			batch.PromptSHA256, batch.InputSnapshotID = run.PromptSHA256, run.InputSnapshotID
		} else if batch.PromptSHA256 != run.PromptSHA256 || batch.InputSnapshotID != run.InputSnapshotID {
			return EvaluatorBatch{}, errors.New("evaluator runs must share the exact prompt and input snapshot")
		}
		for _, finding := range run.Findings {
			if _, exists := findingRun[finding.ID]; exists {
				return EvaluatorBatch{}, fmt.Errorf("duplicate finding %q", finding.ID)
			}
			findingRun[finding.ID], findingFamily[finding.ID] = run.ID, run.ModelFamily
			findingByID[finding.ID] = finding
		}
	}
	seenDispositions := map[string]bool{}
	dispositionByFinding := map[string]FindingDisposition{}
	for _, disposition := range dispositions {
		if _, ok := findingRun[disposition.FindingID]; !ok || seenDispositions[disposition.FindingID] {
			return EvaluatorBatch{}, fmt.Errorf("disposition references unknown or duplicate finding %q", disposition.FindingID)
		}
		seenDispositions[disposition.FindingID] = true
		dispositionByFinding[disposition.FindingID] = disposition
		if err := validateFindingDisposition(disposition, true); err != nil {
			return EvaluatorBatch{}, err
		}
	}
	clusterMap := map[string]*EvaluatorCluster{}
	for findingID, finding := range findingByID {
		key := finding.ClusterKey
		if disposition, ok := dispositionByFinding[findingID]; ok {
			key = disposition.CanonicalClusterKey
		}
		cluster := clusterMap[key]
		if cluster == nil {
			cluster = &EvaluatorCluster{Key: key}
			clusterMap[key] = cluster
		}
		cluster.FindingIDs = append(cluster.FindingIDs, findingID)
		cluster.RunIDs = appendUnique(cluster.RunIDs, findingRun[findingID])
		cluster.ModelFamilies = appendUnique(cluster.ModelFamilies, findingFamily[findingID])
		cluster.ConfidenceValues = appendUnique(cluster.ConfidenceValues, finding.Confidence)
	}
	keys := make([]string, 0, len(clusterMap))
	for key := range clusterMap {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		cluster := *clusterMap[key]
		sort.Strings(cluster.FindingIDs)
		sort.Strings(cluster.RunIDs)
		sort.Strings(cluster.ModelFamilies)
		sort.Strings(cluster.ConfidenceValues)
		batch.Clusters = append(batch.Clusters, cluster)
	}
	id, err := evaluatorContentID("evb_", batchIdentity(batch))
	if err != nil {
		return EvaluatorBatch{}, err
	}
	batch.ID = id
	return batch, nil
}

func NewEvaluatorResolution(batch EvaluatorBatch, dispositions []FindingDisposition) (EvaluatorResolution, error) {
	replayed, err := NewEvaluatorBatch(batch.Runs, batch.Dispositions)
	if err != nil || !reflect.DeepEqual(replayed, batch) {
		return EvaluatorResolution{}, errors.New("evaluator resolution requires a valid batch")
	}
	if len(dispositions) == 0 {
		return EvaluatorResolution{}, errors.New("evaluator resolution requires at least one disposition")
	}
	unresolved := map[string]bool{}
	for _, run := range batch.Runs {
		for _, finding := range run.Findings {
			unresolved[finding.ID] = true
		}
	}
	for _, disposition := range batch.Dispositions {
		if disposition.Status == DispositionUnresolved {
			unresolved[disposition.FindingID] = true
		} else {
			delete(unresolved, disposition.FindingID)
		}
	}
	dispositions = append([]FindingDisposition(nil), dispositions...)
	sort.Slice(dispositions, func(i, j int) bool { return dispositions[i].FindingID < dispositions[j].FindingID })
	seen := map[string]bool{}
	for _, disposition := range dispositions {
		if !unresolved[disposition.FindingID] || seen[disposition.FindingID] {
			return EvaluatorResolution{}, fmt.Errorf("resolution references non-unresolved or duplicate finding %q", disposition.FindingID)
		}
		seen[disposition.FindingID] = true
		if err := validateFindingDisposition(disposition, false); err != nil {
			return EvaluatorResolution{}, err
		}
	}
	resolution := EvaluatorResolution{APIVersion: EvaluatorEvidenceAPIVersion, Kind: "EvaluatorResolution", BatchID: batch.ID, Dispositions: dispositions}
	id, err := evaluatorContentID("evr_", resolutionIdentity(resolution))
	if err != nil {
		return EvaluatorResolution{}, err
	}
	resolution.ID = id
	return resolution, nil
}

func VerifyEvaluatorResolution(batch EvaluatorBatch, resolution EvaluatorResolution) error {
	if resolution.APIVersion != EvaluatorEvidenceAPIVersion || resolution.Kind != "EvaluatorResolution" || !validContentID(resolution.ID, "evr_") || resolution.BatchID != batch.ID {
		return errors.New("unsupported or incomplete evaluator resolution")
	}
	replayed, err := NewEvaluatorResolution(batch, resolution.Dispositions)
	if err != nil || !reflect.DeepEqual(replayed, resolution) {
		return errors.New("evaluator resolution does not match replay")
	}
	return nil
}

func validateFindingDisposition(disposition FindingDisposition, allowUnresolved bool) error {
	validStatus := disposition.Status == DispositionVerifiedDefect || disposition.Status == DispositionFalsePositive || (allowUnresolved && disposition.Status == DispositionUnresolved)
	if !validStatus {
		return fmt.Errorf("finding %q has invalid disposition", disposition.FindingID)
	}
	if disposition.CanonicalClusterKey == "" || disposition.Verifier == "" || disposition.Source == "" || disposition.SourceRevision == "" || disposition.Notes == "" {
		return fmt.Errorf("finding %q disposition requires independent verification provenance", disposition.FindingID)
	}
	if disposition.Status == DispositionVerifiedDefect && disposition.ExecutableGate == "" {
		return fmt.Errorf("verified finding %q requires executable gate", disposition.FindingID)
	}
	if _, err := time.Parse(time.RFC3339Nano, disposition.VerifiedAt); err != nil {
		return fmt.Errorf("finding %q verifiedAt: %w", disposition.FindingID, err)
	}
	return nil
}

func MeasureEvaluatorBatch(batch EvaluatorBatch) ([]EvaluatorMetric, error) {
	replayed, err := NewEvaluatorBatch(batch.Runs, batch.Dispositions)
	if err != nil || !reflect.DeepEqual(replayed, batch) {
		return nil, errors.New("evaluator batch does not match replay")
	}
	dispositionByFinding := map[string]FindingDisposition{}
	for _, disposition := range batch.Dispositions {
		dispositionByFinding[disposition.FindingID] = disposition
	}
	verifiedClusters := map[string]bool{}
	clusterRuns := map[string]map[string]bool{}
	for _, run := range batch.Runs {
		for _, finding := range run.Findings {
			disposition := dispositionByFinding[finding.ID]
			if disposition.Status == DispositionVerifiedDefect {
				key := disposition.CanonicalClusterKey
				verifiedClusters[key] = true
				if clusterRuns[key] == nil {
					clusterRuns[key] = map[string]bool{}
				}
				clusterRuns[key][run.ID] = true
			}
		}
	}
	metrics := make([]EvaluatorMetric, 0, len(batch.Runs))
	for _, run := range batch.Runs {
		started, _ := time.Parse(time.RFC3339Nano, run.StartedAt)
		finished, _ := time.Parse(time.RFC3339Nano, run.FinishedAt)
		metric := EvaluatorMetric{RunID: run.ID, Provider: run.Provider, ModelFamily: run.ModelFamily, Model: run.Model, LatencyNanoseconds: finished.Sub(started).Nanoseconds(), Usage: run.Usage, CostStatus: run.CostStatus, CostMicros: run.CostMicros, CostCurrency: run.CostCurrency, CostUnavailableReason: run.CostUnavailableReason, RecallStatus: EvaluatorMetricUnavailable}
		foundVerifiedClusters := map[string]bool{}
		uniqueVerifiedClusters := map[string]bool{}
		for _, finding := range run.Findings {
			disposition, ok := dispositionByFinding[finding.ID]
			if !ok || disposition.Status == DispositionUnresolved {
				continue
			}
			metric.DisposedFindings++
			if disposition.Status == DispositionVerifiedDefect {
				metric.VerifiedDefects++
				key := disposition.CanonicalClusterKey
				foundVerifiedClusters[key] = true
				if len(clusterRuns[key]) == 1 {
					uniqueVerifiedClusters[key] = true
				}
			} else {
				metric.FalsePositives++
			}
		}
		metric.UniqueVerifiedDefects = int64(len(uniqueVerifiedClusters))
		metric.VerifiedClusters = int64(len(foundVerifiedClusters))
		if metric.DisposedFindings > 0 {
			metric.Precision = big.NewRat(metric.VerifiedDefects, metric.DisposedFindings).RatString()
			metric.FalseDiscoveryRate = big.NewRat(metric.FalsePositives, metric.DisposedFindings).RatString()
		}
		if run.InputAccessStatus == EvaluatorAccessIncomplete {
			metric.RecallUnavailableReason = "evaluator did not have complete access to the pinned input"
		} else if len(verifiedClusters) > 0 {
			metric.RecallStatus = EvaluatorMetricMeasured
			metric.RecallAgainstBatchTruth = big.NewRat(int64(len(foundVerifiedClusters)), int64(len(verifiedClusters))).RatString()
		} else {
			metric.RecallUnavailableReason = "batch contains no verified defect clusters"
		}
		metrics = append(metrics, metric)
	}
	sort.Slice(metrics, func(i, j int) bool { return metrics[i].RunID < metrics[j].RunID })
	return metrics, nil
}

func MeasureEvaluatorBatchAfterResolutions(batch EvaluatorBatch, resolutions []EvaluatorResolution) ([]EvaluatorMetric, error) {
	dispositions := append([]FindingDisposition(nil), batch.Dispositions...)
	byFinding := map[string]int{}
	for i, disposition := range dispositions {
		byFinding[disposition.FindingID] = i
	}
	seenResolutions := map[string]bool{}
	resolvedByResolution := map[string]string{}
	for _, resolution := range resolutions {
		if seenResolutions[resolution.ID] {
			return nil, fmt.Errorf("duplicate evaluator resolution %q", resolution.ID)
		}
		seenResolutions[resolution.ID] = true
		if err := VerifyEvaluatorResolution(batch, resolution); err != nil {
			return nil, err
		}
		for _, disposition := range resolution.Dispositions {
			if prior, exists := resolvedByResolution[disposition.FindingID]; exists {
				return nil, fmt.Errorf("evaluator resolutions %q and %q both dispose finding %q", prior, resolution.ID, disposition.FindingID)
			}
			resolvedByResolution[disposition.FindingID] = resolution.ID
			if index, ok := byFinding[disposition.FindingID]; ok {
				dispositions[index] = disposition
			} else {
				byFinding[disposition.FindingID] = len(dispositions)
				dispositions = append(dispositions, disposition)
			}
		}
	}
	effective, err := NewEvaluatorBatch(batch.Runs, dispositions)
	if err != nil {
		return nil, err
	}
	return MeasureEvaluatorBatch(effective)
}

func evaluatorRunIdentity(run EvaluatorRun) any {
	run.ID = ""
	return run
}

func batchIdentity(batch EvaluatorBatch) any {
	batch.ID = ""
	return batch
}

func resolutionIdentity(resolution EvaluatorResolution) any {
	resolution.ID = ""
	return resolution
}

func evaluatorContentID(prefix string, value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return prefix + hex.EncodeToString(digest[:]), nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validGitObjectID(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
