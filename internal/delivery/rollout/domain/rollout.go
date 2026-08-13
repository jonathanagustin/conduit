// Package domain models a rollout as a scheduling problem in Delivery's own
// vocabulary.
//
// Delivery owns plans, maintenance and rollout intent, so the shape of a
// rollout belongs here rather than in Optimization. Optimization is advisory:
// it is handed a problem and returns a recommendation, and it must not be able
// to read platform state. Keeping the model here preserves that — the context
// that knows the fleet builds the problem, and the context that solves it never
// sees anything else.
//
// Nothing in this package imports another context. The translation to whatever
// a solver wants is an adapter's job.
package domain

import (
	"errors"
	"fmt"
	"sort"
)

// Step is one unit of rollout work: install this entity on this lane.
type Step struct {
	ID string
	// DurationSeconds is how long the step occupies its lane. Zero is not
	// permitted: a step that takes no time is either not a step or an
	// unmeasured one, and both are worth failing on rather than scheduling.
	DurationSeconds int64
	EarliestStart   int64
	// LatestFinish is a deadline. Zero means none, which is the common case;
	// a change freeze or a compliance window is what puts one here.
	LatestFinish int64
	// EligibleLanes names where the step may run. Empty is invalid rather than
	// "anywhere": an unplaceable step should be a compile error, not a solver
	// result nobody can explain.
	EligibleLanes []string
	// Demand is per-dimension consumption while the step runs, matched against
	// a lane's capacity of the same name.
	Demand map[string]int64
	// EstimatedCostMicros is per-lane, because the same work costs differently
	// in different places. Absent means unknown, which is not the same as free.
	EstimatedCostMicros map[string]int64
}

// Lane is a place a step can run — an environment, a spoke, a cluster — with
// renewable capacity in one or more dimensions.
type Lane struct {
	ID       string
	Capacity map[string]int64
	Topology map[string]string
}

// Dependency is an ordering edge. LagSeconds holds the gap a soak or a health
// check needs after the earlier step finishes.
type Dependency struct {
	Before     string
	After      string
	LagSeconds int64
}

// Window is an interval during which a lane accepts no work.
type Window struct {
	Lane  string
	Start int64
	End   int64
}

// Rollout is the assembled problem.
type Rollout struct {
	Origin         string
	HorizonSeconds int64
	Steps          []Step
	Lanes          []Lane
	Dependencies   []Dependency
	Windows        []Window
}

