package modelgovernor

import (
	"context"
	"time"
)

// UsageRecord is one model call as it is metered. Token counts are nil when
// the provider reported none; they are never estimated.
type UsageRecord struct {
	// Day is the UTC calendar day, YYYY-MM-DD.
	Day             string
	Owner           string
	Capability      string
	Transport       string
	ConfiguredModel string
	ProviderModel   string
	Outcome         string
	// Attempts is zero for a call refused before it was tried.
	Attempts         int
	PromptBytes      int64
	ResponseBytes    int64
	PromptTokens     *int64
	CompletionTokens *int64
	Latency          time.Duration
}

// UsageRow is usage summed over the calls that share its key. It answers
// "calls and tokens per owner per day per capability".
type UsageRow struct {
	Day             string `json:"day"`
	Owner           string `json:"owner"`
	Capability      string `json:"capability"`
	Transport       string `json:"transport"`
	ConfiguredModel string `json:"configuredModel"`
	ProviderModel   string `json:"providerModel"`
	Outcome         string `json:"outcome"`
	Calls           int64  `json:"calls"`
	Attempts        int64  `json:"attempts"`
	PromptBytes     int64  `json:"promptBytes"`
	ResponseBytes   int64  `json:"responseBytes"`
	// Token sums are null when no call in the row reported the count.
	// CallsWithTokens says how many did.
	PromptTokens     *int64 `json:"promptTokens"`
	CompletionTokens *int64 `json:"completionTokens"`
	CallsWithTokens  int64  `json:"callsWithTokens"`
	LatencyMillis    int64  `json:"latencyMillis"`
}

// Totals is what a budget is checked against: one owner, one UTC day. Calls
// refused here (throttled, budget_exceeded) are not counted; they reached no
// model.
type Totals struct {
	Calls  int64 `json:"calls"`
	Tokens int64 `json:"tokens"`
}

// UsageQuery selects rows. An empty Owner is every owner.
type UsageQuery struct {
	Owner string
	// FromDay and ToDay are inclusive UTC days.
	FromDay, ToDay string
}

// UsageStore persists usage. An implementation shared by two replicas makes
// the budget hold across them.
type UsageStore interface {
	Record(ctx context.Context, record UsageRecord) error
	OwnerDay(ctx context.Context, owner, day string) (Totals, error)
	Summary(ctx context.Context, query UsageQuery) ([]UsageRow, error)
}

// CountsTowardBudget reports whether an outcome is a call that reached, or
// tried to reach, a model.
func CountsTowardBudget(outcome string) bool {
	return outcome != string(OutcomeThrottled) && outcome != string(OutcomeBudgetExceeded)
}

// Budget is one owner's standing for today.
type Budget struct {
	Day string `json:"day"`
	// Limits are null when unlimited.
	DailyTokenBudget *int64 `json:"dailyTokenBudget"`
	DailyCallBudget  *int64 `json:"dailyCallBudget"`
	Used             Totals `json:"used"`
	RemainingTokens  *int64 `json:"remainingTokens"`
	RemainingCalls   *int64 `json:"remainingCalls"`
	// ResetsAt is the next UTC midnight.
	ResetsAt time.Time `json:"resetsAt"`
}

// BudgetFor reads an owner's standing against the configured budget.
func (governor *Governor) BudgetFor(ctx context.Context, owner string) (Budget, error) {
	now := governor.deps.Clock.Now()
	budget := Budget{Day: Day(now), ResetsAt: NextDay(now)}
	if governor.deps.Usage != nil {
		used, err := governor.deps.Usage.OwnerDay(ctx, owner, budget.Day)
		if err != nil {
			return Budget{}, err
		}
		budget.Used = used
	}
	if limit := governor.config.DailyTokenBudget; limit > 0 {
		remaining := max(limit-budget.Used.Tokens, 0)
		budget.DailyTokenBudget, budget.RemainingTokens = &limit, &remaining
	}
	if limit := governor.config.DailyCallBudget; limit > 0 {
		remaining := max(limit-budget.Used.Calls, 0)
		budget.DailyCallBudget, budget.RemainingCalls = &limit, &remaining
	}
	return budget, nil
}

// Usage reads usage rows for the last days UTC days, today included.
func (governor *Governor) Usage(ctx context.Context, owner string, days int) ([]UsageRow, error) {
	if governor.deps.Usage == nil {
		return []UsageRow{}, nil
	}
	if days < 1 {
		days = 1
	}
	now := governor.deps.Clock.Now().UTC()
	rows, err := governor.deps.Usage.Summary(ctx, UsageQuery{
		Owner: owner, FromDay: Day(now.AddDate(0, 0, -(days - 1))), ToDay: Day(now),
	})
	if rows == nil {
		rows = []UsageRow{}
	}
	return rows, err
}
