package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func succeededFoundationForHealth(t *testing.T) (*MemoryRepository, PlanRecord) {
	t.Helper()
	ctx := context.Background()
	repository := NewMemoryRepository()
	state := foundation()
	state.Entities[0].HealthQuorum = "SIMPLE_MAJORITY"
	record, err := NewLifecycle(repository).CreatePlan(ctx, state)
	if err != nil {
		t.Fatal(err)
	}
	record, err = repository.UpdatePlan(ctx, record.ID, func(current *PlanRecord) error {
		current.Status = PlanSucceeded
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entities := make([]ReportedEntity, 0, len(record.Plan.Steps))
	for _, step := range record.Plan.Steps {
		entities = append(entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy, RolloutStatus: RolloutUpToDate, ConfigStatus: ConfigApplied,
			Lifecycle: LifecycleInstalled, ObservedAt: time.Unix(100, 0),
		})
	}
	if _, err := repository.PutEnvironmentReport(ctx, EnvironmentReport{
		Environment: state.Environment.ID, AgentID: "agent-old", ObservedAt: time.Unix(100, 0), Entities: entities,
	}); err != nil {
		t.Fatal(err)
	}
	return repository, record
}

func TestActiveHealthTargetsUseLatestSucceededPlan(t *testing.T) {
	t.Parallel()
	repository, record := succeededFoundationForHealth(t)
	targets, err := latestHealthTargets(context.Background(), repository, "dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 || targets[1].InstallationID != record.Plan.Steps[1].InstallationID || targets[1].Quorum != "SIMPLE_MAJORITY" {
		t.Fatalf("health targets = %#v", targets)
	}
	if targets[0].Quorum != "ALL" {
		t.Fatalf("default quorum = %q", targets[0].Quorum)
	}
}

func TestActiveHealthObservationRecomputesHealthAndPreservesOtherAxes(t *testing.T) {
	t.Parallel()
	repository, record := succeededFoundationForHealth(t)
	step := record.Plan.Steps[1]
	report, err := applyHealthObservations(context.Background(), repository, "dev-use2", "agent-health", []HealthObservation{{
		InstallationID: step.InstallationID,
		Details: EntityHealthDetails{Nodes: []EntityNodeHealth{
			{NodeID: "pod-a", ProductVersion: step.Version, Containers: []EntityContainerHealth{{
				ContainerID: "container-a", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{ID: "DATABASE", Result: HealthHealthy, Params: map[string]any{}}},
			}}},
			{NodeID: "pod-b", ProductVersion: step.Version, Containers: []EntityContainerHealth{{
				ContainerID: "container-b", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{ID: "DATABASE", Result: HealthWarning, Message: "lagging", Params: map[string]any{"seconds": 5}}},
			}}},
			{NodeID: "pod-c", ProductVersion: step.Version, Containers: []EntityContainerHealth{{
				ContainerID: "container-c", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{ID: "DATABASE", Result: HealthHealthy, Params: map[string]any{}}},
			}}},
		}},
	}}, time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	var updated ReportedEntity
	for _, entity := range report.Entities {
		if entity.InstallationID == step.InstallationID {
			updated = entity
		}
	}
	if updated.Health != HealthHealthy || updated.HealthDetails == nil || updated.HealthDetails.Quorum != "SIMPLE_MAJORITY" ||
		updated.RolloutStatus != RolloutUpToDate || updated.ConfigStatus != ConfigApplied || updated.Lifecycle != LifecycleInstalled ||
		!updated.ObservedAt.Equal(time.Unix(200, 0)) || report.AgentID != "agent-health" {
		t.Fatalf("active health report = %#v", updated)
	}
}

func TestActiveHealthObservationRejectsUnknownTargetsAndOversizedDetails(t *testing.T) {
	t.Parallel()
	repository, record := succeededFoundationForHealth(t)
	_, err := applyHealthObservations(context.Background(), repository, "dev-use2", "agent", []HealthObservation{{
		InstallationID: "absent", Details: EntityHealthDetails{},
	}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "latest succeeded Plan") {
		t.Fatalf("unknown target error = %v", err)
	}
	step := record.Plan.Steps[0]
	_, err = applyHealthObservations(context.Background(), repository, "dev-use2", "agent", []HealthObservation{{
		InstallationID: step.InstallationID,
		Details: EntityHealthDetails{Nodes: []EntityNodeHealth{{
			NodeID: "pod", ProductVersion: step.Version, Containers: []EntityContainerHealth{{
				ContainerID: "container", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{ID: "LARGE", Result: HealthWarning, Message: strings.Repeat("x", 10_000), Params: map[string]any{}}},
			}},
		}}},
	}}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "10,000 bytes") {
		t.Fatalf("oversized details error = %v", err)
	}
}

