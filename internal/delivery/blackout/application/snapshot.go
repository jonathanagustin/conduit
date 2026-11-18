package application

import (
	"sort"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/blackout/domain"
)

const MaximumSnapshotIntervals = 64

// CONDUIT-INVENTED: blackout.IntervalSnapshot
// Displaced-by: An authoritative blackout interval-list and calendar contract.
// Snapshot is one bounded, process-local view; the decision uses the complete
// stored set, including intervals omitted by the display bound.
type Snapshot struct {
	Environment string            `json:"environment"`
	EvaluatedAt time.Time         `json:"evaluatedAt"`
	Blocked     bool              `json:"blocked"`
	Reason      string            `json:"reason"`
	Intervals   []domain.Blackout `json:"intervals"`
	Truncated   bool              `json:"truncated"`
	ValueTier   string            `json:"valueTier"`
	Persistence string            `json:"persistence"`
}

func (service *Service) Snapshot(environment string, at time.Time) Snapshot {
	service.mu.Lock()
	defer service.mu.Unlock()
	reason := domain.Reason(service.blackouts, environment, at)
	rows := make([]domain.Blackout, 0)
	for _, row := range service.blackouts {
		if row.Environment == environment {
			rows = append(rows, row)
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if !rows[i].StartsAt.Equal(rows[j].StartsAt) {
			return rows[i].StartsAt.Before(rows[j].StartsAt)
		}
		return rows[i].EndsAt.Before(rows[j].EndsAt)
	})
	truncated := len(rows) > MaximumSnapshotIntervals
	if truncated {
		rows = rows[:MaximumSnapshotIntervals]
	}
	return Snapshot{Environment: environment, EvaluatedAt: at.UTC(), Blocked: reason != "", Reason: reason, Intervals: rows, Truncated: truncated, ValueTier: "CONDUIT_NATIVE", Persistence: "process-local"}
}
