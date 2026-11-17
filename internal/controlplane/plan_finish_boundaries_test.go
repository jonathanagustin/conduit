package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// These scenarios exercise every write FinishPlan can perform, including both
// branches of automatic environment escalation and legacy execution backfill.
func TestFinishPlanEveryWriteBoundaryIsAtomic(t *testing.T) {
	cases := []finishBoundaryScenario{
		{name: "success", succeeded: true, writes: []string{"plan", "report"}},
		{name: "failure", writes: []string{"plan", "report", "suppression:PLAN_FAILURE"}},
		{name: "failure-without-observations", empty: true, writes: []string{"plan", "suppression:PLAN_FAILURE"}},
		{name: "environment-escalation", escalate: true, writes: []string{"plan", "report", "suppression:PLAN_FAILURE", "suppression:ENVIRONMENT_ESCALATION"}},
		{name: "environment-escalation-removal", remove: true, writes: []string{"plan", "report", "suppression:PLAN_FAILURE", "suppression-update"}},
		{name: "legacy-success-without-execution", succeeded: true, legacy: true, writes: []string{"plan", "report"}},
		{name: "legacy-failure-without-execution", legacy: true, writes: []string{"plan", "report", "suppression:PLAN_FAILURE"}},
		{name: "legacy-failure-with-execution", legacy: true, execution: true, writes: []string{"plan", "report", "suppression:PLAN_FAILURE"}},
		{name: "legacy-failure-with-report-but-no-suppression", legacy: true, execution: true, reported: true, writes: []string{"plan", "suppression:PLAN_FAILURE"}},
	}
	for _, store := range []string{"memory", "file", "postgres"} {
		t.Run(store, func(t *testing.T) {
			for _, test := range cases {
				t.Run(test.name, func(t *testing.T) {
					boundaries := make([]string, 0, len(test.writes)*2+1)
					for _, write := range test.writes {
						boundaries = append(boundaries, "before:"+write, "after:"+write)
					}
					boundaries = append(boundaries, "before:commit")
					for _, boundary := range boundaries {
						t.Run(boundary, func(t *testing.T) {
							exerciseFinishBoundary(t, store, test, boundary, boundaries)
						})
					}
				})
			}
		})
	}
}

type finishBoundaryScenario struct {
	name      string
	succeeded bool
	legacy    bool
	execution bool
	reported  bool
	empty     bool
	escalate  bool
	remove    bool
	writes    []string
}

