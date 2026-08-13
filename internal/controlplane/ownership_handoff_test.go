package controlplane

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestOwnershipHandoffGatesDependenciesRetainsExactStateAndDoesNotRepeat(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	initial := foundation()
	if _, err := lifecycle.CreatePlan(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, installedReport(t, initial)); err != nil {
		t.Fatal(err)
	}
	blocked := initial
	blocked.Environment.Generation = "handoff-blocked"
	blocked.Entities = blocked.Entities[:1]
	blocked.OwnershipHandoffs = []OwnershipHandoff{{InstallationID: "example-api", Rationale: "transfer to application team"}}
	if _, err := lifecycle.CreatePlan(ctx, blocked); err == nil {
		t.Fatal("required dependent remained managed during ownership handoff")
	}

	handoff := initial
	handoff.Environment.Generation = "handoff-approved"
	handoff.Entities = nil
	handoff.OwnershipHandoffs = []OwnershipHandoff{{InstallationID: "example-api", Rationale: "transfer to application team"}}
	record, err := lifecycle.CreatePlan(ctx, handoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Plan.Steps) != 2 || record.Plan.Steps[0].InstallationID != "example-workspace" ||
		record.Plan.Steps[0].Action != planActionUninstall || record.Plan.Steps[1].InstallationID != "example-api" ||
		record.Plan.Steps[1].Action != PlanActionUnmanage {
		t.Fatalf("handoff Plan = %#v", record.Plan.Steps)
	}
	changes, err := NewGovernanceService(repository).planChanges(ctx, record.Plan, Principal{Username: "operator"})
	if err != nil || len(changes) != 2 || changes[1].Type != ChangeTypeDeleteEntity || changes[1].Request["unmanageOnly"] != true {
		t.Fatalf("handoff Change Request shape = %#v, %v", changes, err)
	}
	record, err = lifecycle.ApprovePlan(ctx, record.ID, "reviewer", "approved ownership transfer")
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.LeaseNextPlan(ctx, initial.Environment.ID, "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.StartPlan(ctx, record.ID, record.Lease.Token)
	if err != nil {
		t.Fatal(err)
	}
	report := EnvironmentReport{Entities: []ReportedEntity{
		reportedForHandoffStep(record.Plan.Steps[0], LifecycleUninstalled),
		reportedForHandoffStep(record.Plan.Steps[1], LifecycleInstalled),
	}}
	if _, err := lifecycle.FinishPlan(ctx, record.ID, record.Lease.Token, true, "", "", successfulPlanExecution(record), &report); err != nil {
		t.Fatal(err)
	}
	storedReport, err := repository.GetEnvironmentReport(ctx, initial.Environment.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ReportedEntity{}
	for _, entity := range storedReport.Entities {
		byID[entity.InstallationID] = entity
	}
	if len(storedReport.Entities) != 2 || byID["example-api"].Lifecycle != LifecycleInstalled ||
		byID["example-workspace"].Lifecycle != LifecycleUninstalled {
		t.Fatalf("exact post-handoff report = %#v", storedReport.Entities)
	}
	handoff.Environment.Generation = "handoff-stable"
	stable, err := lifecycle.CreatePlan(ctx, handoff)
	if err != nil || len(stable.Plan.Steps) != 0 {
		t.Fatalf("completed handoff repeated = %#v, %v", stable.Plan.Steps, err)
	}
}

func TestUndoUnmanageIsAllowedOnlyBeforeLease(t *testing.T) {
	ctx := context.Background()
	makePlan := func(t *testing.T) (*Lifecycle, Repository, PlanRecord) {
		t.Helper()
		repository := NewMemoryRepository()
		lifecycle := NewLifecycle(repository)
		state := foundation()
		if _, err := lifecycle.CreatePlan(ctx, state); err != nil {
			t.Fatal(err)
		}
		if _, err := repository.PutEnvironmentReport(ctx, installedReport(t, state)); err != nil {
			t.Fatal(err)
		}
		state.Environment.Generation = "handoff"
		state.Entities = nil
		state.OwnershipHandoffs = []OwnershipHandoff{{InstallationID: "example-api", Rationale: "transfer"}}
		record, err := lifecycle.CreatePlan(ctx, state)
		if err != nil {
			t.Fatal(err)
		}
		return lifecycle, repository, record
	}
	lifecycle, _, record := makePlan(t)
	undone, err := lifecycle.UndoUnmanage(ctx, record.ID, "operator", "keep managed")
	if err != nil || undone.Status != PlanCancelled || undone.Cancellation == nil {
		t.Fatalf("undo = %#v, %v", undone, err)
	}

	lifecycle, _, record = makePlan(t)
	if record, err = lifecycle.ApprovePlan(ctx, record.ID, "reviewer", "approved"); err != nil {
		t.Fatal(err)
	}
	if record, err = lifecycle.LeaseNextPlan(ctx, record.Plan.Environment, "agent", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.UndoUnmanage(ctx, record.ID, "operator", "too late"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("leased undo error = %v", err)
	}
}

func TestOwnershipHandoffCannotDeleteEntitySecret(t *testing.T) {
	plan := Plan{
		Steps: []PlanStep{{
			Action: PlanActionUnmanage, InstallationID: "api", Namespace: "example",
			SecretAccess: []SecretAccess{{Assignment: "database"}},
		}},
		SecretMutations: []SecretMutation{{
			Action: SecretDelete, Namespace: "example", Previous: &SecretAccess{Assignment: "database"},
		}},
	}
	if err := validateHandoffSecretMutations(plan); !errors.Is(err, ErrConflict) {
		t.Fatalf("handoff secret deletion error = %v", err)
	}
}

func reportedForHandoffStep(step PlanStep, lifecycle string) ReportedEntity {
	health := HealthHealthy
	if lifecycle == LifecycleUninstalled {
		health = HealthUnknown
	}
	return ReportedEntity{
		InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
		ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
		Health: health, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied, Lifecycle: lifecycle,
	}
}
