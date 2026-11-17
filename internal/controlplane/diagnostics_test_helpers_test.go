package controlplane

import "time"

func executionAttempt(record PlanRecord) int {
	if record.Lease == nil {
		return 0
	}
	return record.Lease.Attempt
}

func successfulPlanExecution(record PlanRecord) *PlanExecution {
	startedAt := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	tasks := make([]PlanExecutionTask, 0, len(record.Plan.Steps))
	for index, step := range record.Plan.Steps {
		tasks = append(tasks, PlanExecutionTask{
			Sequence: index + 1, Phase: ExecutionPhaseApply, Status: ExecutionTaskSucceeded,
			InstallationID: step.InstallationID, Namespace: step.Namespace,
			StartedAt: startedAt.Add(time.Duration(index) * time.Second), FinishedAt: startedAt.Add(time.Duration(index+1) * time.Second),
		})
	}
	finishedAt := startedAt.Add(time.Duration(len(tasks)+1) * time.Second)
	return &PlanExecution{Attempt: executionAttempt(record), StartedAt: startedAt, FinishedAt: finishedAt, Tasks: tasks}
}

func failedPlanExecution(record PlanRecord, installationID string) *PlanExecution {
	startedAt := time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)
	step := record.Plan.Steps[0]
	for _, candidate := range record.Plan.Steps {
		if candidate.InstallationID == installationID {
			step = candidate
			break
		}
	}
	return &PlanExecution{Attempt: executionAttempt(record), StartedAt: startedAt, FinishedAt: startedAt.Add(time.Second), Tasks: []PlanExecutionTask{{
		Sequence: 1, Phase: ExecutionPhaseApply, Status: ExecutionTaskFailed,
		InstallationID: step.InstallationID, Namespace: step.Namespace,
		StartedAt: startedAt, FinishedAt: startedAt.Add(time.Second), Failure: "execution failed",
	}}}
}
