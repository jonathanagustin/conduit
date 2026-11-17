package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

func receiptRecord(id, owner, day string) modelgovernor.UsageRecord {
	return modelgovernor.UsageRecord{ReceiptID: id, Owner: owner, Day: day,
		Capability: "assistant", Transport: "http", Outcome: "ok", Attempts: 1,
		PromptTokens: count(5), CompletionTokens: count(7)}
}

func RunUsageReceipts(t *testing.T, open func(*testing.T) modelgovernor.UsageStore) {
	t.Run("begin reserves without spending and exact completion is idempotent", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()
		receipt := modelgovernor.UsageReceipt{ID: "first", Owner: "ada", Day: "2026-11-17"}
		for range 2 {
			used, err := store.BeginUsage(ctx, receipt)
			if err != nil || used != (modelgovernor.Totals{}) {
				t.Fatalf("begin = %+v, %v", used, err)
			}
		}
		if _, err := store.BeginUsage(ctx, modelgovernor.UsageReceipt{ID: "blocked", Owner: "ada", Day: receipt.Day}); !errors.Is(err, modelgovernor.ErrUsagePending) {
			t.Fatalf("an unsettled owner/day admitted another receipt: %v", err)
		}
		for _, other := range []modelgovernor.UsageReceipt{{ID: "other-owner", Owner: "grace", Day: receipt.Day}, {ID: "other-day", Owner: "ada", Day: "2026-11-18"}} {
			if _, err := store.BeginUsage(ctx, other); err != nil {
				t.Fatalf("unrelated scope refused: %v", err)
			}
			if err := store.CancelUsage(ctx, other.ID); err != nil {
				t.Fatal(err)
			}
		}
		record := receiptRecord(receipt.ID, receipt.Owner, receipt.Day)
		var group sync.WaitGroup
		for range 8 {
			group.Add(1)
			go func() {
				defer group.Done()
				if err := store.Record(ctx, record); err != nil {
					t.Error(err)
				}
			}()
		}
		group.Wait()
		used, err := store.OwnerDay(ctx, receipt.Owner, receipt.Day)
		if err != nil || used.Calls != 1 || used.Tokens != 12 {
			t.Fatalf("completion retries counted more than once: %+v, %v", used, err)
		}
		changed := record
		changed.ResponseBytes++
		if err := store.Record(ctx, changed); !errors.Is(err, modelgovernor.ErrUsageReceiptConflict) {
			t.Fatalf("different payload replaced completed evidence: %v", err)
		}
		if err := store.CancelUsage(ctx, receipt.ID); !errors.Is(err, modelgovernor.ErrUsageReceiptConflict) {
			t.Fatalf("completed receipt could be cancelled: %v", err)
		}
		if _, err := store.BeginUsage(ctx, receipt); !errors.Is(err, modelgovernor.ErrUsageReceiptConflict) {
			t.Fatalf("completed receipt was opened again: %v", err)
		}
		if _, err := store.BeginUsage(ctx, modelgovernor.UsageReceipt{ID: "next", Owner: receipt.Owner, Day: receipt.Day}); err != nil {
			t.Fatal(err)
		}
		if err := store.CancelUsage(ctx, "next"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("unused receipts can be cancelled without accounting a model call", func(t *testing.T) {
		store := open(t)
		ctx := context.Background()
		receipt := modelgovernor.UsageReceipt{ID: "unused", Owner: "ada", Day: "2026-11-17"}
		if _, err := store.BeginUsage(ctx, receipt); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if err := store.CancelUsage(ctx, receipt.ID); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Record(ctx, receiptRecord(receipt.ID, receipt.Owner, receipt.Day)); !errors.Is(err, modelgovernor.ErrUsageReceiptConflict) {
			t.Fatalf("cancelled receipt accepted accounting: %v", err)
		}
		receipt.ID = "successor"
		used, err := store.BeginUsage(ctx, receipt)
		if err != nil || used.Calls != 0 {
			t.Fatalf("unused cancellation counted a call or left a block: %+v, %v", used, err)
		}
		if err := store.CancelUsage(ctx, receipt.ID); err != nil {
			t.Fatal(err)
		}
	})
}

// RunUsageReceiptContention also runs with two independent PostgreSQL pools.
func RunUsageReceiptContention(t *testing.T, first, second modelgovernor.UsageStore) {
	ctx := context.Background()
	start := make(chan struct{})
	type answer struct {
		receipt modelgovernor.UsageReceipt
		err     error
	}
	results := make(chan answer, 2)
	for index, store := range []modelgovernor.UsageStore{first, second} {
		receipt := modelgovernor.UsageReceipt{ID: []string{"first-pool", "second-pool"}[index], Owner: "ada", Day: "2026-11-17"}
		go func() { <-start; _, err := store.BeginUsage(ctx, receipt); results <- answer{receipt, err} }()
	}
	close(start)
	one, two := <-results, <-results
	if one.err != nil {
		one, two = two, one
	}
	if one.err != nil || !errors.Is(two.err, modelgovernor.ErrUsagePending) {
		t.Fatalf("competing begins returned %v and %v", one.err, two.err)
	}
	record := receiptRecord(one.receipt.ID, "ada", one.receipt.Day)
	if err := first.Record(ctx, record); err != nil {
		t.Fatal(err)
	}
	// Treat the first successful commit's acknowledgement as lost; retry on
	// another pool using exactly the same receipt and final evidence.
	if err := second.Record(ctx, record); err != nil {
		t.Fatal(err)
	}
	used, err := second.BeginUsage(ctx, two.receipt)
	if err != nil || used.Calls != 1 || used.Tokens != 12 {
		t.Fatalf("successor did not see exactly one committed completion: %+v, %v", used, err)
	}
	if err := second.CancelUsage(ctx, two.receipt.ID); err != nil {
		t.Fatal(err)
	}
}
