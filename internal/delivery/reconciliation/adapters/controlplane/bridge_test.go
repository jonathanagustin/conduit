package controlplaneadapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/domain"
	"github.com/jonathanagustin/conduit/internal/delivery/reconciliation/ports"
)

func TestRepositoryTranslatesExactPlanAndReportTargets(t *testing.T) {
	ctx := t.Context()
	inputs := newMemoryInputs()
	repository, err := NewRepository(controlplane.NewMemoryRepository(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 14, 21, 0, 0, 0, time.UTC)
	record := controlplane.PlanRecord{
		ID: "pln_translation", DesiredStateDigest: testDigest("state"),
		DesiredState: &controlplane.DesiredState{
			APIVersion: "platform.delivery/v1alpha1", Kind: "EnvironmentDeployment",
			Environment: controlplane.Environment{ID: "tenant-a", Generation: "generation-1"},
		},
		Plan: controlplane.Plan{Environment: "tenant-a", Generation: "generation-1", Steps: []controlplane.PlanStep{
			{Action: planActionReconcile, InstallationID: "api", Product: "app.example:api", Version: "1.2.3", ReleaseDigest: testDigest("release"), Namespace: "app", ConfigDigest: testDigest("config")},
			{Action: controlplane.PlanActionUnmanage, InstallationID: "foreign"},
		}},
		Status: controlplane.PlanAwaitingApproval, CreatedAt: now, UpdatedAt: now, Revision: 1,
	}
	created, err := repository.CreatePlan(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != record.ID || len(inputs.desired) != 1 {
		t.Fatalf("created=%s desired=%d", created.ID, len(inputs.desired))
	}
	key := domain.Key{Environment: "tenant-a", Installation: "api"}
	desired := inputs.desired[key.String()]
	if desired.Generation != "generation-1" || desired.TargetDigest == "" || desired.PayloadDigest == "" {
		t.Fatalf("desired snapshot=%#v", desired)
	}
	var payload controlplane.DesiredState
	if err := json.Unmarshal(desired.Payload, &payload); err != nil || payload.Environment.ID != "tenant-a" {
		t.Fatalf("desired payload=%#v err=%v", payload, err)
	}
	report := controlplane.EnvironmentReport{
		Environment: "tenant-a", AgentID: "agent-a", PlanID: record.ID, ObservedAt: now.Add(time.Minute),
		Entities: []controlplane.ReportedEntity{{
			InstallationID: "api", Product: "app.example:api", Version: "1.2.3",
			ReleaseDigest: testDigest("release"), Namespace: "app", ConfigDigest: testDigest("config"),
			Health: controlplane.HealthHealthy, RolloutStatus: controlplane.RolloutUpToDate,
			ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleInstalled,
		}},
	}
	if _, err := repository.PutEnvironmentReport(ctx, report); err != nil {
		t.Fatal(err)
	}
	observed := inputs.observations[key.String()]
	if !observed.Complete || observed.Generation != desired.Generation || observed.TargetDigest != desired.TargetDigest {
		t.Fatalf("desired=%#v observed=%#v", desired, observed)
	}
}

func TestPlannerUsesOrdinaryLifecycleAndRejectsSupersededAuthority(t *testing.T) {
	ctx := t.Context()
	state := loadExampleState(t)
	inputs := newMemoryInputs()
	repository, err := NewRepository(controlplane.NewMemoryRepository(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	record, err := controlplane.NewLifecycle(repository).CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Plan.Steps) == 0 {
		t.Fatal("example desired state produced no ordinary Plan steps")
	}
	key := domain.Key{Environment: record.Plan.Environment, Installation: record.Plan.Steps[0].InstallationID}
	desired := inputs.desired[key.String()]
	input := domain.Input{Desired: &desired}
	decision, proposal, err := domain.Decide(input, 1, record.CreatedAt.Add(time.Second), 10*time.Minute)
	if err != nil || proposal == nil || decision.Kind != domain.DecisionProposePlan {
		t.Fatalf("decision=%#v proposal=%#v err=%v", decision, proposal, err)
	}
	planner, err := NewPlanner(repository, inputs)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := planner.Propose(ctx, *proposal)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ProposalID != proposal.ID || receipt.PlanID != record.ID || receipt.CreatedAt.IsZero() {
		t.Fatalf("receipt=%#v record=%s", receipt, record.ID)
	}

	inputs.mu.Lock()
	superseding := desired
	superseding.Generation = "generation-2"
	inputs.desired[key.String()] = superseding
	inputs.mu.Unlock()
	if _, err := planner.Propose(ctx, *proposal); !errors.Is(err, ports.ErrProposalSuperseded) {
		t.Fatalf("superseded proposal error=%v", err)
	}
}

type memoryInputs struct {
	mu           sync.Mutex
	desired      map[string]domain.DesiredSnapshot
	observations map[string]domain.Observation
}

func newMemoryInputs() *memoryInputs {
	return &memoryInputs{desired: map[string]domain.DesiredSnapshot{}, observations: map[string]domain.Observation{}}
}

func (inputs *memoryInputs) UpsertDesired(_ context.Context, snapshot domain.DesiredSnapshot) error {
	inputs.mu.Lock()
	defer inputs.mu.Unlock()
	current, found := inputs.desired[snapshot.Key.String()]
	if !found || snapshot.RecordedAt.After(current.RecordedAt) ||
		(snapshot.RecordedAt.Equal(current.RecordedAt) && snapshot.Generation > current.Generation) {
		inputs.desired[snapshot.Key.String()] = snapshot
	}
	return nil
}

func (inputs *memoryInputs) UpsertObservation(_ context.Context, observation domain.Observation) error {
	inputs.mu.Lock()
	defer inputs.mu.Unlock()
	inputs.observations[observation.Key.String()] = observation
	return nil
}

func (inputs *memoryInputs) CurrentDesired(
	_ context.Context, key domain.Key,
) (domain.DesiredSnapshot, bool, error) {
	inputs.mu.Lock()
	defer inputs.mu.Unlock()
	snapshot, found := inputs.desired[key.String()]
	return snapshot, found, nil
}

func loadExampleState(t *testing.T) controlplane.DesiredState {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".."))
	contents, err := os.ReadFile(filepath.Join(root, "examples", "example-foundation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var state controlplane.DesiredState
	if err := json.Unmarshal(contents, &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func testDigest(value string) string {
	return targetDigest(targetProjection{Product: value})
}
