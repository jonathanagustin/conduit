package composition

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	spoke "github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func approvedCapacityPlan(t *testing.T, repository controlplane.Repository) controlplane.PlanRecord {
	t.Helper()
	lifecycle := controlplane.NewLifecycle(repository)
	record, err := lifecycle.CreatePlan(t.Context(), capacityState("capacity-fault", "generation-one"))
	if err != nil {
		t.Fatal(err)
	}
	record, err = lifecycle.ApprovePlan(t.Context(), record.ID, "capacity-reviewer", "synthetic fault fixture")
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func TestFleetPartitionDoesNotInventExecutionAndRecovers(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	record := approvedCapacityPlan(t, repository)
	metrics := &capacityMetrics{rows: make(map[string]*capacityRPC)}
	var partition atomic.Bool
	client, transport := capacityClient(t, repository, metrics, &partition)
	worker := spoke.Worker{ControlPlane: client, Executor: &capacityExecutor{}, Environment: record.Plan.Environment, AgentID: "partitioned-agent", LeaseDuration: 10 * time.Second}
	partition.Store(true)
	transport.CloseIdleConnections()
	if executed, err := worker.RunOnce(t.Context()); err == nil || executed {
		t.Fatalf("partition result: executed=%v err=%v", executed, err)
	}
	unchanged, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil || unchanged.Status != controlplane.PlanApproved || unchanged.Lease != nil {
		t.Fatalf("unreachable Hub manufactured a lease: %#v %v", unchanged, err)
	}
	if _, err := repository.GetEnvironmentReport(t.Context(), record.Plan.Environment); !errors.Is(err, controlplane.ErrNotFound) {
		t.Fatalf("unreachable Hub manufactured an observation: %v", err)
	}
	partition.Store(false)
	if executed, err := worker.RunOnce(t.Context()); err != nil || !executed {
		t.Fatalf("recovered connection: executed=%v err=%v", executed, err)
	}
	finished, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil || finished.Status != controlplane.PlanSucceeded {
		t.Fatalf("recovered Plan: %#v %v", finished, err)
	}
	if metrics.snapshot()["leases"].Errors != 1 {
		t.Fatalf("missing measured partition: %#v", metrics.snapshot())
	}
}

func TestFleetAgentDisconnectMidPlanIsRecoveredByASecondAgent(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	record := approvedCapacityPlan(t, repository)
	metrics := &capacityMetrics{rows: make(map[string]*capacityRPC)}
	var partition atomic.Bool
	client, transport := capacityClient(t, repository, metrics, &partition)
	executor := &capacityExecutor{entered: make(chan struct{}), block: true}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := (spoke.Worker{ControlPlane: client, Executor: executor, Environment: record.Plan.Environment, AgentID: "disconnected-agent", LeaseDuration: 10 * time.Second}).RunOnce(ctx)
		done <- err
	}()
	select {
	case <-executor.entered:
	case err := <-done:
		t.Fatalf("Agent stopped before execution: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Agent never entered execution")
	}
	running, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil || running.Status != controlplane.PlanRunning || running.Lease == nil {
		t.Fatalf("no actual running lease: %#v %v", running, err)
	}
	// Close pooled TCP connections, refuse subsequent dials, and stop the
	// first Agent while its Apply call is in flight. Its detached final report
	// cannot reach the Hub either. No successful workload is fabricated.
	partition.Store(true)
	transport.CloseIdleConnections()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("disconnected Agent reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disconnected Agent did not stop within its request bound")
	}
	afterDisconnect, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil || !reflect.DeepEqual(afterDisconnect, running) {
		t.Fatalf("unreachable final report changed stored execution: %#v %v", afterDisconnect, err)
	}
	if _, err := repository.GetEnvironmentReport(t.Context(), record.Plan.Environment); !errors.Is(err, controlplane.ErrNotFound) {
		t.Fatalf("disconnected Agent manufactured an observation: %v", err)
	}
	// Use the actual expiry interval of the shipped Agent's minimum lease,
	// rather than editing persisted time or synthesizing a successor attempt.
	wait := time.NewTimer(time.Until(running.Lease.ExpiresAt) + 20*time.Millisecond)
	defer wait.Stop()
	select {
	case <-wait.C:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	healthy, _ := capacityClient(t, repository, metrics, nil)
	if executed, err := (spoke.Worker{ControlPlane: healthy, Executor: &capacityExecutor{}, Environment: record.Plan.Environment, AgentID: "replacement-agent", LeaseDuration: 10 * time.Second}).RunOnce(t.Context()); err != nil || !executed {
		t.Fatalf("successor: executed=%v err=%v", executed, err)
	}
	finished, err := repository.GetPlan(t.Context(), record.ID)
	if err != nil || finished.Status != controlplane.PlanSucceeded || finished.Lease.Attempt != 2 || finished.Execution == nil || finished.Execution.AgentID != "replacement-agent" {
		t.Fatalf("replacement did not produce real attempt-two evidence: %#v %v", finished, err)
	}
	observed, err := repository.GetEnvironmentReport(t.Context(), record.Plan.Environment)
	if err != nil || observed.AgentID != "replacement-agent" || observed.PlanID != record.ID {
		t.Fatalf("replacement report: %#v %v", observed, err)
	}
	partition.Store(false)
	oldExecution := *finished.Execution
	oldExecution.Attempt = running.Lease.Attempt
	oldExecution.AgentID = "disconnected-agent"
	if err := client.Finish(t.Context(), record.Plan.Environment, "disconnected-agent", record.ID, running.Lease.Token, true, "", "", oldExecution, observed); err == nil {
		t.Fatal("expired Agent final report was admitted")
	}
	afterStale, err := repository.GetEnvironmentReport(t.Context(), record.Plan.Environment)
	if err != nil || !reflect.DeepEqual(afterStale, observed) {
		t.Fatalf("expired final report replaced the successor observation: %#v %v", afterStale, err)
	}
}
