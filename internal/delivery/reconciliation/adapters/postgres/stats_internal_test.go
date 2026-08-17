package postgres

import (
	"database/sql"
	"testing"
	"time"
)

// The lag signal is the one an operator alerts on, so the values it can take
// matter more than the query that produces them.
//
// Nothing pending must read as zero rather than as a stale high-water mark: a
// gauge that kept its last value after the queue drained would alert forever.
// And a negative interval must read as zero rather than as a negative age --
// the query excludes rows scheduled for the future, but clock skew between the
// Hub and the database can still produce one, and a negative lag on a dashboard
// reads as a corrupted exporter rather than as the sub-second skew it is.
func TestTheLagSignalIsNeverNegativeAndZeroWhenEmpty(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	for name, testCase := range map[string]struct {
		oldest sql.NullTime
		want   time.Duration
	}{
		"nothing pending":  {sql.NullTime{}, 0},
		"a minute behind":  {sql.NullTime{Time: now.Add(-time.Minute), Valid: true}, time.Minute},
		"clock skew ahead": {sql.NullTime{Time: now.Add(time.Second), Valid: true}, 0},
		"exactly now":      {sql.NullTime{Time: now, Valid: true}, 0},
	} {
		if got := age(now, testCase.oldest); got != testCase.want {
			t.Errorf("%s: age=%v want=%v", name, got, testCase.want)
		}
	}
}
