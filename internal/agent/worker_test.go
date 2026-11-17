package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type fakeControlPlane struct {
	record    controlplane.PlanRecord
	started   bool
	finished  bool
	succeeded bool
	failure   string
	execution controlplane.PlanExecution
	reported  []controlplane.ReportedEntity
	secrets   []controlplane.ReportedSecret
	finishErr error
	// leasedFor records the duration the Worker asked the control plane to
	// hold the lease for, which is otherwise invisible to a caller.
	leasedFor time.Duration
	// renewErr fails the heartbeat, which is how a plan is revoked underneath
	// a running Agent. renewed is closed on the first heartbeat so a test can
	// hold the plan open until one has actually fired.
	renewErr  error
	renewed   chan struct{}
	renewOnce sync.Once
	// finishBudget records how long the final report was given. It runs on a
	// context detached from the plan's, so nothing else can observe it.
	finishBudget time.Duration
}

func (fake *fakeControlPlane) Lease(_ context.Context, _, _ string, duration time.Duration) (controlplane.PlanRecord, error) {
	fake.leasedFor = duration
	return fake.record, nil
}
func (fake *fakeControlPlane) Start(context.Context, string, string, string, string) error {
	fake.started = true
	return nil
}
func (fake *fakeControlPlane) Renew(context.Context, string, string, string, string, time.Duration) error {
	if fake.renewed != nil {
		fake.renewOnce.Do(func() { close(fake.renewed) })
	}
	return fake.renewErr
}
func (fake *fakeControlPlane) Finish(
	ctx context.Context, _ string, _ string, _ string, _ string,
	succeeded bool, failure string, _ string, execution controlplane.PlanExecution, report controlplane.EnvironmentReport,
) error {
	// A real Finish is an HTTP call and fails immediately on a context that is
	// already done. Without that, a report running on the plan's own cancelled
	// context is indistinguishable from one correctly detached from it.
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok {
		fake.finishBudget = time.Until(deadline).Round(time.Second)
	}
	fake.finished, fake.succeeded, fake.failure = true, succeeded, failure
	fake.execution = execution
	fake.reported = append([]controlplane.ReportedEntity(nil), report.Entities...)
	fake.secrets = append([]controlplane.ReportedSecret(nil), report.Secrets...)
	return fake.finishErr
}

type fakeMaterials struct {
	applied    []controlplane.SecretMutation
	rolledBack []controlplane.SecretMutation
	failAction string
	// failNamespace fails one mutation rather than every mutation of a kind,
	// so a partial application can be observed rolling back.
	failNamespace string
	// rollbackErr makes the rollback itself fail, which is the only way to
	// reach the reporting that combines both failures.
	rollbackErr error
}

func (fake *fakeMaterials) ApplySecretMutation(_ context.Context, mutation controlplane.SecretMutation) error {
	if mutation.Action == fake.failAction || (fake.failNamespace != "" && mutation.Namespace == fake.failNamespace) {
		return errors.New("simulated material failure")
	}
	fake.applied = append(fake.applied, mutation)
	return nil
}

func (fake *fakeMaterials) RollbackSecretMutation(ctx context.Context, mutation controlplane.SecretMutation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fake.rolledBack = append(fake.rolledBack, mutation)
	return fake.rollbackErr
}

type fakeExecutor struct {
	// blockUntil holds Apply open so a test can let the lease heartbeat fire
	// while a plan is still running.
	blockUntil chan struct{}
	// rollbackErr fails the step rollback, which is the only way to reach the
	// reporting that names which Installations could not be unwound.
	rollbackErr   error
	failAt        string
	failOutput    string
	failResources []controlplane.ResourceDiagnostic
	applied       []string
	rolledBack    []string
}

func (fake *fakeExecutor) Apply(
	_ context.Context, _ controlplane.Plan, step controlplane.PlanStep,
) (Receipt, error) {
	if fake.blockUntil != nil {
		<-fake.blockUntil
	}
	if step.InstallationID == fake.failAt {
		return Receipt{}, DiagnosticError{Err: errors.New("simulated apply failure"), Output: fake.failOutput, Resources: fake.failResources}
	}
	fake.applied = append(fake.applied, step.InstallationID)
	return Receipt{Step: step, PreviousRevision: len(fake.applied)}, nil
}
func (fake *fakeExecutor) Observe(_ context.Context, step controlplane.PlanStep) (controlplane.ReportedEntity, error) {
	return controlplane.ReportedEntity{
		InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
		ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
		Health: controlplane.HealthHealthy, RolloutStatus: controlplane.RolloutUpToDate,
		ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleInstalled,
	}, nil
}
func (fake *fakeExecutor) Rollback(_ context.Context, receipt Receipt) error {
	fake.rolledBack = append(fake.rolledBack, receipt.Step.InstallationID)
	return fake.rollbackErr
}

