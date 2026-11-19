package controlplane

import (
	"context"
	"testing"
	"time"
)

type projectionHistoryReader struct{ plans []PlanRecord }

func (reader projectionHistoryReader) ListPlans(context.Context, string) ([]PlanRecord, error) {
	return reader.plans, nil
}

func TestNativeEnvironmentProjectionSourcePreservesCommandsMissingStateAndTies(t *testing.T) {
	epoch := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	declaration := func(environment, version string) *DesiredState {
		return &DesiredState{Environment: Environment{ID: environment}, Entities: []Entity{{Name: "app", Product: "native", Version: version}}}
	}
	original := declaration("a", "1")
	original.Recalls = []Recall{{Product: "native", Reason: "original withdrawal"}}
	command := PlanRecord{ID: "command", Plan: Plan{Environment: "a"}, CreatedAt: epoch.Add(time.Hour), DesiredState: declaration("a", "2")}
	// The concrete command value is irrelevant to projection; presence selects it.
	command.Command = &EntityCommand{}
	plans := []PlanRecord{
		{ID: "original", Plan: Plan{Environment: "a"}, CreatedAt: epoch, DesiredState: original}, command,
		{ID: "old", Plan: Plan{Environment: "b"}, CreatedAt: epoch, DesiredState: declaration("b", "1")},
		{ID: "missing", Plan: Plan{Environment: "b"}, CreatedAt: epoch.Add(time.Hour)},
		{ID: "a", Plan: Plan{Environment: "c"}, CreatedAt: epoch, DesiredState: declaration("c", "old")},
		{ID: "z", Plan: Plan{Environment: "c"}, CreatedAt: epoch, DesiredState: declaration("c", "tie-winner")},
	}
	for _, history := range [][]PlanRecord{plans, reverseProjectionHistory(plans)} {
		source := projectionHistoryReader{plans: history}
		inventory, err := (environmentInventoryProjectionPort{repository: source}).EnvironmentInventory(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(inventory) != 2 || inventory[0].ID != "a" || inventory[0].Entities[0].Version != "2" || inventory[1].ID != "c" || inventory[1].Entities[0].Version != "tie-winner" {
			t.Fatalf("latest inventory semantics changed: %+v", inventory)
		}
		lifecycle, err := (releaseLifecycleProjectionPort{repository: source}).ReleaseLifecycle(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if len(lifecycle.Recalls) != 1 || lifecycle.Recalls[0].Reason != "original withdrawal" {
			t.Fatalf("command erased declared withdrawal: %+v", lifecycle)
		}
		inventory[0].Entities[0].Version = "mutated"
		if command.DesiredState.Entities[0].Version != "2" {
			t.Fatal("projection mutated original desired state")
		}
	}
}

func reverseProjectionHistory(plans []PlanRecord) []PlanRecord {
	result := append([]PlanRecord(nil), plans...)
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}
	return result
}
