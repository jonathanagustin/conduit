package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
			if mode == "postgres" {
				postgres := repository.(*PostgresRepository)
				var encoded []byte
				if err := postgres.database.QueryRow(`SELECT state->'promotions'->$1 FROM conduit_repository_state WHERE singleton = TRUE`, cancelled.ID).Scan(&encoded); err != nil {
					t.Fatal(err)
				}
				var legacy PromotionRecord
				if err := json.Unmarshal(encoded, &legacy); err != nil {
					t.Fatal(err)
				}
				if legacy.RunID != "" || legacy.Lifecycle != "" {
					t.Fatal("legacy snapshot became a second lifecycle authority")
				}
				// An older Hub's read/modify/write of the snapshot cannot carry the new
				// fields. Re-write that actual legacy shape, then read from another pool.
				if _, err := postgres.database.Exec(`UPDATE conduit_repository_state SET state = state, generation = generation + 1 WHERE singleton = TRUE`); err != nil {
					t.Fatal(err)
				}
				restored, err := reopen().GetPromotion(ctx, cancelled.ID)
				if err != nil || restored.Lifecycle != catalog.PromotionCancelled || restored.LifecycleReason != request.TerminationReason {
					t.Fatalf("legacy write lost terminal state: %#v, %v", restored, err)
				}
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
			// Older clients drop additive lifecycle fields when round-tripping a
			// decision. Its eligibility identity and replay must remain valid.
			legacy := append([]PromotionRecord(nil), state.Promotions...)
			for index := range legacy {
				legacy[index].RunID, legacy[index].PreviousEvaluationID = "", ""
				legacy[index].Lifecycle, legacy[index].LifecycleReason, legacy[index].ValueTier = "", "", ""
			}
			validation = nil
			validatePromotions(legacy, state.Releases, &validation)
			if len(validation) > 0 {
				t.Fatalf("legacy projection does not replay: %#v", validation)
			}
			tampered := append([]PromotionRecord(nil), state.Promotions...)
			tampered[len(tampered)-1].LifecycleReason = "invented reason"
			validation = nil
			validatePromotions(tampered, state.Releases, &validation)
			if len(validation) == 0 {
				t.Fatal("unreproducible lifecycle reason accepted")
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

func TestPostgresPromotionLifecycleWriteFailureRollsBackDecision(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	repository, err := NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	if _, err := repository.database.Exec(`UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.database.Exec(`CREATE FUNCTION reject_promotion_lifecycle() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected lifecycle storage failure'; END $$;
 CREATE TRIGGER reject_promotion_lifecycle BEFORE INSERT ON conduit_promotion_lifecycle FOR EACH ROW EXECUTE FUNCTION reject_promotion_lifecycle()`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = repository.database.Exec(`DROP TRIGGER IF EXISTS reject_promotion_lifecycle ON conduit_promotion_lifecycle; DROP FUNCTION IF EXISTS reject_promotion_lifecycle()`)
	})
	lifecycle := NewLifecycle(repository)
	request := promotionRequest(time.Now().UTC())
	if _, err := lifecycle.EvaluatePromotion(ctx, request); err == nil {
		t.Fatal("lifecycle storage failure was swallowed")
	}
	records, err := repository.ListPromotions(ctx, request.Release.Product)
	if err != nil || len(records) != 0 {
		t.Fatalf("decision escaped lifecycle rollback: %#v, %v", records, err)
	}
}

func TestPromotionLifecycleHTTPContinuationAndReason(t *testing.T) {
	repository := NewMemoryRepository()
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, AllowInsecureAgentIdentityHeaders: true})
	request := promotionRequest(time.Now().UTC())
	request.Evidence[0].HealthySince = time.Now().UTC()
	post := func(request PromotionEvaluationRequest, expected int) PromotionRecord {
		t.Helper()
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/promotions/evaluations", bytes.NewReader(body)))
		if response.Code != expected {
			t.Fatalf("status=%d, expected=%d, body=%s", response.Code, expected, response.Body.String())
		}
		if expected != http.StatusCreated {
			return PromotionRecord{}
		}
		var result PromotionRecord
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	running := post(request, http.StatusCreated)
	if running.Lifecycle != catalog.PromotionRunning || running.ValueTier != "CONDUIT_NATIVE" {
		t.Fatalf("running transport=%#v", running)
	}
	request.PreviousEvaluationID = running.ID
	request.Termination = catalog.PromotionCancelled
	post(request, http.StatusConflict)
	request.TerminationReason = "Superseded by the security fix"
	cancelled := post(request, http.StatusCreated)
	if cancelled.Lifecycle != catalog.PromotionCancelled || cancelled.LifecycleReason != request.TerminationReason {
		t.Fatalf("terminal transport=%#v", cancelled)
	}
	request.PreviousEvaluationID = cancelled.ID
	request.Termination = ""
	request.TerminationReason = ""
	post(request, http.StatusConflict)
}

func TestPromotionContinuationRejectsPolicyOrCoordinateChanges(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repository := promotionRepository(t)
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	request := promotionRequest(now)
	request.Evidence[0].HealthySince = now
	running, err := lifecycle.EvaluatePromotion(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.PreviousEvaluationID = running.ID
	request.Policy.MinimumHealthyEntities++
	if _, err := lifecycle.EvaluatePromotion(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed policy resumed run: %v", err)
	}
	request.Policy.MinimumHealthyEntities--
	request.Release.Version = "0.3.0"
	if _, err := lifecycle.EvaluatePromotion(context.Background(), request); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed release resumed run: %v", err)
	}
}

func TestPostgresConcurrentPromotionTerminations(t *testing.T) {
	dsn := os.Getenv("CONDUIT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CONDUIT_TEST_POSTGRES_DSN is required")
	}
	ctx := context.Background()
	first, err := NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := first.database.Exec(`UPDATE conduit_repository_state SET generation = 0, state = '{}'::jsonb WHERE singleton = TRUE`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	services := []*Lifecycle{NewLifecycle(first), NewLifecycle(second)}
	for _, service := range services {
		service.now = func() time.Time { return now }
	}
	request := promotionRequest(now)
	request.Evidence[0].HealthySince = now
	running, err := services[0].EvaluatePromotion(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	request.PreviousEvaluationID = running.ID
	request.TerminationReason = "Superseded rollout"
	var wait sync.WaitGroup
	errs := make(chan error, 2)
	start := make(chan struct{})
	for index, termination := range []catalog.PromotionLifecycle{catalog.PromotionCancelled, catalog.PromotionAborted} {
		wait.Add(1)
		go func(index int, termination catalog.PromotionLifecycle) {
			defer wait.Done()
			<-start
			update := request
			update.Termination = termination
			_, err := services[index].EvaluatePromotion(ctx, update)
			errs <- err
		}(index, termination)
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
		t.Fatalf("terminal winners across independent pools=%d", successes)
	}
	records, err := second.ListPromotions(ctx, request.Release.Product)
	if err != nil || len(records) != 2 {
		t.Fatalf("stored history=%#v, %v", records, err)
	}
}
