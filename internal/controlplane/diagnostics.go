package controlplane

import (
	"fmt"

	execdomain "github.com/jonathanagustin/conduit/internal/execution/domain"
)

// planExecutionTaskUpperBound mirrors every task the outbound Spoke can emit
// for one attempt. Besides the successful path (validation, every mutation,
// apply, and observe), it reserves one lease-failure task and every rollback
// task that may follow a late heartbeat failure. Admission uses the failure
// path because diagnostics are most important when convergence goes wrong.
func planExecutionTaskUpperBound(plan Plan) int {
	tasks := 2 // validation plus a possible late lease failure
	for _, mutation := range plan.SecretMutations {
		tasks++
		if mutation.Action != SecretVerify {
			tasks++
		}
	}
	for _, step := range plan.Steps {
		tasks += 2 // apply plus exact observation
		if step.Action != PlanActionUnmanage {
			tasks++
		}
	}
	return tasks
}

func validatePlanExecutionTaskBudget(plan Plan) error {
	required := planExecutionTaskUpperBound(plan)
	if required > execdomain.MaximumExecutionTasks {
		return fmt.Errorf(
			"plan requires up to %d execution evidence tasks, exceeding the durable limit of %d",
			required, execdomain.MaximumExecutionTasks,
		)
	}
	return nil
}

// Reporting is Execution's. What is left here is the Plan-shaped part: what a
// Plan permits an execution to be attributed to, and this package's own error
// vocabulary for a report that does not fit.
func normalizePlanExecution(plan Plan, execution PlanExecution, succeeded bool) (PlanExecution, error) {
	normalized, err := execdomain.NormalizeExecution(planPermits(plan), execution, succeeded)
	if err != nil {
		return PlanExecution{}, fmt.Errorf("%w: %s", ErrInvalidTransition, err)
	}
	return normalized, nil
}

// planPermits narrows a Plan to what an execution may be attributed to.
func planPermits(plan Plan) execdomain.Permitted {
	permitted := execdomain.Permitted{
		Namespaces:              make(map[string]bool, len(plan.Steps)+len(plan.SecretMutations)),
		NamespaceByInstallation: make(map[string]string, len(plan.Steps)),
	}
	for _, step := range plan.Steps {
		permitted.NamespaceByInstallation[step.InstallationID] = step.Namespace
		permitted.Namespaces[step.Namespace] = true
	}
	for _, mutation := range plan.SecretMutations {
		permitted.Namespaces[mutation.Namespace] = true
	}
	return permitted
}
