package controlplane

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestBundleHTTPWorkflow(t *testing.T) {
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(context.Background(), assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureAgentIdentityHeaders:    true,
		AllowInsecureOperatorIdentityHeaders: true,
		BundlePrivateKey:                     mustBundleKey(t),
	})
	pipeline := ExportPipeline{
		DisplayName: "HTTP export", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	}
	body, err := json.Marshal(pipeline)
	if err != nil {
		t.Fatal(err)
	}
	put := httptest.NewRequest(http.MethodPut, "/v1/export-pipelines/http-export", bytes.NewReader(body))
	put.Header.Set("Content-Type", "application/json")
	putResponse := httptest.NewRecorder()
	handler.ServeHTTP(putResponse, put)
	if putResponse.Code != http.StatusOK {
		t.Fatalf("put pipeline = %d %s", putResponse.Code, putResponse.Body.String())
	}
	preBuildPreviewResponse := httptest.NewRecorder()
	handler.ServeHTTP(preBuildPreviewResponse, httptest.NewRequest(http.MethodPost, "/v1/export-pipelines/http-export/preview", nil))
	if preBuildPreviewResponse.Code != http.StatusOK {
		t.Fatalf("pre-build preview = %d %s", preBuildPreviewResponse.Code, preBuildPreviewResponse.Body.String())
	}
	scheduleRunsResponse := httptest.NewRecorder()
	handler.ServeHTTP(scheduleRunsResponse, httptest.NewRequest(http.MethodGet, "/v1/export-pipelines/http-export/schedule-runs", nil))
	if scheduleRunsResponse.Code != http.StatusOK || scheduleRunsResponse.Body.String() != "{\"runs\":[]}\n" {
		t.Fatalf("schedule runs = %d %s", scheduleRunsResponse.Code, scheduleRunsResponse.Body.String())
	}

	buildResponse := httptest.NewRecorder()
	handler.ServeHTTP(buildResponse, httptest.NewRequest(http.MethodPost, "/v1/export-pipelines/http-export/build", nil))
	if buildResponse.Code != http.StatusCreated {
		t.Fatalf("build = %d %s", buildResponse.Code, buildResponse.Body.String())
	}
	var result struct {
		Bundles []BundleEnvelope `json:"bundles"`
	}
	if err := json.Unmarshal(buildResponse.Body.Bytes(), &result); err != nil || len(result.Bundles) != 1 {
		t.Fatalf("build response = %#v, %v", result, err)
	}
	previewBody, err := json.Marshal(result.Bundles[0])
	if err != nil {
		t.Fatal(err)
	}
	preview := httptest.NewRequest(http.MethodPost, "/v1/bundles/preview", bytes.NewReader(previewBody))
	preview.Header.Set("Content-Type", "application/json")
	previewResponse := httptest.NewRecorder()
	handler.ServeHTTP(previewResponse, preview)
	if previewResponse.Code != http.StatusOK {
		t.Fatalf("preview = %d %s", previewResponse.Code, previewResponse.Body.String())
	}
}

