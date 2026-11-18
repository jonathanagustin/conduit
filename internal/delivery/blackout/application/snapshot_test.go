package application

import (
	"fmt"
	"github.com/jonathanagustin/conduit/internal/delivery/blackout/domain"
	"testing"
	"time"
)

func TestSnapshotFiltersCopiesAndBoundsIntervalsWithoutChangingTheDecision(t *testing.T) {
	at := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	service := &Service{}
	// The first stored row belongs to a different environment. The newest east
	// row blocks at evaluation time but lies beyond the display bound.
	service.blackouts = append(service.blackouts, domain.Blackout{Environment: "west", Author: "bea", Reason: "private west reason", StartsAt: at.Add(-time.Hour), EndsAt: at.Add(time.Hour)})
	for i := MaximumSnapshotIntervals; i >= 0; i-- {
		start := at.Add(time.Duration(i-MaximumSnapshotIntervals) * time.Hour)
		service.blackouts = append(service.blackouts, domain.Blackout{Environment: "east", Author: "ada", Reason: fmt.Sprintf("freeze %d", i), StartsAt: start, EndsAt: start.Add(time.Hour)})
	}
	snapshot := service.Snapshot("east", at)
	if !snapshot.Blocked || snapshot.Reason != fmt.Sprintf("freeze %d", MaximumSnapshotIntervals) || !snapshot.Truncated || len(snapshot.Intervals) != MaximumSnapshotIntervals {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if snapshot.Environment != "east" || !snapshot.EvaluatedAt.Equal(at) || snapshot.ValueTier != "CONDUIT_NATIVE" || snapshot.Persistence != "process-local" {
		t.Fatalf("snapshot metadata=%+v", snapshot)
	}
	for i, row := range snapshot.Intervals {
		if row.Environment != "east" || row.Reason == "private west reason" || !row.StartsAt.Equal(at.Add(time.Duration(i-MaximumSnapshotIntervals)*time.Hour)) {
			t.Fatalf("interval %d=%+v", i, row)
		}
	}
	snapshot.Intervals[0].Reason = "tampered returned copy"
	if service.Snapshot("east", at).Intervals[0].Reason == "tampered returned copy" {
		t.Fatal("returned snapshot changed stored state")
	}
	empty := service.Snapshot("missing", at)
	if empty.Blocked || empty.Reason != "" || empty.Truncated || empty.Intervals == nil || len(empty.Intervals) != 0 {
		t.Fatalf("missing snapshot=%+v", empty)
	}
	if service.Snapshot("east", at.Add(time.Hour)).Blocked {
		t.Fatal("end boundary still blocks")
	}
}
