package modelgovernor_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

func TestQueuedModelCallsRecheckTheBudgetAfterAcquiringCapacity(t *testing.T) {
	const queued = 11
	fx := newFixture(modelgovernor.Config{GlobalConcurrency: 1, OwnerConcurrency: 1, DailyCallBudget: 1})
	ctx, cancel := context.WithCancel(as("ada"))
	t.Cleanup(cancel)
	release, first := hold(t, fx.governor, ctx)
	results := make(chan error, queued)
	var invoked atomic.Int64
	for range queued {
		go func() {
			_, err := fx.governor.Do(ctx, assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
				invoked.Add(1)
				return modelgovernor.Result{}, nil
			})
			results <- err
		}()
	}
	// acquire starts its queue timer after the initial budget check. Every
	// caller has therefore checked the still-unspent budget before release.
	eventually(t, "every caller to enter the capacity queue", func() bool {
		return len(fx.clock.Delays()) == queued+1
	})
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	for range queued {
		select {
		case err := <-results:
			if !errors.Is(err, modelgovernor.ErrBudgetExceeded) {
				t.Errorf("queued call returned %v, want a budget refusal", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a queued caller did not finish")
		}
	}
	if got := invoked.Load(); got != 0 {
		t.Fatalf("%d queued callers invoked the model after their predecessor spent the budget", got)
	}
	used, err := fx.usage.OwnerDay(ctx, "ada", modelgovernor.Day(fx.clock.Now()))
	if err != nil || used.Calls != 1 {
		t.Fatalf("usage = %+v, err = %v, want only the first model call", used, err)
	}
}

type observingUsage struct {
	modelgovernor.UsageStore
	record func()
}

func (store observingUsage) Record(ctx context.Context, record modelgovernor.UsageRecord) error {
	store.record()
	return store.UsageStore.Record(ctx, record)
}

func TestModelCapacityIsHeldUntilUsageIsRecorded(t *testing.T) {
	for _, auditFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful audit", true: "failed audit"}[auditFails], func(t *testing.T) {
			var governor *modelgovernor.Governor
			observed := -1
			fx := newFixture(modelgovernor.Config{GlobalConcurrency: 1}, func(deps *modelgovernor.Dependencies) {
				deps.Usage = observingUsage{UsageStore: deps.Usage, record: func() {
					observed, _ = governor.InFlight()
				}}
			})
			governor = fx.governor
			if auditFails {
				fx.audit.fail = errors.New("injected audit failure")
			}
			_, err := governor.Do(as("ada"), assistantCall, nil, reply("ok"))
			if auditFails != errors.Is(err, modelgovernor.ErrAuditUnavailable) {
				t.Fatalf("audit failure = %t, call error = %v", auditFails, err)
			}
			if observed != 1 {
				t.Fatalf("%d slots held while persisting completed usage, want 1", observed)
			}
			if calls, owners := governor.InFlight(); calls != 0 || owners != 0 {
				t.Fatalf("after metering, %d calls and %d owners still hold capacity", calls, owners)
			}
		})
	}
}

func TestQueuedCallIsMeteredOnTheUTCDayItAcquiresCapacity(t *testing.T) {
	fx := newFixture(modelgovernor.Config{GlobalConcurrency: 1, OwnerConcurrency: 1, DailyCallBudget: 1, QueueWait: time.Minute})
	fx.clock.Set(time.Date(2026, 11, 17, 23, 59, 59, 0, time.UTC))
	ctx, cancel := context.WithCancel(as("ada"))
	t.Cleanup(cancel)
	release, first := hold(t, fx.governor, ctx)
	done := make(chan error, 1)
	go func() {
		_, err := fx.governor.Do(ctx, assistantCall, nil, reply("the next day's call"))
		done <- err
	}()
	eventually(t, "the next call to queue", func() bool { return len(fx.clock.Delays()) == 2 })
	fx.clock.Advance(2 * time.Second)
	release()
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the new day's call was refused: %v", err)
	}
	for _, day := range []string{"2026-11-17", "2026-11-18"} {
		used, err := fx.usage.OwnerDay(ctx, "ada", day)
		if err != nil || used.Calls != 1 {
			t.Fatalf("%s usage = %+v, err = %v, want 1 call", day, used, err)
		}
	}
}