func TestExportPipelineRevisionedLifecycleAndAlternateHTTPBuild(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "revisioned-export", DisplayName: "First definition", BuildStrategy: BundleStrategyIncremental,
		Selections: []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{"dev-use2"}, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	pipeline.DisplayName, pipeline.Revision = "Second definition", 1
	pipeline, err = service.PutPipeline(ctx, pipeline)
	if err != nil || pipeline.Revision != 2 {
		t.Fatalf("updated pipeline = %#v, %v", pipeline, err)
	}
	lineage, err := service.Build(ctx, pipeline.ID)
	if err != nil || len(lineage) != 1 || lineage[0].Manifest.Kind != BundleSnapshot {
		t.Fatalf("initial lineage build = %#v, %v", lineage, err)
	}

	if _, err := NewGovernanceService(repository).PutPolicy(ctx, ApprovalPolicy{
		ID: "pipeline-lifecycle", DisplayName: "Pipeline lifecycle", Enabled: true,
		Expression: `type.endsWith("_PIPELINE") ? approve("fixture approval") : no_op()`,
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BundlePrivateKey: mustBundleKey(t)})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/export-pipelines/revisioned-export/build?revision=1&standalone=true", nil))
	if response.Code != http.StatusCreated {
		t.Fatalf("historical build = %d %s", response.Code, response.Body.String())
	}
	var built struct {
		Bundles []BundleEnvelope `json:"bundles"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &built); err != nil || len(built.Bundles) != 1 ||
		built.Bundles[0].Manifest.PipelineRevision != 1 || !built.Bundles[0].Manifest.Standalone ||
		!built.Bundles[0].Manifest.LineageIndependent || built.Bundles[0].Manifest.DependsOn != "" {
		t.Fatalf("historical standalone build = %#v, %v", built, err)
	}
	next, err := service.Build(ctx, pipeline.ID)
	if err != nil || len(next) != 1 || next[0].Manifest.Kind != BundleIncremental ||
		next[0].Manifest.DependsOn != lineage[0].Manifest.ID {
		t.Fatalf("alternate build contaminated incremental lineage: %#v, %v", next, err)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/export-pipelines/revisioned-export/archive", nil))
	archived, err := repository.GetExportPipeline(ctx, pipeline.ID)
	if response.Code != http.StatusCreated || err != nil || archived.Status != ExportPipelineArchived || archived.ArchivedAt.IsZero() {
		t.Fatalf("archived pipeline = %d %#v, %v (%s)", response.Code, archived, err, response.Body.String())
	}
	if _, err := service.Build(ctx, pipeline.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("archived build error = %v", err)
	}
	archived.DisplayName = "forbidden edit"
	if _, err := service.PutPipeline(ctx, archived); !errors.Is(err, ErrConflict) {
		t.Fatalf("archived edit error = %v", err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/export-pipelines/revisioned-export/unarchive", nil))
	active, err := repository.GetExportPipeline(ctx, pipeline.ID)
	if response.Code != http.StatusCreated || err != nil || active.Status != ExportPipelineActive {
		t.Fatalf("unarchived pipeline = %d %#v, %v (%s)", response.Code, active, err, response.Body.String())
	}
	if _, err := service.DeletePipeline(ctx, pipeline.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("active delete error = %v", err)
	}
	if _, err := service.SetPipelineArchived(ctx, pipeline.ID, true); err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/v1/export-pipelines/revisioned-export", nil))
	deleted, err := repository.GetExportPipeline(ctx, pipeline.ID)
	if response.Code != http.StatusCreated || err != nil || deleted.Status != ExportPipelineDeleted || deleted.DeletedAt.IsZero() {
		t.Fatalf("deleted pipeline = %d %#v, %v (%s)", response.Code, deleted, err, response.Body.String())
	}
}

func TestBundleSnapshotIncrementalCheckpointAndAdmission(t *testing.T) {
	ctx := context.Background()
	sourceRepository := NewMemoryRepository()
	lifecycle := NewLifecycle(sourceRepository)
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	if _, err := lifecycle.CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	privateKey, err := GenerateBundleSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	source, err := NewBundleService(sourceRepository, privateKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	source.now = lifecycle.now
	pipeline, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "production-export", DisplayName: "Production export",
		BuildStrategy: BundleStrategyIncremental, CheckpointFrequency: 2,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
		RequireSecurityPass: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.Revision != 1 {
		t.Fatalf("pipeline revision = %d", pipeline.Revision)
	}
	first, err := source.Build(ctx, pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Manifest.Kind != BundleSnapshot || first[0].Manifest.DependsOn != "" {
		t.Fatalf("first build = %#v", first)
	}
	if _, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "minimal-export", DisplayName: "Minimal export", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectProduct, IDs: []string{"app.example:example-workspace"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	minimal, err := source.Build(ctx, "minimal-export")
	if err != nil {
		t.Fatal(err)
	}
	productReleases := 0
	for _, resource := range minimal[0].Manifest.Resources {
		if resource.Type == "PRODUCT_RELEASE" {
			productReleases++
		}
	}
	if productReleases != 2 {
		t.Fatalf("minimal dependency closure has %d Product Releases", productReleases)
	}

	changed := assessedFoundation()
	changed.Environment.Generation = "fedcba9876543210"
	changed.Releases[0].Version = "0.1.1"
	changed.Entities[1].Version = "0.1.1"
	now = now.Add(time.Second)
	if _, err := lifecycle.CreatePlan(ctx, changed); err != nil {
		t.Fatal(err)
	}
	second, err := source.Build(ctx, pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 1 || second[0].Manifest.Kind != BundleIncremental ||
		second[0].Manifest.DependsOn != first[0].Manifest.ID || len(second[0].Manifest.Resources) == 0 {
		t.Fatalf("second build = %#v", second)
	}

	changed.Environment.Generation = "abcdef0123456789"
	changed.Releases = changed.Releases[:1]
	changed.Entities = changed.Entities[1:]
	now = now.Add(time.Second)
	if _, err := lifecycle.CreatePlan(ctx, changed); err != nil {
		t.Fatal(err)
	}
	third, err := source.Build(ctx, pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 2 || third[0].Manifest.Kind != BundleIncremental || third[1].Manifest.Kind != BundleCheckpoint ||
		third[1].Manifest.CheckpointOf != third[0].Manifest.ID || third[1].Manifest.DependsOn != "" {
		t.Fatalf("third build = %#v", third)
	}
	removed := 0
	for _, resource := range third[0].Manifest.Resources {
		if resource.Deleted {
			removed++
		}
	}
	if removed == 0 {
		t.Fatalf("third incremental has no deletion tombstones: %#v", third[0].Manifest.Resources)
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)
	keyID := bundleSigningKeyID(publicKey)
	target, err := NewBundleService(NewMemoryRepository(), mustBundleKey(t), map[string]ed25519.PublicKey{keyID: publicKey})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := target.Preview(ctx, first[0])
	if err != nil || len(preview.Added) == 0 {
		t.Fatalf("snapshot preview = %#v, %v", preview, err)
	}
	imported, err := target.Import(ctx, first[0], true)
	if err != nil {
		t.Fatal(err)
	}
	if imported.Status != "ADMITTED" || len(imported.Tasks) != 4 {
		t.Fatalf("import diagnostics = %#v", imported)
	}
	for index, task := range imported.Tasks {
		if task.Sequence != index+1 || task.Status != BundleImportTaskSucceeded || task.StartedAt.IsZero() || task.FinishedAt.Before(task.StartedAt) {
			t.Fatalf("import task %d = %#v", index, task)
		}
	}
	if _, err := target.Import(ctx, second[0], true); err != nil {
		t.Fatal(err)
	}
	thirdPreview, err := target.Preview(ctx, third[0])
	if err != nil || len(thirdPreview.Removed) == 0 {
		t.Fatalf("deletion preview = %#v, %v", thirdPreview, err)
	}

	tampered := cloneJSON(first[0])
	tampered.Manifest.Resources[0].Payload = json.RawMessage(`{"tampered":true}`)
	if _, err := target.Preview(ctx, tampered); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("tampered preview error = %v", err)
	}

	missingDependencyTarget, err := NewBundleService(NewMemoryRepository(), mustBundleKey(t), map[string]ed25519.PublicKey{keyID: publicKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missingDependencyTarget.Preview(ctx, second[0]); !errors.Is(err, ErrConflict) {
		t.Fatalf("missing dependency error = %v", err)
	}
}

func TestBundleImportPersistsFailedStageDiagnostics(t *testing.T) {
	ctx := context.Background()
	sourceRepository := NewMemoryRepository()
	state := assessedFoundation()
	if _, err := NewLifecycle(sourceRepository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	sourceKey := mustBundleKey(t)
	source, err := NewBundleService(sourceRepository, sourceKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "failed-import", DisplayName: "Failed import", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{state.Environment.ID}, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"}}},
	}); err != nil {
		t.Fatal(err)
	}
	built, err := source.Build(ctx, "failed-import")
	if err != nil {
		t.Fatal(err)
	}

	targetRepository, err := NewFileRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	publicKey := sourceKey.Public().(ed25519.PublicKey)
	target, err := NewBundleService(targetRepository, mustBundleKey(t), map[string]ed25519.PublicKey{bundleSigningKeyID(publicKey): publicKey})
	if err != nil {
		t.Fatal(err)
	}
	staged, err := target.StageImport(ctx, built[0], state.Environment.ID, "importer", false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	changeRequest, err := targetRepository.CreateChangeRequest(ctx, ChangeRequest{
		ID: "crq_" + stableShortID(staged.ID), TargetKind: "BUNDLE_IMPORT", TargetID: staged.ID,
		Environment: state.Environment.ID, Title: "Import", Author: Principal{Username: "importer"},
		Status: ChangeApproved, RequirementsSatisfied: true, CreatedAt: now, UpdatedAt: now, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.LinkImportChangeRequest(ctx, staged.ID, changeRequest.ID); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetRepository.importedCatalogPath(state.Environment.ID), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := target.AdmitImport(ctx, staged.ID); err == nil {
		t.Fatal("malformed catalog unexpectedly admitted")
	}
	failed, err := targetRepository.GetBundleImport(ctx, staged.ID)
	if err != nil || failed.Status != "ERROR" || failed.Error == "" || len(failed.Tasks) != 4 ||
		failed.Tasks[2].Status != BundleImportTaskFailed || failed.Tasks[2].Failure == "" {
		t.Fatalf("failed import diagnostics = %#v, %v", failed, err)
	}
	if strings.Contains(finishFailedImportForTest("token=topsecret"), "topsecret") {
		t.Fatal("task diagnostic retained credential material")
	}
	// The task behind the failure never ran and never will: the import is
	// terminal. Left PENDING it is indistinguishable from a task that is
	// simply queued, so the one screen an operator opens after a failed
	// import cannot say which tasks were even attempted.
	if failed.Tasks[3].Status != BundleImportTaskSkipped {
		t.Errorf("task after the failure = %s, want %s", failed.Tasks[3].Status, BundleImportTaskSkipped)
	}
	// The tasks that did run keep what they reported. Skipping is only ever
	// about tasks that never started, so it must not overwrite a result.
	for _, index := range []int{0, 1} {
		if failed.Tasks[index].Status != BundleImportTaskSucceeded {
			t.Errorf("task %d = %s, want it left SUCCEEDED", index, failed.Tasks[index].Status)
		}
	}
	if failed.Tasks[2].Status != BundleImportTaskFailed {
		t.Errorf("the failed task was overwritten to %s", failed.Tasks[2].Status)
	}
}

// SKIPPED has to mean something narrower than "not succeeded", or it is just
// PENDING with a different name.
func TestBundleImportTasksAreOnlySkippedByAnAbortedImport(t *testing.T) {
	ctx := context.Background()
	sourceRepository := NewMemoryRepository()
	state := assessedFoundation()
	if _, err := NewLifecycle(sourceRepository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	sourceKey := mustBundleKey(t)
	source, err := NewBundleService(sourceRepository, sourceKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "skips", DisplayName: "Skips", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{state.Environment.ID},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	built, err := source.Build(ctx, "skips")
	if err != nil {
		t.Fatal(err)
	}
	publicKey := sourceKey.Public().(ed25519.PublicKey)
	targetRepository := NewMemoryRepository()
	target, err := NewBundleService(targetRepository, mustBundleKey(t),
		map[string]ed25519.PublicKey{bundleSigningKeyID(publicKey): publicKey})
	if err != nil {
		t.Fatal(err)
	}
	staged, err := target.StageImport(ctx, built[0], state.Environment.ID, "importer", false)
	if err != nil {
		t.Fatal(err)
	}

	// Staged and awaiting approval: these tasks genuinely are waiting, and
	// calling them skipped would be a lie in the other direction.
	for index, task := range staged.Tasks {
		if task.Status == BundleImportTaskSkipped {
			t.Fatalf("task %d of a staged import is already %s", index, task.Status)
		}
	}
	pending := 0
	for _, task := range staged.Tasks {
		if task.Status == BundleImportTaskPending {
			pending++
		}
	}
	if pending == 0 {
		t.Fatal("no task is PENDING on a staged import, so this proves nothing")
	}

	now := time.Now().UTC()
	changeRequest, err := targetRepository.CreateChangeRequest(ctx, ChangeRequest{
		ID: "crq_" + stableShortID(staged.ID), TargetKind: "BUNDLE_IMPORT", TargetID: staged.ID,
		Environment: state.Environment.ID, Title: "Import", Author: Principal{Username: "importer"},
		Status: ChangeApproved, RequirementsSatisfied: true, CreatedAt: now, UpdatedAt: now, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.LinkImportChangeRequest(ctx, staged.ID, changeRequest.ID); err != nil {
		t.Fatal(err)
	}
	admitted, err := target.AdmitImport(ctx, staged.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index, task := range admitted.Tasks {
		if task.Status != BundleImportTaskSucceeded {
			t.Errorf("task %d of a clean import = %s, want %s", index, task.Status, BundleImportTaskSucceeded)
		}
	}
}

func TestBundleBuildMissingResourcePolicyFailsClosedOrOmits(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	definition := ExportPipeline{
		ID: "missing-resource", DisplayName: "Missing resource", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{Type: BundleSelectRelease, IDs: []string{"missing.example:product:9.9.9"}}},
	}
	if _, err := service.PutPipeline(ctx, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Build(ctx, definition.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("strict missing resource error = %v", err)
	}
	stored, err := repository.GetExportPipeline(ctx, definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	stored.BuildDespiteMissingResources = true
	if _, err := service.PutPipeline(ctx, stored); err != nil {
		t.Fatal(err)
	}
	built, err := service.Build(ctx, definition.ID)
	if err != nil || len(built) != 1 || len(built[0].Manifest.Resources) != 0 {
		t.Fatalf("permissive missing resource build = %#v, %v", built, err)
	}
}

func finishFailedImportForTest(failure string) string {
	record := BundleImport{Tasks: []BundleImportTask{{Stage: BundleImportTaskCatalog}}}
	now := time.Now().UTC()
	finishBundleImportTask(&record, BundleImportTaskCatalog, BundleImportTaskFailed, now, now, failure)
	return record.Tasks[0].Failure
}

func TestBundleRelaysExactReportedStateOnlyToOptedInEnvironment(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	sourceRepository := NewMemoryRepository()
	state := assessedFoundation()
	if _, err := NewLifecycle(sourceRepository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	report := EnvironmentReport{
		Environment: state.Environment.ID, AgentID: "spoke-agent", PlanID: "pln_source", ObservedAt: now.Add(-time.Minute),
		Entities: []ReportedEntity{{
			InstallationID: "example-api", Product: "app.example:example-api", Version: "0.1.0",
			ObservedAt: now.Add(-time.Minute), Health: HealthHealthy, RolloutStatus: RolloutUpToDate,
			ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled,
		}},
	}
	if _, err := sourceRepository.PutEnvironmentReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	privateKey := mustBundleKey(t)
	source, err := NewBundleService(sourceRepository, privateKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	source.now = func() time.Time { return now }
	if _, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "relay-state", DisplayName: "Relay state", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{state.Environment.ID},
			ReleaseStrategy: BundleReleasesNone,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	built, err := source.Build(ctx, "relay-state")
	if err != nil {
		t.Fatal(err)
	}
	if len(built) != 1 || !bundleHasResource(built[0], BundleResourceEnvironmentReport, state.Environment.ID) {
		t.Fatalf("Bundle does not carry the Environment report: %#v", built)
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)
	trusted := map[string]ed25519.PublicKey{bundleSigningKeyID(publicKey): publicKey}
	newTarget := func(setting string) (*MemoryRepository, *BundleService) {
		repository := NewMemoryRepository()
		targetState := cloneJSON(state)
		targetState.Environment.ConnectionSetting = setting
		targetState.Environment.DeclaredStateSource = SourceEditOnAnotherHub
		if _, createErr := repository.CreatePlan(ctx, PlanRecord{
			ID: "pln_target_" + stableShortID(setting), Plan: Plan{Environment: state.Environment.ID, Generation: "target-generation"},
			DesiredState: &targetState, Status: PlanAwaitingApproval, CreatedAt: now,
		}); createErr != nil {
			t.Fatal(createErr)
		}
		target, targetErr := NewBundleService(repository, mustBundleKey(t), trusted)
		if targetErr != nil {
			t.Fatal(targetErr)
		}
		target.now = func() time.Time { return now }
		return repository, target
	}

	relayedRepository, relayedTarget := newTarget(ConnectionRelayedFromAnotherHub)
	if _, err := relayedTarget.Import(ctx, built[0], false); err != nil {
		t.Fatal(err)
	}
	got, err := relayedRepository.GetEnvironmentReport(ctx, state.Environment.ID)
	if err != nil || got.AgentID != report.AgentID || !got.ObservedAt.Equal(report.ObservedAt) || len(got.Entities) != 1 {
		t.Fatalf("relayed report = %#v, %v", got, err)
	}
	catalog, err := relayedRepository.GetImportedCatalog(ctx, state.Environment.ID)
	if err != nil || catalog.RelayedReportBundleID != built[0].Manifest.ID ||
		catalog.RelayedReportSigningKey != built[0].SigningKeyID {
		t.Fatalf("relay attribution = %#v, %v", catalog, err)
	}

	_, connectedTarget := newTarget(ConnectionAgentsConnected)
	if _, err := connectedTarget.StageImport(ctx, built[0], state.Environment.ID, "operator", false); !errors.Is(err, ErrConflict) {
		t.Fatalf("agents-connected target accepted relayed state: %v", err)
	}
}

func bundleHasResource(envelope BundleEnvelope, resourceType, id string) bool {
	for _, resource := range envelope.Manifest.Resources {
		if resource.Type == resourceType && resource.ID == id && !resource.Deleted {
			return true
		}
	}
	return false
}

func TestBundleInitializationSelectsLatestReleaseAndExportsTeams(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := assessedFoundation()
	older := cloneJSON(state.Releases[0])
	older.Version = "0.0.9"
	state.Releases = append(state.Releases, older)
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	team := Team{Name: "operators", DisplayName: "Operators", Revision: 1}
	if _, err := repository.PutTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.PutPipeline(ctx, ExportPipeline{
		ID: "initialization", DisplayName: "Initialization", BuildStrategy: BundleStrategyIncremental,
		Selections: []BundleSelectionRule{
			{Type: BundleSelectEnvironment, IDs: []string{"dev-use2"}, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"}},
			{Type: BundleSelectTeam, IDs: []string{"operators"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundles, err := service.Build(ctx, "initialization")
	if err != nil {
		t.Fatal(err)
	}
	releases, teams := 0, 0
	for _, resource := range bundles[0].Manifest.Resources {
		switch resource.Type {
		case "PRODUCT_RELEASE":
			releases++
			if resource.ID == older.Product+":"+older.Version {
				t.Fatalf("initialization exported superseded historical Release %s", resource.ID)
			}
		case "TEAM":
			teams++
		}
	}
	if releases != 2 || teams != 1 {
		t.Fatalf("initialization resources: releases=%d teams=%d", releases, teams)
	}
}

func TestIncrementalPipelineDefaultsToCheckpointAfterTenSuccessfulBuilds(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	state := assessedFoundation()
	if _, err := lifecycle.CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	service.now = lifecycle.now
	pipeline, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "default-checkpoints", DisplayName: "Default checkpoints", BuildStrategy: BundleStrategyIncremental,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if pipeline.CheckpointFrequency != DefaultCheckpointFrequency {
		t.Fatalf("checkpoint frequency = %d, want %d", pipeline.CheckpointFrequency, DefaultCheckpointFrequency)
	}
	first, err := service.Build(ctx, pipeline.ID)
	if err != nil || len(first) != 1 || first[0].Manifest.Kind != BundleSnapshot {
		t.Fatalf("initial snapshot = %#v, %v", first, err)
	}
	previousIncremental := first[0].Manifest.ID
	for build := 1; build <= DefaultCheckpointFrequency; build++ {
		now = now.Add(time.Second)
		state.Environment.Generation = fmt.Sprintf("%016x", build)
		if _, err := lifecycle.CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
		bundles, err := service.Build(ctx, pipeline.ID)
		if err != nil {
			t.Fatal(err)
		}
		wantCount := 1
		if build == DefaultCheckpointFrequency {
			wantCount = 2
		}
		if len(bundles) != wantCount || bundles[0].Manifest.Kind != BundleIncremental ||
			bundles[0].Manifest.DependsOn != previousIncremental {
			t.Fatalf("build %d = %#v", build, bundles)
		}
		previousIncremental = bundles[0].Manifest.ID
		if wantCount == 2 && (bundles[1].Manifest.Kind != BundleCheckpoint ||
			bundles[1].Manifest.DependsOn != "" || bundles[1].Manifest.CheckpointOf != previousIncremental) {
			t.Fatalf("checkpoint = %#v", bundles[1].Manifest)
		}
	}

	now = now.Add(time.Second)
	state.Environment.Generation = "000000000000000b"
	if _, err := lifecycle.CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	next, err := service.Build(ctx, pipeline.ID)
	if err != nil || len(next) != 1 || next[0].Manifest.DependsOn != previousIncremental {
		t.Fatalf("post-checkpoint incremental lineage = %#v, %v", next, err)
	}
}

func TestBundleLabelRequirementsUseValuesTypesAndExclusionPrecedence(t *testing.T) {
	ctx := context.Background()
	state := assessedFoundation()
	state.Environment.Labels = map[string]string{"cloud-service-provider": "aws"}
	state.Releases[0].Labels = map[string]string{"export-approved": "yes"}
	state.Releases[1].Labels = map[string]string{"export-approved": "yes", "do-not-export": "true"}
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.PutPipeline(ctx, ExportPipeline{
		ID: "label-filtered", DisplayName: "Label filtered", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
		RequiredLabels: []BundleLabelRequirement{
			{ResourceTypes: []string{BundleLabelEnvironment}, Name: "cloud-service-provider", Values: []string{"aws", "azure"}},
			{ResourceTypes: []string{BundleLabelProductRelease}, Name: "export-approved"},
			{ResourceTypes: []string{BundleLabelProductRelease}, Name: SecurityScannedLabel, Values: []string{"true"}},
			{ResourceTypes: []string{BundleLabelProductRelease}, Name: SecurityScanOutcomeLabel, Values: []string{"pass"}},
		},
		ExcludedLabels: []BundleLabelRequirement{{
			ResourceTypes: []string{BundleLabelProductRelease}, Name: "do-not-export",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundles, err := service.Build(ctx, "label-filtered")
	if err != nil {
		t.Fatal(err)
	}
	resources := map[string]struct{}{}
	for _, resource := range bundles[0].Manifest.Resources {
		resources[resource.Type+":"+resource.ID] = struct{}{}
	}
	if _, exists := resources["ENVIRONMENT:dev-use2"]; !exists {
		t.Fatalf("required Environment label was not scoped correctly: %#v", resources)
	}
	if _, exists := resources["PRODUCT_RELEASE:"+state.Releases[0].Product+":"+state.Releases[0].Version]; !exists {
		t.Fatalf("matching scanned release was omitted: %#v", resources)
	}
	if _, exists := resources["PRODUCT_RELEASE:"+state.Releases[1].Product+":"+state.Releases[1].Version]; exists {
		t.Fatalf("excluded release overrode exclusion precedence: %#v", resources)
	}
}

func TestBundleFileRepositoryPersistsRecords(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	// This fixture exports and re-imports through one Hub. That is only a legal
	// topology when the Environment's settings are owned upstream, so declare it:
	// a Hub that is authoritative for an Environment rejects a Bundle that would
	// overwrite the settings it owns.
	persisted := assessedFoundation()
	persisted.Environment.DeclaredStateSource = SourceEditOnAnotherHub
	if _, err := NewLifecycle(repository).CreatePlan(ctx, persisted); err != nil {
		t.Fatal(err)
	}
	key := mustBundleKey(t)
	service, err := NewBundleService(repository, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "persisted", DisplayName: "Persisted", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	built, err := service.Build(ctx, "persisted")
	if err != nil {
		t.Fatal(err)
	}
	imported, err := service.Import(ctx, built[0], false)
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetExportPipeline(ctx, "persisted"); err != nil {
		t.Fatal(err)
	}
	if revision, err := reopened.GetExportPipelineRevision(ctx, "persisted", 1); err != nil || revision.DisplayName != "Persisted" {
		t.Fatalf("reopened pipeline revision = %#v, %v", revision, err)
	}
	stored, err := reopened.GetBundle(ctx, built[0].Manifest.ID)
	if err != nil || stored.ManifestDigest != built[0].ManifestDigest {
		t.Fatalf("reopened bundle = %#v, %v", stored, err)
	}
	storedImport, err := reopened.GetBundleImport(ctx, imported.ID)
	if err != nil || storedImport.Status != "ADMITTED" || storedImport.ChangeRequestID == "" {
		t.Fatalf("reopened import = %#v, %v", storedImport, err)
	}
	catalog, err := reopened.GetImportedCatalog(ctx, "dev-use2")
	if err != nil || len(catalog.Resources) == 0 || !contains(catalog.BundleIDs, built[0].Manifest.ID) {
		t.Fatalf("reopened imported catalog = %#v, %v", catalog, err)
	}
	plans, err := reopened.ListPlans(ctx, "dev-use2")
	if err != nil || len(plans) != 1 || plans[0].DesiredState == nil {
		t.Fatalf("reopened desired state = %#v, %v", plans, err)
	}
}

func TestBundleArtifactsChunkDeduplicateVerifyAndTrackTargetTransfers(t *testing.T) {
	ctx := context.Background()
	contents := []byte("shared immutable artifact bytes")
	sum := sha256.Sum256(contents)
	artifactDigest := sha256Digest(sum)
	state := assessedFoundation()
	state.Environment.Generation = "bundleartifacts01"
	for index := range state.Releases {
		state.Releases[index].Chart.Digest = artifactDigest
		state.Releases[index].Images[0].Digest = artifactDigest
		state.Releases[index].VulnerabilityAssessment.ArtifactDigests = []string{artifactDigest}
		state.Releases[index].MalwareAssessment = &MalwareAssessment{
			Provider: "malware-scanner", RunID: "malware-1", Status: "COMPLETE", Outcome: "PASS",
			ArtifactDigests: []string{artifactDigest}, AssessedAt: time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC),
		}
	}
	sourceRepository := NewMemoryRepository()
	if _, _, err := sourceRepository.PutBlob(ctx, artifactDigest, "application/vnd.oci.image.layer.v1.tar", bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLifecycle(sourceRepository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	key := mustBundleKey(t)
	source, err := NewBundleService(sourceRepository, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "artifact-export", DisplayName: "Artifact export", BuildStrategy: BundleStrategySnapshot,
		Selections:       []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{"dev-use2"}, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"}}},
		IncludeArtifacts: true, RequireSecurityPass: true, RequireMalwarePass: true, ChunkSizeBytes: 7, EnableLayerDedup: true,
	}); err != nil {
		t.Fatal(err)
	}
	full, err := source.BuildForTarget(ctx, "artifact-export", "target-a")
	if err != nil {
		t.Fatal(err)
	}
	if len(full[0].Manifest.ArtifactLayers) != 1 || len(full[0].Chunks) != 5 {
		t.Fatalf("layers=%#v chunks=%d", full[0].Manifest.ArtifactLayers, len(full[0].Chunks))
	}
	anyHub, err := source.Build(ctx, "artifact-export")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.ConfirmTransfer(ctx, anyHub[0].Manifest.ID, "target-a"); !errors.Is(err, ErrConflict) {
		t.Fatalf("Any Hub Bundle was eligible for target dedup confirmation: %v", err)
	}
	tampered := cloneJSON(full[0])
	tampered.Chunks[0].Data[0] ^= 0xff
	if _, err := source.Preview(ctx, tampered); !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("tampered chunk error = %v", err)
	}

	publicKey := key.Public().(ed25519.PublicKey)
	targetRepository := NewMemoryRepository()
	target, err := NewBundleService(targetRepository, mustBundleKey(t), map[string]ed25519.PublicKey{bundleSigningKeyID(publicKey): publicKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := target.Import(ctx, full[0], true); err != nil {
		t.Fatal(err)
	}
	if descriptor, reader, err := targetRepository.OpenBlob(ctx, artifactDigest); err != nil {
		t.Fatal(err)
	} else {
		stored, _ := io.ReadAll(reader)
		_ = reader.Close()
		if descriptor.Size != int64(len(contents)) || !bytes.Equal(stored, contents) {
			t.Fatalf("stored descriptor=%#v bytes=%q", descriptor, stored)
		}
	}
	confirmation, err := source.ConfirmTransfer(ctx, full[0].Manifest.ID, "target-a")
	if err != nil {
		t.Fatal(err)
	}
	deduplicated, err := source.BuildForTarget(ctx, "artifact-export", "target-a")
	if err != nil {
		t.Fatal(err)
	}
	if !deduplicated[0].Manifest.ArtifactLayers[0].Transferred || len(deduplicated[0].Chunks) != 0 {
		t.Fatalf("target-specific Bundle was not deduplicated: %#v", deduplicated[0])
	}
	if _, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "shared-artifact-export", DisplayName: "Shared artifact export", BuildStrategy: BundleStrategySnapshot,
		Selections:       []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{"dev-use2"}, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"}}},
		IncludeArtifacts: true, EnableLayerDedup: true, ShareLayerDedupAcrossPipelines: true,
	}); err != nil {
		t.Fatal(err)
	}
	shared, err := source.BuildForTarget(ctx, "shared-artifact-export", "target-a")
	if err != nil || !shared[0].Manifest.ArtifactLayers[0].Transferred || len(shared[0].Chunks) != 0 {
		t.Fatalf("cross-Pipeline target deduplication = %#v, %v", shared, err)
	}
	if _, err := target.StageImport(ctx, deduplicated[0], "dev-use2", "importer", true); err != nil {
		t.Fatalf("target with confirmed layer rejected Bundle: %v", err)
	}
	fresh, err := NewBundleService(NewMemoryRepository(), mustBundleKey(t), map[string]ed25519.PublicKey{bundleSigningKeyID(publicKey): publicKey})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.StageImport(ctx, deduplicated[0], "dev-use2", "importer", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("fresh target accepted omitted layer: %v", err)
	}
	if _, err := source.RescindTransfer(ctx, confirmation); err != nil {
		t.Fatal(err)
	}
	restored, err := source.BuildForTarget(ctx, "artifact-export", "target-a")
	if err != nil {
		t.Fatal(err)
	}
	if restored[0].Manifest.ArtifactLayers[0].Transferred || len(restored[0].Chunks) == 0 {
		t.Fatalf("rescinded transfer still omitted bytes: %#v", restored[0])
	}
	stale, err := source.BuildForTarget(ctx, "artifact-export", "target-b")
	if err != nil {
		t.Fatal(err)
	}
	staleCreatedAt := stale[0].Manifest.CreatedAt
	source.now = func() time.Time { return staleCreatedAt.Add(7*24*time.Hour + time.Nanosecond) }
	if _, err := source.ConfirmTransfer(ctx, stale[0].Manifest.ID, "target-b"); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale target Bundle was eligible for dedup confirmation: %v", err)
	}
	// A Bundle stamped ahead of this Hub's clock cannot have its age measured: the
	// subtraction is negative and reads as comfortably inside the window. Skew
	// between two Hubs is ordinary, so the future-dated Bundle is refused on its own
	// terms rather than confirmed at an age nobody knows. This has to come before
	// any successful confirmation below -- once a confirmation record exists, a
	// second attempt is refused for its own reasons and would pass either way.
	source.now = func() time.Time { return staleCreatedAt.Add(-time.Second) }
	if _, err := source.ConfirmTransfer(ctx, stale[0].Manifest.ID, "target-b"); !errors.Is(err, ErrConflict) {
		t.Fatalf("future-dated Bundle was eligible for dedup confirmation: %v", err)
	}

	// The window is seven days inclusive: a Bundle exactly at the boundary is still
	// confirmable, and the operator who transfers on day seven is not turned away.
	source.now = func() time.Time { return staleCreatedAt.Add(7 * 24 * time.Hour) }
	boundary, err := source.ConfirmTransfer(ctx, stale[0].Manifest.ID, "target-b")
	if err != nil {
		t.Fatalf("Bundle exactly at the window boundary was refused: %v", err)
	}
	if _, err := source.RescindTransfer(ctx, boundary); err != nil {
		t.Fatal(err)
	}
}

func TestBundleArtifactExportRequiresExactMalwareEvidence(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "malware-gated", DisplayName: "Malware gated", BuildStrategy: BundleStrategySnapshot,
		Selections:       []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{"dev-use2"}, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"}}},
		IncludeArtifacts: true, RequireMalwarePass: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Build(ctx, "malware-gated"); !errors.Is(err, ErrConflict) {
		t.Fatalf("build without malware evidence error = %v", err)
	}
}

func TestBundleHTTPImportIsAtomicPerEnvironmentAndGoverned(t *testing.T) {
	ctx := context.Background()
	key := mustBundleKey(t)
	sourceRepository := NewMemoryRepository()
	if _, err := NewLifecycle(sourceRepository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	source, err := NewBundleService(sourceRepository, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.PutPipeline(ctx, ExportPipeline{
		ID: "governed", DisplayName: "Governed", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{"dev-use2"}, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"}}},
	}); err != nil {
		t.Fatal(err)
	}
	built, err := source.Build(ctx, "governed")
	if err != nil {
		t.Fatal(err)
	}

	targetRepository := NewMemoryRepository()
	governance := NewGovernanceService(targetRepository)
	for _, team := range []Team{
		{Name: "importers", DisplayName: "Importers", AdminGroups: []string{"importers"}, MemberGroups: []string{"importers"}},
		{Name: "bundle-reviewers", DisplayName: "Bundle reviewers", AdminGroups: []string{"reviewers"}, MemberGroups: []string{"reviewers"}},
	} {
		if _, err := governance.PutTeam(ctx, team); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{ResourceRID: environmentRID("dev-use2"), Team: "importers", Operations: []string{OperationImportBundle, OperationReadResource}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutPolicy(ctx, ApprovalPolicy{ID: "bundle-review", DisplayName: "Bundle review", Enabled: true, Expression: `type.startsWith("IMPORT_") ? require_team("bundle-reviewers", "review imported catalog") : no_op()`}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(targetRepository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BundlePrivateKey: key})
	response := governanceRequest(t, handler, http.MethodPost, "/v1/bundles/import", "importer", "importers", map[string]any{
		"bundle": built[0], "requireSecurityPass": true, "targetEnvironments": []string{"dev-use2"},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("import = %d %s", response.Code, response.Body.String())
	}
	var proposed struct {
		Imports        []BundleImport  `json:"imports"`
		ChangeRequests []ChangeRequest `json:"changeRequests"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &proposed); err != nil {
		t.Fatal(err)
	}
	if len(proposed.Imports) != 1 || proposed.Imports[0].Status != "PENDING_APPROVAL" || len(proposed.ChangeRequests) != 1 {
		t.Fatalf("proposed=%#v", proposed)
	}
	if _, err := targetRepository.GetImportedCatalog(ctx, "dev-use2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("catalog admitted before approval: %v", err)
	}
	response = governanceRequest(t, handler, http.MethodPost, "/v1/change-requests/"+proposed.ChangeRequests[0].ID+"/approve", "reviewer", "reviewers", map[string]string{"rationale": "catalog verified"})
	if response.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", response.Code, response.Body.String())
	}
	stored, err := targetRepository.GetBundleImport(ctx, proposed.Imports[0].ID)
	if err != nil || stored.Status != "ADMITTED" || stored.ImportedAt.IsZero() {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	catalog, err := targetRepository.GetImportedCatalog(ctx, "dev-use2")
	if err != nil || len(catalog.Resources) == 0 || !contains(catalog.BundleIDs, built[0].Manifest.ID) {
		t.Fatalf("catalog=%#v err=%v", catalog, err)
	}
}

func mustBundleKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	key, err := GenerateBundleSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// A Bundle manifest's identity is computed over its resources as an ordered
// slice, so the order they are selected in reaches the Bundle ID and the
// signature over it. Two builds of the same content must therefore agree on
// that order, and the comparator that guarantees it was unpinned.
//
// This drives a real build rather than re-sorting a fixture: the first version
// of this test duplicated the comparator inline, passed, and did not notice the
// production one being reversed.
func TestBundleResourceOrderIsDeterministicAndReachesTheIdentity(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "ordering-export", DisplayName: "Ordering", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	envelopes, err := service.Build(ctx, pipeline.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(envelopes) == 0 {
		t.Fatal("build produced no envelope")
	}
	resources := envelopes[0].Manifest.Resources
	if len(resources) < 2 {
		t.Fatalf("build selected %d resources; too few to order", len(resources))
	}

	// Ordering is by type, then by id within a type, and the build must emit it
	// already settled -- map iteration would otherwise vary between builds.
	for index := 1; index < len(resources); index++ {
		previous, current := resources[index-1], resources[index]
		if previous.Type > current.Type ||
			(previous.Type == current.Type && previous.ID >= current.ID) {
			t.Fatalf("resources are not ordered by (type, id) at %d: %s/%s then %s/%s",
				index, previous.Type, previous.ID, current.Type, current.ID)
		}
	}

	// The identity is order-sensitive, which is why the order has to be settled
	// before it is computed rather than left to map iteration.
	stable, err := bundleManifestIdentity(envelopes[0].Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if stable != envelopes[0].Manifest.ID {
		t.Fatalf("manifest id %s does not match its recomputed identity %s", envelopes[0].Manifest.ID, stable)
	}
	permuted := envelopes[0].Manifest
	permuted.Resources = append([]BundleResource(nil), resources...)
	permuted.Resources[0], permuted.Resources[len(permuted.Resources)-1] =
		permuted.Resources[len(permuted.Resources)-1], permuted.Resources[0]
	shuffled, err := bundleManifestIdentity(permuted)
	if err != nil {
		t.Fatal(err)
	}
	if shuffled == stable {
		t.Fatal("identity ignores resource order; settling that order would then be pointless")
	}
}

// An Export Pipeline's numeric bounds and its dependent-flag rules decide what
// a Bundle build is allowed to do before it starts moving artifacts between
// Hubs. Neither the bounds nor the dependencies were pinned.
func TestExportPipelineBoundsAndDependentFlags(t *testing.T) {
	t.Parallel()
	base := func(mutate func(*ExportPipeline)) ExportPipeline {
		pipeline := ExportPipeline{
			ID: "export", DisplayName: "Export", BuildStrategy: BundleStrategyIncremental,
			Selections: []BundleSelectionRule{{
				Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
				ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
			}},
		}
		mutate(&pipeline)
		return pipeline
	}
	rejects := func(pipeline ExportPipeline, field string) bool {
		for _, err := range validateExportPipeline(pipeline) {
			if strings.Contains(err.Path, field) {
				return true
			}
		}
		return false
	}

	for _, test := range []struct {
		name     string
		pipeline ExportPipeline
		field    string
		want     bool
	}{
		// Zero is "no checkpointing", not an invalid frequency.
		{name: "checkpoint unset", pipeline: base(func(*ExportPipeline) {}), field: "checkpointFrequency"},
		{name: "checkpoint at ceiling", pipeline: base(func(p *ExportPipeline) { p.CheckpointFrequency = 10000 }), field: "checkpointFrequency"},
		{name: "checkpoint over", pipeline: base(func(p *ExportPipeline) { p.CheckpointFrequency = 10001 }), field: "checkpointFrequency", want: true},
		{name: "checkpoint negative", pipeline: base(func(p *ExportPipeline) { p.CheckpointFrequency = -1 }), field: "checkpointFrequency", want: true},
		// Chunk size zero means the default; the ceiling is 64 MiB.
		{name: "chunk unset", pipeline: base(func(*ExportPipeline) {}), field: "chunkSizeBytes"},
		{name: "chunk at ceiling", pipeline: base(func(p *ExportPipeline) { p.ChunkSizeBytes = 64 * 1024 * 1024 }), field: "chunkSizeBytes"},
		{name: "chunk over", pipeline: base(func(p *ExportPipeline) { p.ChunkSizeBytes = 64*1024*1024 + 1 }), field: "chunkSizeBytes", want: true},
		{name: "chunk negative", pipeline: base(func(p *ExportPipeline) { p.ChunkSizeBytes = -1 }), field: "chunkSizeBytes", want: true},
		// Checkpointing only means anything for an incremental lineage.
		{
			name:  "checkpoint on a snapshot pipeline",
			field: "checkpointFrequency", want: true,
			pipeline: base(func(p *ExportPipeline) {
				p.BuildStrategy, p.CheckpointFrequency = BundleStrategySnapshot, 5
			}),
		},
		// Each of these asks for something only meaningful when the thing it
		// depends on is also on, so the pair is validated rather than the flag.
		{
			name: "malware pass without artifacts", field: "requireMalwarePass", want: true,
			pipeline: base(func(p *ExportPipeline) { p.RequireMalwarePass = true }),
		},
		{
			name: "malware pass with artifacts", field: "requireMalwarePass",
			pipeline: base(func(p *ExportPipeline) { p.RequireMalwarePass, p.IncludeArtifacts = true, true }),
		},
		{
			name: "layer dedup without artifacts", field: "enableLayerDedup", want: true,
			pipeline: base(func(p *ExportPipeline) { p.EnableLayerDedup = true }),
		},
		{
			name: "shared dedup without dedup", field: "shareLayerDedupAcrossPipelines", want: true,
			pipeline: base(func(p *ExportPipeline) { p.ShareLayerDedupAcrossPipelines, p.IncludeArtifacts = true, true }),
		},
		{
			name: "shared dedup with dedup", field: "shareLayerDedupAcrossPipelines",
			pipeline: base(func(p *ExportPipeline) {
				p.ShareLayerDedupAcrossPipelines, p.EnableLayerDedup, p.IncludeArtifacts = true, true, true
			}),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := rejects(test.pipeline, test.field); got != test.want {
				t.Fatalf("%s rejected=%v, want %v (%v)", test.field, got, test.want, validateExportPipeline(test.pipeline))
			}
		})
	}
}

// A Bundle build has three outcomes, not two: it succeeded, it succeeded with
// advisories, or it failed. Before advisories a Pipeline could name a Product,
// find nothing to ship for it, and report the same clean success as a build
// that shipped everything -- so an operator transferring the Bundle learned
// what it was missing only from whatever went wrong downstream.
func TestBundleBuildAdvisesWhenASelectedProductShipsNothing(t *testing.T) {
	ctx := context.Background()

	build := func(t *testing.T, selections []BundleSelectionRule) []BundleEnvelope {
		t.Helper()
		repository := NewMemoryRepository()
		if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "export", DisplayName: "Export", BuildStrategy: BundleStrategySnapshot, Selections: selections,
		}); err != nil {
			t.Fatal(err)
		}
		bundles, err := service.Build(ctx, "export")
		if err != nil {
			t.Fatalf("a Product that ships nothing must advise, not fail the build: %v", err)
		}
		return bundles
	}

	subjects := func(t *testing.T, bundles []BundleEnvelope) []string {
		t.Helper()
		advisories := bundles[0].Manifest.Advisories
		if len(advisories) == 0 {
			return nil
		}
		// Select the advisory this test is about. A build can carry more than one
		// kind -- these fixtures ask for MINIMAL, which is separately advised --
		// and asserting on the whole list would make this test fail whenever an
		// unrelated advisory is added.
		var omissions []BundleAdvisory
		for _, advisory := range advisories {
			if advisory.Kind == BundleAdvisoryNoProductReleases {
				omissions = append(omissions, advisory)
			}
		}
		if len(omissions) != 1 {
			t.Fatalf("advisories = %#v, want the omissions grouped into one", advisories)
		}
		return omissions[0].Subjects
	}

	product := func(ids []string, channels ...string) BundleSelectionRule {
		return BundleSelectionRule{
			Type: BundleSelectProduct, IDs: ids, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: channels,
		}
	}

	t.Run("names a Product that is not in the catalog at all", func(t *testing.T) {
		bundles := build(t, []BundleSelectionRule{product([]string{"app.example:example-api", "app.example:absent"}, "RELEASE")})
		if got := subjects(t, bundles); len(got) != 1 || got[0] != "app.example:absent" {
			t.Errorf("subjects = %v, want only app.example:absent", got)
		}
		// The Product that did resolve is still in the Bundle: an advisory
		// reports an omission, it does not abandon the build.
		var shipped bool
		for _, resource := range bundles[0].Manifest.Resources {
			if resource.ID == "app.example:example-api:0.1.0" {
				shipped = true
			}
		}
		if !shipped {
			t.Error("the Product that did resolve was not exported")
		}
	})

	t.Run("names a Product held on no requested Release Channel", func(t *testing.T) {
		// The Product exists and has a Release; the Release is not on the
		// Channel the rule asked for. This is the case the advisory is really
		// about -- "absent from the catalog" is already an error elsewhere.
		bundles := build(t, []BundleSelectionRule{product([]string{"app.example:example-api"}, "PREVIEW")})
		if got := subjects(t, bundles); len(got) != 1 || got[0] != "app.example:example-api" {
			t.Errorf("subjects = %v, want only app.example:example-api", got)
		}
	})

	t.Run("stays silent when every selected Product ships", func(t *testing.T) {
		bundles := build(t, []BundleSelectionRule{product([]string{"app.example:example-api"}, "RELEASE")})
		if got := bundles[0].Manifest.Advisories; got != nil {
			t.Errorf("advisories = %#v on a build that omitted nothing", got)
		}
	})

	t.Run("does not name a Product a later rule supplied", func(t *testing.T) {
		// Whether a Product is in the Bundle is not knowable rule by rule. A
		// per-rule advisory would report an omission that the finished Bundle
		// contradicts, which is worse than no advisory at all.
		bundles := build(t, []BundleSelectionRule{
			product([]string{"app.example:example-api"}, "PREVIEW"),
			product([]string{"app.example:example-api"}, "RELEASE"),
		})
		if got := bundles[0].Manifest.Advisories; got != nil {
			t.Errorf("advisories = %#v, but a later rule supplied the Product", got)
		}
	})

	t.Run("names a Product once however many Channels it was asked for on", func(t *testing.T) {
		bundles := build(t, []BundleSelectionRule{product([]string{"app.example:absent"}, "RELEASE", "PREVIEW", "SNAPSHOT")})
		if got := subjects(t, bundles); len(got) != 1 {
			t.Errorf("subjects = %v, want the Product reported once", got)
		}
	})

	t.Run("names a Product once however many rules missed it", func(t *testing.T) {
		bundles := build(t, []BundleSelectionRule{
			product([]string{"app.example:absent"}, "PREVIEW"),
			product([]string{"app.example:absent"}, "SNAPSHOT"),
		})
		if got := subjects(t, bundles); len(got) != 1 {
			t.Errorf("subjects = %v, want the Product reported once", got)
		}
	})

	t.Run("orders the Products it names", func(t *testing.T) {
		// Subjects reach an operator's screen and a Bundle's signed manifest.
		// Ranging over the selection would order them by the rules, but the
		// misses within one rule come from a map, so without sorting the same
		// Pipeline advises differently on each build.
		bundles := build(t, []BundleSelectionRule{product([]string{"app.example:zulu", "app.example:alpha", "app.example:mike"}, "RELEASE")})
		got := subjects(t, bundles)
		if strings.Join(got, ",") != "app.example:alpha,app.example:mike,app.example:zulu" {
			t.Errorf("subjects = %v, want them sorted", got)
		}
	})

	t.Run("says nothing about a rule that asked for no Releases", func(t *testing.T) {
		// A NONE rule selects resources other than Releases. Reporting the
		// Products it named as omitted would advise on every such rule.
		bundles := build(t, []BundleSelectionRule{
			{Type: BundleSelectProduct, IDs: []string{"app.example:absent"}, ReleaseStrategy: BundleReleasesNone},
			product([]string{"app.example:example-api"}, "RELEASE"),
		})
		if got := bundles[0].Manifest.Advisories; got != nil {
			t.Errorf("advisories = %#v for a rule that asked for no Releases", got)
		}
	})

	t.Run("does not blame the Release Channels for a Product its own labels excluded", func(t *testing.T) {
		// The Product has a Release, and it is on the requested Channel; the
		// Pipeline's own ExcludedLabels then drop it. Reconciling against the
		// finished Bundle alone would report it, and report it as having no
		// Release on its Channels -- which is false, and points whoever reads
		// it at the Release Channels instead of at the label rule they wrote.
		// A deliberate label exclusion may deserve an advisory of its own; it
		// does not deserve this one.
		repository := NewMemoryRepository()
		state := assessedFoundation()
		state.Releases[0].Labels = map[string]string{"do-not-export": "true"}
		if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "excluded", DisplayName: "Excluded", BuildStrategy: BundleStrategySnapshot,
			Selections: []BundleSelectionRule{product([]string{state.Releases[0].Product}, "RELEASE")},
			ExcludedLabels: []BundleLabelRequirement{{
				ResourceTypes: []string{BundleLabelProductRelease}, Name: "do-not-export",
			}},
		}); err != nil {
			t.Fatal(err)
		}
		bundles, err := service.Build(ctx, "excluded")
		if err != nil {
			t.Fatal(err)
		}
		for _, resource := range bundles[0].Manifest.Resources {
			if resource.Type == "PRODUCT_RELEASE" {
				t.Fatalf("the excluded Release was exported after all: %s", resource.ID)
			}
		}
		if got := bundles[0].Manifest.Advisories; got != nil {
			t.Errorf("advisories = %#v, but the Product does have a Release on the requested Channel", got)
		}
	})

	t.Run("does not blame the Release Channels on an incremental build carrying history", func(t *testing.T) {
		// An incremental Pipeline's second build selects the whole Release
		// history rather than the latest, down a separate branch that has to
		// account for what it satisfied in its own terms. Missing that, every
		// Product it ships would be reconciled purely against the finished
		// Bundle, and a label-excluded one would again be blamed on its
		// Release Channels.
		repository := NewMemoryRepository()
		state := assessedFoundation()
		state.Releases[0].Labels = map[string]string{"do-not-export": "true"}
		if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "history", DisplayName: "History", BuildStrategy: BundleStrategyIncremental,
			Selections: []BundleSelectionRule{{
				Type: BundleSelectProduct, IDs: []string{state.Releases[0].Product},
				ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
			}},
			ExcludedLabels: []BundleLabelRequirement{{
				ResourceTypes: []string{BundleLabelProductRelease}, Name: "do-not-export",
			}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Build(ctx, "history"); err != nil {
			t.Fatal(err)
		}
		// Only now does a build carry history: the branch needs a predecessor.
		bundles, err := service.Build(ctx, "history")
		if err != nil {
			t.Fatal(err)
		}
		if got := bundles[0].Manifest.Advisories; got != nil {
			t.Errorf("advisories = %#v, but the Product does have a Release on the requested Channel", got)
		}
	})

	t.Run("advises from the pre-build preview, before anything is built", func(t *testing.T) {
		// A preview exists so an operator can see what a build would produce
		// without producing it. An omission only discoverable by building is
		// the one thing a preview is meant to save them from.
		repository := NewMemoryRepository()
		if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "previewed", DisplayName: "Previewed", BuildStrategy: BundleStrategySnapshot,
			Selections: []BundleSelectionRule{product([]string{"app.example:absent"}, "RELEASE")},
		}); err != nil {
			t.Fatal(err)
		}
		preview, err := service.PreviewExportPipeline(ctx, "previewed")
		if err != nil {
			t.Fatal(err)
		}
		if len(preview.Advisories) != 1 || len(preview.Advisories[0].Subjects) != 1 ||
			preview.Advisories[0].Subjects[0] != "app.example:absent" {
			t.Fatalf("preview advisories = %#v", preview.Advisories)
		}
		if bundles, err := service.Build(ctx, "previewed"); err != nil {
			t.Fatal(err)
		} else if len(bundles[0].Manifest.Advisories) != 1 {
			// A preview that advises differently from the build it previews is
			// worse than one that does not advise at all.
			t.Errorf("the build did not repeat what the preview advised: %#v", bundles[0].Manifest.Advisories)
		}
	})

	t.Run("carries the advisory in the signed manifest and omits the field otherwise", func(t *testing.T) {
		// The manifest is what crosses to the other Hub. An advisory the
		// receiving side cannot see is not an advisory.
		advised, err := json.Marshal(build(t, []BundleSelectionRule{product([]string{"app.example:absent"}, "RELEASE")})[0].Manifest)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(advised), `"advisories"`) || !strings.Contains(string(advised), "app.example:absent") {
			t.Errorf("manifest does not carry the advisory: %s", advised)
		}
		clean, err := json.Marshal(build(t, []BundleSelectionRule{product([]string{"app.example:example-api"}, "RELEASE")})[0].Manifest)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(clean), `"advisories"`) {
			t.Errorf("a clean build emitted an advisories field: %s", clean)
		}
	})
}

