// Package scheduleadapter translates a Delivery rollout into the problem shape
// the Optimization context solves.
//
// The translation lives here rather than in either context. Delivery must not
// depend on how a solver is fed, and Optimization must not learn what a spoke
// is — it is handed a problem and returns a recommendation, and a gate parses
// its imports to keep it that way.
//
// It is deliberately a pure function over immutable data: no clock, no
// repository, nothing to mock. Everything that could be wrong about a rollout
// was already refused by the domain, so this is only ever renaming.
package scheduleadapter

import (
	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
	"github.com/jonathanagustin/conduit/internal/optimization"
)

// TimeUnit is seconds throughout. Delivery measures rollouts in seconds and the
// solver takes a unit, so this states it once rather than at every call.
const TimeUnit = "seconds"

// ToDiscreteScheduleProblem renders a validated rollout as a solver problem.
func ToDiscreteScheduleProblem(rollout domain.Rollout) optimization.DiscreteScheduleProblem {
	problem := optimization.DiscreteScheduleProblem{
		Origin:         rollout.Origin,
		TimeUnit:       TimeUnit,
		HorizonSeconds: rollout.HorizonSeconds,
		Tasks:          make([]optimization.DiscreteTask, 0, len(rollout.Steps)),
		Lanes:          make([]optimization.DiscreteLane, 0, len(rollout.Lanes)),
	}

	for _, step := range rollout.Steps {
		problem.Tasks = append(problem.Tasks, optimization.DiscreteTask{
			ID:              step.ID,
			DurationSeconds: step.DurationSeconds,
			EarliestStart:   step.EarliestStart,
			LatestFinish:    step.LatestFinish,
			EligibleLanes:   append([]string(nil), step.EligibleLanes...),
			Demand:          copyInt64Map(step.Demand),
			// Absent stays absent. An unknown cost must not arrive as zero,
			// which would rank it as free against a known one.
			EstimatedCostMicros: copyInt64Map(step.EstimatedCostMicros),
		})
	}

	for _, lane := range rollout.Lanes {
		problem.Lanes = append(problem.Lanes, optimization.DiscreteLane{
			ID:       lane.ID,
			Capacity: copyInt64Map(lane.Capacity),
			Topology: copyStringMap(lane.Topology),
		})
	}

	for _, dependency := range rollout.Dependencies {
		problem.Precedences = append(problem.Precedences, optimization.DiscretePrecedence{
			Before:     dependency.Before,
			After:      dependency.After,
			LagSeconds: dependency.LagSeconds,
		})
	}

	for _, window := range rollout.Windows {
		problem.MaintenanceWindows = append(problem.MaintenanceWindows, optimization.DiscreteMaintenanceWindow{
			Lane:  window.Lane,
			Start: window.Start,
			End:   window.End,
		})
	}

	return problem
}

// The maps are copied rather than shared. A problem handed to a solver is a
// snapshot, and a caller that later edits its own rollout must not be able to
// change what was already submitted.

func copyInt64Map(source map[string]int64) map[string]int64 {
	if source == nil {
		return nil
	}
	copied := make(map[string]int64, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}

func copyStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	copied := make(map[string]string, len(source))
	for key, value := range source {
		copied[key] = value
	}
	return copied
}
