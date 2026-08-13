// Package preflightadapter reads substrate preflight records as rollout lane
// observations.
//
// This is the bridge that was missing. Lane capacity is observed rather than
// declared, and the observations do have a home after all — the optimizer's
// preflight store keeps a record per environment, carrying the capacity
// evidence, when it was taken, and how long it may be trusted.
//
// That last field is the point of this package. An observation is only
// evidence while it is fresh, and the record says how fresh that is, so
// scheduling against an expired one is scheduling against a fleet that has
// moved. The platform's rule is that a stale fact is STALE rather than an
// optimistic default, and here the optimistic default would place work on
// capacity that may already be gone.
package preflightadapter

import (
	"fmt"
	"time"

	"github.com/jonathanagustin/conduit/internal/delivery/rollout/adapters/environmentadapter"
	"github.com/jonathanagustin/conduit/internal/optimization"
)

// Observations converts preflight records into lane observations, refusing any
// that has outlived its own staleness bound.
//
// The clock is a parameter because freshness is judged against a moment, and a
// package that reads the wall clock cannot be tested for the boundary that
// matters.
func Observations(records []optimization.SubstratePreflightRecord, now time.Time) ([]environmentadapter.Observation, error) {
	observations := make([]environmentadapter.Observation, 0, len(records))

	for _, record := range records {
		if record.Environment == "" {
			return nil, fmt.Errorf("preflight record %q names no environment", record.ID)
		}

		snapshotAt, err := time.Parse(time.RFC3339, record.Request.SnapshotAt)
		if err != nil {
			return nil, fmt.Errorf("preflight record %q has an unreadable snapshot time %q: %w", record.ID, record.Request.SnapshotAt, err)
		}

		// A record with no stated bound is not implicitly eternal. Nothing here
		// can decide how long that substrate may be trusted, so it is refused
		// rather than assumed current.
		maximum := record.Request.MaximumStalenessSeconds
		if maximum <= 0 {
			return nil, fmt.Errorf(
				"preflight record %q for %q states no staleness bound, so nothing here can say whether it is still evidence",
				record.ID, record.Environment,
			)
		}

		age := now.Sub(snapshotAt)
		if age < 0 {
			return nil, fmt.Errorf(
				"preflight record %q for %q was taken %s in the future; a clock disagreement is not a fresh observation",
				record.ID, record.Environment, -age,
			)
		}
		if age > time.Duration(maximum)*time.Second {
			return nil, fmt.Errorf(
				"preflight record %q for %q is %s old against its own %ds bound; the fleet it describes has moved",
				record.ID, record.Environment, age.Truncate(time.Second), maximum,
			)
		}

		observations = append(observations, environmentadapter.Observation{
			Environment: record.Environment,
			Available:   record.Request.Capacity.Available,
		})
	}

	return observations, nil
}
