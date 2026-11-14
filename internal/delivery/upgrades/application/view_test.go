package application

import (
	"context"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestDependenciesMatchTheReleaseAndUpgradesExplainWhoIsBehind(t *testing.T) {
	ctx := context.Background()
	repository := controlplane.NewMemoryRepository()
	now := time.Date(2026, 11, 14, 18, 0, 0, 0, time.UTC)
	if _, err := repository.CreatePlan(ctx, controlplane.PlanRecord{
		ID: "pln", CreatedAt: now, Status: controlplane.PlanSucceeded,
		Plan: controlplane.Plan{Environment: "east", Steps: []controlplane.PlanStep{
			{InstallationID: "ledger", Product: "ledger", Version: "1.2.0"},
			{InstallationID: "gateway", Product: "gateway", Version: "2.0.0"},
		}},
		DesiredState: &controlplane.DesiredState{Releases: []controlplane.Release{{
			Product: "ledger", Version: "1.2.0",
			Dependencies: []controlplane.Dependency{{Product: "gateway", MinimumVersion: "2.0.0", MaximumVersion: "2.x"}},
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{
		Environment: "east", ObservedAt: now, Entities: []controlplane.ReportedEntity{
			{InstallationID: "ledger", Product: "ledger", Version: "1.1.0", ObservedAt: now},
			{InstallationID: "gateway", Product: "gateway", Version: "2.0.0", ObservedAt: now},
		},
	}); err != nil {
		t.Fatal(err)
	}
	dependencies, err := Dependencies(ctx, repository, "ledger", "1.2.0")
	if err != nil || len(dependencies) != 1 || dependencies[0].Product != "gateway" || dependencies[0].MinimumVersion != "2.0.0" {
		t.Fatalf("dependencies = %#v %v", dependencies, err)
	}
	rows, err := Behind(ctx, repository, map[string]string{"east\x00ledger": "unmet dependency gateway"})
	if err != nil || len(rows) != 1 || rows[0].InstallationID != "ledger" || rows[0].Reason != "unmet dependency gateway" {
		t.Fatalf("behind = %#v %v", rows, err)
	}
}
