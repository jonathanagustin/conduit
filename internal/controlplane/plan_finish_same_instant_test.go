package controlplane

import (
	"reflect"
	"testing"
	"time"
)

func TestFinishPlanLegacyBackfillKeepsSameInstantObservation(t *testing.T) {
	for _, store := range []string{"memory", "file", "postgres"} {
		t.Run(store, func(t *testing.T) {
			base, _ := newFinishBoundaryStore(t, store)
			lifecycle, running := startFoundationPlan(t, base)
			if _, err := base.UpdatePlan(t.Context(), running.ID, func(record *PlanRecord) error {
				record.Status = PlanSucceeded
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			later := running.UpdatedAt
			successor := finishBoundaryReport(running, true)
			successor.Environment, successor.PlanID, successor.ObservedAt = running.Plan.Environment, "pln_successor", later
			want, err := base.PutEnvironmentReport(t.Context(), successor)
			if err != nil {
				t.Fatal(err)
			}
			lifecycle.now = func() time.Time { return later.Add(time.Hour) }
			report := finishBoundaryReport(running, true)
			finished, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, true, "", "", successfulPlanExecution(running), &report)
			if err != nil {
				t.Fatal(err)
			}
			got, err := base.GetEnvironmentReport(t.Context(), running.Plan.Environment)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("legacy backfill rewound reported state: got=%#v want=%#v error=%v", got, want, err)
			}
			if !finished.CompletionRecorded || !finished.UpdatedAt.Equal(running.UpdatedAt) {
				t.Fatalf("legacy completion time/receipt = %v %v", finished.UpdatedAt, finished.CompletionRecorded)
			}
		})
	}
}

func TestFinishPlanFirstCompletionReplacesPriorObservationAtSameInstant(t *testing.T) {
	for _, store := range []string{"memory", "file", "postgres"} {
		t.Run(store, func(t *testing.T) {
			base, _ := newFinishBoundaryStore(t, store)
			lifecycle, running := startFoundationPlan(t, base)
			previous := finishBoundaryReport(running, true)
			previous.Environment, previous.PlanID, previous.ObservedAt = running.Plan.Environment, "pln_previous", running.UpdatedAt
			if _, err := base.PutEnvironmentReport(t.Context(), previous); err != nil {
				t.Fatal(err)
			}
			report := finishBoundaryReport(running, true)
			finished, err := lifecycle.FinishPlan(t.Context(), running.ID, running.Lease.Token, true, "", "", successfulPlanExecution(running), &report)
			if err != nil {
				t.Fatal(err)
			}
			observed, err := base.GetEnvironmentReport(t.Context(), running.Plan.Environment)
			if err != nil || observed.PlanID != running.ID || !finished.CompletionRecorded || finished.Status != PlanSucceeded {
				t.Fatalf("first completion did not publish its exact observation: plan=%#v report=%#v error=%v", finished.Status, observed, err)
			}
		})
	}
}