// A recall says a Release must not be installed anywhere. Nothing in the Bundle
// path knew that, so a recalled Release was still selected and still shipped --
// and the receiving Hub, which trusts the Bundle as its catalog, learned
// nothing about the recall.
func TestBundleSelectionExcludesRecalledReleases(t *testing.T) {
	ctx := context.Background()
	const (
		report  = "app.example:example-report"
		console = "app.example:example-console"
	)

	// Two versions of one Product, so a recall on the newer leaves something
	// to fall back to and the difference between "excluded" and "nothing
	// shipped" stays visible.
	// A Product no Entity runs. Recalling a Release an Entity has selected
	// makes the desired state invalid -- correctly, and separately tested --
	// which would stop these cases reaching the Bundle path at all.
	catalogued := func(state *DesiredState, product string, versions ...string) {
		for _, version := range versions {
			release := cloneJSON(state.Releases[0])
			release.Product = product
			release.Version = version
			release.Chart.Tag = version
			release.Images[0].Tag = version
			release.Dependencies = nil
			state.Releases = append(state.Releases, release)
		}
	}

	build := func(t *testing.T, recalls []Recall, selections []BundleSelectionRule) BundleEnvelope {
		t.Helper()
		state := assessedFoundation()
		catalogued(&state, report, "0.0.9", "0.1.0")
		state.Recalls = recalls
		repository := NewMemoryRepository()
		if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "recalls", DisplayName: "Recalls", BuildStrategy: BundleStrategySnapshot, Selections: selections,
		}); err != nil {
			t.Fatal(err)
		}
		bundles, err := service.Build(ctx, "recalls")
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		return bundles[0]
	}
	shipped := func(bundle BundleEnvelope) []string {
		versions := make([]string, 0)
		for _, resource := range bundle.Manifest.Resources {
			if resource.Type == "PRODUCT_RELEASE" {
				versions = append(versions, resource.ID)
			}
		}
		sort.Strings(versions)
		return versions
	}
	recallOf := func(product, version string) Recall {
		return Recall{
			ID: "rcl_" + version, Product: product, StartingVersion: version, SingleVersion: true,
			Reason: "a test recall", Strategy: RecallStrategy{Type: RecallRollForward},
		}
	}
	product := func(ids []string, channels ...string) BundleSelectionRule {
		return BundleSelectionRule{
			Type: BundleSelectProduct, IDs: ids, ReleaseStrategy: BundleReleasesAll, ReleaseChannels: channels,
		}
	}

	t.Run("falls back to the newest version that is not recalled", func(t *testing.T) {
		bundle := build(t, []Recall{recallOf(report, "0.1.0")}, []BundleSelectionRule{product([]string{report}, "RELEASE")})
		got := shipped(bundle)
		if len(got) != 1 || got[0] != report+":0.0.9" {
			t.Errorf("shipped %v, want only the unrecalled %s:0.0.9", got, report)
		}
		if bundle.Manifest.Advisories != nil {
			t.Errorf("advisories = %#v, but a Release was shipped", bundle.Manifest.Advisories)
		}
	})

	t.Run("advises when every Release of a Product is recalled", func(t *testing.T) {
		// This is the case upstream's advisory names: no *unrecalled* Product
		// Releases on the specified Release Channels.
		bundle := build(t, []Recall{recallOf(report, "0.0.9"), recallOf(report, "0.1.0")},
			[]BundleSelectionRule{product([]string{report}, "RELEASE")})
		if got := shipped(bundle); len(got) != 0 {
			t.Errorf("shipped %v, want nothing", got)
		}
		advisories := bundle.Manifest.Advisories
		if len(advisories) != 1 || advisories[0].Kind != BundleAdvisoryNoProductReleases ||
			len(advisories[0].Subjects) != 1 || advisories[0].Subjects[0] != report {
			t.Fatalf("advisories = %#v", advisories)
		}
		if !strings.Contains(advisories[0].Message, "unrecalled") {
			t.Errorf("message %q does not say the Releases were recalled", advisories[0].Message)
		}
	})

	t.Run("a reverted recall excludes nothing", func(t *testing.T) {
		reverted := recallOf(report, "0.1.0")
		reverted.Reverted = true
		bundle := build(t, []Recall{reverted}, []BundleSelectionRule{product([]string{report}, "RELEASE")})
		if got := shipped(bundle); len(got) != 1 || got[0] != report+":0.1.0" {
			t.Errorf("shipped %v, want the reverted recall to be ignored", got)
		}
	})

	t.Run("a recalled Release does not return as somebody else's dependency", func(t *testing.T) {
		// Dependency closure reads the catalog directly, so without a recall
		// check there, excluding a Release from selection accomplishes
		// nothing: it walks back in as something else's requirement.
		dependent := func(t *testing.T, minimum string, recalls []Recall) ([]string, error) {
			t.Helper()
			state := assessedFoundation()
			catalogued(&state, report, "0.0.9", "0.1.0")
			catalogued(&state, console, "1.0.0")
			state.Releases[len(state.Releases)-1].Dependencies = []Dependency{
				{Product: report, MinimumVersion: minimum, MaximumVersion: "0.1.999"},
			}
			state.Recalls = recalls
			repository := NewMemoryRepository()
			if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
				t.Fatal(err)
			}
			service, err := NewBundleService(repository, mustBundleKey(t), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.PutPipeline(ctx, ExportPipeline{
				ID: "dependency", DisplayName: "Dependency", BuildStrategy: BundleStrategySnapshot,
				Selections: []BundleSelectionRule{product([]string{console}, "RELEASE")},
			}); err != nil {
				t.Fatal(err)
			}
			bundles, err := service.Build(ctx, "dependency")
			if err != nil {
				return nil, err
			}
			return shipped(bundles[0]), nil
		}

		t.Run("takes an older version in the same range", func(t *testing.T) {
			got, err := dependent(t, "0.0.9", []Recall{recallOf(report, "0.1.0")})
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			for _, coordinate := range got {
				if coordinate == report+":0.1.0" {
					t.Fatalf("the recalled Release returned through dependency closure: %v", got)
				}
			}
			if !containsString(got, report+":0.0.9") {
				t.Errorf("shipped %v, want the unrecalled version of the dependency", got)
			}
		})

		t.Run("fails the build when the whole range is recalled", func(t *testing.T) {
			// The alternative is shipping a Bundle whose dependency was
			// recalled, which is the thing a recall exists to stop.
			_, err := dependent(t, "0.1.0", []Recall{recallOf(report, "0.1.0")})
			if err == nil {
				t.Fatal("a Bundle built with its only dependency recalled")
			}
			if !strings.Contains(err.Error(), "unavailable dependency") {
				t.Errorf("error = %v, want it to name the unavailable dependency", err)
			}
		})
	})

	t.Run("historical Releases are filtered too", func(t *testing.T) {
		// An incremental Pipeline's later builds select the whole history down
		// a separate branch, which would otherwise carry the recalled Release
		// even though the latest-only path excludes it.
		state := assessedFoundation()
		catalogued(&state, report, "0.0.9", "0.1.0")
		state.Recalls = []Recall{recallOf(report, "0.1.0")}
		repository := NewMemoryRepository()
		if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "history", DisplayName: "History", BuildStrategy: BundleStrategyIncremental,
			Selections: []BundleSelectionRule{{
				Type: BundleSelectProduct, IDs: []string{report},
				ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
			}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Build(ctx, "history"); err != nil {
			t.Fatal(err)
		}
		bundles, err := service.Build(ctx, "history")
		if err != nil {
			t.Fatal(err)
		}
		for _, coordinate := range shipped(bundles[0]) {
			if coordinate == report+":0.1.0" {
				t.Fatalf("history carried the recalled Release: %v", shipped(bundles[0]))
			}
		}
	})

	t.Run("an explicitly named Release is still exported", func(t *testing.T) {
		// Deliberate, and the narrow scope of this change. Naming an exact
		// coordinate is an operator overriding the catalog, and dropping it
		// silently is the failure the advisories exist to prevent. Whether an
		// override should have to be declared is a separate question from
		// whether selection should honour recalls.
		bundle := build(t, []Recall{recallOf(report, "0.1.0")}, []BundleSelectionRule{
			{Type: BundleSelectRelease, IDs: []string{report + ":0.1.0"}},
		})
		if got := shipped(bundle); len(got) != 1 || got[0] != report+":0.1.0" {
			t.Errorf("shipped %v, want the explicitly named Release", got)
		}
	})
}

