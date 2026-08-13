package controlplane

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestExportPipelineScheduleValidationAndDefaults(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, err := NewBundleService(NewMemoryRepository(), mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	base := ExportPipeline{
		ID: "scheduled", DisplayName: "Scheduled", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{"prod"}, ReleaseStrategy: BundleReleasesNone}},
	}
	base.Schedule = &ExportPipelineSchedule{Enabled: true}
	stored, err := service.PutPipeline(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Schedule.Cron != DefaultExportCron || stored.Schedule.TimeZone != "UTC" {
		t.Fatalf("schedule defaults = %#v", stored.Schedule)
	}

	invalidCron := cloneJSON(base)
	invalidCron.ID, invalidCron.Revision = "invalid-cron", 0
	invalidCron.Schedule = &ExportPipelineSchedule{Enabled: true, Cron: "not a cron", TimeZone: "UTC"}
	if _, err := service.PutPipeline(ctx, invalidCron); err == nil {
		t.Fatal("invalid cron was accepted")
	}
	invalidZone := cloneJSON(base)
	invalidZone.ID, invalidZone.Revision = "invalid-zone", 0
	invalidZone.Schedule = &ExportPipelineSchedule{Enabled: true, Cron: "0 12 * * *", TimeZone: "Mars/Olympus"}
	if _, err := service.PutPipeline(ctx, invalidZone); err == nil {
		t.Fatal("invalid timezone was accepted")
	}
	if !exportPipelineScheduleDue(ExportPipeline{
		Status:   ExportPipelineActive,
		Schedule: &ExportPipelineSchedule{Enabled: true, Cron: "0 12 * * *", TimeZone: "America/New_York"},
	}, time.Date(2026, 8, 3, 16, 0, 0, 0, time.UTC)) {
		t.Fatal("timezone-aware noon schedule was not due at 16:00 UTC during daylight time")
	}
	if exportPipelineScheduleDue(ExportPipeline{
		Status:   ExportPipelineActive,
		Schedule: &ExportPipelineSchedule{Enabled: false, Cron: "* * * * *", TimeZone: "UTC"},
	}, time.Date(2026, 8, 3, 16, 0, 0, 0, time.UTC)) {
		t.Fatal("disabled schedule was due")
	}
}

func TestExportPipelinePreBuildPreviewRefreshesWithoutMutation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	state := assessedFoundation()
	if _, err := lifecycle.CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "preview-export", DisplayName: "Preview", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{state.Environment.ID},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
		Schedule: &ExportPipelineSchedule{Cron: "0 12 * * *", TimeZone: "America/New_York"},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.PreviewExportPipeline(ctx, pipeline.ID)
	if err != nil || len(first.Added) == 0 {
		t.Fatalf("first preview = %#v, %v", first, err)
	}
	if bundles, err := repository.ListBundles(ctx, pipeline.ID); err != nil || len(bundles) != 0 {
		t.Fatalf("preview mutated Bundle lineage = %#v, %v", bundles, err)
	}
	if _, err := service.Build(ctx, pipeline.ID); err != nil {
		t.Fatal(err)
	}
	second, err := service.PreviewExportPipeline(ctx, pipeline.ID)
	if err != nil || len(second.Added) != 0 || len(second.Updated) != 0 || len(second.Removed) != 0 || len(second.Unchanged) == 0 {
		t.Fatalf("unchanged preview = %#v, %v", second, err)
	}
	state.Environment.Generation = "fedcba9876543210"
	if _, err := lifecycle.CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	refreshed, err := service.PreviewExportPipeline(ctx, pipeline.ID)
	if err != nil || len(refreshed.Updated) == 0 {
		t.Fatalf("refreshed preview = %#v, %v", refreshed, err)
	}
	pipeline.Schedule.DisablePreBuildPreview = true
	if _, err := service.PutPipeline(ctx, pipeline); err != nil {
		t.Fatal(err)
	}
	if _, err := service.PreviewExportPipeline(ctx, pipeline.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("disabled preview error = %v", err)
	}
}

func TestScheduledExportPipelineBuildIsSingleFlight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := assessedFoundation()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	slot := time.Date(2026, 8, 3, 12, 34, 0, 0, time.UTC)
	service.now = func() time.Time { return slot }
	if _, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "single-flight", DisplayName: "Single flight", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{state.Environment.ID},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
		Schedule: &ExportPipelineSchedule{Enabled: true, Cron: "* * * * *", TimeZone: "UTC"},
	}); err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	runCount := 0
	var resultMu sync.Mutex
	var runErr error
	for range 12 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			runs, err := service.RunDueExportPipelines(ctx, slot)
			resultMu.Lock()
			defer resultMu.Unlock()
			runCount += len(runs)
			runErr = errors.Join(runErr, err)
		}()
	}
	wait.Wait()
	if runErr != nil || runCount != 1 {
		t.Fatalf("scheduled runs = %d, %v", runCount, runErr)
	}
	bundles, err := repository.ListBundles(ctx, "single-flight")
	if err != nil || len(bundles) != 1 {
		t.Fatalf("scheduled Bundles = %#v, %v", bundles, err)
	}
	runs, err := repository.ListExportPipelineScheduleRuns(ctx, "single-flight")
	if err != nil || len(runs) != 1 || runs[0].Status != ExportScheduleSucceeded ||
		len(runs[0].BundleIDs) != 1 || runs[0].BundleIDs[0] != bundles[0].Manifest.ID {
		t.Fatalf("durable schedule run = %#v, %v", runs, err)
	}
}