func TestWorkerExecutesAndReportsExactPlan(t *testing.T) {
	control := &fakeControlPlane{record: leasedRecord()}
	executor := &fakeExecutor{}
	worker := Worker{ControlPlane: control, Executor: executor, Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute}
	executed, err := worker.RunOnce(context.Background())
	if err != nil || !executed {
		t.Fatalf("RunOnce() = %v, %v", executed, err)
	}
	if !control.started || !control.finished || !control.succeeded {
		t.Fatalf("unexpected lifecycle: %#v", control)
	}
	if len(control.reported) != 2 || control.reported[1].InstallationID != "workspace" {
		t.Fatalf("unexpected report: %#v", control.reported)
	}
	if len(control.execution.Tasks) != 5 || control.execution.Tasks[0].Phase != controlplane.ExecutionPhaseValidate ||
		control.execution.Tasks[4].Phase != controlplane.ExecutionPhaseObserve {
		t.Fatalf("execution diagnostics = %#v", control.execution)
	}
}

func TestWorkerObservesUnmanagedEntityWithoutApplyingOrRollingBack(t *testing.T) {
	record := leasedRecord()
	record.Plan.Steps = record.Plan.Steps[:1]
	record.Plan.Steps[0].Action = controlplane.PlanActionUnmanage
	control := &fakeControlPlane{record: record}
	executor := &fakeExecutor{}
	worker := Worker{ControlPlane: control, Executor: executor, Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute}
	executed, err := worker.RunOnce(context.Background())
	if err != nil || !executed || !control.succeeded {
		t.Fatalf("unmanage RunOnce = %v, %v, success=%v", executed, err, control.succeeded)
	}
	if len(executor.applied) != 0 || len(executor.rolledBack) != 0 {
		t.Fatalf("handoff mutated resources: applied=%#v rolledBack=%#v", executor.applied, executor.rolledBack)
	}
	if len(control.reported) != 1 || control.reported[0].Lifecycle != controlplane.LifecycleInstalled {
		t.Fatalf("handoff report = %#v", control.reported)
	}
}

func TestWorkerReportsValidationForNoOpPlan(t *testing.T) {
	record := leasedRecord()
	record.Plan.Steps = nil
	control := &fakeControlPlane{record: record}
	worker := Worker{ControlPlane: control, Executor: &fakeExecutor{}, Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute}
	executed, err := worker.RunOnce(context.Background())
	if err != nil || !executed || !control.succeeded {
		t.Fatalf("no-op execution executed=%v succeeded=%v err=%v", executed, control.succeeded, err)
	}
	if len(control.execution.Tasks) != 1 || control.execution.Tasks[0].Phase != controlplane.ExecutionPhaseValidate || control.execution.Tasks[0].Status != controlplane.ExecutionTaskSucceeded {
		t.Fatalf("no-op diagnostics = %#v", control.execution)
	}
}

func TestWorkerRollsBackCompletedStepsInReverse(t *testing.T) {
	control := &fakeControlPlane{record: leasedRecord()}
	executor := &fakeExecutor{failAt: "workspace", failOutput: "rollout rejected token=very-secret", failResources: []controlplane.ResourceDiagnostic{{
		APIVersion: "apps/v1", Kind: "Deployment", Namespace: "example", Name: "workspace", Reason: "ProgressDeadlineExceeded",
	}}}
	worker := Worker{ControlPlane: control, Executor: executor, Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute}
	executed, err := worker.RunOnce(context.Background())
	if err == nil || !executed {
		t.Fatalf("RunOnce() = %v, %v", executed, err)
	}
	if !control.finished || control.succeeded || control.failure == "" {
		t.Fatalf("failed plan was not recorded: %#v", control)
	}
	if len(executor.rolledBack) != 1 || executor.rolledBack[0] != "api" {
		t.Fatalf("rollback order = %#v", executor.rolledBack)
	}
	if len(control.execution.Tasks) != 5 || control.execution.Tasks[3].Status != controlplane.ExecutionTaskFailed ||
		control.execution.Tasks[4].Status != controlplane.ExecutionTaskRolledBack {
		t.Fatalf("failure diagnostics = %#v", control.execution.Tasks)
	}
	failedTask := control.execution.Tasks[3]
	if failedTask.AgentOutput != "rollout rejected token=[REDACTED]" || len(failedTask.Resources) != 1 || failedTask.Resources[0].Name != "workspace" {
		t.Fatalf("bounded failure evidence = %#v", failedTask)
	}
	if len(control.reported) != 0 {
		t.Fatalf("rolled-back state must not be reported as current: %#v", control.reported)
	}
}

