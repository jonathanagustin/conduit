package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFinishPlanRollsBackPartialEvidenceAndRetryCompletes(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		exerciseAtomicFinish(t, NewMemoryRepository())
	})
	t.Run("file", func(t *testing.T) {
		repository, err := NewFileRepository(filepath.Join(t.TempDir(), "delivery-state"))
		if err != nil {
			t.Fatal(err)
		}
		exerciseAtomicFinish(t, repository)
	})
	t.Run("postgres", func(t *testing.T) {
		dataSourceName := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
		if dataSourceName == "" {
			t.Skip("CONDUIT_TEST_POSTGRES_DSN is required for the PostgreSQL integration gate")
		}
		repository, err := NewPostgresRepository(t.Context(), dataSourceName)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = repository.Close() })
		if _, err := repository.database.ExecContext(t.Context(), `
TRUNCATE conduit_repository_blobs;
UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
			t.Fatal(err)
		}
		exerciseAtomicFinish(t, repository)
	})
}

func TestFinishPlanRetryCompletesATerminalPlanMissingEvidence(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		exerciseTerminalRetry(t, NewMemoryRepository())
	})
	t.Run("file", func(t *testing.T) {
		repository, err := NewFileRepository(filepath.Join(t.TempDir(), "delivery-state"))
		if err != nil {
			t.Fatal(err)
		}
		exerciseTerminalRetry(t, repository)
	})
}

func exerciseAtomicFinish(t *testing.T, base Repository) {
	t.Helper()
	transactional, ok := base.(TransactionalRepository)
	if !ok {
		t.Fatal("store does not commit plan finish as one transaction")
	}
	injected := errors.New("injected suppression failure")
	repository := &suppressionFaultRepository{Repository: base, tx: transactional, fail: injected}
	lifecycle, running := startFoundationPlan(t, repository)
	before, err := base.GetPlan(t.Context(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	report := failedObservedState(running, "example-api")
	_, err = lifecycle.FinishPlan(
		t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api",
		failedPlanExecution(running, "example-api"), &report,
	)
	if !errors.Is(err, injected) {
		t.Fatalf("finish error = %v", err)
	}
	stored, err := base.GetPlan(t.Context(), running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != PlanRunning || stored.Revision != before.Revision {
		t.Fatalf("partial finish committed status %#v revision %d, was %d", stored.Status, stored.Revision, before.Revision)
	}
	if _, err := base.GetEnvironmentReport(t.Context(), running.Plan.Environment); !errors.Is(err, ErrNotFound) {
		t.Fatalf("observed state after rolled-back finish = %v", err)
	}
	suppressions, err := base.ListSuppressions(t.Context(), running.Plan.Environment)
	if err != nil || len(suppressions) != 0 {
		t.Fatalf("suppressions after rolled-back finish = %#v %v", suppressions, err)
	}

	repository.fail = nil
	report = failedObservedState(running, "example-api")
	finished, err := lifecycle.FinishPlan(
		t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api",
		failedPlanExecution(running, "example-api"), &report,
	)
	if err != nil {
		t.Fatal(err)
	}
	assertFailedEvidence(t, base, finished)

	report = failedObservedState(running, "example-api")
	if _, err := lifecycle.FinishPlan(
		t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api",
		failedPlanExecution(running, "example-api"), &report,
	); err != nil {
		t.Fatal(err)
	}
	assertFailedEvidence(t, base, finished)

	report = failedObservedState(running, "example-api")
	if _, err := lifecycle.FinishPlan(
		t.Context(), running.ID, running.Lease.Token, true, "", "",
		successfulPlanExecution(running), &report,
	); err == nil {
		t.Fatal("a failed plan was rewritten as succeeded")
	}
	assertFailedEvidence(t, base, finished)
}

func exerciseTerminalRetry(t *testing.T, repository Repository) {
	t.Helper()
	lifecycle, running := startFoundationPlan(t, repository)
	_, err := repository.UpdatePlan(t.Context(), running.ID, func(record *PlanRecord) error {
		record.Status = PlanFailed
		record.Failure = "chart rollout failed"
		record.FailedInstallation = "example-api"
		record.Execution = failedPlanExecution(*record, "example-api")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	report := failedObservedState(running, "example-api")
	finished, err := lifecycle.FinishPlan(
		t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api",
		failedPlanExecution(running, "example-api"), &report,
	)
	if err != nil {
		t.Fatalf("retry of a terminal plan missing evidence: %v", err)
	}
	if finished.Status != PlanFailed {
		t.Fatalf("status = %s", finished.Status)
	}
	assertFailedEvidence(t, repository, finished)
	report = failedObservedState(running, "example-api")
	if _, err := lifecycle.FinishPlan(
		t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api",
		failedPlanExecution(running, "example-api"), &report,
	); err != nil {
		t.Fatal(err)
	}
	assertFailedEvidence(t, repository, finished)
}

func assertFailedEvidence(t *testing.T, repository Repository, finished PlanRecord) {
	t.Helper()
	stored, err := repository.GetPlan(context.Background(), finished.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != PlanFailed || stored.Execution == nil {
		t.Fatalf("plan = %#v", stored.Status)
	}
	observed, err := repository.GetEnvironmentReport(context.Background(), finished.Plan.Environment)
	if err != nil || observed.PlanID != finished.ID || len(observed.Entities) != 1 {
		t.Fatalf("observed state = %#v %v", observed, err)
	}
	suppressions, err := repository.ListSuppressions(context.Background(), finished.Plan.Environment)
	if err != nil || len(suppressions) != 1 || suppressions[0].Source != SuppressionPlanFailure || suppressions[0].PlanID != finished.ID {
		t.Fatalf("suppressions = %#v %v", suppressions, err)
	}
}

func startFoundationPlan(t *testing.T, repository Repository) (*Lifecycle, PlanRecord) {
	t.Helper()
	lifecycle := NewLifecycle(repository)
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	lifecycle.now = func() time.Time { return now }
	lifecycle.newToken = func() (string, error) { return "lease-token", nil }
	record, err := lifecycle.CreatePlan(context.Background(), foundation())
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.ApprovePlan(context.Background(), record.ID, "reviewer", "approved")
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.LeaseNextPlan(context.Background(), "dev-use2", "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.StartPlan(context.Background(), record.ID, record.Lease.Token)
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle, record
}

func failedObservedState(record PlanRecord, installationID string) EnvironmentReport {
	var step PlanStep
	for _, candidate := range record.Plan.Steps {
		if candidate.InstallationID == installationID {
			step = candidate
		}
	}
	return EnvironmentReport{Entities: []ReportedEntity{{
		InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
		ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
		Health: HealthUnhealthy, RolloutStatus: RolloutBlocked, ConfigStatus: ConfigApplied, Lifecycle: LifecycleInstalled,
	}}}
}

type suppressionFaultRepository struct {
	Repository
	tx   TransactionalRepository
	fail error
}

func (repository *suppressionFaultRepository) CreateSuppression(ctx context.Context, record SuppressionRecord) (SuppressionRecord, error) {
	if repository.fail != nil {
		return SuppressionRecord{}, repository.fail
	}
	return repository.Repository.CreateSuppression(ctx, record)
}

func (repository *suppressionFaultRepository) Transaction(ctx context.Context, operation func(Repository) error) error {
	return repository.tx.Transaction(ctx, func(working Repository) error {
		return operation(&suppressionFaultRepository{Repository: working, tx: working.(TransactionalRepository), fail: repository.fail})
	})
}
