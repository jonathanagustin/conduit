package modelgovernor_test

import (
	"context"
	"errors"
	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/memory"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type observedBudgetUsage struct {
	modelgovernor.UsageStore
	reads chan struct{}
}

func (store observedBudgetUsage) OwnerDay(ctx context.Context, owner, day string) (modelgovernor.Totals, error) {
	total, err := store.UsageStore.OwnerDay(ctx, owner, day)
	select {
	case store.reads <- struct{}{}:
	default:
	}
	return total, err
}
func TestQueuedModelCallsRecheckBudgetBeforeInvoking(t *testing.T) {
	usage := memory.NewUsage()
	reads := make(chan struct{}, 64)
	fx := newFixture(modelgovernor.Config{DailyCallBudget: 1, OwnerConcurrency: 1, GlobalConcurrency: 1}, func(deps *modelgovernor.Dependencies) {
		deps.Usage = observedBudgetUsage{UsageStore: usage, reads: reads}
	})
	ctx, cancel := context.WithTimeout(as("ada"), 5*time.Second)
	defer cancel()
	entered, finish := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(finish) }) }
	defer release()
	var attempted atomic.Int64
	invoke := func(ctx context.Context) (modelgovernor.Result, error) {
		if attempted.Add(1) == 1 {
			close(entered)
			select {
			case <-finish:
			case <-ctx.Done():
				return modelgovernor.Result{}, ctx.Err()
			}
		}
		return modelgovernor.Result{Response: []byte("ok")}, nil
	}
	const queued = 6
	results := make(chan error, queued+1)
	call := func() { _, err := fx.governor.Do(ctx, assistantCall, nil, invoke); results <- err }
	go call()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first model call never entered")
	}
	for len(reads) > 0 {
		<-reads
	}
	for range queued {
		go call()
	}
	for range queued {
		select {
		case <-reads:
		case <-ctx.Done():
			t.Fatal("queued calls did not read the initial budget")
		}
	}
	release()
	for range queued + 1 {
		err := <-results
		if err != nil && !errors.Is(err, modelgovernor.ErrBudgetExceeded) {
			t.Fatal(err)
		}
	}
	if got := attempted.Load(); got != 1 {
		t.Fatalf("%d calls reached the model with budget 1 and concurrency 1; queued admission reused a stale budget", got)
	}
}

type delayedBudgetWrite struct {
	modelgovernor.UsageStore
	entered, release chan struct{}
}

func (store delayedBudgetWrite) Record(ctx context.Context, record modelgovernor.UsageRecord) error {
	close(store.entered)
	select {
	case <-store.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return store.UsageStore.Record(ctx, record)
}
func TestModelCallRetainsAdmissionUntilUsageIsRecorded(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	fx := newFixture(modelgovernor.Config{DailyCallBudget: 1, OwnerConcurrency: 1, GlobalConcurrency: 1}, func(deps *modelgovernor.Dependencies) {
		deps.Usage = delayedBudgetWrite{UsageStore: memory.NewUsage(), entered: entered, release: release}
	})
	ctx, cancel := context.WithTimeout(as("ada"), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := fx.governor.Do(ctx, assistantCall, nil, reply("ok")); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("usage write never started")
	}
	if calls, _ := fx.governor.InFlight(); calls != 1 {
		t.Errorf("admission released before metering: in-flight=%d", calls)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("metered call did not complete")
	}
	if calls, owners := fx.governor.InFlight(); calls != 0 || owners != 0 {
		t.Fatalf("completed call leaked admission: %d calls, %d owners", calls, owners)
	}
}