func TestWorkerRollsBackWhenCancellationWinsFinishRace(t *testing.T) {
	control := &fakeControlPlane{record: leasedRecord(), finishErr: ErrPlanCancelled}
	executor := &fakeExecutor{}
	worker := Worker{ControlPlane: control, Executor: executor, Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute}
	executed, err := worker.RunOnce(context.Background())
	if err != nil || !executed {
		t.Fatalf("RunOnce() = %v, %v", executed, err)
	}
	if len(executor.rolledBack) != 2 || executor.rolledBack[0] != "workspace" || executor.rolledBack[1] != "api" {
		t.Fatalf("cancellation rollback order = %#v", executor.rolledBack)
	}
}

func TestWorkerExecutesReportsAndRollsBackSecretMutations(t *testing.T) {
	record := leasedRecord()
	access := controlplane.SecretAccess{
		Assignment: "database", Revision: 2, ProviderRef: "vault://database",
		KubernetesSecretName: "database", Keys: []string{"password"},
	}
	record.Plan.SecretMutations = []controlplane.SecretMutation{{
		Position: 1, Action: controlplane.SecretCreate, Namespace: "example", Current: &access,
	}}
	control := &fakeControlPlane{record: record}
	materials := &fakeMaterials{}
	worker := Worker{
		ControlPlane: control, Executor: &fakeExecutor{}, Materials: materials,
		Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	if executed, err := worker.RunOnce(context.Background()); err != nil || !executed {
		t.Fatalf("RunOnce() = %v, %v", executed, err)
	}
	if len(materials.applied) != 1 || len(control.secrets) != 1 || control.secrets[0].Namespace != "example" {
		t.Fatalf("material execution = %#v report = %#v", materials.applied, control.secrets)
	}

	control = &fakeControlPlane{record: record}
	materials = &fakeMaterials{}
	deleteAccess := access
	deleteAccess.Assignment = "retired"
	record.Plan.SecretMutations = append(record.Plan.SecretMutations, controlplane.SecretMutation{
		Position: 2, Action: controlplane.SecretDelete, Namespace: "example", Previous: &deleteAccess,
	})
	control.record = record
	worker = Worker{
		ControlPlane: control, Executor: &fakeExecutor{failAt: "workspace"}, Materials: materials,
		Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("workload failure unexpectedly succeeded")
	}
	if len(materials.applied) != 1 || materials.applied[0].Action != controlplane.SecretCreate {
		t.Fatalf("secret deletion ran before workload completion: %#v", materials.applied)
	}
	if len(materials.rolledBack) != 1 || materials.rolledBack[0].Action != controlplane.SecretCreate {
		t.Fatalf("secret rollback = %#v", materials.rolledBack)
	}
}

func TestWorkerRejectsSecretAccessWithoutLifecycleMutation(t *testing.T) {
	record := leasedRecord()
	record.Plan.Steps[0].SecretAccess = []controlplane.SecretAccess{{Assignment: "database", Revision: 1}}
	control := &fakeControlPlane{record: record}
	worker := Worker{
		ControlPlane: control, Executor: &fakeExecutor{}, Materials: &fakeMaterials{},
		Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("uncovered secret access unexpectedly executed")
	}
}

func leasedRecord() controlplane.PlanRecord {
	steps := []controlplane.PlanStep{
		{Position: 1, InstallationID: "api", Entity: "api", Product: "app:api", Version: "1.0.0", Namespace: "example", ConfigDigest: "sha256:config-a"},
		{Position: 2, InstallationID: "workspace", Entity: "workspace", Product: "app:workspace", Version: "1.0.0", Namespace: "example", ConfigDigest: "sha256:config-b", DependsOn: []string{"api"}},
	}
	return controlplane.PlanRecord{
		ID: "pln_test", Plan: controlplane.Plan{Environment: "dev", Generation: "abc", Steps: steps},
		Status: controlplane.PlanLeased,
		Lease:  &controlplane.PlanLease{AgentID: "agent-dev", Token: "opaque", ExpiresAt: time.Now().Add(time.Minute)},
	}
}

// The lease duration is what the Hub uses to decide the Agent has died. Too
// short and a healthy Agent loses its plan mid-apply; too long and a crashed
// one blocks the Environment until the lease expires. Both ends of the
// accepted range are inclusive, and an unset duration takes two minutes.
func TestWorkerBoundsTheLeaseDuration(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		configured time.Duration
		wantLease  time.Duration
		wantError  bool
	}{
		"unset takes two minutes": {configured: 0, wantLease: 2 * time.Minute},
		"the floor is accepted":   {configured: 10 * time.Second, wantLease: 10 * time.Second},
		"the ceiling is accepted": {configured: 15 * time.Minute, wantLease: 15 * time.Minute},
		"below the floor":         {configured: 9 * time.Second, wantError: true},
		"above the ceiling":       {configured: 15*time.Minute + time.Second, wantError: true},
		"negative":                {configured: -time.Second, wantError: true},
	} {
		control := &fakeControlPlane{record: leasedRecord()}
		worker := Worker{
			ControlPlane: control, Executor: &fakeExecutor{},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: test.configured,
		}
		_, err := worker.RunOnce(context.Background())
		if (err != nil) != test.wantError {
			t.Errorf("%s: error = %v, want error %v", name, err, test.wantError)
			continue
		}
		if test.wantError {
			if !strings.Contains(err.Error(), "lease duration") {
				t.Errorf("%s: error %q does not name the lease duration", name, err)
			}
			continue
		}
		if control.leasedFor != test.wantLease {
			t.Errorf("%s: leased for %v, want %v", name, control.leasedFor, test.wantLease)
		}
	}
}