// Chunk boundaries, at the two lengths that expose them. A mutation sweep found both the loop bound
// and the default chunk size unpinned: the existing artifact test uses a 31-byte blob at a chunk
// size of 7, which is not an exact multiple, so the loop's last step always overshoots and the
// off-by-one never shows.
func TestBundleChunkingIsExactAtTheBoundaries(t *testing.T) {
	ctx := context.Background()

	build := func(t *testing.T, contents []byte, chunkSize int) BundleEnvelope {
		t.Helper()
		sum := sha256.Sum256(contents)
		digest := sha256Digest(sum)
		state := assessedFoundation()
		state.Environment.Generation = "chunkboundary001"
		for index := range state.Releases {
			state.Releases[index].Chart.Digest = digest
			state.Releases[index].Images[0].Digest = digest
			state.Releases[index].VulnerabilityAssessment.ArtifactDigests = []string{digest}
		}
		repository := NewMemoryRepository()
		if _, _, err := repository.PutBlob(ctx, digest, "application/vnd.oci.image.layer.v1.tar",
			bytes.NewReader(contents)); err != nil {
			t.Fatal(err)
		}
		if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "chunks", DisplayName: "Chunks", BuildStrategy: BundleStrategySnapshot,
			Selections: []BundleSelectionRule{{
				Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
				ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
			}},
			IncludeArtifacts: true, ChunkSizeBytes: chunkSize,
		}); err != nil {
			t.Fatal(err)
		}
		bundles, err := service.Build(ctx, "chunks")
		if err != nil {
			t.Fatal(err)
		}
		return bundles[0]
	}

	t.Run("a length that divides exactly yields no trailing empty chunk", func(t *testing.T) {
		// 16 bytes at a chunk size of 8. The final offset lands exactly on the length, and a
		// loop that runs while offset <= len takes one more step and emits a zero-byte chunk --
		// a chunk the receiving Hub must fetch, verify and store for nothing.
		exact := make([]byte, 16)
		for index := range exact {
			exact[index] = byte(index)
		}
		bundle := build(t, exact, 8)
		layer := bundle.Manifest.ArtifactLayers[0]
		if len(layer.ChunkDigests) != 2 {
			t.Fatalf("chunk digests = %d, want exactly 2 for 16 bytes at 8", len(layer.ChunkDigests))
		}
		for _, chunk := range bundle.Chunks {
			if chunk.Size == 0 {
				t.Errorf("an empty chunk was emitted: %s", chunk.Digest)
			}
		}
	})

	t.Run("a length that does not divide keeps its short tail", func(t *testing.T) {
		// Distinct bytes throughout: identical chunks are content-addressed and collapse to one
		// payload, which would hide a miscounted tail.
		varied := make([]byte, 20)
		for index := range varied {
			varied[index] = byte(index)
		}
		bundle := build(t, varied, 8)
		layer := bundle.Manifest.ArtifactLayers[0]
		if len(layer.ChunkDigests) != 3 {
			t.Fatalf("chunk digests = %d, want 8+8+4", len(layer.ChunkDigests))
		}
		var sizes []int
		for _, chunk := range bundle.Chunks {
			sizes = append(sizes, chunk.Size)
		}
		sort.Ints(sizes)
		if len(sizes) != 3 || sizes[0] != 4 || sizes[1] != 8 || sizes[2] != 8 {
			t.Errorf("chunk sizes = %v, want 4, 8, 8", sizes)
		}
	})

	t.Run("an unset chunk size uses the whole-artifact default", func(t *testing.T) {
		// ChunkSizeBytes 0 selects the 4 MiB default. Anything smaller than that is one chunk,
		// so a default that shrinks shows up immediately as a split.
		bundle := build(t, bytes.Repeat([]byte("c"), 4096), 0)
		layer := bundle.Manifest.ArtifactLayers[0]
		if len(layer.ChunkDigests) != 1 {
			t.Errorf("a 4 KiB artifact split into %d chunks; the default chunk size is 4 MiB",
				len(layer.ChunkDigests))
		}
	})
}

