package controlplane

import (
	"context"
	"errors"
	"testing"
)

// This fixture deliberately has no Repository writes, transaction, approval or
// execution methods. Broadening either stored port breaks compilation here.
type projectionReadOnlyPlans struct{ failure error }

func (reader projectionReadOnlyPlans) ListPlans(context.Context, string) ([]PlanRecord, error) {
	return nil, reader.failure
}

func TestEnvironmentProjectionsAcceptReadOnlyPlansAndPreserveFailure(t *testing.T) {
	failure := errors.New("repository unavailable")
	reader := projectionReadOnlyPlans{failure: failure}
	inventory := environmentInventoryProjectionPort{repository: reader}
	lifecycle := releaseLifecycleProjectionPort{repository: reader}
	if _, err := inventory.EnvironmentInventory(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("inventory failure changed: %v", err)
	}
	if _, err := lifecycle.ReleaseLifecycle(t.Context()); !errors.Is(err, failure) {
		t.Fatalf("lifecycle failure changed: %v", err)
	}
}
