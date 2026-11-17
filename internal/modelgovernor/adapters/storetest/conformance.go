// Package storetest is the conformance suite both modelgovernor store
// adapters run. The memory adapter runs it on every `go test`; the PostgreSQL
// adapter runs it when CONDUIT_TEST_POSTGRES_DSN names a server.
package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

var epoch = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)

func count(n int64) *int64 { return &n }

// RunUsage exercises a UsageStore. open returns an empty store.
func RunUsage(t *testing.T, open func(*testing.T) modelgovernor.UsageStore) {
	ctx := context.Background()
	call := func(owner, capability, outcome string, prompt, completion *int64) modelgovernor.UsageRecord {
		attempts := 2
		switch outcome {
		case "throttled", "budget_exceeded", "usage_unavailable":
			attempts = 0
		}
		return modelgovernor.UsageRecord{
			Day: "2026-11-17", Owner: owner, Capability: capability, Transport: "http", ConfiguredModel: "m",
			ProviderModel: "m-1", Outcome: outcome, Attempts: attempts, PromptBytes: 10, ResponseBytes: 20,
			PromptTokens: prompt, CompletionTokens: completion, Latency: 150 * time.Millisecond,
		}
	}
	t.Run("calls sharing a key sum into one row", func(t *testing.T) {
		store := open(t)
		for range 3 {
			if err := store.Record(ctx, call("ada", "assistant", "ok", count(5), count(7))); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := store.Summary(ctx, modelgovernor.UsageQuery{Owner: "ada", FromDay: "2026-11-17", ToDay: "2026-11-17"})
		if err != nil || len(rows) != 1 {
			t.Fatalf("rows = %+v, err = %v", rows, err)
		}
		row := rows[0]
		if row.Calls != 3 || row.Attempts != 6 || row.PromptBytes != 30 || row.ResponseBytes != 60 || row.LatencyMillis != 450 ||
			row.PromptTokens == nil || *row.PromptTokens != 15 || row.CompletionTokens == nil || *row.CompletionTokens != 21 ||
			row.CallsWithTokens != 3 || row.Day != "2026-11-17" {
			t.Fatalf("row = %+v", row)
		}
	})
	t.Run("tokens stay null until a provider reports them", func(t *testing.T) {
		store := open(t)
		if err := store.Record(ctx, call("ada", "planning", "ok", nil, nil)); err != nil {
			t.Fatal(err)
		}
		rows, _ := store.Summary(ctx, modelgovernor.UsageQuery{FromDay: "2026-11-17", ToDay: "2026-11-17"})
		if len(rows) != 1 || rows[0].PromptTokens != nil || rows[0].CompletionTokens != nil || rows[0].CallsWithTokens != 0 {
			t.Fatalf("rows = %+v", rows)
		}
		if err := store.Record(ctx, call("ada", "planning", "ok", count(4), nil)); err != nil {
			t.Fatal(err)
		}
		rows, _ = store.Summary(ctx, modelgovernor.UsageQuery{FromDay: "2026-11-17", ToDay: "2026-11-17"})
		if rows[0].PromptTokens == nil || *rows[0].PromptTokens != 4 || rows[0].CompletionTokens != nil || rows[0].CallsWithTokens != 1 {
			t.Fatalf("rows = %+v", rows)
		}
	})
	t.Run("a day's totals are one owner's and leave out refused calls", func(t *testing.T) {
		store := open(t)
		records := []modelgovernor.UsageRecord{
			call("ada", "assistant", "ok", count(5), count(7)),
			call("ada", "planning", "upstream_error", nil, nil),
			call("ada", "assistant", "throttled", nil, nil),
			call("ada", "assistant", "budget_exceeded", nil, nil),
			call("grace", "assistant", "ok", count(100), count(100)),
		}
		other := call("ada", "assistant", "ok", count(1000), count(1000))
		other.Day = "2026-11-16"
		for _, record := range append(records, other) {
			if err := store.Record(ctx, record); err != nil {
				t.Fatal(err)
			}
		}
		totals, err := store.OwnerDay(ctx, "ada", "2026-11-17")
		if err != nil || totals.Calls != 2 || totals.Tokens != 12 {
			t.Fatalf("totals = %+v, err = %v", totals, err)
		}
		if empty, err := store.OwnerDay(ctx, "nobody", "2026-11-17"); err != nil || empty != (modelgovernor.Totals{}) {
			t.Fatalf("empty = %+v, err = %v", empty, err)
		}
		mine, _ := store.Summary(ctx, modelgovernor.UsageQuery{Owner: "grace", FromDay: "2026-11-01", ToDay: "2026-11-30"})
		if len(mine) != 1 || mine[0].Owner != "grace" {
			t.Fatalf("an owner filter returned %+v", mine)
		}
		all, _ := store.Summary(ctx, modelgovernor.UsageQuery{FromDay: "2026-11-17", ToDay: "2026-11-17"})
		if len(all) != 5 {
			t.Fatalf("one day of every owner = %d rows, want 5", len(all))
		}
	})
	t.Run("concurrent records all land", func(t *testing.T) {
		store := open(t)
		var group sync.WaitGroup
		for range 20 {
			group.Add(1)
			go func() {
				defer group.Done()
				if err := store.Record(ctx, call("ada", "assistant", "ok", count(1), count(1))); err != nil {
					t.Error(err)
				}
			}()
		}
		group.Wait()
		totals, _ := store.OwnerDay(ctx, "ada", "2026-11-17")
		if totals.Calls != 20 || totals.Tokens != 40 {
			t.Fatalf("totals = %+v", totals)
		}
	})
}

func record(run string, round int, prompt, response string, expires time.Time) modelgovernor.TranscriptRecord {
	return modelgovernor.TranscriptRecord{
		RunID: run, SessionID: "assistant-thread/t1", Owner: "ada", Capability: "assistant", Round: round,
		SentPromptSHA256: modelgovernor.Digest([]byte(prompt)), ReceivedResponseSHA256: modelgovernor.Digest([]byte(response)),
		StoredPromptSHA256: modelgovernor.Digest([]byte(prompt)), StoredResponseSHA256: modelgovernor.Digest([]byte(response)),
		Prompt: []byte(prompt), Response: []byte(response), CreatedAt: epoch.Add(time.Duration(round) * time.Second), ExpiresAt: expires,
	}
}

// RunTranscripts exercises a TranscriptStore. open returns an empty store.
func RunTranscripts(t *testing.T, open func(*testing.T) modelgovernor.TranscriptStore) {
	ctx := context.Background()
	later := epoch.Add(24 * time.Hour)
	t.Run("a run reads back in round order with its bytes", func(t *testing.T) {
		store := open(t)
		for _, round := range []int{2, 1, 3} {
			if err := store.Put(ctx, record("airun_a", round, "p", "", later), 0); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.Put(ctx, record("airun_b", 1, "other", "run", later), 0); err != nil {
			t.Fatal(err)
		}
		got, err := store.Run(ctx, "airun_a", epoch)
		if err != nil || len(got) != 3 {
			t.Fatalf("got %d records, err = %v", len(got), err)
		}
		for index, item := range got {
			if item.Round != index+1 || string(item.Prompt) != "p" || item.Response == nil && len(item.Response) != 0 ||
				item.SentPromptSHA256 != modelgovernor.Digest([]byte("p")) || !item.ExpiresAt.Equal(later) || item.Owner != "ada" {
				t.Fatalf("record %d = %+v", index, item)
			}
		}
	})
	t.Run("the run cap refuses the write that would pass it", func(t *testing.T) {
		store := open(t)
		if err := store.Put(ctx, record("airun_a", 1, "12345", "12345", later), 16); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(ctx, record("airun_a", 2, "1234", "123", later), 16); !errors.Is(err, modelgovernor.ErrTranscriptRunFull) {
			t.Fatalf("a write past the cap returned %v", err)
		}
		if err := store.Put(ctx, record("airun_a", 2, "123", "123", later), 16); err != nil {
			t.Fatalf("a write up to the cap was refused: %v", err)
		}
		if err := store.Put(ctx, record("airun_other", 1, "12345", "12345", later), 16); err != nil {
			t.Fatalf("the cap is per run: %v", err)
		}
	})
	t.Run("expired records are not read and only they are deleted", func(t *testing.T) {
		store := open(t)
		if err := store.Put(ctx, record("airun_a", 1, "old", "old", epoch.Add(time.Hour)), 0); err != nil {
			t.Fatal(err)
		}
		if err := store.Put(ctx, record("airun_a", 2, "new", "new", epoch.Add(3*time.Hour)), 0); err != nil {
			t.Fatal(err)
		}
		now := epoch.Add(time.Hour) // exactly at the first expiry
		got, _ := store.Run(ctx, "airun_a", now)
		if len(got) != 1 || got[0].Round != 2 {
			t.Fatalf("an expired, unswept record was read: %+v", got)
		}
		deleted, err := store.DeleteExpired(ctx, now)
		if err != nil || deleted != 1 {
			t.Fatalf("deleted = %d, err = %v", deleted, err)
		}
		if again, _ := store.DeleteExpired(ctx, now); again != 0 {
			t.Fatalf("a second sweep deleted %d", again)
		}
		// Read as of the past: the unexpired record is still there.
		got, _ = store.Run(ctx, "airun_a", epoch)
		if len(got) != 1 || got[0].Round != 2 {
			t.Fatalf("the sweep removed an unexpired record: %+v", got)
		}
	})
	t.Run("two sweepers at once delete each record once", func(t *testing.T) {
		store := open(t)
		for round := 1; round <= 30; round++ {
			if err := store.Put(ctx, record("airun_a", round, "p", "r", epoch.Add(time.Minute)), 0); err != nil {
				t.Fatal(err)
			}
		}
		var group sync.WaitGroup
		var mu sync.Mutex
		var total int64
		for range 2 {
			group.Add(1)
			go func() {
				defer group.Done()
				deleted, err := store.DeleteExpired(ctx, epoch.Add(time.Hour))
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				total += deleted
				mu.Unlock()
			}()
		}
		group.Wait()
		if total != 30 {
			t.Fatalf("two sweepers deleted %d records between them, want 30", total)
		}
	})
	t.Run("runs are summarized and filtered", func(t *testing.T) {
		store := open(t)
		_ = store.Put(ctx, record("airun_a", 1, "pp", "rr", later), 0)
		_ = store.Put(ctx, record("airun_a", 2, "pp", "rr", later), 0)
		other := record("airun_b", 1, "p", "r", later)
		other.Owner, other.SessionID = "grace", "planning-session/s9"
		_ = store.Put(ctx, other, 0)
		expired := record("airun_c", 1, "p", "r", epoch.Add(-time.Hour))
		_ = store.Put(ctx, expired, 0)
		all, err := store.Runs(ctx, modelgovernor.RunFilter{}, epoch)
		if err != nil || len(all) != 2 {
			t.Fatalf("runs = %+v, err = %v", all, err)
		}
		mine, _ := store.Runs(ctx, modelgovernor.RunFilter{Owner: "ada"}, epoch)
		if len(mine) != 1 || mine[0].RunID != "airun_a" || mine[0].Rounds != 2 || mine[0].Bytes != 8 {
			t.Fatalf("ada's runs = %+v", mine)
		}
		session, _ := store.Runs(ctx, modelgovernor.RunFilter{SessionID: "planning-session/s9"}, epoch)
		if len(session) != 1 || session[0].RunID != "airun_b" {
			t.Fatalf("session runs = %+v", session)
		}
	})
}
