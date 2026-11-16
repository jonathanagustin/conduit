package controlplane

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestComputeReservationIsExclusiveAndReclaimable(t *testing.T) {
	now := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	t.Run("memory", func(t *testing.T) {
		exerciseComputeReservation(t, NewMemoryRepository(), now)
	})
	t.Run("file", func(t *testing.T) {
		repository, err := NewFileRepository(filepath.Join(t.TempDir(), "delivery-state"))
		if err != nil {
			t.Fatal(err)
		}
		exerciseComputeReservation(t, repository, now)
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
		exerciseComputeReservation(t, repository, now)
	})
}

type computeStore interface {
	ReserveComputeExecution(ctx context.Context, sessionID string, now time.Time, lease time.Duration) (ComputeReservation, error)
	CompleteComputeExecution(ctx context.Context, sessionID, token string, receipt ComputeReceipt, now time.Time) (ComputeReservation, error)
	ReleaseComputeExecution(ctx context.Context, sessionID, token string) error
}

func exerciseComputeReservation(t *testing.T, repository computeStore, now time.Time) {
	t.Helper()
	ctx := t.Context()
	var wins atomic.Int32
	var busy atomic.Int32
	var gate sync.WaitGroup
	gate.Add(8)
	started := make(chan struct{})
	for range 8 {
		go func() {
			defer gate.Done()
			<-started
			_, err := repository.ReserveComputeExecution(ctx, "ps_one", now, time.Minute)
			if err == nil {
				wins.Add(1)
				return
			}
			if errors.Is(err, ErrComputeBusy) {
				busy.Add(1)
			}
		}()
	}
	close(started)
	gate.Wait()
	if wins.Load() != 1 || busy.Load() != 7 {
		t.Fatalf("wins = %d busy = %d", wins.Load(), busy.Load())
	}

	held, err := repository.ReserveComputeExecution(ctx, "ps_crash", now, time.Minute)
	if err != nil || held.Receipt != nil {
		t.Fatal(err, held)
	}
	reclaimed, err := repository.ReserveComputeExecution(ctx, "ps_crash", now.Add(2*time.Minute), time.Minute)
	if err != nil {
		t.Fatalf("expired reservation was not reclaimed: %v", err)
	}
	if reclaimed.Token == "" || reclaimed.Token == held.Token {
		t.Fatalf("reclaimed reservation = %+v, held token %s", reclaimed, held.Token)
	}
	if _, err := repository.CompleteComputeExecution(ctx, "ps_crash", held.Token, ComputeReceipt{Outcome: "completed"}, now.Add(2*time.Minute)); !errors.Is(err, ErrComputeReservationLost) {
		t.Fatalf("old holder completed after reclaim: %v", err)
	}
	completed, err := repository.CompleteComputeExecution(ctx, "ps_crash", reclaimed.Token, ComputeReceipt{
		PlanRevisionID: "ps_crash", PlanDigest: "digest", Outcome: "completed",
	}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != ComputeStatusCompleted || completed.Receipt == nil || completed.Receipt.PlanDigest != "digest" {
		t.Fatalf("completion did not record the receipt: %+v", completed)
	}
	replay, err := repository.ReserveComputeExecution(ctx, "ps_crash", now.Add(3*time.Minute), time.Minute)
	if err != nil || replay.Status != ComputeStatusCompleted || replay.Receipt == nil || replay.Receipt.PlanDigest != "digest" {
		t.Fatalf("repeat reservation = %+v %v", replay, err)
	}
}