// The whole-history selection branch, and the checkpoint that punctuates it. A mutation sweep found
// three guards here unpinned, all of them reachable only from an incremental pipeline's SECOND
// build — the first has no lineage, so it never takes the branch.
func TestIncrementalHistorySelectionAndCheckpointSequence(t *testing.T) {
	ctx := context.Background()
	const product = "app.example:example-report"

	// Two unrecalled versions, so shipping the history and shipping the latest differ observably.
	// With one version each, or with the older one recalled, the two are indistinguishable and the
	// branch could be skipped unnoticed.
	build := func(t *testing.T, channels []string, frequency int) []BundleEnvelope {
		t.Helper()
		state := assessedFoundation()
		for _, version := range []string{"0.0.9", "0.1.0"} {
			release := cloneJSON(state.Releases[0])
			release.Product, release.Version = product, version
			release.Chart.Tag, release.Images[0].Tag = version, version
			release.Dependencies = nil
			state.Releases = append(state.Releases, release)
		}
		repository := NewMemoryRepository()
		if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "history", DisplayName: "History", BuildStrategy: BundleStrategyIncremental,
			CheckpointFrequency: frequency,
			Selections: []BundleSelectionRule{{
				Type: BundleSelectProduct, IDs: []string{product},
				ReleaseStrategy: BundleReleasesAll, ReleaseChannels: channels,
			}},
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Build(ctx, "history"); err != nil {
			t.Fatal(err)
		}
		second, err := service.Build(ctx, "history")
		if err != nil {
			t.Fatal(err)
		}
		return second
	}
	shipped := func(bundles []BundleEnvelope) map[string]bool {
		seen := map[string]bool{}
		for _, bundle := range bundles {
			for _, resource := range bundle.Manifest.Resources {
				if resource.Type == "PRODUCT_RELEASE" {
					seen[resource.ID] = true
				}
			}
		}
		return seen
	}

	t.Run("the checkpoint carries every version, not only the newest", func(t *testing.T) {
		// The incremental Bundle itself ships only the delta, so the history selection is
		// invisible there — the first build already sent both. The CHECKPOINT is where it
		// shows: a checkpoint summarises the whole selected set, not the change.
		bundles := build(t, []string{"RELEASE"}, 1)
		if len(bundles) != 2 {
			t.Fatalf("expected a Bundle and its checkpoint, got %d", len(bundles))
		}
		got := shipped(bundles[1:])
		for _, version := range []string{"0.0.9", "0.1.0"} {
			if !got[product+":"+version] {
				t.Errorf("%s is absent from the checkpoint; an incremental build selects the "+
					"whole Release history", version)
			}
		}
	})

	t.Run("a checkpoint follows the Bundle it summarises", func(t *testing.T) {
		// CheckpointFrequency 1 makes every incremental build emit one. Its sequence must be the
		// next one after the Bundle it checkpoints, or the lineage reads out of order.
		bundles := build(t, []string{"RELEASE"}, 1)
		if len(bundles) != 2 {
			t.Fatalf("expected a Bundle and its checkpoint, got %d", len(bundles))
		}
		if bundles[1].Manifest.Kind != BundleCheckpoint {
			t.Fatalf("second Bundle is %s, not a checkpoint", bundles[1].Manifest.Kind)
		}
		if bundles[1].Manifest.Sequence != bundles[0].Manifest.Sequence+1 {
			t.Errorf("checkpoint sequence %d does not follow %d",
				bundles[1].Manifest.Sequence, bundles[0].Manifest.Sequence)
		}
		if bundles[1].Manifest.CheckpointOf != bundles[0].Manifest.ID {
			t.Errorf("checkpoint names %q, not the Bundle it summarises %q",
				bundles[1].Manifest.CheckpointOf, bundles[0].Manifest.ID)
		}
	})
}

