package domain

import "math"

// CONDUIT-INVENTED: assistant.ReportedTurnUsage
// Displaced-by: An authoritative assistant contract for reported per-turn token usage and incomplete multi-round counts.

// ReportedTokenUsage contains only provider-reported counts. A nil side is
// unknown, not zero. Turn totals are complete only if every round reports that
// side; they do not account for unreported retries or establish billing.
type ReportedTokenUsage struct {
	PromptTokens     *int64 `json:"promptTokens"`
	CompletionTokens *int64 `json:"completionTokens"`
}

// IncludeReportedUsage returns a new total, without changing either input.
// A non-nil total with nil counts remembers an earlier unreported round.
func IncludeReportedUsage(total, next *ReportedTokenUsage) *ReportedTokenUsage {
	var prompt, completion *int64
	if next != nil {
		prompt, completion = next.PromptTokens, next.CompletionTokens
	}
	if total == nil {
		return &ReportedTokenUsage{PromptTokens: addReported(nil, prompt, true), CompletionTokens: addReported(nil, completion, true)}
	}
	return &ReportedTokenUsage{PromptTokens: addReported(total.PromptTokens, prompt, false), CompletionTokens: addReported(total.CompletionTokens, completion, false)}
}

func addReported(total, next *int64, first bool) *int64 {
	if next == nil || *next < 0 || (!first && (total == nil || *total < 0)) {
		return nil
	}
	value := *next
	if !first {
		if *total > math.MaxInt64-value {
			return nil
		}
		value += *total
	}
	return &value
}

// Clone isolates stored counts from callers and returned thread snapshots.
func (usage *ReportedTokenUsage) Clone() *ReportedTokenUsage {
	if usage == nil {
		return nil
	}
	return &ReportedTokenUsage{PromptTokens: addReported(nil, usage.PromptTokens, true), CompletionTokens: addReported(nil, usage.CompletionTokens, true)}
}

// Valid reports whether each present count is nonnegative.
func (usage *ReportedTokenUsage) Valid() bool {
	return usage == nil || ((usage.PromptTokens == nil || *usage.PromptTokens >= 0) && (usage.CompletionTokens == nil || *usage.CompletionTokens >= 0))
}
