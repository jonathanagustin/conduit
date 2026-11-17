package agent

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// recordingExecutor remembers exactly what it was handed and answers with
// values that are its own, so a test can tell which executor ran and whether
// anything was altered on the way in or out.
type recordingExecutor struct {
	name      string
	applied   []controlplane.PlanStep
	plans     []controlplane.Plan
	observed  []controlplane.PlanStep
	rolledOut []Receipt
	err       error
}

func (executor *recordingExecutor) Apply(_ context.Context, plan controlplane.Plan, step controlplane.PlanStep) (Receipt, error) {
	executor.plans = append(executor.plans, plan)
	executor.applied = append(executor.applied, step)
	return Receipt{Step: step, PreviousRevision: 7, Fence: ReleaseFence{Name: executor.name}}, executor.err
}

func (executor *recordingExecutor) Observe(_ context.Context, step controlplane.PlanStep) (controlplane.ReportedEntity, error) {
	executor.observed = append(executor.observed, step)
	return controlplane.ReportedEntity{InstallationID: step.InstallationID, Product: executor.name}, executor.err
}

func (executor *recordingExecutor) Rollback(_ context.Context, receipt Receipt) error {
	executor.rolledOut = append(executor.rolledOut, receipt)
	return executor.err
}

func helmStep() controlplane.PlanStep {
	return controlplane.PlanStep{
		InstallationID: "inst-helm", Namespace: "web", Values: []byte(`{"replicas":2}`),
		Chart: controlplane.Artifact{Repository: "registry.example/web", Tag: "1.0.0", Digest: "sha256:" + strings.Repeat("a", 64)},
	}
}

func packageStep(kind string) controlplane.PlanStep {
	return controlplane.PlanStep{
		InstallationID: "inst-pkg", Namespace: "excalidraw",
		Chart: controlplane.Artifact{
			Kind: kind, Repository: "registry.example/packages/excalidraw", Digest: "sha256:" + strings.Repeat("c", 64),
			Architectures: []string{"amd64"}, PackageName: "excalidraw",
		},
	}
}

func TestKindExecutorRoutesEachStepByItsArtifactKind(t *testing.T) {
	t.Parallel()
	helm, pkg := &recordingExecutor{name: "helm"}, &recordingExecutor{name: "package"}
	dispatch := KindExecutor{Helm: helm, Package: pkg}
	plan := controlplane.Plan{Environment: "prod", Generation: "g1"}

	explicit := helmStep()
	explicit.Chart.Kind = controlplane.ArtifactKindHelmChart
	routes := []struct {
		step controlplane.PlanStep
		want *recordingExecutor
	}{
		{helmStep(), helm},
		{explicit, helm},
		{controlplane.PlanStep{InstallationID: "inst-old", Action: controlplane.PlanActionUnmanage}, helm},
		{packageStep(controlplane.ArtifactKindZarfPackage), pkg},
		{packageStep(controlplane.ArtifactKindUDSBundle), pkg},
	}
	for _, route := range routes {
		receipt, err := dispatch.Apply(context.Background(), plan, route.step)
		if err != nil || receipt.Fence.Name != route.want.name {
			t.Fatalf("Apply %s went to %q (%v), want %s", route.step.InstallationID, receipt.Fence.Name, err, route.want.name)
		}
		entity, err := dispatch.Observe(context.Background(), route.step)
		if err != nil || entity.Product != route.want.name {
			t.Fatalf("Observe %s went to %q (%v), want %s", route.step.InstallationID, entity.Product, err, route.want.name)
		}
		before := len(route.want.rolledOut)
		if err := dispatch.Rollback(context.Background(), receipt); err != nil || len(route.want.rolledOut) != before+1 {
			t.Fatalf("Rollback %s did not return to %s (%v)", route.step.InstallationID, route.want.name, err)
		}
	}
	if len(helm.applied) != 3 || len(pkg.applied) != 2 {
		t.Fatalf("helm ran %d steps and package ran %d", len(helm.applied), len(pkg.applied))
	}
}

// The Helm path is a pass-through: the executor is handed the very plan and
// step the worker supplied, and the worker gets back the very receipt, entity
// and error the executor returned. Nothing on the way is rewritten.
func TestKindExecutorPassesAHelmStepThroughUnchanged(t *testing.T) {
	t.Parallel()
	failure := errors.New("helm said no")
	helm := &recordingExecutor{name: "helm", err: failure}
	dispatch := KindExecutor{Helm: helm}
	plan, step := controlplane.Plan{Environment: "prod", Generation: "g1"}, helmStep()

	receipt, err := dispatch.Apply(context.Background(), plan, step)
	direct, directErr := (&recordingExecutor{name: "helm", err: failure}).Apply(context.Background(), plan, step)
	if !reflect.DeepEqual(receipt, direct) || err != directErr {
		t.Fatalf("Apply through the dispatcher = %#v, %v; direct = %#v, %v", receipt, err, direct, directErr)
	}
	if !reflect.DeepEqual(helm.applied, []controlplane.PlanStep{step}) || !reflect.DeepEqual(helm.plans, []controlplane.Plan{plan}) {
		t.Fatalf("Helm was handed %#v / %#v", helm.plans, helm.applied)
	}
	entity, err := dispatch.Observe(context.Background(), step)
	if entity.InstallationID != step.InstallationID || err != failure || !reflect.DeepEqual(helm.observed, []controlplane.PlanStep{step}) {
		t.Fatalf("Observe = %#v, %v", entity, err)
	}
	if err := dispatch.Rollback(context.Background(), receipt); err != failure || !reflect.DeepEqual(helm.rolledOut, []Receipt{receipt}) {
		t.Fatalf("Rollback = %v, handed %#v", err, helm.rolledOut)
	}
}

