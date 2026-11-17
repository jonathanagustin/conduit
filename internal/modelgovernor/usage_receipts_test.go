package modelgovernor_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

type usageWriteOutage struct {
	modelgovernor.UsageStore
	fail, commitBeforeError bool
}

type allUsageWritesFail struct {
	modelgovernor.UsageStore
	begins int
}

func (store *allUsageWritesFail) BeginUsage(ctx context.Context, receipt modelgovernor.UsageReceipt) (modelgovernor.Totals, error) {
	store.begins++
	return store.UsageStore.BeginUsage(ctx, receipt)
}

func (*allUsageWritesFail) Record(context.Context, modelgovernor.UsageRecord) error {
	return errors.New("injected failed usage write with healthy reads")
}

func TestOnlyBudgetedCallsStopWhenAllUsageWritesFail(t *testing.T) {
	for _, budgeted := range []bool{true, false} {
		t.Run(map[bool]string{true: "budgeted", false: "unbudgeted"}[budgeted], func(t *testing.T) {
			limit := int64(0)
			if budgeted {
				limit = 1
			}
			var outage *allUsageWritesFail
			fx := newFixture(modelgovernor.Config{DailyCallBudget: limit, GlobalConcurrency: 1, OwnerConcurrency: 1}, func(deps *modelgovernor.Dependencies) {
				outage = &allUsageWritesFail{UsageStore: deps.Usage}
				deps.Usage = outage
				deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			})
			invoked := 0
			for range 12 {
				_, _ = fx.governor.Do(as("ada"), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) { invoked++; return modelgovernor.Result{}, nil })
			}
			want, receipts := 12, 0
			if budgeted {
				want, receipts = 1, 1
			}
			if invoked != want || outage.begins != receipts {
				t.Fatalf("budgeted=%t invoked %d models and began %d receipts, want %d models/%d receipts", budgeted, invoked, outage.begins, want, receipts)
			}
		})
	}
}

func (store *usageWriteOutage) Record(ctx context.Context, record modelgovernor.UsageRecord) error {
	if store.fail && record.ReceiptID != "" && record.Owner == "ada" {
		if store.commitBeforeError {
			if err := store.UsageStore.Record(ctx, record); err != nil {
				return err
			}
		}
		return errors.New("injected usage completion acknowledgement failure")
	}
	return store.UsageStore.Record(ctx, record)
}

func TestBudgetedUsageWriteFailureStopsSequentialCallsAndRecoversExactlyOnce(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(map[bool]string{false: "write refused", true: "commit acknowledgement lost"}[committed], func(t *testing.T) {
			var outage *usageWriteOutage
			fx := newFixture(modelgovernor.Config{DailyCallBudget: 2, GlobalConcurrency: 1, OwnerConcurrency: 1}, func(deps *modelgovernor.Dependencies) {
				outage = &usageWriteOutage{UsageStore: deps.Usage, fail: true, commitBeforeError: committed}
				deps.Usage = outage
				deps.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
			})
			invoked := 0
			invoke := func(context.Context) (modelgovernor.Result, error) {
				invoked++
				return modelgovernor.Result{PromptTokens: tokens(7)}, nil
			}
			result, err := fx.governor.Do(as("ada"), assistantCall, nil, invoke)
			if err != nil {
				t.Fatal(err)
			}
			// The caller owns its result after return; retained recovery
			// evidence must not share mutable token-count pointers with it.
			*result.PromptTokens = 99
			for range 11 {
				if _, err := fx.governor.Do(as("ada"), assistantCall, nil, invoke); !errors.Is(err, modelgovernor.ErrUsageUnavailable) {
					t.Fatalf("known unmetered spend admitted another call: %v", err)
				}
			}
			if invoked != 1 {
				t.Fatalf("%d sequential model invocations while usage could not be acknowledged, want 1", invoked)
			}
			if _, err := fx.governor.Do(as("grace"), assistantCall, nil, reply("unrelated owner")); err != nil {
				t.Fatalf("one owner's failure blocked another: %v", err)
			}
			outage.fail = false
			if _, err := fx.governor.Do(as("ada"), assistantCall, nil, invoke); err != nil {
				t.Fatalf("exact recovery could not admit remaining budget: %v", err)
			}
			if _, err := fx.governor.Do(as("ada"), assistantCall, nil, invoke); !errors.Is(err, modelgovernor.ErrBudgetExceeded) {
				t.Fatalf("the spent budget returned %v", err)
			}
			used, err := fx.usage.OwnerDay(context.Background(), "ada", modelgovernor.Day(fx.clock.Now()))
			if invoked != 2 || err != nil || used.Calls != 2 || used.Tokens != 14 {
				t.Fatalf("recovery lost or duplicated spend: invocations=%d usage=%+v err=%v", invoked, used, err)
			}
		})
	}
}

type usageBeginOutage struct {
	modelgovernor.UsageStore
	fail, commitBeforeError, failCancellation bool
}

func (store *usageBeginOutage) BeginUsage(ctx context.Context, receipt modelgovernor.UsageReceipt) (modelgovernor.Totals, error) {
	if store.fail {
		if store.commitBeforeError {
			if _, err := store.UsageStore.BeginUsage(ctx, receipt); err != nil {
				return modelgovernor.Totals{}, err
			}
		}
		return modelgovernor.Totals{}, errors.New("injected begin acknowledgement failure")
	}
	return store.UsageStore.BeginUsage(ctx, receipt)
}

