package domain

import (
	"strings"
	"testing"
	"time"
)

var executionFixtureTime = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

func permits() Permitted {
	return Permitted{
		Namespaces:              map[string]bool{"apps": true, "secrets": true},
		NamespaceByInstallation: map[string]string{"api": "apps"},
	}
}

func task(sequence int, phase, status string, offset time.Duration) PlanExecutionTask {
	return PlanExecutionTask{
		Sequence: sequence, Phase: phase, Status: status,
		StartedAt:  executionFixtureTime.Add(offset),
		FinishedAt: executionFixtureTime.Add(offset + time.Second),
	}
}

func report(tasks ...PlanExecutionTask) PlanExecution {
	return PlanExecution{
		Attempt: 1, AgentID: "spoke-dev",
		StartedAt:  executionFixtureTime,
		FinishedAt: executionFixtureTime.Add(time.Hour),
		Tasks:      tasks,
	}
}

func TestNormalizeExecutionAcceptsAWellFormedSuccessfulReport(t *testing.T) {
	t.Parallel()
	applied := task(2, ExecutionPhaseApply, ExecutionTaskSucceeded, 2*time.Second)
	applied.InstallationID, applied.Namespace = "api", "apps"
	normalized, err := NormalizeExecution(permits(), report(
		task(1, ExecutionPhaseValidate, ExecutionTaskSucceeded, 0), applied,
	), true)
	if err != nil {
		t.Fatal(err)
	}
	// The timestamps come back in UTC so two reports of the same run compare
	// equal whatever zone the Agent kept.
	if normalized.StartedAt.Location() != time.UTC || normalized.FinishedAt.Location() != time.UTC {
		t.Fatalf("timestamps were not normalized: %#v", normalized)
	}
}

func TestNormalizeExecutionRejectsAReportThisPlanCouldNotHaveProduced(t *testing.T) {
	t.Parallel()
	unattributed := task(1, ExecutionPhaseApply, ExecutionTaskSucceeded, 0)
	unattributed.Namespace = "kube-system"

	wrongNamespace := task(1, ExecutionPhaseApply, ExecutionTaskSucceeded, 0)
	wrongNamespace.InstallationID, wrongNamespace.Namespace = "api", "secrets"

	unknownInstallation := task(1, ExecutionPhaseApply, ExecutionTaskSucceeded, 0)
	unknownInstallation.InstallationID = "worker"

	overlapping := task(2, ExecutionPhaseApply, ExecutionTaskSucceeded, 0)
	overlapping.StartedAt = executionFixtureTime.Add(-time.Minute)

	failedWithoutReason := task(1, ExecutionPhaseApply, ExecutionTaskFailed, 0)

	rollbackStatusWithoutPhase := task(1, ExecutionPhaseApply, ExecutionTaskRolledBack, 0)

	tooManyResources := task(1, ExecutionPhaseApply, ExecutionTaskSucceeded, 0)
	tooManyResources.Resources = make([]ResourceDiagnostic, MaximumTaskResources+1)
	for index := range tooManyResources.Resources {
		tooManyResources.Resources[index] = ResourceDiagnostic{Kind: "Pod", Name: "p"}
	}

	unnamedResource := task(1, ExecutionPhaseApply, ExecutionTaskSucceeded, 0)
	unnamedResource.Resources = []ResourceDiagnostic{{Kind: "Pod"}}

	for name, testCase := range map[string]struct {
		execution PlanExecution
		succeeded bool
	}{
		"no tasks at all":                      {execution: report(), succeeded: true},
		"a task out of sequence":               {execution: report(task(2, ExecutionPhaseValidate, ExecutionTaskSucceeded, 0)), succeeded: true},
		"a phase nobody emits":                 {execution: report(task(1, "CONTEMPLATE", ExecutionTaskSucceeded, 0)), succeeded: true},
		"a status nobody reports":              {execution: report(task(1, ExecutionPhaseApply, "MAYBE", 0)), succeeded: true},
		"a namespace the Plan never named":     {execution: report(unattributed), succeeded: true},
		"an installation in another namespace": {execution: report(wrongNamespace), succeeded: true},
		"an installation the Plan has not":     {execution: report(unknownInstallation), succeeded: true},
		"tasks that overlap in time": {execution: report(
			task(1, ExecutionPhaseValidate, ExecutionTaskSucceeded, 0), overlapping,
		), succeeded: true},
		"a failure with no reason given": {execution: report(failedWithoutReason)},
		// Phase and status must agree: a rollback status outside a rollback
		// phase describes a run that did not happen.
		"a rollback status in an apply phase":  {execution: report(rollbackStatusWithoutPhase)},
		"more resources than a task may carry": {execution: report(tooManyResources), succeeded: true},
		"a resource with no name":              {execution: report(unnamedResource), succeeded: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NormalizeExecution(permits(), testCase.execution, testCase.succeeded); err == nil {
				t.Fatal("a report this Plan could not have produced was accepted")
			}
		})
	}

	// A finish before a start is not a run.
	backwards := report(task(1, ExecutionPhaseValidate, ExecutionTaskSucceeded, 0))
	backwards.FinishedAt = backwards.StartedAt.Add(-time.Second)
	if _, err := NormalizeExecution(permits(), backwards, true); err == nil {
		t.Fatal("an execution finishing before it started was accepted")
	}
}

