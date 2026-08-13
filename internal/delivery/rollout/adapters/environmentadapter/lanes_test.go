package environmentadapter

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

func observation(environment string) Observation {
	return Observation{
		Environment: environment,
		Available: optimization.CapacityVector{
			PodSlots: 40, MilliCPU: 16000, MemoryBytes: 68719476736,
			IPAddresses: 250, EphemeralStorageBytes: 107374182400,
		},
		Topology: map[string]string{"region": "use2"},
	}
}

func TestEveryDimensionSurvives(t *testing.T) {
	lanes, err := Lanes([]string{"spoke-a"}, []Observation{observation("spoke-a")})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{
		PodSlots: 40, MilliCPU: 16000, MemoryBytes: 68719476736,
		IPAddresses: 250, EphemeralStorageBytes: 107374182400,
	}
	for dimension, expected := range want {
		if lanes[0].Capacity[dimension] != expected {
			t.Fatalf("%s: got %d, want %d", dimension, lanes[0].Capacity[dimension], expected)
		}
	}
	if lanes[0].Topology["region"] != "use2" {
		t.Fatalf("topology lost: %+v", lanes[0].Topology)
	}
}

// The rule this package exists for. An unobserved environment must not become
// an unconstrained lane, which would attract every step in the rollout.
func TestAnUnobservedEnvironmentIsRefusedByName(t *testing.T) {
	_, err := Lanes([]string{"spoke-a", "spoke-b"}, []Observation{observation("spoke-a")})
	if err == nil {
		t.Fatal("an unobserved environment was accepted")
	}
	if !strings.Contains(err.Error(), "spoke-b") {
		t.Fatalf("the refusal does not name the environment: %v", err)
	}
}

// Known-empty: no observations at all must not produce lanes.
func TestNoObservationsProducesNoLanes(t *testing.T) {
	lanes, err := Lanes([]string{"spoke-a"}, nil)
	if err == nil {
		t.Fatalf("an entirely unobserved fleet was accepted, producing %+v", lanes)
	}
}

func TestNoEnvironmentsIsRefused(t *testing.T) {
	if _, err := Lanes(nil, []Observation{observation("spoke-a")}); err == nil {
		t.Fatal("an empty fleet was accepted")
	}
}

// Two observations for one environment leaves currency undecidable here, and
// guessing would silently pick one.
func TestTwoObservationsForOneEnvironmentAreRefused(t *testing.T) {
	_, err := Lanes([]string{"spoke-a"}, []Observation{observation("spoke-a"), observation("spoke-a")})
	if err == nil {
		t.Fatal("duplicate observations were accepted")
	}
}

func TestAnObservationWithNoEnvironmentIsRefused(t *testing.T) {
	if _, err := Lanes([]string{"spoke-a"}, []Observation{{}}); err == nil {
		t.Fatal("a nameless observation was accepted")
	}
}

// A dimension observed as zero is carried rather than omitted, so the problem
// says which dimension is exhausted rather than merely that the lane is full.
func TestAnExhaustedDimensionIsCarriedNotDropped(t *testing.T) {
	exhausted := observation("spoke-a")
	exhausted.Available.IPAddresses = 0

	lanes, err := Lanes([]string{"spoke-a"}, []Observation{exhausted})
	if err != nil {
		t.Fatal(err)
	}
	value, declared := lanes[0].Capacity[IPAddresses]
	if !declared {
		t.Fatal("an exhausted dimension was dropped; the problem cannot say which one is full")
	}
	if value != 0 {
		t.Fatalf("expected zero, got %d", value)
	}
}

func TestOutputIsCanonicalWhateverOrderTheInputArrives(t *testing.T) {
	forward, err := Lanes([]string{"spoke-b", "spoke-a"}, []Observation{observation("spoke-a"), observation("spoke-b")})
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := Lanes([]string{"spoke-a", "spoke-b"}, []Observation{observation("spoke-b"), observation("spoke-a")})
	if err != nil {
		t.Fatal(err)
	}
	for index := range forward {
		if forward[index].ID != reverse[index].ID {
			t.Fatalf("order depended on input:\n  %+v\n  %+v", forward, reverse)
		}
	}
}
