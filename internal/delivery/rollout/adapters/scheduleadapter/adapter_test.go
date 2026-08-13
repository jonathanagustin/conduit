package scheduleadapter

import (
	"reflect"
	"testing"

	"github.com/jonathanagustin/conduit/internal/delivery/rollout/domain"
)

func rollout() domain.Rollout {
	return domain.Rollout{
		Origin:         "delivery/rollout",
		HorizonSeconds: 3600,
		Lanes: []domain.Lane{
			{ID: "spoke-a", Capacity: map[string]int64{"slots": 2}, Topology: map[string]string{"region": "use2"}},
		},
		Steps: []domain.Step{{
			ID: "install-agent", DurationSeconds: 600, EarliestStart: 60, LatestFinish: 1800,
			EligibleLanes: []string{"spoke-a"},
			Demand:        map[string]int64{"slots": 1},
		}},
		Dependencies: []domain.Dependency{{Before: "install-agent", After: "install-agent-2", LagSeconds: 30}},
		Windows:      []domain.Window{{Lane: "spoke-a", Start: 0, End: 120}},
	}
}

func TestEveryFieldSurvivesTheTranslation(t *testing.T) {
	problem := ToDiscreteScheduleProblem(rollout())

	if problem.Origin != "delivery/rollout" || problem.TimeUnit != TimeUnit || problem.HorizonSeconds != 3600 {
		t.Fatalf("identity lost: %+v", problem)
	}
	if len(problem.Tasks) != 1 || len(problem.Lanes) != 1 || len(problem.Precedences) != 1 || len(problem.MaintenanceWindows) != 1 {
		t.Fatalf("counts wrong: %+v", problem)
	}

	task := problem.Tasks[0]
	if task.ID != "install-agent" || task.DurationSeconds != 600 || task.EarliestStart != 60 || task.LatestFinish != 1800 {
		t.Fatalf("task fields lost: %+v", task)
	}
	if !reflect.DeepEqual(task.Demand, map[string]int64{"slots": 1}) {
		t.Fatalf("demand lost: %+v", task.Demand)
	}
	if !reflect.DeepEqual(problem.Lanes[0].Topology, map[string]string{"region": "use2"}) {
		t.Fatalf("topology lost: %+v", problem.Lanes[0])
	}
	if problem.Precedences[0].LagSeconds != 30 {
		t.Fatalf("lag lost: %+v", problem.Precedences[0])
	}
}

// An unknown cost must not arrive as zero. Zero is a real price and would rank
// as the cheapest option against every known one.
func TestAbsentCostStaysAbsent(t *testing.T) {
	problem := ToDiscreteScheduleProblem(rollout())

	if problem.Tasks[0].EstimatedCostMicros != nil {
		t.Fatalf("an absent cost became %v", problem.Tasks[0].EstimatedCostMicros)
	}
}

// The problem is a snapshot. Editing the rollout afterwards must not reach
// inside something already handed to a solver.
func TestTheProblemDoesNotShareMapsWithTheRollout(t *testing.T) {
	source := rollout()
	problem := ToDiscreteScheduleProblem(source)

	source.Steps[0].Demand["slots"] = 99
	source.Lanes[0].Capacity["slots"] = 99
	source.Lanes[0].Topology["region"] = "elsewhere"

	if problem.Tasks[0].Demand["slots"] != 1 {
		t.Fatal("demand was shared, not copied")
	}
	if problem.Lanes[0].Capacity["slots"] != 2 {
		t.Fatal("capacity was shared, not copied")
	}
	if problem.Lanes[0].Topology["region"] != "use2" {
		t.Fatal("topology was shared, not copied")
	}
}

// An empty rollout translates to an empty problem rather than to nil slices
// that a JSON encoder would render as null.
func TestEmptyRolloutTranslatesToEmptyProblem(t *testing.T) {
	problem := ToDiscreteScheduleProblem(domain.Rollout{})

	if problem.Tasks == nil || problem.Lanes == nil {
		t.Fatalf("empty rollout produced nil slices: %+v", problem)
	}
	if len(problem.Tasks) != 0 || len(problem.Lanes) != 0 {
		t.Fatalf("empty rollout produced content: %+v", problem)
	}
}
