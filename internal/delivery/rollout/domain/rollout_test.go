package domain

import "testing"

func valid() Rollout {
	return Rollout{
		Origin:         "test",
		HorizonSeconds: 3600,
		Lanes: []Lane{
			{ID: "spoke-a", Capacity: map[string]int64{"slots": 2}},
			{ID: "spoke-b", Capacity: map[string]int64{"slots": 1}},
		},
		Steps: []Step{
			{ID: "install-hub", DurationSeconds: 300, EligibleLanes: []string{"spoke-a"}, Demand: map[string]int64{"slots": 1}},
			{ID: "install-agent", DurationSeconds: 600, EligibleLanes: []string{"spoke-a", "spoke-b"}, Demand: map[string]int64{"slots": 1}},
		},
		Dependencies: []Dependency{{Before: "install-hub", After: "install-agent", LagSeconds: 60}},
		Windows:      []Window{{Lane: "spoke-b", Start: 0, End: 120}},
	}
}

func TestValidRolloutIsAccepted(t *testing.T) {
	if err := valid().Validate(); err != nil {
		t.Fatalf("valid rollout rejected: %v", err)
	}
}

// Each of these is a rollout that cannot be scheduled. The point of validating
// is that the reason is named here rather than discovered as an unexplained
// INFEASIBLE from a solver.
//
// Every case asserts the reason, not merely that something was refused. A
// mutation that trips a different check would otherwise pass this test while
// proving nothing about the check it was written for.
func TestImpossibleRolloutsAreRefused(t *testing.T) {
	for name, testCase := range map[string]struct {
		mutate func(*Rollout)
		reason string
	}{
		"no horizon":          {func(r *Rollout) { r.HorizonSeconds = 0 }, "horizon must be positive"},
		"no lanes":            {func(r *Rollout) { r.Lanes = nil }, "no lanes"},
		"no steps":            {func(r *Rollout) { r.Steps = nil }, "no steps"},
		"duplicate lane":      {func(r *Rollout) { r.Lanes = append(r.Lanes, r.Lanes[0]) }, "declared twice"},
		"duplicate step":      {func(r *Rollout) { r.Steps = append(r.Steps, r.Steps[0]) }, "declared twice"},
		"zero duration":       {func(r *Rollout) { r.Steps[0].DurationSeconds = 0 }, "no duration"},
		"no eligible lane":    {func(r *Rollout) { r.Steps[0].EligibleLanes = nil }, "no eligible lane"},
		"unknown lane":        {func(r *Rollout) { r.Steps[0].EligibleLanes = []string{"nowhere"} }, "does not exist"},
		"unknown before":      {func(r *Rollout) { r.Dependencies[0].Before = "ghost" }, "unknown step"},
		"unknown after":       {func(r *Rollout) { r.Dependencies[0].After = "ghost" }, "unknown step"},
		"self dependency":     {func(r *Rollout) { r.Dependencies[0].After = r.Dependencies[0].Before }, "depends on itself"},
		"negative lag":        {func(r *Rollout) { r.Dependencies[0].LagSeconds = -1 }, "negative lag"},
		"window unknown lane": {func(r *Rollout) { r.Windows[0].Lane = "nowhere" }, "unknown lane"},
		"inverted window":     {func(r *Rollout) { r.Windows[0].End = r.Windows[0].Start }, "ends before it starts"},

		// The three that matter most: until now the problem shape has been
		// structurally feasible, so no real backend has returned INFEASIBLE.
		"deadline before the work fits": {func(r *Rollout) { r.Steps[0].LatestFinish = 100 }, "cannot finish by"},
		"deadline beyond the horizon":   {func(r *Rollout) { r.Steps[0].LatestFinish = 999999 }, "beyond the horizon"},
		"demand no lane can meet":       {func(r *Rollout) { r.Steps[0].Demand = map[string]int64{"slots": 99} }, "more than any eligible lane"},

		// A dimension a lane never declares is absent capacity, not unlimited.
		"demands an undeclared dimension": {func(r *Rollout) { r.Steps[0].Demand = map[string]int64{"gpus": 1} }, "more than any eligible lane"},
	} {
		t.Run(name, func(t *testing.T) {
			rollout := valid()
			testCase.mutate(&rollout)

			err := rollout.Validate()
			if err == nil {
				t.Fatal("expected refusal, got none")
			}
			if !contains(err.Error(), testCase.reason) {
				t.Fatalf("refused for the wrong reason:\n  got:  %v\n  want: something naming %q", err, testCase.reason)
			}
		})
	}
}

func TestDependencyCycleIsNamed(t *testing.T) {
	rollout := valid()
	rollout.Dependencies = append(rollout.Dependencies, Dependency{Before: "install-agent", After: "install-hub"})

	err := rollout.Validate()
	if err == nil {
		t.Fatal("a cycle was accepted")
	}
	if got := err.Error(); got == "" || !contains(got, "cycle") {
		t.Fatalf("error does not name the cycle: %v", err)
	}
}

// A deadline that exactly fits is feasible; the boundary is the interesting
// case and off-by-one here would refuse schedulable work.
func TestDeadlineThatExactlyFitsIsAccepted(t *testing.T) {
	rollout := valid()
	rollout.Steps[0].EarliestStart = 60
	rollout.Steps[0].LatestFinish = 60 + rollout.Steps[0].DurationSeconds

	if err := rollout.Validate(); err != nil {
		t.Fatalf("an exactly-fitting deadline was refused: %v", err)
	}
}

func contains(haystack, needle string) bool {
	for index := 0; index+len(needle) <= len(haystack); index++ {
		if haystack[index:index+len(needle)] == needle {
			return true
		}
	}
	return false
}
