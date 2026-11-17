// Package memory keeps model usage and transcripts in this process. It is
// what the file repository runs with: nothing survives a restart and nothing
// is shared between replicas, so a budget is per process.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/modelgovernor"
)

type usageKey struct {
	day, owner, capability, transport, configured, provider, outcome string
}

// Usage is safe for concurrent use.
type Usage struct {
	mu   sync.Mutex
	rows map[usageKey]*modelgovernor.UsageRow
}

func NewUsage() *Usage { return &Usage{rows: map[usageKey]*modelgovernor.UsageRow{}} }

func (store *Usage) Record(_ context.Context, record modelgovernor.UsageRecord) error {
	key := usageKey{record.Day, record.Owner, record.Capability, record.Transport, record.ConfiguredModel, record.ProviderModel, record.Outcome}
	store.mu.Lock()
	defer store.mu.Unlock()
	row := store.rows[key]
	if row == nil {
		row = &modelgovernor.UsageRow{
			Day: record.Day, Owner: record.Owner, Capability: record.Capability, Transport: record.Transport,
			ConfiguredModel: record.ConfiguredModel, ProviderModel: record.ProviderModel, Outcome: record.Outcome,
		}
		store.rows[key] = row
	}
	row.Calls++
	row.Attempts += int64(record.Attempts)
	row.PromptBytes += record.PromptBytes
	row.ResponseBytes += record.ResponseBytes
	row.LatencyMillis += record.Latency.Milliseconds()
	row.PromptTokens = add(row.PromptTokens, record.PromptTokens)
	row.CompletionTokens = add(row.CompletionTokens, record.CompletionTokens)
	if record.PromptTokens != nil || record.CompletionTokens != nil {
		row.CallsWithTokens++
	}
	return nil
}

// add keeps a sum null until a count arrives.
func add(sum, count *int64) *int64 {
	if count == nil {
		return sum
	}
	total := *count
	if sum != nil {
		total += *sum
	}
	return &total
}

func (store *Usage) OwnerDay(_ context.Context, owner, day string) (modelgovernor.Totals, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	var totals modelgovernor.Totals
	for key, row := range store.rows {
		if key.owner != owner || key.day != day || !modelgovernor.CountsTowardBudget(key.outcome) {
			continue
		}
		totals.Calls += row.Calls
		if row.PromptTokens != nil {
			totals.Tokens += *row.PromptTokens
		}
		if row.CompletionTokens != nil {
			totals.Tokens += *row.CompletionTokens
		}
	}
	return totals, nil
}

func (store *Usage) Summary(_ context.Context, query modelgovernor.UsageQuery) ([]modelgovernor.UsageRow, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	out := []modelgovernor.UsageRow{}
	for key, row := range store.rows {
		if (query.Owner != "" && key.owner != query.Owner) || key.day < query.FromDay || key.day > query.ToDay {
			continue
		}
		copied := *row
		copied.PromptTokens, copied.CompletionTokens = add(nil, row.PromptTokens), add(nil, row.CompletionTokens)
		out = append(out, copied)
	}
	sort.Slice(out, func(i, j int) bool { return rowLess(out[i], out[j]) })
	return out, nil
}

func rowLess(left, right modelgovernor.UsageRow) bool {
	l := []string{left.Day, left.Owner, left.Capability, left.Transport, left.ConfiguredModel, left.ProviderModel, left.Outcome}
	r := []string{right.Day, right.Owner, right.Capability, right.Transport, right.ConfiguredModel, right.ProviderModel, right.Outcome}
	for index := range l {
		if l[index] != r[index] {
			if index == 0 {
				return l[index] > r[index] // newest day first
			}
			return l[index] < r[index]
		}
	}
	return false
}

// Transcripts is safe for concurrent use.
type Transcripts struct {
	mu      sync.Mutex
	records []modelgovernor.TranscriptRecord
}

func NewTranscripts() *Transcripts { return &Transcripts{} }

func (store *Transcripts) Put(_ context.Context, record modelgovernor.TranscriptRecord, runCapBytes int64) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	size := int64(len(record.Prompt) + len(record.Response))
	for _, existing := range store.records {
		if existing.RunID == record.RunID {
			size += int64(len(existing.Prompt) + len(existing.Response))
		}
	}
	if runCapBytes > 0 && size > runCapBytes {
		return modelgovernor.ErrTranscriptRunFull
	}
	record.Prompt, record.Response = append([]byte{}, record.Prompt...), append([]byte{}, record.Response...)
	store.records = append(store.records, record)
	return nil
}

func (store *Transcripts) Run(_ context.Context, runID string, now time.Time) ([]modelgovernor.TranscriptRecord, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	out := []modelgovernor.TranscriptRecord{}
	for _, record := range store.records {
		if record.RunID == runID && record.ExpiresAt.After(now) {
			record.Prompt, record.Response = append([]byte{}, record.Prompt...), append([]byte{}, record.Response...)
			out = append(out, record)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Round < out[j].Round })
	return out, nil
}

func (store *Transcripts) Runs(_ context.Context, filter modelgovernor.RunFilter, now time.Time) ([]modelgovernor.RunSummary, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	byRun := map[string]*modelgovernor.RunSummary{}
	for _, record := range store.records {
		if !record.ExpiresAt.After(now) || (filter.Owner != "" && record.Owner != filter.Owner) ||
			(filter.SessionID != "" && record.SessionID != filter.SessionID) {
			continue
		}
		summary := byRun[record.RunID]
		if summary == nil {
			summary = &modelgovernor.RunSummary{
				RunID: record.RunID, SessionID: record.SessionID, Owner: record.Owner, Capability: record.Capability,
				FirstAt: record.CreatedAt, ExpiresAt: record.ExpiresAt,
			}
			byRun[record.RunID] = summary
		}
		summary.Rounds++
		summary.Bytes += int64(len(record.Prompt) + len(record.Response))
		if record.CreatedAt.Before(summary.FirstAt) {
			summary.FirstAt = record.CreatedAt
		}
		if record.ExpiresAt.Before(summary.ExpiresAt) {
			summary.ExpiresAt = record.ExpiresAt
		}
	}
	out := make([]modelgovernor.RunSummary, 0, len(byRun))
	for _, summary := range byRun {
		out = append(out, *summary)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].FirstAt.Equal(out[j].FirstAt) {
			return out[i].FirstAt.After(out[j].FirstAt)
		}
		return out[i].RunID < out[j].RunID
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (store *Transcripts) DeleteExpired(_ context.Context, now time.Time) (int64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	kept := store.records[:0]
	var deleted int64
	for _, record := range store.records {
		if record.ExpiresAt.After(now) {
			kept = append(kept, record)
		} else {
			deleted++
		}
	}
	store.records = kept
	return deleted, nil
}