// Secret deletions applied after a plan's steps are rolled back in reverse
// order when a later one fails. Order matters because these mutations are not
// independent: a namespace's access can depend on one created earlier, so
// undoing them forwards can remove what a later undo still needs. Nothing
// asserted the direction, only that a rollback happened.
func TestWorkerRollsBackSecretDeletionsInReverse(t *testing.T) {
	t.Parallel()
	record := leasedRecord()
	record.Plan.SecretMutations = []controlplane.SecretMutation{
		{Position: 1, Action: controlplane.SecretDelete, Namespace: "ns-a",
			Previous: &controlplane.SecretAccess{Assignment: "assignment-a"}},
		{Position: 2, Action: controlplane.SecretDelete, Namespace: "ns-b",
			Previous: &controlplane.SecretAccess{Assignment: "assignment-b"}},
		{Position: 3, Action: controlplane.SecretDelete, Namespace: "ns-c",
			Previous: &controlplane.SecretAccess{Assignment: "assignment-c"}},
	}
	materials := &fakeMaterials{failNamespace: "ns-c"}
	worker := Worker{
		ControlPlane: &fakeControlPlane{record: record}, Executor: &fakeExecutor{},
		Materials: materials, Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("a failing secret deletion was reported as success")
	}

	var applied, rolledBack []string
	for _, mutation := range materials.applied {
		applied = append(applied, mutation.Namespace)
	}
	for _, mutation := range materials.rolledBack {
		rolledBack = append(rolledBack, mutation.Namespace)
	}
	if strings.Join(applied, ",") != "ns-a,ns-b" {
		t.Fatalf("applied = %v, want ns-a then ns-b before the failure", applied)
	}
	// Reverse of the order they were applied in, not the order they appear in
	// the plan.
	if strings.Join(rolledBack, ",") != "ns-b,ns-a" {
		t.Errorf("rolled back = %v, want ns-b then ns-a", rolledBack)
	}
}

// When a secret mutation fails, the reported failure names the assignment it
// was for. The name comes from the mutation's current access, falling back to
// its previous one, and only then to "unknown" -- a delete has no current
// access, so without the fallback every failed deletion is reported against
// an assignment nobody can identify.
func TestWorkerNamesTheAssignmentAFailedMutationWasFor(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		mutation controlplane.SecretMutation
		want     string
	}{
		"current access names it": {
			mutation: controlplane.SecretMutation{Position: 1, Action: controlplane.SecretCreate, Namespace: "ns-a",
				Current: &controlplane.SecretAccess{Assignment: "from-current"}},
			want: "from-current",
		},
		"previous access is the fallback": {
			mutation: controlplane.SecretMutation{Position: 1, Action: controlplane.SecretCreate, Namespace: "ns-a",
				Previous: &controlplane.SecretAccess{Assignment: "from-previous"}},
			want: "from-previous",
		},
		"neither is reported as unknown": {
			mutation: controlplane.SecretMutation{Position: 1, Action: controlplane.SecretCreate, Namespace: "ns-a"},
			want:     "unknown",
		},
	} {
		record := leasedRecord()
		record.Plan.SecretMutations = []controlplane.SecretMutation{test.mutation}
		worker := Worker{
			ControlPlane: &fakeControlPlane{record: record}, Executor: &fakeExecutor{},
			Materials:   &fakeMaterials{failNamespace: "ns-a"},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
		}
		_, err := worker.RunOnce(context.Background())
		if err == nil {
			t.Errorf("%s: a failing mutation was reported as success", name)
			continue
		}
		if !strings.Contains(err.Error(), test.want) {
			t.Errorf("%s: error %q does not name %q", name, err, test.want)
		}
	}
}

