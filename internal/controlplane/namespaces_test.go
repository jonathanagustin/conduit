package controlplane

import (
	"context"
	"strings"
	"testing"
	"time"
)

func boolPointer(value bool) *bool { return &value }

func namespaceFoundation() DesiredState {
	state := foundation()
	state.Environment.Namespaces = []NamespaceTarget{{RID: "ri.namespace.example", Name: "example", Description: "application namespace"}}
	return state
}

func finishNamespacePlan(t *testing.T, lifecycle *Lifecycle, state DesiredState) {
	t.Helper()
	record, err := lifecycle.CreatePlan(context.Background(), state)
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.ApprovePlan(context.Background(), record.ID, "reviewer", "approved")
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.LeaseNextPlan(context.Background(), state.Environment.ID, "agent", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.StartPlan(context.Background(), record.ID, record.Lease.Token)
	if err != nil {
		t.Fatal(err)
	}
	report := installedReport(t, state)
	if _, err := lifecycle.FinishPlan(context.Background(), record.ID, record.Lease.Token, true, "", "", successfulPlanExecution(record), &report); err != nil {
		t.Fatal(err)
	}
}

func TestNamespaceNameIsImmutableForAnExistingEntity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	lifecycle := NewLifecycle(NewMemoryRepository())
	state := namespaceFoundation()
	finishNamespacePlan(t, lifecycle, state)
	state.Environment.Generation = "namespace-move"
	state.Environment.Namespaces = append(state.Environment.Namespaces, NamespaceTarget{RID: "ri.namespace.other", Name: "other"})
	state.Entities[1].Namespace = "other"
	if _, err := lifecycle.CreatePlan(ctx, state); err == nil || !strings.Contains(err.Error(), "immutable after Entity creation") {
		t.Fatalf("namespace move error = %v", err)
	}
}

func TestNamespaceDeletionRequiresPriorUnprotectAndNoInstalledEntities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	state := namespaceFoundation()
	finishNamespacePlan(t, lifecycle, state)
	deleted := cloneJSON(state)
	deleted.Environment.Generation = "delete-protected"
	deleted.Environment.Namespaces = nil
	deleted.Entities = nil
	if _, err := lifecycle.CreatePlan(ctx, deleted); err == nil || !strings.Contains(err.Error(), "explicitly unprotected") {
		t.Fatalf("protected deletion error = %v", err)
	}

	unprotected := cloneJSON(state)
	unprotected.Environment.Generation = "unprotected"
	unprotected.Environment.Namespaces[0].DeletionProtected = boolPointer(false)
	finishNamespacePlan(t, lifecycle, unprotected)
	deleted.Environment.Generation = "delete-after-unprotect"
	if _, err := lifecycle.CreatePlan(ctx, deleted); err == nil || !strings.Contains(err.Error(), "reports uninstalled") {
		t.Fatalf("unobserved Entity deletion error = %v", err)
	}

	report := installedReport(t, state)
	for index := range report.Entities {
		report.Entities[index].Lifecycle = LifecycleUninstalled
		report.Entities[index].Health = HealthUnknown
	}
	if _, err := repository.PutEnvironmentReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	if _, err := lifecycle.CreatePlan(ctx, deleted); err != nil {
		t.Fatalf("safe namespace deletion: %v", err)
	}
}

func TestSameProductMayUseSeparateNamedEntitiesInDifferentNamespaces(t *testing.T) {
	t.Parallel()
	state := namespaceFoundation()
	state.Environment.Namespaces = append(state.Environment.Namespaces, NamespaceTarget{RID: "ri.namespace.second", Name: "second"})
	state.Entities = append(state.Entities, Entity{
		Name: "example-api-second", Product: "app.example:example-api", Version: "0.1.0", Namespace: "second",
	})
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, step := range plan.Steps {
		if step.InstallationID == "example-api-second" && step.Namespace == "second" {
			found = true
		}
	}
	if !found {
		t.Fatalf("second namespace installation absent: %#v", plan.Steps)
	}
	state.Entities[len(state.Entities)-1].Namespace = "example"
	if errs := Validate(state); len(errs) == 0 || !strings.Contains(errs[0].Message, "unique within a Kubernetes namespace") {
		t.Fatalf("same-namespace duplicate Product errors = %#v", errs)
	}
}