// A Spoke that was never configured for Zarf must not crash on a package step,
// must not hand it to Helm, and must say what is missing.
func TestKindExecutorBlocksAPackageStepWhenUnconfigured(t *testing.T) {
	t.Parallel()
	helm := &recordingExecutor{name: "helm"}
	dispatch := KindExecutor{Helm: helm, PackageHint: "set PACKAGE_EXECUTOR_ENABLED=true"}
	for _, kind := range []string{controlplane.ArtifactKindZarfPackage, controlplane.ArtifactKindUDSBundle} {
		step := packageStep(kind)
		_, applyErr := dispatch.Apply(context.Background(), controlplane.Plan{}, step)
		_, observeErr := dispatch.Observe(context.Background(), step)
		rollbackErr := dispatch.Rollback(context.Background(), Receipt{Step: step})
		for call, err := range map[string]error{"Apply": applyErr, "Observe": observeErr, "Rollback": rollbackErr} {
			var diagnostic DiagnosticError
			if !errors.Is(err, ErrExecutorBlocked) || !errors.As(err, &diagnostic) {
				t.Fatalf("%s of a %s = %v, want a blocked diagnostic", call, kind, err)
			}
			if len(diagnostic.Resources) != 1 || diagnostic.Resources[0].Reason != "Blocked" ||
				diagnostic.Resources[0].Name != step.InstallationID ||
				!strings.Contains(err.Error(), kind) || !strings.Contains(err.Error(), "PACKAGE_EXECUTOR_ENABLED") {
				t.Fatalf("%s diagnostic = %v / %#v", call, err, diagnostic.Resources)
			}
		}
	}
	if len(helm.applied)+len(helm.observed)+len(helm.rolledOut) != 0 {
		t.Fatalf("a package step reached Helm: %#v", helm)
	}

	unknown := packageStep("oci-thing")
	if _, err := (KindExecutor{Helm: helm, Package: helm}).Apply(context.Background(), controlplane.Plan{}, unknown); !errors.Is(err, ErrExecutorBlocked) {
		t.Fatalf("an unknown artifact kind was executed: %v", err)
	}
}

// End to end through the worker: a blocked package step fails the Plan with
// the diagnostic attached to its task, and the Helm step before it is rolled
// back by the executor that applied it.
func TestWorkerReportsABlockedPackageStepAndRollsBackHelm(t *testing.T) {
	t.Parallel()
	helm := &recordingExecutor{name: "helm"}
	controlPlane := &fakeControlPlane{record: controlplane.PlanRecord{
		ID: "plan-mixed", Lease: &controlplane.PlanLease{Token: "lease-token"},
		Plan: controlplane.Plan{Environment: "prod", Steps: []controlplane.PlanStep{
			helmStep(), packageStep(controlplane.ArtifactKindZarfPackage),
		}},
	}}
	worker := Worker{
		ControlPlane: controlPlane, Environment: "prod", AgentID: "agent-1",
		Executor: KindExecutor{Helm: helm, PackageHint: "set PACKAGE_EXECUTOR_ENABLED=true"},
	}
	executed, err := worker.RunOnce(context.Background())
	if !executed || !errors.Is(err, ErrExecutorBlocked) {
		t.Fatalf("RunOnce = %t, %v", executed, err)
	}
	if !controlPlane.finished || controlPlane.succeeded || !strings.Contains(controlPlane.failure, "PACKAGE_EXECUTOR_ENABLED") {
		t.Fatalf("finish: finished=%t succeeded=%t failure=%q", controlPlane.finished, controlPlane.succeeded, controlPlane.failure)
	}
	if len(helm.applied) != 1 || len(helm.rolledOut) != 1 || helm.rolledOut[0].Step.InstallationID != "inst-helm" {
		t.Fatalf("helm applied %d and rolled back %#v", len(helm.applied), helm.rolledOut)
	}
	var blockedTask *controlplane.PlanExecutionTask
	for index, task := range controlPlane.execution.Tasks {
		if task.InstallationID == "inst-pkg" && task.Status == controlplane.ExecutionTaskFailed {
			blockedTask = &controlPlane.execution.Tasks[index]
		}
	}
	if blockedTask == nil || len(blockedTask.Resources) != 1 || blockedTask.Resources[0].Reason != "Blocked" {
		t.Fatalf("the blocked step's diagnostic did not reach the execution record: %#v", controlPlane.execution.Tasks)
	}
}
