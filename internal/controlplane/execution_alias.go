package controlplane

import execution "github.com/jonathanagustin/conduit/internal/execution/domain"

// An Agent's claim on a Plan and the evidence it reports afterwards are
// Execution's. These aliases keep this package's call sites spelled as they
// were; what moved is the deciding.
type (
	PlanLease          = execution.Lease
	PlanExecution      = execution.PlanExecution
	PlanExecutionTask  = execution.PlanExecutionTask
	ResourceDiagnostic = execution.ResourceDiagnostic
)

const (
	ExecutionPhaseValidate       = execution.ExecutionPhaseValidate
	ExecutionPhaseLease          = execution.ExecutionPhaseLease
	ExecutionPhaseLegacy         = execution.ExecutionPhaseLegacy
	ExecutionPhaseSecretMutation = execution.ExecutionPhaseSecretMutation
	ExecutionPhaseApply          = execution.ExecutionPhaseApply
	ExecutionPhaseObserve        = execution.ExecutionPhaseObserve
	ExecutionPhaseRollback       = execution.ExecutionPhaseRollback

	ExecutionTaskSucceeded      = execution.ExecutionTaskSucceeded
	ExecutionTaskFailed         = execution.ExecutionTaskFailed
	ExecutionTaskRolledBack     = execution.ExecutionTaskRolledBack
	ExecutionTaskRollbackFailed = execution.ExecutionTaskRollbackFailed

	// The bounds an Agent's report must fit inside. maximumDiagnosticFailure is
	// also the bound Bundle import task failures are held to: it is the same
	// kind of text arriving at a different door.
	maximumExecutionTasks       = execution.MaximumExecutionTasks
	maximumTaskResources        = execution.MaximumTaskResources
	maximumDiagnosticFailure    = execution.MaximumDiagnosticFailure
	maximumDiagnosticOutput     = execution.MaximumDiagnosticOutput
	maximumResourceMessage      = execution.MaximumResourceMessage
	maximumExecutionOutputTotal = execution.MaximumExecutionOutputTotal
	maximumExecutionDataTotal   = execution.MaximumExecutionDataTotal
	maximumResourceIdentifier   = execution.MaximumResourceIdentifier
)

// RedactDiagnosticText bounds Agent-supplied diagnostics and removes common
// credential shapes before they can enter durable state or API output.
func RedactDiagnosticText(value string, maximum int) string {
	return execution.RedactDiagnosticText(value, maximum)
}
