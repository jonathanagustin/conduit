package controlplane

import (
	"context"
	"errors"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFinishPlanFileCommitFailureRestoresOriginalTree(t *testing.T) {
	base, reopen := newFinishBoundaryStore(t, "file")
	lifecycle, running := startFoundationPlan(t, base)
	before := finishBoundarySnapshot(t, base)
	lostStaging := false
	lifecycle.repository = &finishCommitFaultRepository{Repository: base, beforeCommit: func(view Repository) error {
		// Model a staging tree disappearing after all callback writes. The
		// real commit moves the old root aside, fails the second rename, then
		// must restore the original root before returning the error.
		lostStaging = true
		return os.RemoveAll(view.(*FileRepository).root)
	}}
	report := failedObservedState(running, "example-api")
	_, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(running, "example-api"), &report)
	if err == nil || !strings.Contains(err.Error(), "commit repository transaction") || !lostStaging {
		t.Fatalf("real filesystem commit failure was not reached: %v", err)
	}
	if got := finishBoundarySnapshot(t, reopen()); got != before {
		t.Fatal("failed final rename did not restore the original repository tree")
	}
	lifecycle.repository = base
	report = failedObservedState(running, "example-api")
	finished, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(running, "example-api"), &report)
	if err != nil {
		t.Fatal(err)
	}
	assertFailedEvidence(t, reopen(), finished)
}

func TestFinishPlanPostgresWriteAndCommitFailuresRollBack(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		name := "state-write"
		if deferred {
			name = "deferred-commit"
		}
		t.Run(name, func(t *testing.T) {
			base, reopen := newFinishBoundaryStore(t, "postgres")
			postgres := base.(*PostgresRepository)
			lifecycle, running := startFoundationPlan(t, base)
			before := finishBoundarySnapshot(t, base)
			var generation int64
			if err := postgres.database.QueryRowContext(t.Context(), "SELECT generation FROM conduit_repository_state WHERE singleton = TRUE").Scan(&generation); err != nil {
				t.Fatal(err)
			}
			trigger := `CREATE TRIGGER test_finish_reject BEFORE UPDATE ON conduit_repository_state FOR EACH ROW EXECUTE FUNCTION test_finish_reject()`
			if deferred {
				trigger = `CREATE CONSTRAINT TRIGGER test_finish_reject AFTER UPDATE ON conduit_repository_state DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION test_finish_reject()`
			}
			if _, err := postgres.database.ExecContext(t.Context(), `CREATE FUNCTION test_finish_reject() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected finish persistence failure'; END $$`); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, _ = postgres.database.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS test_finish_reject ON conduit_repository_state; DROP FUNCTION IF EXISTS test_finish_reject()`)
			})
			if _, err := postgres.database.ExecContext(t.Context(), trigger); err != nil {
				t.Fatal(err)
			}
			report := failedObservedState(running, "example-api")
			_, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(running, "example-api"), &report)
			stage := "write repository state"
			if deferred {
				stage = "commit repository transaction"
			}
			if err == nil || !strings.Contains(err.Error(), stage) || !strings.Contains(err.Error(), "injected finish persistence failure") {
				t.Fatalf("real SQL persistence failure was not reached: %v", err)
			}
			if _, err := postgres.database.ExecContext(t.Context(), `DROP TRIGGER test_finish_reject ON conduit_repository_state; DROP FUNCTION test_finish_reject()`); err != nil {
				t.Fatal(err)
			}
			var after int64
			if err := postgres.database.QueryRowContext(t.Context(), "SELECT generation FROM conduit_repository_state WHERE singleton = TRUE").Scan(&after); err != nil || generation != after {
				t.Fatalf("SQL generation committed during rejected persistence: %d -> %d, error=%v", generation, after, err)
			}
			if got := finishBoundarySnapshot(t, reopen()); got != before {
				t.Fatal("SQL persistence failure left partial completion, evidence or invalidations")
			}
			report = failedObservedState(running, "example-api")
			finished, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, false, "chart rollout failed", "example-api", failedPlanExecution(running, "example-api"), &report)
			if err != nil {
				t.Fatal(err)
			}
			assertFailedEvidence(t, reopen(), finished)
		})
	}
}

// The HTTP caller must not acknowledge or announce a transaction that failed
// after its last evidence write. Retrying the same payload can then complete.
func TestFinishPlanHTTPAcknowledgesOnlyCommittedEvidence(t *testing.T) {
	base := NewMemoryRepository()
	_, running := startFoundationPlan(t, base)
	var err error
	running, err = base.UpdatePlan(t.Context(), running.ID, func(record *PlanRecord) error {
		record.Lease.ExpiresAt = time.Now().Add(time.Hour)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before := finishBoundarySnapshot(t, base)
	fault := &finishBoundaryFault{boundary: "before:commit", err: errors.New("injected commit refusal")}
	recorded := &recordedAnnouncements{}
	handler := NewHandler(&finishBoundaryRepository{Repository: base, fault: fault}, HandlerOptions{
		AllowInsecureAgentIdentityHeaders: true, Announcements: recorded.all(),
	})
	report := failedObservedState(running, "example-api")
	body := map[string]any{
		"status": PlanFailed, "failure": "chart rollout failed", "failedInstallationId": "example-api",
		"execution": failedPlanExecution(running, "example-api"), "entities": report.Entities,
	}
	response := finishRequest(t, handler, running, running.Lease.Token, body)
	if response.Code == http.StatusOK || len(recorded.failures) != 0 {
		t.Fatalf("uncommitted finish acknowledged/announced: status=%d announcements=%v", response.Code, recorded.failures)
	}
	if got := finishBoundarySnapshot(t, base); got != before {
		t.Fatal("refused HTTP finish committed partial effects")
	}
	fault.boundary = ""
	response = finishRequest(t, handler, running, running.Lease.Token, body)
	if response.Code != http.StatusOK || len(recorded.failures) != 1 {
		t.Fatalf("committed finish not acknowledged/announced: status=%d announcements=%v", response.Code, recorded.failures)
	}
	committed := finishBoundarySnapshot(t, base)
	response = finishRequest(t, handler, running, running.Lease.Token, body)
	if response.Code != http.StatusOK || len(recorded.failures) != 2 || !reflect.DeepEqual(recorded.failures[0], recorded.failures[1]) {
		t.Fatalf("retry changed terminal notification identity: status=%d announcements=%v", response.Code, recorded.failures)
	}
	if got := finishBoundarySnapshot(t, base); got != committed {
		t.Fatal("HTTP retry changed committed evidence")
	}
}

type finishCommitFaultRepository struct {
	Repository
	beforeCommit func(Repository) error
}

func (repository *finishCommitFaultRepository) Transaction(ctx context.Context, operation func(Repository) error) error {
	return repository.Repository.(TransactionalRepository).Transaction(ctx, func(view Repository) error {
		if err := operation(view); err != nil {
			return err
		}
		return repository.beforeCommit(view)
	})
}
