// Package environmentadapter turns environments and their observed substrate
// into rollout lanes.
//
// A lane's capacity is not a property of an Environment record — it is
// observed, and the observation goes stale. Nothing persists a substrate
// observation per environment today, so the caller supplies them, and this
// package's real job is what to do when one is missing.
//
// It refuses. The platform's own rule is that a missing or stale fact is
// UNKNOWN, never an optimistic default, and the optimistic default here would
// be the worst kind: an environment with no observation would read as a lane
// with no constraints and attract every step in the rollout.
package environmentadapter

import (
	"fmt"
	"sort"

	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
	"github.com/jonathanagustin/conduit/internal/optimization"
)

// Capacity dimension names, matching the fields of the platform's own
// CapacityVector so a step's demand and a lane's capacity are expressed in one
// vocabulary rather than two that must be kept in step.
const (
	PodSlots              = "podSlots"
	MilliCPU              = "milliCpu"
	MemoryBytes           = "memoryBytes"
	IPAddresses           = "ipAddresses"
	EphemeralStorageBytes = "ephemeralStorageBytes"
)

// Observation is one environment's substrate as most recently seen.
type Observation struct {
	Environment string
	Available   optimization.CapacityVector
	// Topology carries whatever placement labels the environment declares —
	// region, zone, tier. It is opaque here; the solver's topology rules give
	// it meaning.
	Topology map[string]string
}

// Lanes renders the named environments as lanes.
//
// Every environment must have an observation. An environment absent from the
// observations is refused by name rather than dropped, because a rollout
// silently missing a lane schedules onto the remainder and looks like it
// succeeded.
func Lanes(environments []string, observations []Observation) ([]domain.Lane, error) {
	if len(environments) == 0 {
		return nil, fmt.Errorf("no environments to schedule onto")
	}

	observed := map[string]Observation{}
	for _, observation := range observations {
		if observation.Environment == "" {
			return nil, fmt.Errorf("observation names no environment")
		}
		if _, duplicate := observed[observation.Environment]; duplicate {
			return nil, fmt.Errorf("environment %q has two observations; which one is current is undecidable here", observation.Environment)
		}
		observed[observation.Environment] = observation
	}

	lanes := make([]domain.Lane, 0, len(environments))
	for _, environment := range environments {
		observation, known := observed[environment]
		if !known {
			return nil, fmt.Errorf(
				"environment %q has no substrate observation; its capacity is unknown, and an unconstrained lane would attract every step",
				environment,
			)
		}

		capacity := map[string]int64{
			PodSlots:              observation.Available.PodSlots,
			MilliCPU:              observation.Available.MilliCPU,
			MemoryBytes:           observation.Available.MemoryBytes,
			IPAddresses:           observation.Available.IPAddresses,
			EphemeralStorageBytes: observation.Available.EphemeralStorageBytes,
		}

		// A dimension observed as zero is a lane that cannot take work in it.
		// Keeping it rather than omitting it is deliberate: the domain treats
		// an undeclared dimension as absent capacity, so dropping a zero and
		// declaring it are the same refusal, and carrying it says which
		// dimension is exhausted.
		lanes = append(lanes, domain.Lane{
			ID:       environment,
			Capacity: capacity,
			Topology: observation.Topology,
		})
	}

	sort.Slice(lanes, func(i, j int) bool { return lanes[i].ID < lanes[j].ID })
	return lanes, nil
}