// Quorum decides how many healthy nodes are enough, and each mode counts
// differently. Only one mode had a test, so the arithmetic in the others was
// free to be wrong.
func TestHealthQuorumModesCountTheirOwnWay(t *testing.T) {
	t.Parallel()
	// Node results speak the Apollo health vocabulary -- HEALTHY, REPAIRING,
	// WARNING, ERROR, TERMINAL -- and not the lifecycle one. worseHealth ranks only
	// those five, so a node marked UNHEALTHY ranks equal to a healthy one and a
	// quorum failure comes back HEALTHY. That is worth knowing before writing a
	// test against it, and it is why every failing node below is an ERROR.
	nodes := func(healthy, unhealthy int) []EntityNodeHealth {
		var result []EntityNodeHealth
		for index := 0; index < healthy; index++ {
			result = append(result, EntityNodeHealth{NodeID: fmt.Sprintf("h%d", index), Result: HealthHealthy})
		}
		for index := 0; index < unhealthy; index++ {
			result = append(result, EntityNodeHealth{NodeID: fmt.Sprintf("u%d", index), Result: HealthError})
		}
		return result
	}
	for _, testCase := range []struct {
		name               string
		quorum             string
		healthy, unhealthy int
		want               string
	}{
		{"ANY is satisfied by one", "ANY", 1, 4, HealthHealthy},
		{"ANY still needs one", "ANY", 0, 4, HealthError},
		{"NO_GUARANTEE is satisfied by one", "NO_GUARANTEE", 1, 4, HealthHealthy},
		{"ALL_BUT_ONE tolerates exactly one", "ALL_BUT_ONE", 4, 1, HealthHealthy},
		{"ALL_BUT_ONE does not tolerate two", "ALL_BUT_ONE", 3, 2, HealthError},
		{"ALL_BUT_ONE of a single node still needs it", "ALL_BUT_ONE", 0, 1, HealthError},
		{"ALL_BUT_ONE of a single healthy node passes", "ALL_BUT_ONE", 1, 0, HealthHealthy},
		// A majority of four is three, not two: half plus one, not half.
		{"SIMPLE_MAJORITY of four needs three", "SIMPLE_MAJORITY", 3, 1, HealthHealthy},
		{"SIMPLE_MAJORITY of four is not met by two", "SIMPLE_MAJORITY", 2, 2, HealthError},
		{"SIMPLE_MAJORITY of five needs three", "SIMPLE_MAJORITY", 3, 2, HealthHealthy},
		{"SIMPLE_MAJORITY of five is not met by two", "SIMPLE_MAJORITY", 2, 3, HealthError},
		{"an unset quorum needs every node", "", 4, 1, HealthError},
		{"an unset quorum is met by every node", "", 5, 0, HealthHealthy},
		{"UNKNOWN cannot be decided", "UNKNOWN", 5, 0, HealthIndeterminate},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			details := EntityHealthDetails{Quorum: testCase.quorum, Nodes: nodes(testCase.healthy, testCase.unhealthy)}
			if got := healthResult(details); got != testCase.want {
				t.Errorf("healthResult = %q, wanted %q", got, testCase.want)
			}
		})
	}
	if got := healthResult(EntityHealthDetails{Quorum: "ALL"}); got != HealthUnknown {
		t.Errorf("no nodes at all = %q", got)
	}
}

