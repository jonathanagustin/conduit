package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// The transport seam calls the real Hub lifecycle. Only the timing hook is
// synthetic: it delivers a successful renewal after a successor has taken over.
type successorLifecycleClient struct {
	lifecycle  *controlplane.Lifecycle
	afterRenew func(context.Context) error
}

func (client successorLifecycleClient) Lease(ctx context.Context, environment, agent string, duration time.Duration) (controlplane.PlanRecord, error) {
	record, err := client.lifecycle.LeaseNextPlan(ctx, environment, agent, duration)
	if errors.Is(err, controlplane.ErrNoPlanAvailable) {
		return record, ErrNoPlan
	}
	return record, err
}

func (client successorLifecycleClient) Start(ctx context.Context, _, _, planID, token string) error {
	_, err := client.lifecycle.StartPlan(ctx, planID, token)
	return err
}

func (client successorLifecycleClient) Renew(ctx context.Context, _, _, planID, token string, duration time.Duration) error {
	_, err := client.lifecycle.RenewLease(ctx, planID, token, duration)
	if err == nil && client.afterRenew != nil {
		err = client.afterRenew(ctx)
	}
	return err
}

func (client successorLifecycleClient) Finish(ctx context.Context, _, _, planID, token string,
	succeeded bool, failure, failedInstallation string, execution controlplane.PlanExecution, report controlplane.EnvironmentReport,
) error {
	_, err := client.lifecycle.FinishPlan(ctx, planID, token, succeeded, failure, failedInstallation, &execution, &report)
	return err
}

type successorScenarioExecutor struct {
	HelmExecutor
	observe       func(context.Context, controlplane.PlanStep) (controlplane.ReportedEntity, error)
	rollbackCalls int
}

func (executor *successorScenarioExecutor) Observe(ctx context.Context, step controlplane.PlanStep) (controlplane.ReportedEntity, error) {
	return executor.observe(ctx, step)
}

func (executor *successorScenarioExecutor) Rollback(ctx context.Context, receipt Receipt) error {
	executor.rollbackCalls++
	return executor.HelmExecutor.Rollback(ctx, receipt)
}

