package preflightadapter

import (
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

var now = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

func record(environment string, takenSecondsAgo, boundSeconds int64) optimization.SubstratePreflightRecord {
	return optimization.SubstratePreflightRecord{
		ID:          "pf-" + environment,
		Environment: environment,
		Request: optimization.SubstratePreflightRequest{
			SnapshotAt:              now.Add(-time.Duration(takenSecondsAgo) * time.Second).Format(time.RFC3339),
			MaximumStalenessSeconds: boundSeconds,
			Capacity: optimization.CapacityEvidence{
				Available: optimization.CapacityVector{PodSlots: 40, MilliCPU: 16000},
			},
		},
	}
}

func TestAFreshRecordBecomesAnObservation(t *testing.T) {
	got, err := Observations([]optimization.SubstratePreflightRecord{record("spoke-a", 60, 600)}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Environment != "spoke-a" {
		t.Fatalf("got %+v", got)
	}
	if got[0].Available.PodSlots != 40 || got[0].Available.MilliCPU != 16000 {
		t.Fatalf("capacity lost: %+v", got[0].Available)
	}
}

// The rule this package exists for. An expired observation describes a fleet
// that has moved, and scheduling against it places work on capacity that may
// already be gone.
func TestAnExpiredRecordIsRefused(t *testing.T) {
	_, err := Observations([]optimization.SubstratePreflightRecord{record("spoke-a", 601, 600)}, now)
	if err == nil {
		t.Fatal("a stale observation was accepted")
	}
	if !strings.Contains(err.Error(), "has moved") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

// The boundary is the interesting case: exactly at the bound is still evidence.
func TestARecordExactlyAtItsBoundIsStillFresh(t *testing.T) {
	if _, err := Observations([]optimization.SubstratePreflightRecord{record("spoke-a", 600, 600)}, now); err != nil {
		t.Fatalf("a record exactly at its bound was refused: %v", err)
	}
}

// No stated bound is not implicitly eternal.
func TestARecordWithNoStalenessBoundIsRefused(t *testing.T) {
	_, err := Observations([]optimization.SubstratePreflightRecord{record("spoke-a", 60, 0)}, now)
	if err == nil {
		t.Fatal("a record with no bound was accepted")
	}
	if !strings.Contains(err.Error(), "no staleness bound") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

// A future snapshot is a clock disagreement, not freshness. Treating it as
// fresh would make a wrong clock look like the newest possible evidence.
func TestARecordFromTheFutureIsRefused(t *testing.T) {
	_, err := Observations([]optimization.SubstratePreflightRecord{record("spoke-a", -60, 600)}, now)
	if err == nil {
		t.Fatal("a future observation was accepted")
	}
	if !strings.Contains(err.Error(), "future") {
		t.Fatalf("refused for the wrong reason: %v", err)
	}
}

func TestAnUnreadableSnapshotTimeIsRefused(t *testing.T) {
	broken := record("spoke-a", 60, 600)
	broken.Request.SnapshotAt = "whenever"

	if _, err := Observations([]optimization.SubstratePreflightRecord{broken}, now); err == nil {
		t.Fatal("an unreadable snapshot time was accepted")
	}
}

func TestARecordWithNoEnvironmentIsRefused(t *testing.T) {
	nameless := record("", 60, 600)

	if _, err := Observations([]optimization.SubstratePreflightRecord{nameless}, now); err == nil {
		t.Fatal("a record naming no environment was accepted")
	}
}

// Known-empty: no records is no observations, not an error. What to do about an
// environment with no observation is the lane adapter's decision, and it
// refuses; deciding it twice in two places is how the two come to disagree.
func TestNoRecordsProducesNoObservations(t *testing.T) {
	got, err := Observations(nil, now)
	if err != nil {
		t.Fatalf("an empty set was refused: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("an empty set produced %+v", got)
	}
}