// A rollback that itself fails must not replace the original failure. An
// operator reading only "rollback: ..." cannot tell what went wrong first, and
// the original error is the one that explains why a rollback was attempted.
func TestWorkerReportsBothTheFailureAndAFailedRollback(t *testing.T) {
	t.Parallel()
	record := leasedRecord()
	record.Plan.SecretMutations = []controlplane.SecretMutation{
		{Position: 1, Action: controlplane.SecretCreate, Namespace: "ns-a",
			Current: &controlplane.SecretAccess{Assignment: "assignment-a"}},
		{Position: 2, Action: controlplane.SecretCreate, Namespace: "ns-b",
			Current: &controlplane.SecretAccess{Assignment: "assignment-b"}},
	}
	control := &fakeControlPlane{record: record}
	worker := Worker{
		ControlPlane: control, Executor: &fakeExecutor{},
		Materials: &fakeMaterials{
			failNamespace: "ns-b",
			rollbackErr:   errors.New("secret store unreachable"),
		},
		Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("a failing plan was reported as success")
	}
	if control.failure == "" {
		t.Fatal("no failure was recorded with the Hub")
	}
	// Both, in that order: what failed, then what could not be undone.
	if !strings.Contains(control.failure, "assignment-b") {
		t.Errorf("recorded failure %q omits the original failure", control.failure)
	}
	if !strings.Contains(control.failure, "rollback: ") ||
		!strings.Contains(control.failure, "secret store unreachable") {
		t.Errorf("recorded failure %q omits the failed rollback", control.failure)
	}
}

// A plan revoked underneath a running Agent is not a failure to report back:
// the Hub already knows, and the lease this Agent would report under is gone.
// It still rolls back, and if that rollback fails the Agent must say so --
// otherwise a cancelled plan that left half its secrets in place looks
// identical to one that unwound cleanly.
func TestWorkerReportsARollbackThatFailedUnderACancelledPlan(t *testing.T) {
	t.Parallel()
	record := leasedRecord()
	record.Plan.SecretMutations = []controlplane.SecretMutation{
		{Position: 1, Action: controlplane.SecretCreate, Namespace: "ns-a",
			Current: &controlplane.SecretAccess{Assignment: "assignment-a"}},
	}

	t.Run("a clean rollback under cancellation is not an error", func(t *testing.T) {
		t.Parallel()
		control := &fakeControlPlane{record: record, renewErr: ErrPlanCancelled}
		worker := Worker{
			ControlPlane: control, Executor: &fakeExecutor{}, Materials: &fakeMaterials{},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: 10 * time.Second,
		}
		executed, err := worker.RunOnce(context.Background())
		if !executed {
			t.Error("a cancelled plan was reported as not executed")
		}
		if err != nil {
			t.Errorf("a cancelled plan with a clean rollback returned %v", err)
		}
	})

	t.Run("a heartbeat failure that is not cancellation still fails the plan", func(t *testing.T) {
		t.Parallel()
		// A lease that cannot be renewed is not a revocation: the Hub may
		// simply be unreachable. The plan still fails, because continuing to
		// apply changes on a lease that may have expired is the thing the
		// heartbeat exists to prevent -- and reporting success here would
		// record a plan as applied on an authority that had lapsed.
		renewed := make(chan struct{})
		control := &fakeControlPlane{record: record, renewErr: errors.New("hub unreachable"), renewed: renewed}
		blocked := make(chan struct{})
		worker := Worker{
			ControlPlane: control, Executor: &fakeExecutor{blockUntil: blocked}, Materials: &fakeMaterials{},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: 10 * time.Second,
		}
		// Bounded rather than a bare receive. A mutation that stops the
		// heartbeat firing would otherwise leave this goroutine blocked, the
		// executor never released and RunOnce never returning -- so the test
		// hangs instead of failing, and a mutation run waits on it forever.
		go func() {
			select {
			case <-renewed:
			case <-time.After(30 * time.Second):
			}
			close(blocked)
		}()
		_, err := worker.RunOnce(context.Background())
		if err == nil {
			t.Fatal("a plan whose lease could not be renewed was reported as clean")
		}
		if !strings.Contains(err.Error(), "renew plan lease") ||
			!strings.Contains(err.Error(), "hub unreachable") {
			t.Errorf("error %q does not identify the failed renewal", err)
		}
	})

	t.Run("a failed rollback under cancellation is reported", func(t *testing.T) {
		t.Parallel()
		// The heartbeat fires at a third of the lease, so this waits out one
		// tick at the ten-second minimum. That wall time is the only way to
		// reach a revocation arriving mid-plan; the ticker has no injection
		// point.
		renewed := make(chan struct{})
		control := &fakeControlPlane{record: record, renewErr: ErrPlanCancelled, renewed: renewed}
		blocked := make(chan struct{})
		executor := &fakeExecutor{blockUntil: blocked}
		worker := Worker{
			ControlPlane: control, Executor: executor,
			Materials:   &fakeMaterials{rollbackErr: errors.New("secret store unreachable")},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: 10 * time.Second,
		}
		started := time.Now()
		// Bounded rather than a bare receive. A mutation that stops the
		// heartbeat firing would otherwise leave this goroutine blocked, the
		// executor never released and RunOnce never returning -- so the test
		// hangs instead of failing, and a mutation run waits on it forever.
		go func() {
			select {
			case <-renewed:
			case <-time.After(30 * time.Second):
			}
			close(blocked)
		}()
		_, err := worker.RunOnce(context.Background())
		if err == nil {
			t.Fatal("a cancelled plan whose rollback failed was reported as clean")
		}
		// The heartbeat runs at a third of the lease, so it must arrive well
		// inside it. A renewal that fires at or after the lease expires never
		// renews anything: the Hub has already reclaimed the plan.
		if elapsed := time.Since(started); elapsed >= 10*time.Second/2 {
			t.Errorf("the first heartbeat took %v, more than half the ten-second lease", elapsed)
		}
		if !strings.Contains(err.Error(), "cancelled plan rollback") ||
			!strings.Contains(err.Error(), "secret store unreachable") {
			t.Errorf("error %q does not identify a failed rollback under cancellation", err)
		}
	})
}