func TestFileRepositoryScheduledExportDoesNotRepeatAfterRestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "state")
	repository, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	state := assessedFoundation()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	key := mustBundleKey(t)
	service, err := NewBundleService(repository, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	slot := time.Date(2026, 8, 3, 12, 34, 0, 0, time.UTC)
	service.now = func() time.Time { return slot }
	if _, err := service.PutPipeline(ctx, ExportPipeline{
		ID: "restart-safe", DisplayName: "Restart safe", BuildStrategy: BundleStrategySnapshot,
		Selections: []BundleSelectionRule{{
			Type: BundleSelectEnvironment, IDs: []string{state.Environment.ID},
			ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
		}},
		Schedule: &ExportPipelineSchedule{Enabled: true, Cron: "* * * * *", TimeZone: "UTC"},
	}); err != nil {
		t.Fatal(err)
	}
	if runs, err := service.RunDueExportPipelines(ctx, slot); err != nil || len(runs) != 1 {
		t.Fatalf("first scheduled run = %#v, %v", runs, err)
	}

	reopened, err := NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := NewBundleService(reopened, key, nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.now = func() time.Time { return slot }
	if runs, err := restarted.RunDueExportPipelines(ctx, slot); err != nil || len(runs) != 0 {
		t.Fatalf("restarted duplicate run = %#v, %v", runs, err)
	}
	bundles, err := reopened.ListBundles(ctx, "restart-safe")
	if err != nil || len(bundles) != 1 {
		t.Fatalf("restarted Bundle lineage = %#v, %v", bundles, err)
	}
	runs, err := reopened.ListExportPipelineScheduleRuns(ctx, "restart-safe")
	if err != nil || len(runs) != 1 || runs[0].Status != ExportScheduleSucceeded {
		t.Fatalf("restarted schedule evidence = %#v, %v", runs, err)
	}
}

func TestScheduledExportPipelineFailureIsDurableAndDoesNotBlockOthers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := assessedFoundation()
	if _, err := NewLifecycle(repository).CreatePlan(ctx, state); err != nil {
		t.Fatal(err)
	}
	service, err := NewBundleService(repository, mustBundleKey(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	slot := time.Date(2026, 8, 3, 12, 34, 0, 0, time.UTC)
	service.now = func() time.Time { return slot }
	for _, pipeline := range []ExportPipeline{
		{
			ID: "a-failing", DisplayName: "Failing", BuildStrategy: BundleStrategySnapshot,
			Selections: []BundleSelectionRule{{Type: BundleSelectEnvironment, IDs: []string{"missing"}, ReleaseStrategy: BundleReleasesNone}},
			Schedule:   &ExportPipelineSchedule{Enabled: true, Cron: "* * * * *", TimeZone: "UTC"},
		},
		{
			ID: "b-successful", DisplayName: "Successful", BuildStrategy: BundleStrategySnapshot,
			Selections: []BundleSelectionRule{{
				Type: BundleSelectEnvironment, IDs: []string{state.Environment.ID},
				ReleaseStrategy: BundleReleasesAll, ReleaseChannels: []string{"RELEASE"},
			}},
			Schedule: &ExportPipelineSchedule{Enabled: true, Cron: "* * * * *", TimeZone: "UTC"},
		},
	} {
		if _, err := service.PutPipeline(ctx, pipeline); err != nil {
			t.Fatal(err)
		}
	}
	runs, err := service.RunDueExportPipelines(ctx, slot)
	if err == nil || len(runs) != 2 {
		t.Fatalf("mixed scheduled results = %#v, %v", runs, err)
	}
	byPipeline := map[string]ExportPipelineScheduleRun{}
	for _, run := range runs {
		byPipeline[run.PipelineID] = run
	}
	if byPipeline["a-failing"].Status != ExportScheduleFailed || byPipeline["a-failing"].Failure == "" ||
		byPipeline["b-successful"].Status != ExportScheduleSucceeded || len(byPipeline["b-successful"].BundleIDs) != 1 {
		t.Fatalf("mixed durable results = %#v", byPipeline)
	}
}
