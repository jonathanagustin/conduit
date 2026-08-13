package agent

import (
	"errors"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type DiagnosticError struct {
	Err       error
	Output    string
	Resources []controlplane.ResourceDiagnostic
}

func (failure DiagnosticError) Error() string {
	if failure.Err == nil {
		return "execution failed"
	}
	return failure.Err.Error()
}

func (failure DiagnosticError) Unwrap() error { return failure.Err }

type executionRecorder struct {
	clock     func() time.Time
	execution controlplane.PlanExecution
}

func newExecutionRecorder(clock func() time.Time) *executionRecorder {
	if clock == nil {
		clock = time.Now
	}
	startedAt := clock().UTC()
	return &executionRecorder{clock: clock, execution: controlplane.PlanExecution{StartedAt: startedAt}}
}

func (recorder *executionRecorder) now() time.Time     { return recorder.clock().UTC() }
func (recorder *executionRecorder) started() time.Time { return recorder.execution.StartedAt }

func (recorder *executionRecorder) add(phase, status, installationID, namespace string, startedAt time.Time, taskErr error) {
	finishedAt := recorder.now()
	task := controlplane.PlanExecutionTask{
		Sequence: len(recorder.execution.Tasks) + 1, Phase: phase, Status: status,
		InstallationID: installationID, Namespace: namespace, StartedAt: startedAt, FinishedAt: finishedAt,
	}
	if taskErr != nil {
		task.Failure = controlplane.RedactDiagnosticText(taskErr.Error(), 2048)
		var diagnostic DiagnosticError
		if errors.As(taskErr, &diagnostic) {
			task.AgentOutput = controlplane.RedactDiagnosticText(diagnostic.Output, 4096)
			task.Resources = append([]controlplane.ResourceDiagnostic(nil), diagnostic.Resources...)
		}
	}
	recorder.execution.Tasks = append(recorder.execution.Tasks, task)
}

func (recorder *executionRecorder) finish() controlplane.PlanExecution {
	recorder.execution.FinishedAt = recorder.now()
	return recorder.execution
}
