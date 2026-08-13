package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPlanExecutionTaskBudgetMatchesWorkerFailureEnvelope(t *testing.T) {
	plan := Plan{
		SecretMutations: []SecretMutation{
			{Action: SecretCreate},
			{Action: SecretVerify},
			{Action: SecretDelete},
		},
		Steps: []PlanStep{
			{Action: planActionReconcile},
			{Action: PlanActionUnmanage},
		},
	}
	// validate + possible lease failure + 3 mutation attempts + 2 mutation
	// rollbacks + 4 apply/observe tasks + 1 managed-step rollback.
	if got, want := planExecutionTaskUpperBound(plan), 12; got != want {
		t.Fatalf("execution task upper bound = %d, want %d", got, want)
	}
}

func TestBuildPlanRejectsAnUnreportableExecutionEnvelope(t *testing.T) {
	state := foundation()
	state.Releases = state.Releases[:1]
	state.Releases[0].Dependencies = nil
	state.Entities = nil
	for index := 0; index < 171; index++ {
		state.Entities = append(state.Entities, Entity{
			Name: fmt.Sprintf("api-%03d", index), Product: state.Releases[0].Product,
			Version: state.Releases[0].Version, Namespace: fmt.Sprintf("example-%03d", index),
		})
	}
	if _, err := BuildPlan(state); err == nil || !strings.Contains(err.Error(), "515 execution evidence tasks") {
		t.Fatalf("oversized Plan error = %v", err)
	}

	// Equality is admissible: 2 + (170 * 3) exactly consumes the durable
	// failure-evidence budget and must not be rejected as if the bound were >=.
	state.Entities = state.Entities[:170]
	if _, err := BuildPlan(state); err != nil {
		t.Fatalf("Plan at exact execution task limit was rejected: %v", err)
	}
}