// Both Workers run their real apply/observe/rollback/report loops over one Hub
// repository and one simulated Helm release. This deliberately crosses the
// lease-renewal/ownership-check boundary rather than calling Rollback directly.
func TestStaleWorkerRollbackPreservesARunningSuccessorAndItsReports(t *testing.T) {
	for _, liveFence := range []string{"successor", "missing", "malformed"} {
		t.Run(liveFence, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			repository := controlplane.NewMemoryRepository()
			lifecycle := controlplane.NewLifecycle(repository)
			step := releaseStep("entity-name", "custom-release")
			record, err := repository.CreatePlan(ctx, controlplane.PlanRecord{
				ID: "pln_successor", Status: controlplane.PlanApproved,
				Plan: controlplane.Plan{Environment: "dev", Generation: "generation-one", Steps: []controlplane.PlanStep{step}},
			})
			if err != nil {
				t.Fatal(err)
			}
			priorReport, err := repository.PutEnvironmentReport(ctx, controlplane.EnvironmentReport{
				Environment: "dev", AgentID: "previous-agent", PlanID: "previous-plan",
			})
			if err != nil {
				t.Fatal(err)
			}
			runner := &helmRunner{step: step}
			helm := HelmExecutor{Runner: runner, Timeout: time.Minute}
			ready, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			successorExecutor := &successorScenarioExecutor{HelmExecutor: helm}
			successorExecutor.observe = func(ctx context.Context, step controlplane.PlanStep) (controlplane.ReportedEntity, error) {
				close(ready)
				select {
				case <-release:
					return helm.Observe(ctx, step)
				case <-ctx.Done():
					return controlplane.ReportedEntity{}, ctx.Err()
				}
			}
			staleExecutor := &successorScenarioExecutor{
				HelmExecutor: helm,
				observe: func(context.Context, controlplane.PlanStep) (controlplane.ReportedEntity, error) {
					return controlplane.ReportedEntity{}, errors.New("predecessor observation failed")
				},
			}
			type outcome struct {
				executed bool
				err      error
			}
			successorDone := make(chan outcome, 1)
			var successorRecord controlplane.PlanRecord
			var successorDescription string
			var commandBoundary int
			control := successorLifecycleClient{lifecycle: lifecycle}
			control.afterRenew = func(ctx context.Context) error {
				// The old renewal really succeeded. Move just its expiry into the
				// past to model time passing while that reply was delayed; no sleep
				// or fabricated successor lease/attempt is involved.
				if _, err := repository.UpdatePlan(ctx, record.ID, func(current *controlplane.PlanRecord) error {
					current.Lease.ExpiresAt = time.Now().Add(-time.Second)
					return nil
				}); err != nil {
					return err
				}
				go func() {
					executed, err := (Worker{ControlPlane: successorLifecycleClient{lifecycle: lifecycle}, Executor: successorExecutor,
						Environment: "dev", AgentID: "successor", LeaseDuration: 15 * time.Minute}).RunOnce(ctx)
					successorDone <- outcome{executed, err}
				}()
				select {
				case <-ready:
				case result := <-successorDone:
					t.Fatalf("successor stopped before observation: %#v", result)
				case <-ctx.Done():
					return ctx.Err()
				}
				var err error
				successorRecord, err = repository.GetPlan(ctx, record.ID)
				if err != nil {
					return err
				}
				if successorRecord.Status != controlplane.PlanRunning || successorRecord.Lease.Attempt != 2 || successorRecord.Lease.AgentID != "successor" {
					t.Fatalf("successor did not own a running attempt: %#v", successorRecord)
				}
				successorDescription = runner.description
				fence, ok := parseReleaseFence(successorDescription)
				if !ok || fence.Attempt != 2 || fence.Name != step.HelmRelease() {
					t.Fatalf("successor apply did not write its fence: %q", successorDescription)
				}
				// A status response with unreadable ownership is not authority for
				// the old receipt either. The simulated live release remains B's.
				switch liveFence {
				case "missing":
					runner.statusDescription = releaseDescription(step)
				case "malformed":
					runner.statusDescription = releaseDescription(step) + releaseFenceMarker + "invalid"
				}
				commandBoundary = len(runner.commands)
				return nil
			}
			executed, staleErr := (Worker{ControlPlane: control, Executor: staleExecutor,
				Environment: "dev", AgentID: "predecessor", LeaseDuration: 15 * time.Minute}).RunOnce(ctx)
			if !executed || staleErr == nil || !strings.Contains(staleErr.Error(), "finish failed plan") || !errors.Is(staleErr, controlplane.ErrLeaseRejected) {
				t.Fatalf("stale worker result: executed=%v err=%v", executed, staleErr)
			}
			if staleExecutor.rollbackCalls != 1 {
				t.Fatalf("expected real worker rollback attempt, got %d", staleExecutor.rollbackCalls)
			}
			for _, command := range runner.commands[commandBoundary:] {
				if len(command) > 1 && (command[1] == "uninstall" || command[1] == "rollback") {
					t.Errorf("stale worker mutated successor release: %v", command)
				}
			}
			if !runner.installed || runner.description != successorDescription {
				t.Errorf("successor release changed: installed=%v description=%q", runner.installed, runner.description)
			}
			afterStale, err := repository.GetPlan(ctx, record.ID)
			if err != nil || !reflect.DeepEqual(afterStale, successorRecord) {
				t.Fatalf("stale finish changed successor plan: %#v; before %#v; err=%v", afterStale, successorRecord, err)
			}
			afterReport, err := repository.GetEnvironmentReport(ctx, "dev")
			if err != nil || !reflect.DeepEqual(afterReport, priorReport) {
				t.Fatalf("stale finish changed reports: %#v; before %#v; err=%v", afterReport, priorReport, err)
			}
			// Restore truthful status and let the still-running successor report.
			runner.statusDescription = ""
			unblock()
			select {
			case result := <-successorDone:
				if !result.executed || result.err != nil {
					t.Fatalf("successor did not complete: %#v", result)
				}
			case <-ctx.Done():
				t.Fatal("successor did not finish")
			}
			completed, err := repository.GetPlan(ctx, record.ID)
			if err != nil || completed.Status != controlplane.PlanSucceeded || completed.Execution == nil || completed.Execution.Attempt != 2 || completed.Execution.AgentID != "successor" {
				t.Fatalf("successor evidence = %#v, %v", completed, err)
			}
			report, err := repository.GetEnvironmentReport(ctx, "dev")
			if err != nil || report.AgentID != "successor" || report.PlanID != record.ID || len(report.Entities) != 1 || report.Entities[0].ReleaseDigest != step.ReleaseDigest {
				t.Fatalf("successor report = %#v, %v", report, err)
			}
		})
	}
}