// The final report to the Hub runs on a context detached from the plan's, so
// that a cancelled or timed-out plan is still recorded rather than vanishing.
// Detached means unbounded unless it is given its own deadline, and an Agent
// blocked forever on its last report holds the Environment's lease until the
// Hub expires it.
func TestWorkerBoundsTheFinalReport(t *testing.T) {
	t.Parallel()

	t.Run("a succeeding plan reports within thirty seconds", func(t *testing.T) {
		t.Parallel()
		control := &fakeControlPlane{record: leasedRecord()}
		worker := Worker{
			ControlPlane: control, Executor: &fakeExecutor{},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
		}
		if _, err := worker.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		if control.finishBudget != 30*time.Second {
			t.Errorf("finish budget = %v, want 30s", control.finishBudget)
		}
	})

	t.Run("a failing plan reports within thirty seconds too", func(t *testing.T) {
		t.Parallel()
		// The failure path builds its own context, so the success path's
		// deadline says nothing about it.
		control := &fakeControlPlane{record: leasedRecord()}
		worker := Worker{
			ControlPlane: control, Executor: &fakeExecutor{failAt: "workspace"},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
		}
		if _, err := worker.RunOnce(context.Background()); err == nil {
			t.Fatal("a failing plan was reported as success")
		}
		if control.finishBudget != 30*time.Second {
			t.Errorf("finish budget = %v, want 30s", control.finishBudget)
		}
	})

	t.Run("the report survives a cancelled parent context", func(t *testing.T) {
		t.Parallel()
		// This is the whole point of detaching: the caller giving up must not
		// stop the Hub being told what happened. The plan is held open until
		// the context has actually been cancelled, so the cancellation is
		// guaranteed to precede the final report rather than racing it.
		control := &fakeControlPlane{record: leasedRecord()}
		blocked := make(chan struct{})
		worker := Worker{
			ControlPlane: control, Executor: &fakeExecutor{blockUntil: blocked},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		close(blocked)
		if _, err := worker.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
		if !control.finished {
			t.Error("the Hub was never told the plan finished; the report was not detached")
		}
	})

	t.Run("a cancelled caller still reports and does not roll back", func(t *testing.T) {
		t.Parallel()
		// The final report stays detached so the Hub hears the failure. Rollback
		// does not: the caller's cancellation is the signal that this attempt
		// must stop touching the cluster.
		record := leasedRecord()
		record.Plan.SecretMutations = []controlplane.SecretMutation{
			{Position: 1, Action: controlplane.SecretCreate, Namespace: "ns-a",
				Current: &controlplane.SecretAccess{Assignment: "assignment-a"}},
		}
		control := &fakeControlPlane{record: record}
		materials := &fakeMaterials{}
		blocked := make(chan struct{})
		worker := Worker{
			ControlPlane: control, Executor: &fakeExecutor{failAt: "workspace", blockUntil: blocked},
			Materials:   materials,
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		close(blocked)
		if _, err := worker.RunOnce(ctx); err == nil {
			t.Fatal("a failing plan was reported as success")
		}
		if !control.finished {
			t.Error("the Hub was never told the plan failed; the report was not detached")
		}
		if len(materials.rolledBack) != 0 {
			t.Errorf("rolled back %d mutations after the caller cancelled", len(materials.rolledBack))
		}
	})

	t.Run("rollback stops when the lease is no longer held", func(t *testing.T) {
		t.Parallel()
		// Renewal is checked before the first unwind. An expired lease must
		// abort that unwind even though the earlier steps already changed the
		// environment, because the Hub may already have assigned the plan.
		record := leasedRecord()
		record.Plan.SecretMutations = []controlplane.SecretMutation{
			{Position: 1, Action: controlplane.SecretCreate, Namespace: "ns-a",
				Current: &controlplane.SecretAccess{Assignment: "assignment-a"}},
			{Position: 2, Action: controlplane.SecretCreate, Namespace: "ns-b",
				Current: &controlplane.SecretAccess{Assignment: "assignment-b"}},
		}
		materials := &fakeMaterials{failNamespace: "ns-b"}
		control := &fakeControlPlane{record: record, renewErr: errors.New("lease expired")}
		worker := Worker{
			ControlPlane: control, Executor: &fakeExecutor{}, Materials: materials,
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
		}
		if _, err := worker.RunOnce(context.Background()); err == nil {
			t.Fatal("a failing plan was reported as success")
		}
		if len(materials.rolledBack) != 0 {
			t.Fatalf("rolled back %d mutations after the lease was gone", len(materials.rolledBack))
		}
		if !strings.Contains(control.failure, "lease is no longer held") {
			t.Fatalf("failure %q does not say the lease stopped the rollback", control.failure)
		}
	})
}

// A failed deletion names the assignment it was for, and a delete carries only
// a previous access -- there is nothing current about a secret being removed.
// Without the nil check this panics on a mutation whose previous access the
// Hub did not send.
func TestWorkerNamesAFailedDeletionOrSaysUnknown(t *testing.T) {
	t.Parallel()
	for name, test := range map[string]struct {
		mutation controlplane.SecretMutation
		want     string
	}{
		"a previous access names it": {
			mutation: controlplane.SecretMutation{Position: 1, Action: controlplane.SecretDelete,
				Namespace: "ns-a", Previous: &controlplane.SecretAccess{Assignment: "assignment-a"}},
			want: "assignment-a",
		},
		"no access at all is unknown": {
			mutation: controlplane.SecretMutation{Position: 1, Action: controlplane.SecretDelete, Namespace: "ns-a"},
			want:     "unknown",
		},
	} {
		record := leasedRecord()
		record.Plan.SecretMutations = []controlplane.SecretMutation{test.mutation}
		worker := Worker{
			ControlPlane: &fakeControlPlane{record: record}, Executor: &fakeExecutor{},
			Materials:   &fakeMaterials{failNamespace: "ns-a"},
			Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
		}
		_, err := worker.RunOnce(context.Background())
		if err == nil {
			t.Errorf("%s: a failing deletion was reported as success", name)
			continue
		}
		if !strings.Contains(err.Error(), "delete secret mutation "+test.want) {
			t.Errorf("%s: error %q does not name %q", name, err, test.want)
		}
	}
}

// Reporting a failed plan can itself fail, and when it does the Agent has two
// things to say: what went wrong, and that the Hub was never told. Returning
// only the reporting error loses the original cause -- and the Hub, not having
// heard, still believes the plan is running.
func TestWorkerReportsWhenItCouldNotReportAFailure(t *testing.T) {
	t.Parallel()
	control := &fakeControlPlane{record: leasedRecord(), finishErr: errors.New("hub unreachable")}
	worker := Worker{
		ControlPlane: control, Executor: &fakeExecutor{failAt: "workspace"},
		Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	_, err := worker.RunOnce(context.Background())
	if err == nil {
		t.Fatal("a plan that failed and could not be reported was returned as clean")
	}
	// The original failure first, then the reporting failure.
	if !strings.Contains(err.Error(), "simulated apply failure") {
		t.Errorf("error %q lost the original failure", err)
	}
	if !strings.Contains(err.Error(), "finish failed plan") || !strings.Contains(err.Error(), "hub unreachable") {
		t.Errorf("error %q does not say the Hub was never told", err)
	}
}

// A rollback keeps going after a failure and reports every one it hit. Both
// halves matter: stopping at the first leaves the rest of the Environment
// half-changed with nothing said about it, and reporting only one leaves an
// operator fixing a single namespace and finding the next failure by hand.
func TestWorkerReportsEveryRollbackFailureNotJustTheFirst(t *testing.T) {
	t.Parallel()
	record := leasedRecord()
	record.Plan.SecretMutations = []controlplane.SecretMutation{
		{Position: 1, Action: controlplane.SecretCreate, Namespace: "ns-a",
			Current: &controlplane.SecretAccess{Assignment: "assignment-a"}},
		{Position: 2, Action: controlplane.SecretCreate, Namespace: "ns-b",
			Current: &controlplane.SecretAccess{Assignment: "assignment-b"}},
		{Position: 3, Action: controlplane.SecretCreate, Namespace: "ns-c",
			Current: &controlplane.SecretAccess{Assignment: "assignment-c"}},
	}
	control := &fakeControlPlane{record: record}
	// ns-c fails to apply, so ns-a and ns-b are rolled back -- and both of
	// those rollbacks fail too.
	materials := &fakeMaterials{failNamespace: "ns-c", rollbackErr: errors.New("secret store unreachable")}
	worker := Worker{
		ControlPlane: control, Executor: &fakeExecutor{}, Materials: materials,
		Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("a failing plan was reported as success")
	}
	if len(materials.rolledBack) != 2 {
		t.Errorf("rolled back %d mutations, want both despite the first failing", len(materials.rolledBack))
	}
	for _, namespace := range []string{"ns-a", "ns-b"} {
		if !strings.Contains(control.failure, "secret "+namespace) {
			t.Errorf("recorded failure %q does not name %s", control.failure, namespace)
		}
	}
}

// The same for step rollbacks: every Installation that could not be unwound is
// named, not just the last one tried.
func TestWorkerReportsEveryStepRollbackFailure(t *testing.T) {
	t.Parallel()
	control := &fakeControlPlane{record: leasedRecord()}
	executor := &fakeExecutor{failAt: "workspace", rollbackErr: errors.New("helm rollback rejected")}
	worker := Worker{
		ControlPlane: control, Executor: executor,
		Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("a failing plan was reported as success")
	}
	if !strings.Contains(control.failure, "api: ") ||
		!strings.Contains(control.failure, "helm rollback rejected") {
		t.Errorf("recorded failure %q does not name the Installation that could not be rolled back", control.failure)
	}
}

// Rollback runs three separate loops -- the deletions applied after the steps,
// the steps themselves, and the mutations applied before them -- and each
// records its own failures. Covering one says nothing about the others, which
// is why this uses deletions specifically: a plan of creations never reaches
// the post-material loop at all.
func TestWorkerReportsFailuresRollingBackPostStepDeletions(t *testing.T) {
	t.Parallel()
	record := leasedRecord()
	record.Plan.SecretMutations = []controlplane.SecretMutation{
		{Position: 1, Action: controlplane.SecretDelete, Namespace: "ns-a",
			Previous: &controlplane.SecretAccess{Assignment: "assignment-a"}},
		{Position: 2, Action: controlplane.SecretDelete, Namespace: "ns-b",
			Previous: &controlplane.SecretAccess{Assignment: "assignment-b"}},
		{Position: 3, Action: controlplane.SecretDelete, Namespace: "ns-c",
			Previous: &controlplane.SecretAccess{Assignment: "assignment-c"}},
	}
	control := &fakeControlPlane{record: record}
	// ns-c fails to delete, so ns-a and ns-b are rolled back and both fail.
	materials := &fakeMaterials{failNamespace: "ns-c", rollbackErr: errors.New("secret store unreachable")}
	worker := Worker{
		ControlPlane: control, Executor: &fakeExecutor{}, Materials: materials,
		Environment: "dev", AgentID: "agent-dev", LeaseDuration: time.Minute,
	}
	if _, err := worker.RunOnce(context.Background()); err == nil {
		t.Fatal("a failing deletion was reported as success")
	}
	for _, namespace := range []string{"ns-a", "ns-b"} {
		if !strings.Contains(control.failure, "secret "+namespace) {
			t.Errorf("recorded failure %q does not name %s", control.failure, namespace)
		}
	}
}
