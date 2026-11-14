package controlplane

import (
	"context"
	"testing"
)

func TestNotifyPlanApprovedStaysQuietUnlessThePlanIsApproved(t *testing.T) {
	if err := notifyPlanApproved(context.Background(), HandlerOptions{}, PlanRecord{ID: "pln", Status: PlanApproved}); err != nil {
		t.Fatal(err)
	}
	called := 0
	options := HandlerOptions{AfterPlanApproved: func(_ context.Context, planID string) error {
		called++
		if planID != "pln" {
			t.Fatalf("plan = %s", planID)
		}
		return nil
	}}
	if err := notifyPlanApproved(context.Background(), options, PlanRecord{ID: "pln", Status: PlanAwaitingApproval}); err != nil || called != 0 {
		t.Fatalf("awaiting approval called %d: %v", called, err)
	}
	if err := notifyPlanApproved(context.Background(), options, PlanRecord{Status: PlanApproved}); err != nil || called != 0 {
		t.Fatalf("missing id called %d: %v", called, err)
	}
	if err := notifyPlanApproved(context.Background(), options, PlanRecord{ID: "pln", Status: PlanApproved}); err != nil || called != 1 {
		t.Fatalf("approved called %d: %v", called, err)
	}
}
