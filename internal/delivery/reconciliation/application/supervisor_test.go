package application

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
)

func TestSupervisorPrimeSynchronizesRepairsAndDrains(t *testing.T) {
	now := time.Date(2026, 8, 14, 22, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	store := newFakeStore()
	planner := newFakePlanner(clock)
	worker := newTestWorker(t, store, planner, clock)
	key := domain.Key{Environment: "dev", Installation: "catalog"}
	synchronizer := &fakeSynchronizer{sync: func() {
		store.upsertDesired(desiredSnapshot(key, 1, "target-a", now), false)
	}}
	supervisor, err := NewSupervisor(worker, synchronizer, SupervisorConfig{
		PollInterval: time.Second, SyncInterval: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := supervisor.Prime(context.Background()); err != nil {
		t.Fatal(err)
	}
	if synchronizer.calls != 1 || len(store.decisions) != 1 || len(store.audits) != 1 ||
		len(store.receipts) != 1 || len(store.queue) != 0 || len(store.outbox) != 0 {
		t.Fatalf("sync=%d decisions=%d audits=%d receipts=%d queue=%d outbox=%d",
			synchronizer.calls, len(store.decisions), len(store.audits), len(store.receipts), len(store.queue), len(store.outbox))
	}
}

type fakeSynchronizer struct {
	calls int
	sync  func()
}

func (synchronizer *fakeSynchronizer) Sync(context.Context) error {
	synchronizer.calls++
	if synchronizer.sync != nil {
		synchronizer.sync()
	}
	return nil
}