// An Environment or Module rule may name no Release Channel at all — the validator exempts exactly
// those two — and an empty channel set means "do not filter", not "match nothing". Read the other
// way round, such a Pipeline silently ships no Product Releases and the build still reports
// success. A mutation sweep found the guard unpinned: every other test names a Channel.
func TestASelectionRuleWithNoReleaseChannelTakesEveryChannel(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "unchannelled", DisplayName: "Unchannelled", BuildStrategy: BundleStrategyIncremental,
		CheckpointFrequency: 1,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, // no ReleaseChannels
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Build(ctx, "unchannelled"); err != nil {
		t.Fatal(err)
	}
	// The second build takes the whole-history branch, and its checkpoint carries the full set.
	bundles, err := service.Build(ctx, "unchannelled")
	if err != nil {
		t.Fatal(err)
	}
	if len(bundles) != 2 {
		t.Fatalf("expected a Bundle and its checkpoint, got %d", len(bundles))
	}
	releases := 0
	for _, resource := range bundles[1].Manifest.Resources {
		if resource.Type == "PRODUCT_RELEASE" {
			releases++
		}
	}
	if releases == 0 {
		t.Error("a rule naming no Release Channel shipped no Product Release; an empty channel " +
			"set must not filter everything out")
	}
}

// Three guards the sweep found unreached, each a bound nothing had stood on.
func TestBundleBoundsAtTheirEdges(t *testing.T) {
	t.Parallel()

	t.Run("a schedule failure message is truncated at its bound", func(t *testing.T) {
		// The message is persisted on the Pipeline's schedule run. Unbounded, one runaway error
		// carries an arbitrary amount of text into every subsequent read of that record.
		short := boundedExportScheduleFailure(errors.New(strings.Repeat("e", 1024)))
		if len(short) != 1024 {
			t.Errorf("a message exactly at the bound was altered: %d", len(short))
		}
		long := boundedExportScheduleFailure(errors.New(strings.Repeat("e", 1025)))
		if len(long) != 1024 {
			t.Errorf("a message past the bound was kept at %d", len(long))
		}
		if got := boundedExportScheduleFailure(errors.New("  spaced  ")); got != "spaced" {
			t.Errorf("surrounding space survived: %q", got)
		}
	})

	t.Run("every published selection rule type is accepted", func(t *testing.T) {
		// The five documented rule types. Dropping any one from the accepted set refuses a
		// Pipeline the contract allows, and nothing named them together.
		ctx := context.Background()
		repository := NewMemoryRepository()
		if _, err := NewLifecycle(repository).CreatePlan(ctx, assessedFoundation()); err != nil {
			t.Fatal(err)
		}
		service, err := NewBundleService(repository, mustBundleKey(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, ruleType := range []string{
			BundleSelectEnvironment, BundleSelectModule, BundleSelectProduct,
			BundleSelectRelease, BundleSelectTeam,
		} {
			rule := BundleSelectionRule{Type: ruleType, IDs: []string{"anything"}}
			if ruleType != BundleSelectRelease && ruleType != BundleSelectTeam {
				rule.ReleaseStrategy, rule.ReleaseChannels = BundleReleasesAll, []string{"RELEASE"}
			}
			_, err := service.PutPipeline(ctx, ExportPipeline{
				ID: "types-" + strings.ToLower(ruleType), DisplayName: "Types",
				BuildStrategy: BundleStrategySnapshot, Selections: []BundleSelectionRule{rule},
			})
			if err != nil && strings.Contains(err.Error(), "must be ENVIRONMENT") {
				t.Errorf("%s was refused as an unknown selection rule type", ruleType)
			}
		}
		unknown := BundleSelectionRule{
			Type: "SOMETHING_ELSE", IDs: []string{"x"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}
		if _, err := service.PutPipeline(ctx, ExportPipeline{
			ID: "types-unknown", DisplayName: "Types", BuildStrategy: BundleStrategySnapshot,
			Selections: []BundleSelectionRule{unknown},
		}); err == nil {
			t.Error("an unknown selection rule type was accepted")
		}
	})
}

// An alternate build is a re-issue of an older Pipeline revision, standing outside
// the incremental lineage. It must not reach for Release history the way a lineage
// build does: it has no predecessor to be a delta against, so history would put
// every superseded version into a Bundle asked for one revision's worth of state.
func TestAlternateBuildDoesNotReachForReleaseHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, channelFoundation("0.1.0", "0.2.0")); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "history-export", DisplayName: "History export", BuildStrategy: BundleStrategyIncremental,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	releasesIn := func(envelope BundleEnvelope) []string {
		var versions []string
		for _, resource := range envelope.Manifest.Resources {
			if resource.Type == "PRODUCT_RELEASE" && !resource.Deleted {
				versions = append(versions, resource.ID)
			}
		}
		sort.Strings(versions)
		return versions
	}

	// The first build establishes the lineage; it carries only the latest Release.
	first, err := service.Build(ctx, pipeline.ID)
	if err != nil || len(first) != 1 {
		t.Fatalf("lineage build = %#v, %v", first, err)
	}
	if got := releasesIn(first[0]); len(got) != 1 || !strings.HasSuffix(got[0], ":0.2.0") {
		t.Fatalf("first Bundle carried %v, wanted only the latest Release", got)
	}

	// A second lineage build is a delta, and this is where history is wanted: the
	// superseded Release is what the delta adds.
	second, err := service.Build(ctx, pipeline.ID)
	if err != nil || len(second) != 1 {
		t.Fatalf("incremental build = %#v, %v", second, err)
	}
	if got := releasesIn(second[0]); len(got) != 1 || !strings.HasSuffix(got[0], ":0.1.0") {
		t.Fatalf("incremental delta carried %v, wanted the superseded Release", got)
	}

	// The alternate build stands outside that lineage, so it gets the same single
	// Release the first build did -- not the history the delta was entitled to.
	alternate, err := service.BuildRevisionForTarget(ctx, pipeline.ID, pipeline.Revision, "", true)
	if err != nil || len(alternate) != 1 {
		t.Fatalf("alternate build = %#v, %v", alternate, err)
	}
	if !alternate[0].Manifest.LineageIndependent {
		t.Fatalf("alternate build joined the lineage: %#v", alternate[0].Manifest)
	}
	if got := releasesIn(alternate[0]); len(got) != 1 || !strings.HasSuffix(got[0], ":0.2.0") {
		t.Errorf("alternate build carried Release history: %v", got)
	}
}

// MINIMAL is accepted by validation and then not honoured. Until it is
// implemented, the build has to say so: the Bundle it produces is
// indistinguishable from one that honoured the request, and the difference only
// surfaces at the far end, where the upgrade cannot complete for want of the
// intermediate Releases.
func TestMinimalReleaseStrategyIsReportedAsUnimplemented(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, channelFoundation("0.1.0", "0.2.0")); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	build := func(t *testing.T, id, strategy string) BundleEnvelope {
		t.Helper()
		pipeline, err := service.PutPipeline(ctx, ExportPipeline{
			ID: id, DisplayName: id, BuildStrategy: BundleStrategySnapshot,
			Selections: []BundleSelectionRule{{
				Type: BundleSelectEnvironment, IDs: []string{"dev-use2"},
				ReleaseStrategy: strategy, ReleaseChannels: []string{"RELEASE"},
			}},
		})
		if err != nil {
			t.Fatal(err)
		}
		built, err := service.Build(ctx, pipeline.ID)
		if err != nil || len(built) != 1 {
			t.Fatalf("%s build = %#v, %v", strategy, built, err)
		}
		return built[0]
	}

	minimal := build(t, "minimal-export", BundleReleasesMinimal)
	var advisory *BundleAdvisory
	for index, candidate := range minimal.Manifest.Advisories {
		if candidate.Kind == BundleAdvisoryUnimplementedStrategy {
			advisory = &minimal.Manifest.Advisories[index]
		}
	}
	if advisory == nil {
		t.Fatalf("a MINIMAL build carried no advisory: %#v", minimal.Manifest.Advisories)
	}
	if len(advisory.Subjects) != 1 || advisory.Subjects[0] != BundleSelectEnvironment+":dev-use2" {
		t.Errorf("the advisory named %v, not the rule that asked", advisory.Subjects)
	}

	// The advisory is about the request, not the result: a build that asked for
	// ALL produces the same contents here and must NOT carry it.
	all := build(t, "all-export", BundleReleasesAll)
	for _, candidate := range all.Manifest.Advisories {
		if candidate.Kind == BundleAdvisoryUnimplementedStrategy {
			t.Errorf("an ALL build was reported as unimplemented: %#v", candidate)
		}
	}

	// And the report does not change what ships. Both builds carry the same
	// Releases, which is precisely why the advisory is needed.
	releases := func(envelope BundleEnvelope) []string {
		var versions []string
		for _, resource := range envelope.Manifest.Resources {
			if resource.Type == "PRODUCT_RELEASE" && !resource.Deleted {
				versions = append(versions, resource.ID)
			}
		}
		sort.Strings(versions)
		return versions
	}
	if strings.Join(releases(minimal), ",") != strings.Join(releases(all), ",") {
		t.Errorf("MINIMAL and ALL diverged: %v against %v", releases(minimal), releases(all))
	}
}
