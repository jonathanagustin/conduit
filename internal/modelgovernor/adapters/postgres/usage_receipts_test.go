package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
	"github.com/jonathanagustin/conduit/internal/modelgovernor/adapters/storetest"
)

func TestPostgresUsageReceipts(t *testing.T) {
	storetest.RunUsageReceipts(t, func(t *testing.T) modelgovernor.UsageStore { return open(t) })
}

func TestPostgresAttemptedCalls(t *testing.T) {
	storetest.RunAttemptedCalls(t, func(t *testing.T) modelgovernor.UsageStore { return open(t) })
}

func TestPostgresUsageReceiptsContendAcrossIndependentPools(t *testing.T) {
	dsn := freshDSN(t)
	first, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	storetest.RunUsageReceiptContention(t, first, second)
}

func TestPostgresUsageCompletionRollsBackTheAggregateAndReceiptTogether(t *testing.T) {
	for _, table := range []string{"conduit_model_usage_daily", "conduit_model_usage_receipts"} {
		t.Run(table, func(t *testing.T) {
			store := open(t)
			ctx := context.Background()
			receipt := modelgovernor.UsageReceipt{ID: "interrupted-completion", Owner: "ada", Day: "2026-11-17"}
			if _, err := store.BeginUsage(ctx, receipt); err != nil {
				t.Fatal(err)
			}
			// Both names are fixed literals above. These objects live only in
			// this test's pgtest.Fresh database, never in the shared database.
			if _, err := store.db.ExecContext(ctx, `CREATE FUNCTION reject_model_usage() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected metering failure'; END $$`); err != nil {
				t.Fatal(err)
			}
			if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER reject_model_usage BEFORE INSERT OR UPDATE ON `+table+` FOR EACH ROW EXECUTE FUNCTION reject_model_usage()`); err != nil {
				t.Fatal(err)
			}
			record := modelgovernor.UsageRecord{ReceiptID: receipt.ID, Owner: receipt.Owner, Day: receipt.Day, Capability: "assistant", Transport: "http", Outcome: "ok", Attempts: 1}
			if err := store.Record(ctx, record); err == nil {
				t.Fatal("injected SQL write failure was ignored")
			}
			used, err := store.OwnerDay(ctx, receipt.Owner, receipt.Day)
			if err != nil || used.Calls != 0 {
				t.Fatalf("failed completion partially changed totals: %+v, %v", used, err)
			}
			if _, err := store.BeginUsage(ctx, modelgovernor.UsageReceipt{ID: "must-remain-blocked", Owner: receipt.Owner, Day: receipt.Day}); !errors.Is(err, modelgovernor.ErrUsagePending) {
				t.Fatalf("failed SQL completion reopened admission: %v", err)
			}
			if _, err := store.db.ExecContext(ctx, `DROP TRIGGER reject_model_usage ON `+table); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if err := store.Record(ctx, record); err != nil {
					t.Fatal(err)
				}
			}
			used, err = store.OwnerDay(ctx, receipt.Owner, receipt.Day)
			if err != nil || used.Calls != 1 {
				t.Fatalf("recovery lost or double-counted usage: %+v, %v", used, err)
			}
		})
	}
}

func TestPostgresUnsettledReceiptSurvivesPoolAndGovernorRestart(t *testing.T) {
	dsn := freshDSN(t)
	ctx := context.Background()
	first, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	day := "2026-11-17"
	if _, err := first.BeginUsage(ctx, modelgovernor.UsageReceipt{ID: "provider-outcome-lost-on-restart", Owner: "ada", Day: day}); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	newGovernor := func(at time.Time) *modelgovernor.Governor {
		return modelgovernor.New(modelgovernor.Config{DailyCallBudget: 1}, modelgovernor.Dependencies{Usage: restarted, Clock: fixedClock{now: at}})
	}
	start := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	called := 0
	call := func(governor *modelgovernor.Governor, owner string) error {
		ctx := modelgovernor.WithScope(ctx, modelgovernor.Scope{Owner: func() string { return owner }})
		_, err := governor.Do(ctx, modelgovernor.Call{Capability: modelgovernor.CapabilityAssistant, Transport: modelgovernor.TransportHTTP}, nil, func(context.Context) (modelgovernor.Result, error) { called++; return modelgovernor.Result{}, nil })
		return err
	}
	if err := call(newGovernor(start), "ada"); !errors.Is(err, modelgovernor.ErrBusy) || called != 0 {
		t.Fatalf("restart reset the accounting latch: calls=%d err=%v", called, err)
	}
	if err := call(newGovernor(start), "grace"); err != nil || called != 1 {
		t.Fatalf("orphan blocked unrelated owner: calls=%d err=%v", called, err)
	}
	if err := call(newGovernor(start.Add(24*time.Hour)), "ada"); err != nil || called != 2 {
		t.Fatalf("orphan blocked next UTC day: calls=%d err=%v", called, err)
	}
	if _, err := restarted.BeginUsage(ctx, modelgovernor.UsageReceipt{ID: "old-day-still-blocked", Owner: "ada", Day: day}); !errors.Is(err, modelgovernor.ErrUsagePending) {
		t.Fatalf("new-day admission erased unresolved old-day evidence: %v", err)
	}
}