func exerciseFinishBoundary(t *testing.T, store string, test finishBoundaryScenario, boundary string, boundaries []string) {
	t.Helper()
	base, reopen := newFinishBoundaryStore(t, store)
	lifecycle, running := startFoundationPlan(t, base)
	if test.escalate {
		var err error
		running, err = base.UpdatePlan(t.Context(), running.ID, func(record *PlanRecord) error {
			record.Plan.AutoSuppressionThreshold = 1
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		seedFinishSuppression(t, base, running, SuppressionEntity, SuppressionPlanFailure, "example-workspace")
	}
	if test.remove {
		seedFinishSuppression(t, base, running, SuppressionEnvironment, SuppressionEnvironmentEscalate, "")
	}
	if test.legacy {
		var err error
		running, err = base.UpdatePlan(t.Context(), running.ID, func(record *PlanRecord) error {
			record.Status = PlanSucceeded
			if !test.succeeded {
				record.Status = PlanFailed
				record.Failure = "chart rollout failed"
				record.FailedInstallation = "example-api"
			}
			if test.execution {
				record.Execution = failedPlanExecution(*record, "example-api")
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if test.reported {
		report := finishBoundaryReport(running, test.succeeded)
		report.Environment, report.PlanID, report.ObservedAt = running.Plan.Environment, running.ID, running.UpdatedAt
		if _, err := base.PutEnvironmentReport(t.Context(), report); err != nil {
			t.Fatal(err)
		}
	}
	before := finishBoundarySnapshot(t, base)
	fault := &finishBoundaryFault{boundary: boundary, err: errors.New("injected " + boundary)}
	lifecycle.repository = &finishBoundaryRepository{Repository: base, fault: fault}
	finish := func(success bool, token string) (PlanRecord, error) {
		report := finishBoundaryReport(running, success)
		var execution *PlanExecution
		failure, installation := "", ""
		if success {
			execution = successfulPlanExecution(running)
		} else {
			execution = failedPlanExecution(running, "example-api")
			failure, installation = "chart rollout failed", "example-api"
			if test.empty {
				report = EnvironmentReport{}
			}
		}
		return lifecycle.FinishPlan(t.Context(), running.ID, token, success, failure, installation, execution, &report)
	}
	if _, err := finish(test.succeeded, running.Lease.Token); !errors.Is(err, fault.err) {
		t.Fatalf("fault was not reached: error=%v, writes=%v", err, fault.seen)
	}
	if got := finishBoundarySnapshot(t, reopen()); got != before {
		t.Fatal("a failed finish changed persisted state, evidence, suppressions or invalidations")
	}
	fault.boundary = ""
	fault.seen = nil
	finished, err := finish(test.succeeded, running.Lease.Token)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fault.seen, boundaries) {
		t.Fatalf("write paths changed: got %v, want %v", fault.seen, boundaries)
	}
	wantStatus := PlanFailed
	if test.succeeded {
		wantStatus = PlanSucceeded
	}
	if !finished.CompletionRecorded || finished.Execution == nil || finished.Status != wantStatus {
		t.Fatalf("completion receipt/status/execution = %#v", finished)
	}
	observed, observedErr := base.GetEnvironmentReport(t.Context(), running.Plan.Environment)
	if test.empty {
		if !errors.Is(observedErr, ErrNotFound) {
			t.Fatalf("empty failed observation = %#v %v", observed, observedErr)
		}
	} else if observedErr != nil || observed.PlanID != running.ID || len(observed.Entities) != len(finishBoundaryReport(running, test.succeeded).Entities) {
		t.Fatalf("committed observation = %#v %v", observed, observedErr)
	}
	suppressions, err := base.ListSuppressions(t.Context(), running.Plan.Environment)
	wantSuppressions := 1
	if test.succeeded {
		wantSuppressions = 0
	} else if test.escalate {
		wantSuppressions = 3
	} else if test.remove {
		wantSuppressions = 2
	}
	if err != nil || len(suppressions) != wantSuppressions {
		t.Fatalf("committed suppressions = %#v %v", suppressions, err)
	}
	for _, suppression := range suppressions {
		if test.remove && suppression.Source == SuppressionEnvironmentEscalate && suppression.RemovedAt == nil {
			t.Fatal("stale environment escalation was not removed")
		}
	}
	committed := finishBoundarySnapshot(t, reopen())
	// A real retry happens at another time, often after lease expiry.
	now := lifecycle.now().Add(2 * time.Hour)
	lifecycle.now = func() time.Time { return now }
	if _, err := finish(test.succeeded, running.Lease.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := finish(!test.succeeded, running.Lease.Token); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("opposite terminal retry = %v", err)
	}
	if _, err := finish(test.succeeded, "another-token"); !errors.Is(err, ErrLeaseRejected) {
		t.Fatalf("foreign lease retry = %v", err)
	}
	if got := finishBoundarySnapshot(t, reopen()); got != committed {
		t.Fatal("a repeated or rejected finish changed committed state")
	}
}

func newFinishBoundaryStore(t *testing.T, store string) (Repository, func() Repository) {
	t.Helper()
	switch store {
	case "memory":
		repository := NewMemoryRepository()
		return repository, func() Repository { return repository }
	case "file":
		root := filepath.Join(t.TempDir(), "state")
		open := func() Repository {
			repository, err := NewFileRepository(root)
			if err != nil {
				t.Fatal(err)
			}
			return repository
		}
		return open(), open
	case "postgres":
		dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
		}
		open := func() Repository {
			repository, err := NewPostgresRepository(t.Context(), dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repository.Close() })
			return repository
		}
		repository := open().(*PostgresRepository)
		if _, err := repository.database.ExecContext(t.Context(), `
TRUNCATE conduit_repository_blobs;
UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
			t.Fatal(err)
		}
		return repository, open
	default:
		t.Fatalf("unknown store %q", store)
		return nil, nil
	}
}

// Include native invalidations as well as public records: rolled-back evidence
// must not leave a phantom live notification. File/SQL snapshots are read from
// a reopened store, rather than only from the transaction's original handle.
func finishBoundarySnapshot(t *testing.T, repository Repository) string {
	t.Helper()
	var value any
	switch repository := repository.(type) {
	case *MemoryRepository:
		repository.mu.Lock()
		defer repository.mu.Unlock()
		value = repository.stateLocked()
	case *FileRepository:
		files := map[string]string{}
		if err := filepath.WalkDir(repository.root, func(path string, entry os.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			data, err := os.ReadFile(path)
			if err == nil {
				files[path[len(repository.root):]] = string(data)
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		value = files
	case *PostgresRepository:
		var data []byte
		if err := repository.database.QueryRowContext(t.Context(), "SELECT state FROM conduit_repository_state WHERE singleton = TRUE").Scan(&data); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unsupported snapshot store %T", repository)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func seedFinishSuppression(t *testing.T, repository Repository, record PlanRecord, scope SuppressionScope, source SuppressionSource, installation string) {
	t.Helper()
	if _, err := repository.CreateSuppression(t.Context(), SuppressionRecord{
		ID: "sup_prior", Environment: record.Plan.Environment, InstallationID: installation,
		Scope: scope, Source: source, Reason: "prior failure", StartsAt: record.UpdatedAt,
		CreatedAt: record.UpdatedAt, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
}

func finishBoundaryReport(record PlanRecord, succeeded bool) EnvironmentReport {
	if !succeeded {
		return failedObservedState(record, "example-api")
	}
	report := EnvironmentReport{}
	for _, step := range record.Plan.Steps {
		report.Entities = append(report.Entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled,
		})
	}
	return report
}

type finishBoundaryFault struct {
	boundary string
	err      error
	seen     []string
}

func (fault *finishBoundaryFault) at(boundary string) error {
	fault.seen = append(fault.seen, boundary)
	if fault.boundary == boundary {
		return fault.err
	}
	return nil
}

type finishBoundaryRepository struct {
	Repository
	fault *finishBoundaryFault
}

func (repository *finishBoundaryRepository) Transaction(ctx context.Context, operation func(Repository) error) error {
	return repository.Repository.(TransactionalRepository).Transaction(ctx, func(view Repository) error {
		if err := operation(&finishBoundaryRepository{Repository: view, fault: repository.fault}); err != nil {
			return err
		}
		return repository.fault.at("before:commit")
	})
}

func (repository *finishBoundaryRepository) UpdatePlan(ctx context.Context, id string, update func(*PlanRecord) error) (PlanRecord, error) {
	if err := repository.fault.at("before:plan"); err != nil {
		return PlanRecord{}, err
	}
	record, err := repository.Repository.UpdatePlan(ctx, id, update)
	if err != nil {
		return record, err
	}
	return record, repository.fault.at("after:plan")
}

func (repository *finishBoundaryRepository) PutEnvironmentReport(ctx context.Context, report EnvironmentReport) (EnvironmentReport, error) {
	if err := repository.fault.at("before:report"); err != nil {
		return EnvironmentReport{}, err
	}
	stored, err := repository.Repository.PutEnvironmentReport(ctx, report)
	if err != nil {
		return stored, err
	}
	return stored, repository.fault.at("after:report")
}

func (repository *finishBoundaryRepository) CreateSuppression(ctx context.Context, suppression SuppressionRecord) (SuppressionRecord, error) {
	boundary := "suppression:" + string(suppression.Source)
	if err := repository.fault.at("before:" + boundary); err != nil {
		return SuppressionRecord{}, err
	}
	stored, err := repository.Repository.CreateSuppression(ctx, suppression)
	if err != nil {
		return stored, err
	}
	return stored, repository.fault.at("after:" + boundary)
}

func (repository *finishBoundaryRepository) UpdateSuppression(ctx context.Context, id string, update func(*SuppressionRecord) error) (SuppressionRecord, error) {
	if err := repository.fault.at("before:suppression-update"); err != nil {
		return SuppressionRecord{}, err
	}
	stored, err := repository.Repository.UpdateSuppression(ctx, id, update)
	if err != nil {
		return stored, err
	}
	return stored, repository.fault.at("after:suppression-update")
}

func TestFinishPlanRetryPreservesSuccessorEvidenceAndRemovedSuppression(t *testing.T) {
	for _, store := range []string{"memory", "file", "postgres"} {
		t.Run(store, func(t *testing.T) {
			base, reopen := newFinishBoundaryStore(t, store)
			lifecycle, running := startFoundationPlan(t, base)
			report := failedObservedState(running, "example-api")
			if _, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(running, "example-api"), &report); err != nil {
				t.Fatal(err)
			}
			later := lifecycle.now().Add(time.Hour)
			lifecycle.now = func() time.Time { return later }
			suppressions, err := base.ListSuppressions(t.Context(), running.Plan.Environment)
			if err != nil || len(suppressions) != 1 {
				t.Fatalf("suppressions = %v %v", suppressions, err)
			}
			if _, err := base.UpdateSuppression(t.Context(), suppressions[0].ID, func(record *SuppressionRecord) error {
				record.RemovedAt, record.RemovedBy = &later, "operator"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			successor := report
			successor.PlanID, successor.ObservedAt = "pln_successor", later
			successor.Entities[0].Health = HealthHealthy
			if _, err := base.PutEnvironmentReport(t.Context(), successor); err != nil {
				t.Fatal(err)
			}
			before := finishBoundarySnapshot(t, reopen())
			report = failedObservedState(running, "example-api")
			if _, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(running, "example-api"), &report); err != nil {
				t.Fatal(err)
			}
			if got := finishBoundarySnapshot(t, reopen()); got != before {
				t.Fatal("old completion replayed its report or recreated a removed suppression")
			}
		})
	}
}

func TestFinishPlanLegacyBackfillKeepsNewerObservation(t *testing.T) {
	for _, store := range []string{"memory", "file", "postgres"} {
		t.Run(store, func(t *testing.T) {
			base, _ := newFinishBoundaryStore(t, store)
			lifecycle, running := startFoundationPlan(t, base)
			if _, err := base.UpdatePlan(t.Context(), running.ID, func(record *PlanRecord) error {
				record.Status = PlanSucceeded
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			later := lifecycle.now().Add(time.Hour)
			successor := finishBoundaryReport(running, true)
			successor.Environment, successor.PlanID, successor.ObservedAt = running.Plan.Environment, "pln_successor", later
			want, err := base.PutEnvironmentReport(t.Context(), successor)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle.now = func() time.Time { return later.Add(time.Hour) }
			report := finishBoundaryReport(running, true)
			finished, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, true, "", "", successfulPlanExecution(running), &report)
			if err != nil {
				t.Fatal(err)
			}
			got, err := base.GetEnvironmentReport(t.Context(), running.Plan.Environment)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("legacy backfill rewound reported state: got=%#v want=%#v error=%v", got, want, err)
			}
			if !finished.CompletionRecorded || !finished.UpdatedAt.Equal(running.UpdatedAt) {
				t.Fatalf("legacy completion time/receipt = %v %v", finished.UpdatedAt, finished.CompletionRecorded)
			}
		})
	}
}

func TestFinishPlanLegacyReceiptBackfillPreservesSuppressionDecision(t *testing.T) {
	for _, store := range []string{"memory", "file", "postgres"} {
		t.Run(store, func(t *testing.T) {
			for _, successor := range []bool{false, true} {
				reportKind := "original-report"
				if successor {
					reportKind = "successor-report"
				}
				t.Run(reportKind, func(t *testing.T) {
					for _, removed := range []bool{false, true} {
						decision := "active"
						if removed {
							decision = "operator-removed"
						}
						t.Run(decision, func(t *testing.T) {
							exerciseLegacySuppressionDecision(t, store, successor, removed)
						})
					}
				})
			}
		})
	}
}

func exerciseLegacySuppressionDecision(t *testing.T, store string, successor, removed bool) {
	t.Helper()
	base, reopen := newFinishBoundaryStore(t, store)
	lifecycle, running := startFoundationPlan(t, base)
	report := failedObservedState(running, "example-api")
	completed, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(running, "example-api"), &report)
	if err != nil {
		t.Fatal(err)
	}
	// The receipt is absent from every pre-upgrade Plan JSON.
	if _, err := base.UpdatePlan(t.Context(), running.ID, func(record *PlanRecord) error {
		record.CompletionRecorded = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	later := lifecycle.now().Add(time.Hour)
	lifecycle.now = func() time.Time { return later }
	if removed {
		suppressions, err := base.ListSuppressions(t.Context(), running.Plan.Environment)
		if err != nil || len(suppressions) != 1 {
			t.Fatalf("suppression fixture = %#v %v", suppressions, err)
		}
		if _, err := lifecycle.RemoveSuppression(t.Context(), suppressions[0].ID, "operator"); err != nil {
			t.Fatal(err)
		}
	}
	if successor {
		report.PlanID, report.ObservedAt = "pln_successor", later
		if _, err := base.PutEnvironmentReport(t.Context(), report); err != nil {
			t.Fatal(err)
		}
	}
	wantReport, err := base.GetEnvironmentReport(t.Context(), running.Plan.Environment)
	if err != nil {
		t.Fatal(err)
	}
	wantSuppressions, err := base.ListSuppressions(t.Context(), running.Plan.Environment)
	if err != nil {
		t.Fatal(err)
	}
	later = later.Add(time.Hour)
	report = failedObservedState(running, "example-api")
	finished, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(running, "example-api"), &report)
	if err != nil {
		t.Fatal(err)
	}
	if !finished.CompletionRecorded || finished.Status != PlanFailed || !finished.UpdatedAt.Equal(completed.UpdatedAt) || !reflect.DeepEqual(finished.Execution, completed.Execution) {
		t.Fatalf("legacy backfill changed completion evidence: %#v", finished)
	}
	stored := reopen()
	gotReport, err := stored.GetEnvironmentReport(t.Context(), running.Plan.Environment)
	if err != nil || !reflect.DeepEqual(gotReport, wantReport) {
		t.Fatalf("legacy backfill changed the existing report: %#v %v", gotReport, err)
	}
	gotSuppressions, err := stored.ListSuppressions(t.Context(), running.Plan.Environment)
	if err != nil || !reflect.DeepEqual(gotSuppressions, wantSuppressions) {
		t.Fatalf("legacy backfill replayed a recorded suppression decision: %#v %v", gotSuppressions, err)
	}
}
