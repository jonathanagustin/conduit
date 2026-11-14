package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func closedMaintenanceOverride(step controlplane.PlanStep, start, end time.Time) controlplane.PlanStep {
	step.Maintenance = controlplane.MaintenanceConstraint{Downtime: &controlplane.WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{"MONDAY/10:00-MONDAY/11:00"}}, NoDowntime: &controlplane.WeeklyMaintenanceWindow{TimeZone: "UTC", Intervals: []string{"MONDAY/10:00-MONDAY/11:00"}}, Overrides: []controlplane.MaintenanceOverride{{InstallationID: step.InstallationID, Type: "NO_DOWNTIME", StartsAt: start, EndsAt: end, Reason: "CAB-364", ApprovedBy: "change-request:crq_364"}}}
	return step
}
func TestWorkerRefusesExpiredTemporaryWindowBeforeExecutor(t *testing.T) {
	record := leasedRecord()
	now := time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC)
	record.Plan.Steps[0] = closedMaintenanceOverride(record.Plan.Steps[0], now.Add(-time.Hour), now)
	control := &fakeControlPlane{record: record}
	executor := &fakeExecutor{}
	worker := Worker{ControlPlane: control, Executor: executor, Environment: "dev", AgentID: "agent", LeaseDuration: time.Minute, Now: func() time.Time { return now }}
	_, err := worker.RunOnce(t.Context())
	if err == nil || !strings.Contains(err.Error(), "maintenance window closed") || len(executor.applied) != 0 || control.succeeded {
		t.Fatal("expired override mutated", executor.applied, err)
	}
}

type expiringHelmRunner struct {
	base *helmRunner
	now  *time.Time
	end  time.Time
}

func (r *expiringHelmRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	result, err := r.base.Run(ctx, name, args...)
	if name == "helm" && len(args) > 0 && args[0] == "template" {
		*r.now = r.end
	}
	return result, err
}
func TestHelmRechecksExpiryAfterRenderingBeforeUpgrade(t *testing.T) {
	start := time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC)
	now := start
	end := start.Add(time.Hour)
	step := controlplane.PlanStep{InstallationID: "foundation", Entity: "foundation", Product: "app:foundation", Version: "1.2.3", Namespace: "example", ConfigDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", Chart: controlplane.Artifact{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, Images: []controlplane.Artifact{{Repository: "registry.example/foundation", Tag: "1.2.3", Digest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}}}
	step = closedMaintenanceOverride(step, start, end)
	base := &helmRunner{step: step}
	runner := &expiringHelmRunner{base, &now, end}
	_, err := (HelmExecutor{Runner: runner, Now: func() time.Time { return now }}).Apply(t.Context(), controlplane.Plan{}, step)
	if err == nil || !strings.Contains(err.Error(), "maintenance window closed") {
		t.Fatal("expired rendered workload applied", err)
	}
	for _, command := range base.commands {
		if len(command) > 1 && command[0] == "helm" && command[1] == "upgrade" {
			t.Fatal("upgrade ran after expiry")
		}
	}
}
func TestHelmRefusesExpiredRestartAndUninstall(t *testing.T) {
	now := time.Date(2026, 8, 4, 11, 0, 0, 0, time.UTC)
	for _, action := range []string{"restart", "uninstall"} {
		t.Run(action, func(t *testing.T) {
			step := closedMaintenanceOverride(controlplane.PlanStep{InstallationID: "foundation", Action: action}, now.Add(-time.Hour), now)
			runner := &helmRunner{step: step, installed: true}
			if _, err := (HelmExecutor{Runner: runner, Now: func() time.Time { return now }}).Apply(t.Context(), controlplane.Plan{}, step); err == nil {
				t.Fatal("expired mutation admitted")
			}
			if len(runner.commands) != 0 {
				t.Fatal("mutation attempted", runner.commands)
			}
		})
	}
}