func (store *usageBeginOutage) CancelUsage(ctx context.Context, id string) error {
	if store.failCancellation {
		return errors.New("injected cancellation failure")
	}
	return store.UsageStore.CancelUsage(ctx, id)
}

func TestFailedUsageReservationNeverInvokesAndAmbiguousBeginIsSafelyCancelled(t *testing.T) {
	for _, scenario := range []struct {
		name                    string
		committed, cleanupFails bool
	}{
		{"begin failed before commit", false, false}, {"begin acknowledgement lost", true, false}, {"begin and cancellation acknowledgements unavailable", true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var outage *usageBeginOutage
			fx := newFixture(modelgovernor.Config{DailyCallBudget: 1}, func(deps *modelgovernor.Dependencies) {
				outage = &usageBeginOutage{UsageStore: deps.Usage, fail: true, commitBeforeError: scenario.committed, failCancellation: scenario.cleanupFails}
				deps.Usage = outage
			})
			invoked := false
			invoke := func(context.Context) (modelgovernor.Result, error) {
				invoked = true
				return modelgovernor.Result{}, nil
			}
			if _, err := fx.governor.Do(as("ada"), assistantCall, nil, invoke); !errors.Is(err, modelgovernor.ErrUsageUnavailable) || invoked {
				t.Fatalf("failed begin invoked a provider: called=%t err=%v", invoked, err)
			}
			outage.fail = false
			_, err := fx.governor.Do(as("ada"), assistantCall, nil, invoke)
			if scenario.cleanupFails {
				if !errors.Is(err, modelgovernor.ErrBusy) || invoked {
					t.Fatalf("uncertain receipt was silently reset: called=%t err=%v", invoked, err)
				}
			} else if err != nil || !invoked {
				t.Fatalf("unused reservation was not released: called=%t err=%v", invoked, err)
			}
			used, err := fx.usage.OwnerDay(context.Background(), "ada", modelgovernor.Day(fx.clock.Now()))
			want := int64(1)
			if scenario.cleanupFails {
				want = 0
			}
			if err != nil || used.Calls != want {
				t.Fatalf("begin failure counted nonexistent spend: %+v, %v", used, err)
			}
		})
	}
}

type spendBeforeUsageReservation struct {
	modelgovernor.UsageStore
	spend bool
}

func (store *spendBeforeUsageReservation) BeginUsage(ctx context.Context, receipt modelgovernor.UsageReceipt) (modelgovernor.Totals, error) {
	if store.spend {
		store.spend = false
		if err := store.UsageStore.Record(ctx, modelgovernor.UsageRecord{Owner: receipt.Owner, Day: receipt.Day, Capability: "assistant", Transport: "http", Outcome: "ok", Attempts: 1}); err != nil {
			return modelgovernor.Totals{}, err
		}
	}
	return store.UsageStore.BeginUsage(ctx, receipt)
}

func TestBudgetIsRecheckedWithTheAtomicUsageReservationAndUnusedReceiptIsCancelled(t *testing.T) {
	fx := newFixture(modelgovernor.Config{DailyCallBudget: 1}, func(deps *modelgovernor.Dependencies) {
		deps.Usage = &spendBeforeUsageReservation{UsageStore: deps.Usage, spend: true}
	})
	invoked := false
	_, err := fx.governor.Do(as("ada"), assistantCall, nil, func(context.Context) (modelgovernor.Result, error) {
		invoked = true
		return modelgovernor.Result{}, nil
	})
	if !errors.Is(err, modelgovernor.ErrBudgetExceeded) || invoked {
		t.Fatalf("stale pre-reservation read admitted a model: invoked=%t err=%v", invoked, err)
	}
	probe := modelgovernor.UsageReceipt{ID: "unused-reservation-was-released", Owner: "ada", Day: modelgovernor.Day(fx.clock.Now())}
	used, err := fx.usage.BeginUsage(context.Background(), probe)
	if err != nil || used.Calls != 1 {
		t.Fatalf("refused call retained a reservation or counted spend: %+v, %v", used, err)
	}
	if err := fx.usage.CancelUsage(context.Background(), probe.ID); err != nil {
		t.Fatal(err)
	}
}

func TestAnUnsettledPriorDayDoesNotBlockTheNewUTCDay(t *testing.T) {
	fx := newFixture(modelgovernor.Config{DailyCallBudget: 1})
	oldDay := modelgovernor.Day(fx.clock.Now())
	if _, err := fx.usage.BeginUsage(context.Background(), modelgovernor.UsageReceipt{ID: "orphan", Owner: "ada", Day: oldDay}); err != nil {
		t.Fatal(err)
	}
	called := false
	invoke := func(context.Context) (modelgovernor.Result, error) { called = true; return modelgovernor.Result{}, nil }
	if _, err := fx.governor.Do(as("ada"), assistantCall, nil, invoke); !errors.Is(err, modelgovernor.ErrBusy) || called {
		t.Fatalf("orphan did not block its day: called=%t err=%v", called, err)
	}
	fx.clock.Advance(24 * time.Hour)
	if _, err := fx.governor.Do(as("ada"), assistantCall, nil, invoke); err != nil || !called {
		t.Fatalf("prior-day orphan blocked the new day: called=%t err=%v", called, err)
	}
	if _, err := fx.usage.BeginUsage(context.Background(), modelgovernor.UsageReceipt{ID: "still-blocked", Owner: "ada", Day: oldDay}); !errors.Is(err, modelgovernor.ErrUsagePending) {
		t.Fatalf("rollover silently cleared the prior-day receipt: %v", err)
	}
}