// Validate refuses a rollout that cannot be scheduled, before a solver is asked
// to discover the same thing more slowly and less legibly.
//
// The deadline and capacity-coverage checks matter more than they look. The
// problem shape has been structurally feasible until now — positive capacities,
// at least one eligible lane, and no upper bound on makespan — so no real
// backend has ever returned INFEASIBLE. Deadlines are what make a rollout
// genuinely impossible, and a step whose demand no eligible lane can ever meet
// is impossible in a way that reads as a solver bug if it reaches the solver.
func (rollout Rollout) Validate() error {
	if rollout.HorizonSeconds <= 0 {
		return errors.New("rollout horizon must be positive")
	}

	lanes := map[string]Lane{}
	for _, lane := range rollout.Lanes {
		if lane.ID == "" {
			return errors.New("lane has no id")
		}
		if _, seen := lanes[lane.ID]; seen {
			return fmt.Errorf("lane %q is declared twice", lane.ID)
		}
		lanes[lane.ID] = lane
	}
	if len(lanes) == 0 {
		return errors.New("rollout has no lanes")
	}

	steps := map[string]Step{}
	for _, step := range rollout.Steps {
		if step.ID == "" {
			return errors.New("step has no id")
		}
		if _, seen := steps[step.ID]; seen {
			return fmt.Errorf("step %q is declared twice", step.ID)
		}
		if step.DurationSeconds <= 0 {
			return fmt.Errorf("step %q has no duration", step.ID)
		}
		if step.EarliestStart < 0 {
			return fmt.Errorf("step %q starts before zero", step.ID)
		}
		if len(step.EligibleLanes) == 0 {
			return fmt.Errorf("step %q names no eligible lane", step.ID)
		}

		placeable := false
		for _, id := range step.EligibleLanes {
			lane, known := lanes[id]
			if !known {
				return fmt.Errorf("step %q is eligible for lane %q, which does not exist", step.ID, id)
			}
			if fits(step.Demand, lane.Capacity) {
				placeable = true
			}
		}
		if !placeable {
			return fmt.Errorf("step %q demands more than any eligible lane can ever supply", step.ID)
		}

		if step.LatestFinish != 0 {
			if step.LatestFinish < step.EarliestStart+step.DurationSeconds {
				return fmt.Errorf(
					"step %q cannot finish by %d: it needs %ds and cannot start before %d",
					step.ID, step.LatestFinish, step.DurationSeconds, step.EarliestStart,
				)
			}
			if step.LatestFinish > rollout.HorizonSeconds {
				return fmt.Errorf("step %q has a deadline beyond the horizon", step.ID)
			}
		}

		steps[step.ID] = step
	}
	if len(steps) == 0 {
		return errors.New("rollout has no steps")
	}

	for _, dependency := range rollout.Dependencies {
		if _, known := steps[dependency.Before]; !known {
			return fmt.Errorf("dependency names unknown step %q", dependency.Before)
		}
		if _, known := steps[dependency.After]; !known {
			return fmt.Errorf("dependency names unknown step %q", dependency.After)
		}
		if dependency.Before == dependency.After {
			return fmt.Errorf("step %q depends on itself", dependency.Before)
		}
		if dependency.LagSeconds < 0 {
			return fmt.Errorf("dependency %q -> %q has negative lag", dependency.Before, dependency.After)
		}
	}
	if cycle := findCycle(rollout.Dependencies); len(cycle) > 0 {
		return fmt.Errorf("dependencies form a cycle: %v", cycle)
	}

	for _, window := range rollout.Windows {
		if _, known := lanes[window.Lane]; !known {
			return fmt.Errorf("maintenance window names unknown lane %q", window.Lane)
		}
		if window.End <= window.Start {
			return fmt.Errorf("maintenance window on lane %q ends before it starts", window.Lane)
		}
	}

	return nil
}

// fits reports whether every dimension the step demands is present on the lane
// and large enough. A dimension the lane does not declare is absent capacity,
// not unlimited capacity — the opposite reading turns a typo into an
// unconstrained placement.
func fits(demand, capacity map[string]int64) bool {
	for dimension, wanted := range demand {
		available, declared := capacity[dimension]
		if !declared || available < wanted {
			return false
		}
	}
	return true
}

// findCycle returns one cycle if the dependency graph has any, in a stable
// order so the message does not change between runs on the same input.
func findCycle(dependencies []Dependency) []string {
	successors := map[string][]string{}
	for _, dependency := range dependencies {
		successors[dependency.Before] = append(successors[dependency.Before], dependency.After)
	}
	for node := range successors {
		sort.Strings(successors[node])
	}

	roots := make([]string, 0, len(successors))
	for node := range successors {
		roots = append(roots, node)
	}
	sort.Strings(roots)

	const (
		unvisited = 0
		active    = 1
		done      = 2
	)
	state := map[string]int{}
	var path []string

	var walk func(node string) []string
	walk = func(node string) []string {
		state[node] = active
		path = append(path, node)
		for _, next := range successors[node] {
			switch state[next] {
			case active:
				for index, seen := range path {
					if seen == next {
						return append(append([]string{}, path[index:]...), next)
					}
				}
			case unvisited:
				if cycle := walk(next); len(cycle) > 0 {
					return cycle
				}
			}
		}
		path = path[:len(path)-1]
		state[node] = done
		return nil
	}

	for _, node := range roots {
		if state[node] == unvisited {
			if cycle := walk(node); len(cycle) > 0 {
				return cycle
			}
		}
	}
	return nil
}
