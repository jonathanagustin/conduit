package pipeline

import (
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// RegisterWorker binds the workflow and its activities to a loopback task
// queue. The caller supplies the client; this process does not dial Temporal
// Cloud and does not open a listener of its own.
func RegisterWorker(c client.Client, taskQueue string, acts Activities) worker.Worker {
	if taskQueue == "" {
		taskQueue = TaskQueue
	}
	work := worker.New(c, taskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize:     MaxConcurrentActivities,
		MaxConcurrentWorkflowTaskExecutionSize: MaxConcurrentWorkflowTasks,
		WorkerStopTimeout:                      time.Second,
	})
	work.RegisterWorkflowWithOptions(PackagingWorkflow, workflow.RegisterOptions{Name: WorkflowName})
	registerActivities(work, acts)
	return work
}

func registerActivities(registry worker.ActivityRegistry, acts Activities) {
	registry.RegisterActivityWithOptions(acts.Assess, activity.RegisterOptions{Name: ActAssess})
	registry.RegisterActivityWithOptions(acts.Propose, activity.RegisterOptions{Name: ActPropose})
	registry.RegisterActivityWithOptions(acts.Validate, activity.RegisterOptions{Name: ActValidate})
	registry.RegisterActivityWithOptions(acts.ReadApproval, activity.RegisterOptions{Name: ActReadApproval})
	registry.RegisterActivityWithOptions(acts.Handoff, activity.RegisterOptions{Name: ActHandoff})
}