// What normalising an Agent's health report settles before anything reads it.
func TestNormalizeHealthDetailsSettlesNodesChecksAndOrder(t *testing.T) {
	t.Parallel()

	// A node reporting no containers has not reported health; calling it healthy
	// because nothing said otherwise is the one answer it must not give.
	details := EntityHealthDetails{Nodes: []EntityNodeHealth{
		{NodeID: "node-b", ProductVersion: "1.0.0"},
		{NodeID: "node-a", ProductVersion: "1.0.0", Containers: []EntityContainerHealth{
			{ContainerID: "main", Liveness: true, Readiness: true, Checks: []HealthCheck{
				{ID: "STARTUP", Result: HealthHealthy},
			}},
		}},
	}}
	if err := normalizeHealthDetails("details", &details); err != nil {
		t.Fatal(err)
	}
	// Sorted by node identifier, so two reports of the same nodes compare equal.
	if details.Nodes[0].NodeID != "node-a" || details.Nodes[1].NodeID != "node-b" {
		t.Errorf("nodes were not ordered: %#v", details.Nodes)
	}
	if got := details.Nodes[1].Result; got != HealthUnknown {
		t.Errorf("a node with no containers resolved to %q", got)
	}
	if got := details.Nodes[0].Result; got != HealthHealthy {
		t.Errorf("a node with a healthy container resolved to %q", got)
	}
	// An absent parameter set becomes an empty one, so a reader never has to
	// distinguish "no parameters" from "not reported".
	if params := details.Nodes[0].Containers[0].Checks[0].Params; params == nil {
		t.Error("a check with no parameters kept a nil parameter map")
	} else if len(params) != 0 {
		t.Errorf("a check with no parameters gained %#v", params)
	}

	// A container that is not both live and ready is an error regardless of its
	// checks, and the node takes the worst of its containers.
	unready := EntityHealthDetails{Nodes: []EntityNodeHealth{{
		NodeID: "node-a", ProductVersion: "1.0.0", Containers: []EntityContainerHealth{
			{ContainerID: "main", Liveness: true, Readiness: false},
			{ContainerID: "sidecar", Liveness: true, Readiness: true},
		},
	}}}
	if err := normalizeHealthDetails("details", &unready); err != nil {
		t.Fatal(err)
	}
	if got := unready.Nodes[0].Result; got != HealthError {
		t.Errorf("a node with an unready container resolved to %q", got)
	}
}

// Health details are Agent-supplied and land in durable Plan state, so their size
// is bounded. The bound was asserted from above only, which a rule that refuses
// everything satisfies just as well; this pins where it actually falls.
func TestHealthDetailsSizeBoundIsAssertedAtItsEdge(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository, record := succeededFoundationForHealth(t)
	step := record.Plan.Steps[1]

	// The padding length is measured rather than guessed: serialise the details
	// with an empty pad, then make up the difference exactly.
	build := func(pad int) EntityHealthDetails {
		// The quorum is overwritten from the Plan's target before the size is
		// taken, so the fixture has to carry the same one or the measurement is
		// short by the difference in those two strings.
		return EntityHealthDetails{Quorum: "SIMPLE_MAJORITY", Nodes: []EntityNodeHealth{{
			NodeID: "pod-a", ProductVersion: step.Version, Containers: []EntityContainerHealth{{
				ContainerID: "container-a", Liveness: true, Readiness: true,
				Checks: []HealthCheck{{
					ID: "DATABASE", Result: HealthHealthy,
					Params: map[string]any{"note": strings.Repeat("x", pad)},
				}},
			}},
		}}}
	}
	// The size is measured after normalisation, which fills in each node's and
	// container's result and defaults absent parameters -- so the padding has to
	// be measured the same way, not against the details as written.
	normalized := build(0)
	if err := normalizeHealthDetails("details", &normalized); err != nil {
		t.Fatal(err)
	}
	empty, err := json.Marshal(normalized)
	if err != nil {
		t.Fatal(err)
	}
	atLimit := 10_000 - len(empty)
	if atLimit < 1 {
		t.Fatalf("the fixture is already %d bytes, larger than the bound", len(empty))
	}

	if _, err := applyHealthObservations(ctx, repository, "dev-use2", "agent-health",
		[]HealthObservation{{InstallationID: step.InstallationID, Details: build(atLimit)}}, time.Unix(200, 0)); err != nil {
		t.Errorf("details of exactly 10,000 bytes were refused: %v", err)
	}
	if _, err := applyHealthObservations(ctx, repository, "dev-use2", "agent-health",
		[]HealthObservation{{InstallationID: step.InstallationID, Details: build(atLimit + 1)}}, time.Unix(200, 0)); err == nil {
		t.Error("details of 10,001 bytes were accepted")
	}
}
