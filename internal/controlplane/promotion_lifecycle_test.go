package controlplane

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
)

func TestPromotionRunLifecyclePersistsTerminalReasons(t *testing.T) {
	for _, mode := range []string{"memory", "file", "postgres"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			var repository Repository = NewMemoryRepository()
			reopen := func() Repository { return repository }
			if mode == "file" {
				root := t.TempDir()
				open := func() Repository {
					store, err := NewFileRepository(root)
					if err != nil {
						t.Fatal(err)
					}
					return store
				}
				repository, reopen = open(), open
			}
			if mode == "postgres" {
				dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("CONDUIT_TEST_POSTGRES_DSN is required")
				}
				open := func() Repository {
					store, err := NewPostgresRepository(ctx, dsn)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = store.Close() })
					return store
				}
				repository, reopen = open(), open
				if _, err := repository.(*PostgresRepository).database.Exec(`UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
					t.Fatal(err)
				}
			}
			state := foundation()
			if _, err := repository.CreatePlan(ctx, PlanRecord{ID: "pln_run_fixture", Plan: Plan{Environment: state.Environment.ID}, DesiredState: &state, CreatedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
			lifecycle := NewLifecycle(repository)
			lifecycle.now = func() time.Time { return now }
			request := promotionRequest(now)
			request.Evidence[0].HealthySince = now
			running, err := lifecycle.EvaluatePromotion(ctx, request)
			if err != nil || running.Lifecycle != catalog.PromotionRunning || running.Status != PromotionBlocked || running.RunID == "" {
				t.Fatalf("running = %#v, %v", running, err)
			}
			repository = reopen()
			lifecycle = NewLifecycle(repository)
			now = now.Add(time.Minute)
			lifecycle.now = func() time.Time { return now }
			request.PreviousEvaluationID = running.ID
			request.Termination = catalog.PromotionCancelled
			request.TerminationReason = "Rollout superseded by an approved fix"
			cancelled, err := lifecycle.EvaluatePromotion(ctx, request)
			if err != nil || cancelled.Lifecycle != catalog.PromotionCancelled || cancelled.LifecycleReason != request.TerminationReason || cancelled.Status != PromotionBlocked || cancelled.RunID != running.RunID {
				t.Fatalf("cancelled = %#v, %v", cancelled, err)
			}
			original, err := repository.GetPromotion(ctx, running.ID)
			if err != nil || original.Lifecycle != catalog.PromotionRunning {
				t.Fatalf("earlier evidence was rewritten: %#v, %v", original, err)
			}
			request.PreviousEvaluationID = cancelled.ID
			request.Termination = ""
			request.TerminationReason = ""
			request.Evidence[0].HealthySince = now.Add(-72 * time.Hour)
			if _, err := lifecycle.EvaluatePromotion(ctx, request); !errors.Is(err, ErrConflict) {
				t.Fatalf("terminal run resumed: %v", err)
			}
			// A deliberate new evaluation creates a new run, rather than reviving one.
			request.PreviousEvaluationID = ""
			request.EvaluationStartedAt = time.Time{}
			succeeded, err := lifecycle.EvaluatePromotion(ctx, request)
			if err != nil || succeeded.Lifecycle != catalog.PromotionSucceeded || succeeded.Status != PromotionPassed || succeeded.RunID == running.RunID {
				t.Fatalf("new run = %#v, %v", succeeded, err)
			}
		})
	}
}

func TestPromotionRunContinuesToSuccessOrTimeout(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "succeeded", true: "aborted"}[timeout], func(t *testing.T) {
			now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
			lifecycle := NewLifecycle(promotionRepository(t))
			lifecycle.now = func() time.Time { return now }
			request := promotionRequest(now)
			request.Policy.MinimumHealthyDurationSeconds = 60
			request.Evidence[0].HealthySince = now
			if timeout {
				request.Policy.MaximumEvaluationDurationSeconds = 30
			}
			running, err := lifecycle.EvaluatePromotion(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			now = now.Add(2 * time.Minute)
			request.PreviousEvaluationID = running.ID
			request.Evidence[0].ObservedAt = now
			final, err := lifecycle.EvaluatePromotion(context.Background(), request)
			if err != nil {
				t.Fatal(err)
			}
			expected := catalog.PromotionSucceeded
			if timeout {
				expected = catalog.PromotionAborted
			}
			if final.Lifecycle != expected || final.RunID != running.RunID || final.PreviousEvaluationID != running.ID {
				t.Fatalf("final = %#v", final)
			}
			if timeout && final.LifecycleReason == "" {
				t.Fatal("timeout lost reason")
			}
			state := foundation()
			state.Releases = []Release{request.Release}
			state.Promotions = []PromotionRecord{running, final}
			var validation []ValidationError
			validatePromotions(state.Promotions, state.Releases, &validation)
			if len(validation) > 0 {
				t.Fatalf("run history does not replay: %#v", validation)
			}
		})
	}
}

func TestPromotionRunConcurrentTerminalUpdatesHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repository := promotionRepository(t)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)
	request.Evidence[0].HealthySince = now
	running, err := lifecycle.EvaluatePromotion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	request.PreviousEvaluationID = running.ID
	request.TerminationReason = "Operator stopped the rollout"
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	start := make(chan struct{})
	for _, termination := range []catalog.PromotionLifecycle{catalog.PromotionCancelled, catalog.PromotionAborted} {
		wait.Add(1)
		go func(termination catalog.PromotionLifecycle) {
			defer wait.Done()
			<-start
			update := request
			update.Termination = termination
			_, err := lifecycle.EvaluatePromotion(ctx, update)
			errs <- err
		}(termination)
	}
	close(start)
	wait.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrConflict) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("terminal winners = %d", successes)
	}
}