// The outcome the Plan recorded and the outcome the tasks describe must agree.
// A success carrying a failed task, or a failure carrying none, means one of
// the two is wrong and neither can be trusted over the other.
func TestNormalizeExecutionRequiresTasksToAgreeWithTheOutcome(t *testing.T) {
	t.Parallel()
	failure := task(1, ExecutionPhaseApply, ExecutionTaskFailed, 0)
	failure.Failure = "helm upgrade failed"
	success := task(1, ExecutionPhaseApply, ExecutionTaskSucceeded, 0)

	if _, err := NormalizeExecution(permits(), report(failure), false); err != nil {
		t.Fatalf("a failed run reporting a failed task was rejected: %v", err)
	}
	if _, err := NormalizeExecution(permits(), report(success), true); err != nil {
		t.Fatalf("a successful run reporting a succeeded task was rejected: %v", err)
	}
	if _, err := NormalizeExecution(permits(), report(failure), true); err == nil {
		t.Fatal("a successful run carrying a failed task was accepted")
	}
	if _, err := NormalizeExecution(permits(), report(success), false); err == nil {
		t.Fatal("a failed run carrying no failed task was accepted")
	}

	// A successful run cannot contain rollback work: nothing was rolled back.
	rolledBack := task(1, ExecutionPhaseRollback, ExecutionTaskRolledBack, 0)
	if _, err := NormalizeExecution(permits(), report(rolledBack), true); err == nil {
		t.Fatal("a successful run containing a rollback task was accepted")
	}
}

func TestNormalizeExecutionRedactsAgentTextAndBoundsTheAggregate(t *testing.T) {
	t.Parallel()
	leaking := task(1, ExecutionPhaseApply, ExecutionTaskFailed, 0)
	leaking.Failure = "authorization: Bearer abcdef123456 rejected"
	leaking.AgentOutput = "password=hunter2 while applying"
	leaking.Resources = []ResourceDiagnostic{{
		Kind: "Secret", Name: "db", Message: "token: swordfish",
	}}
	normalized, err := NormalizeExecution(permits(), report(leaking), false)
	if err != nil {
		t.Fatal(err)
	}
	got := normalized.Tasks[0]
	for _, leaked := range []string{"abcdef123456", "hunter2", "swordfish"} {
		if strings.Contains(got.Failure+got.AgentOutput+got.Resources[0].Message, leaked) {
			t.Fatalf("a credential survived into durable state: %#v", got)
		}
	}

	// The aggregate bound is per execution, not per task. Each task's output is
	// already truncated to MaximumDiagnosticOutput, so one task can never reach
	// it -- the bound exists because many separately-acceptable tasks add up to
	// a durable record nothing can serve.
	perTask := strings.Repeat("y", MaximumDiagnosticOutput)
	count := MaximumExecutionOutputTotal/MaximumDiagnosticOutput + 1
	tasks := make([]PlanExecutionTask, 0, count)
	for index := 0; index < count; index++ {
		verbose := task(index+1, ExecutionPhaseApply, ExecutionTaskSucceeded, time.Duration(index)*2*time.Second)
		verbose.AgentOutput = perTask
		tasks = append(tasks, verbose)
	}
	// One fewer is accepted, so the rejection below is the bound and not some
	// other rule these tasks happen to trip.
	if _, err := NormalizeExecution(permits(), report(tasks[:count-1]...), true); err != nil {
		t.Fatalf("an execution just under the aggregate output bound was rejected: %v", err)
	}
	if _, err := NormalizeExecution(permits(), report(tasks...), true); err == nil {
		t.Fatal("an execution over the aggregate output bound was accepted")
	}
}

func TestRedactDiagnosticTextRemovesCredentialShapesAndHonoursItsBound(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct{ in, mustNotContain, mustContain string }{
		"a bearer token": {
			in: "Authorization: Bearer abc.def.ghi", mustNotContain: "abc.def.ghi", mustContain: "REDACTED",
		},
		"an assigned secret": {
			in: "api_key=s3cr3t and more", mustNotContain: "s3cr3t", mustContain: "api_key",
		},
		"a private key block": {
			in:             "-----BEGIN RSA PRIVATE KEY-----\nMIIabc\n-----END RSA PRIVATE KEY-----",
			mustNotContain: "MIIabc", mustContain: "REDACTED PEM",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// The key's name is kept and its value removed. Dropping the name
			// too would leave an operator unable to tell what was set.
			got := RedactDiagnosticText(testCase.in, 4096)
			if strings.Contains(got, testCase.mustNotContain) || !strings.Contains(got, testCase.mustContain) {
				t.Fatalf("RedactDiagnosticText(%q) = %q", testCase.in, got)
			}
		})
	}

	// Truncation never splits a rune: the durable record has to stay valid
	// UTF-8 whatever byte the bound lands on.
	multibyte := strings.Repeat("é", 40)
	for bound := 1; bound <= 20; bound++ {
		got := RedactDiagnosticText(multibyte, bound)
		if !utf8ValidString(got) {
			t.Fatalf("bound %d produced invalid UTF-8: %q", bound, got)
		}
	}
	// A bound of zero or less means unbounded.
	if RedactDiagnosticText("unbounded", 0) != "unbounded" {
		t.Fatal("a zero bound truncated")
	}
}

func utf8ValidString(value string) bool {
	for _, r := range value {
		if r == '�' && !strings.Contains(value, "�") {
			return false
		}
	}
	return true
}