func TestLeaseRejectsPersistedPlanOutsideExecutionTaskBudget(t *testing.T) {
	repository := NewMemoryRepository()
	steps := make([]PlanStep, 171)
	for index := range steps {
		steps[index] = PlanStep{
			Position: index + 1, Action: planActionReconcile,
			InstallationID: fmt.Sprintf("api-%03d", index), Namespace: fmt.Sprintf("example-%03d", index),
		}
	}
	now := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	_, err := repository.CreatePlan(context.Background(), PlanRecord{
		ID: "pln_legacy_oversized", Plan: Plan{Environment: "dev-use2", Steps: steps},
		Status: PlanApproved, CreatedAt: now, UpdatedAt: now, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle := NewLifecycle(repository)
	lifecycle.now = func() time.Time { return now }
	_, err = lifecycle.LeaseNextPlan(context.Background(), "dev-use2", "agent-dev", time.Minute)
	if !errors.Is(err, ErrLeaseRejected) || !strings.Contains(err.Error(), "515 execution evidence tasks") {
		t.Fatalf("oversized persisted Plan lease error = %v", err)
	}
}

func TestPlanExecutionDiagnosticsAreRedactedPersistedAndProjected(t *testing.T) {
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.newToken = func() (string, error) { return "lease-token", nil }
	record, err := lifecycle.CreatePlan(context.Background(), foundation())
	if err != nil {
		t.Fatal(err)
	}
	record, _ = lifecycle.ApprovePlan(context.Background(), record.ID, "reviewer", "approved")
	record, _ = lifecycle.LeaseNextPlan(context.Background(), record.Plan.Environment, "agent-dev", time.Minute)
	record, _ = lifecycle.StartPlan(context.Background(), record.ID, record.Lease.Token)
	execution := failedPlanExecution(record, "example-api")
	namespace := execution.Tasks[0].Namespace
	execution.Tasks[0].AgentOutput = "Authorization: Bearer abc123 password=hunter2\n" + strings.Repeat("x", 5000)
	execution.Tasks[0].Resources = []ResourceDiagnostic{{
		APIVersion: "apps/v1", Kind: "Deployment", Namespace: namespace, Name: "example-api",
		Reason: "ProgressDeadlineExceeded", Message: "probe failed token=abc123",
	}}
	record, err = lifecycle.FinishPlan(
		context.Background(), record.ID, record.Lease.Token, false, "rollout failed", "example-api", execution, &EnvironmentReport{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if record.Execution == nil || record.Execution.AgentID != "agent-dev" || record.Execution.Attempt != 1 {
		t.Fatalf("execution attribution = %#v", record.Execution)
	}
	encoded, _ := json.Marshal(record.Execution)
	if strings.Contains(string(encoded), "abc123") || strings.Contains(string(encoded), "hunter2") || len(record.Execution.Tasks[0].AgentOutput) > maximumDiagnosticOutput+len("…") {
		t.Fatalf("diagnostics were not bounded and redacted: %s", encoded)
	}

	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"operator"},
	})
	request := httptest.NewRequest(http.MethodGet, "/v1/plans/"+record.ID, nil)
	request.Header.Set("X-Platform-Subject", "operator")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"resources":[{"apiVersion":"apps/v1","kind":"Deployment"`) || strings.Contains(response.Body.String(), "hunter2") {
		t.Fatalf("HTTP diagnostics projection status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestPlanExecutionDiagnosticsRejectUnorderedOrUnattributedTasks(t *testing.T) {
	record := PlanRecord{Plan: Plan{Environment: "dev", Steps: []PlanStep{{InstallationID: "api", Namespace: "default"}}}}
	execution := successfulPlanExecution(record)
	execution.Tasks[0].Sequence = 2
	if _, err := normalizePlanExecution(record.Plan, *execution, true); err == nil {
		t.Fatal("out-of-order task was accepted")
	}
	execution = successfulPlanExecution(record)
	execution.Tasks[0].InstallationID = "unknown"
	if _, err := normalizePlanExecution(record.Plan, *execution, true); err == nil {
		t.Fatal("unattributed task was accepted")
	}
	execution = failedPlanExecution(record, "api")
	execution.Tasks[0].Resources = []ResourceDiagnostic{{Kind: "Deployment", Namespace: "other", Name: "api"}}
	if _, err := normalizePlanExecution(record.Plan, *execution, false); err == nil {
		t.Fatal("resource outside the task namespace was accepted")
	}
	execution = failedPlanExecution(record, "api")
	execution.Tasks[0].Status = ExecutionTaskRolledBack
	if _, err := normalizePlanExecution(record.Plan, *execution, false); err == nil {
		t.Fatal("rollback status outside the rollback phase was accepted")
	}
}

func TestPlanExecutionDiagnosticsRejectOversizedAggregate(t *testing.T) {
	record := PlanRecord{Plan: Plan{Environment: "dev", Steps: []PlanStep{{InstallationID: "api", Namespace: "default"}}}}
	startedAt := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	execution := PlanExecution{StartedAt: startedAt}
	for index := 0; index < 300; index++ {
		status := ExecutionTaskSucceeded
		failure := ""
		if index == 299 {
			status = ExecutionTaskFailed
			failure = "failed"
		}
		taskStartedAt := startedAt.Add(time.Duration(index) * time.Second)
		execution.Tasks = append(execution.Tasks, PlanExecutionTask{
			Sequence: index + 1, Phase: ExecutionPhaseApply, Status: status,
			InstallationID: "api", Namespace: "default", Failure: failure,
			StartedAt: taskStartedAt, FinishedAt: taskStartedAt.Add(time.Second),
			Resources: []ResourceDiagnostic{{Kind: "Deployment", Namespace: "default", Name: "api", Message: strings.Repeat("x", maximumResourceMessage)}},
		})
	}
	execution.FinishedAt = startedAt.Add(301 * time.Second)
	if _, err := normalizePlanExecution(record.Plan, execution, false); err == nil {
		t.Fatal("oversized aggregate diagnostics were accepted")
	}
}

func TestPlanExecutionDiagnosticsAcceptOlderAgentAsLegacyTask(t *testing.T) {
	repository := NewMemoryRepository()
	lifecycle := NewLifecycle(repository)
	lifecycle.newToken = func() (string, error) { return "lease-token", nil }
	record, _ := lifecycle.CreatePlan(context.Background(), foundation())
	record, _ = lifecycle.ApprovePlan(context.Background(), record.ID, "reviewer", "approved")
	record, _ = lifecycle.LeaseNextPlan(context.Background(), record.Plan.Environment, "old-agent", time.Minute)
	record, _ = lifecycle.StartPlan(context.Background(), record.ID, record.Lease.Token)
	record, err := lifecycle.FinishPlan(
		context.Background(), record.ID, record.Lease.Token, false, "legacy failure", "example-api", nil, &EnvironmentReport{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if record.Execution == nil || len(record.Execution.Tasks) != 1 || record.Execution.Tasks[0].Phase != ExecutionPhaseLegacy || record.Execution.AgentID != "old-agent" {
		t.Fatalf("legacy execution = %#v", record.Execution)
	}
}

// executionOfTasks builds a valid PlanExecution whose only interesting property
// is the one the caller sets. Every bound below is a number an Agent can push a
// report against, so each is asserted at the value that must be accepted as well
// as the one that must not -- a bound tested only from the outside is satisfied
// by refusing everything.
func executionOfTasks(namespace string, tasks []PlanExecutionTask) (Plan, PlanExecution) {
	startedAt := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	for index := range tasks {
		tasks[index].Sequence = index + 1
		tasks[index].Phase = ExecutionPhaseApply
		tasks[index].Status = ExecutionTaskSucceeded
		tasks[index].InstallationID = "api"
		tasks[index].StartedAt = startedAt.Add(time.Duration(index) * time.Second)
		tasks[index].FinishedAt = startedAt.Add(time.Duration(index+1) * time.Second)
		if tasks[index].Namespace == "" {
			tasks[index].Namespace = namespace
		}
	}
	plan := Plan{Environment: "dev", Steps: []PlanStep{{InstallationID: "api", Namespace: namespace}}}
	return plan, PlanExecution{
		StartedAt: startedAt, FinishedAt: startedAt.Add(time.Duration(len(tasks)+1) * time.Second), Tasks: tasks,
	}
}

func TestPlanExecutionBoundsAreAssertedAtTheirEdges(t *testing.T) {
	t.Parallel()

	accepts := func(t *testing.T, what, namespace string, tasks []PlanExecutionTask) {
		t.Helper()
		plan, execution := executionOfTasks(namespace, tasks)
		if _, err := normalizePlanExecution(plan, execution, true); err != nil {
			t.Errorf("%s was refused: %v", what, err)
		}
	}
	refuses := func(t *testing.T, what, namespace string, tasks []PlanExecutionTask) {
		t.Helper()
		plan, execution := executionOfTasks(namespace, tasks)
		if _, err := normalizePlanExecution(plan, execution, true); err == nil {
			t.Errorf("%s was accepted", what)
		}
	}

	t.Run("task count", func(t *testing.T) {
		accepts(t, "an execution of exactly the task limit", "default", make([]PlanExecutionTask, maximumExecutionTasks))
		refuses(t, "an execution one task over the limit", "default", make([]PlanExecutionTask, maximumExecutionTasks+1))
		refuses(t, "an execution of no tasks", "default", nil)
	})

	t.Run("resources per task", func(t *testing.T) {
		resources := func(count int) []ResourceDiagnostic {
			result := make([]ResourceDiagnostic, count)
			for index := range result {
				result[index] = ResourceDiagnostic{Kind: "Deployment", Name: "api", Namespace: "default"}
			}
			return result
		}
		accepts(t, "a task carrying exactly the resource limit", "default",
			[]PlanExecutionTask{{Resources: resources(maximumTaskResources)}})
		refuses(t, "a task one resource over the limit", "default",
			[]PlanExecutionTask{{Resources: resources(maximumTaskResources + 1)}})
	})

	t.Run("resource identifier lengths", func(t *testing.T) {
		// Each of the four identifiers is bounded separately, and a report that
		// overruns any one of them is unattributable rather than merely long.
		atLimit := strings.Repeat("n", maximumResourceIdentifier)
		for _, field := range []struct {
			name string
			set  func(*ResourceDiagnostic, string)
		}{
			{"apiVersion", func(r *ResourceDiagnostic, v string) { r.APIVersion = v }},
			{"kind", func(r *ResourceDiagnostic, v string) { r.Kind = v }},
			{"name", func(r *ResourceDiagnostic, v string) { r.Name = v }},
		} {
			t.Run(field.name, func(t *testing.T) {
				build := func(value string) []PlanExecutionTask {
					resource := ResourceDiagnostic{Kind: "Deployment", Name: "api", Namespace: "default"}
					field.set(&resource, value)
					return []PlanExecutionTask{{Resources: []ResourceDiagnostic{resource}}}
				}
				accepts(t, field.name+" at exactly the identifier limit", "default", build(atLimit))
				refuses(t, field.name+" one over the identifier limit", "default", build(atLimit+"n"))
			})
		}
		t.Run("namespace", func(t *testing.T) {
			// A resource namespace has to match the task's, so the bound is only
			// reachable with a Plan whose own namespace is that long.
			accepts(t, "a namespace at exactly the identifier limit", atLimit, []PlanExecutionTask{{
				Resources: []ResourceDiagnostic{{Kind: "Deployment", Name: "api", Namespace: atLimit}},
			}})
			refuses(t, "a namespace one over the identifier limit", atLimit+"n", []PlanExecutionTask{{
				Resources: []ResourceDiagnostic{{Kind: "Deployment", Name: "api", Namespace: atLimit + "n"}},
			}})
		})
	})

	t.Run("aggregate Agent output", func(t *testing.T) {
		// Agent output is capped per task and again across the execution, so the
		// aggregate is reachable only by several tasks each at their own cap.
		outputTasks := func(count int) []PlanExecutionTask {
			tasks := make([]PlanExecutionTask, count)
			for index := range tasks {
				tasks[index].AgentOutput = strings.Repeat("o", maximumDiagnosticOutput)
			}
			return tasks
		}
		exactly := maximumExecutionOutputTotal / maximumDiagnosticOutput
		accepts(t, "output totalling exactly the aggregate limit", "default", outputTasks(exactly))
		refuses(t, "output one task over the aggregate limit", "default", outputTasks(exactly+1))
	})

	t.Run("aggregate diagnostic data", func(t *testing.T) {
		// Failure text is outside the Agent-output total but inside this one, so
		// it is what reaches this bound without tripping the other first.
		failureTasks := func(count int) []PlanExecutionTask {
			tasks := make([]PlanExecutionTask, count)
			for index := range tasks {
				tasks[index].Failure = strings.Repeat("f", maximumDiagnosticFailure)
			}
			return tasks
		}
		exactly := maximumExecutionDataTotal / maximumDiagnosticFailure
		accepts(t, "diagnostics totalling exactly the aggregate limit", "default", failureTasks(exactly))
		refuses(t, "diagnostics one task over the aggregate limit", "default", failureTasks(exactly+1))

		// Failure and Agent output are added together, not netted against each
		// other: a difference would leave this execution comfortably inside.
		mixed := make([]PlanExecutionTask, 114)
		for index := range mixed {
			mixed[index].Failure = strings.Repeat("f", maximumDiagnosticFailure)
			mixed[index].AgentOutput = strings.Repeat("o", 256)
		}
		refuses(t, "failure and output that only exceed the limit when added", "default", mixed)
	})

	t.Run("resource fields are added together", func(t *testing.T) {
		// Every field of a resource counts towards the aggregate. Two hundred
		// resources put it just over the limit; netting any single field against
		// the rest puts it back under, which is what makes each addition visible.
		tasks := make([]PlanExecutionTask, 4)
		for taskIndex := range tasks {
			resources := make([]ResourceDiagnostic, 50)
			for index := range resources {
				resources[index] = ResourceDiagnostic{
					APIVersion: "apps/v1", Kind: "Deployment", Namespace: "default", Name: "api-000000",
					Reason: strings.Repeat("r", 256), Message: strings.Repeat("m", maximumResourceMessage),
				}
			}
			tasks[taskIndex].Resources = resources
		}
		refuses(t, "resources whose fields only exceed the limit when added", "default", tasks)
	})
}

// RedactDiagnosticText is the only thing standing between Agent-supplied text and
// durable Plan state, so both what it keeps and what it removes are asserted.
func TestRedactDiagnosticTextKeepsTheKeyAndHonoursItsBound(t *testing.T) {
	t.Parallel()

	// The key name survives; only its value goes. An operator reading a redacted
	// diagnostic still needs to know which credential was in there.
	for _, testCase := range []struct{ in, want string }{
		{"token: abcdef", "token=[REDACTED]"},
		{"password=hunter2", "password=[REDACTED]"},
		// The bearer pattern runs first, so this one is redacted twice over --
		// once as a bearer token and again as an authorization value.
		{"Authorization: Bearer abc.def", "Authorization=[REDACTED] [REDACTED]"},
	} {
		if got := RedactDiagnosticText(testCase.in, 0); got != testCase.want {
			t.Errorf("RedactDiagnosticText(%q) = %q, wanted %q", testCase.in, got, testCase.want)
		}
	}

	// A maximum of one is a real bound, not a synonym for "unbounded": only a
	// maximum below one means no limit at all.
	if got := RedactDiagnosticText("abcdef", 1); got != "a…" {
		t.Errorf("a maximum of one truncated to %q", got)
	}
	if got := RedactDiagnosticText("abcdef", 0); got != "abcdef" {
		t.Errorf("a maximum of zero truncated to %q", got)
	}
	if got := RedactDiagnosticText("abcdef", 6); got != "abcdef" {
		t.Errorf("text exactly at its maximum was truncated to %q", got)
	}
	if got := RedactDiagnosticText("abcdef", 5); got != "abcde…" {
		t.Errorf("text one over its maximum became %q", got)
	}
}

// The error has to name the task it is about: an execution can carry hundreds,
// and "some task is invalid" leaves the Agent author to find which.
func TestPlanExecutionErrorsNameTheOffendingTask(t *testing.T) {
	t.Parallel()
	tasks := make([]PlanExecutionTask, 4)
	plan, execution := executionOfTasks("default", tasks)
	execution.Tasks[2].Phase = "NOT_A_PHASE"
	_, err := normalizePlanExecution(plan, execution, true)
	if err == nil || !strings.Contains(err.Error(), "task 3 ") {
		t.Errorf("error for the third task = %v", err)
	}
}
