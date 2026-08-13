package controlplane

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maximumExecutionTasks       = 512
	maximumTaskResources        = 64
	maximumDiagnosticFailure    = 2048
	maximumDiagnosticOutput     = 4096
	maximumResourceMessage      = 1024
	maximumExecutionOutputTotal = 32 * 1024
	maximumExecutionDataTotal   = 256 * 1024
	maximumResourceIdentifier   = 253
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
	if required > maximumExecutionTasks {
		return fmt.Errorf(
			"plan requires up to %d execution evidence tasks, exceeding the durable limit of %d",
			required, maximumExecutionTasks,
		)
	}
	return nil
}

var (
	diagnosticBearerPattern = regexp.MustCompile(`(?i)\b(bearer)\s+[^\s,;]+`)
	diagnosticValuePattern  = regexp.MustCompile(`(?i)\b(authorization|password|passwd|token|secret|api[_-]?key)\s*[:=]\s*[^\s,;]+`)
	diagnosticPEMPattern    = regexp.MustCompile(`(?s)-----BEGIN [^-]*(?:PRIVATE KEY|CERTIFICATE)-----.*?-----END [^-]*(?:PRIVATE KEY|CERTIFICATE)-----`)
)

// RedactDiagnosticText bounds Agent-supplied diagnostics and removes common
// credential shapes before they can enter durable Plan state or API output.
func RedactDiagnosticText(value string, maximum int) string {
	value = strings.ToValidUTF8(strings.TrimSpace(value), "�")
	value = diagnosticPEMPattern.ReplaceAllString(value, "[REDACTED PEM]")
	value = diagnosticBearerPattern.ReplaceAllString(value, "$1 [REDACTED]")
	value = diagnosticValuePattern.ReplaceAllStringFunc(value, func(match string) string {
		separator := strings.IndexAny(match, ":=")
		if separator < 0 {
			return "[REDACTED]"
		}
		return strings.TrimSpace(match[:separator]) + "=[REDACTED]"
	})
	if maximum < 1 || len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return strings.TrimSpace(value) + "…"
}

func normalizePlanExecution(plan Plan, execution PlanExecution, succeeded bool) (PlanExecution, error) {
	if execution.StartedAt.IsZero() || execution.FinishedAt.IsZero() || execution.FinishedAt.Before(execution.StartedAt) {
		return PlanExecution{}, fmt.Errorf("%w: execution requires ordered start and finish timestamps", ErrInvalidTransition)
	}
	if len(execution.Tasks) == 0 || len(execution.Tasks) > maximumExecutionTasks {
		return PlanExecution{}, fmt.Errorf("%w: execution must contain between 1 and %d tasks", ErrInvalidTransition, maximumExecutionTasks)
	}
	steps := make(map[string]PlanStep, len(plan.Steps))
	allowedNamespaces := make(map[string]bool, len(plan.Steps)+len(plan.SecretMutations))
	for _, step := range plan.Steps {
		steps[step.InstallationID] = step
		allowedNamespaces[step.Namespace] = true
	}
	for _, mutation := range plan.SecretMutations {
		allowedNamespaces[mutation.Namespace] = true
	}
	allowedPhases := map[string]bool{
		ExecutionPhaseValidate: true, ExecutionPhaseLease: true, ExecutionPhaseLegacy: true, ExecutionPhaseSecretMutation: true, ExecutionPhaseApply: true,
		ExecutionPhaseObserve: true, ExecutionPhaseRollback: true,
	}
	allowedStatuses := map[string]bool{
		ExecutionTaskSucceeded: true, ExecutionTaskFailed: true,
		ExecutionTaskRolledBack: true, ExecutionTaskRollbackFailed: true,
	}
	outputTotal := 0
	diagnosticTotal := 0
	hasFailure := false
	previousFinish := execution.StartedAt
	for index := range execution.Tasks {
		task := &execution.Tasks[index]
		if task.Sequence != index+1 || !allowedPhases[task.Phase] || !allowedStatuses[task.Status] {
			return PlanExecution{}, fmt.Errorf("%w: execution task %d has invalid sequence, phase, or status", ErrInvalidTransition, index+1)
		}
		if task.StartedAt.IsZero() || task.FinishedAt.IsZero() || task.StartedAt.Before(previousFinish) || task.FinishedAt.Before(task.StartedAt) || task.FinishedAt.After(execution.FinishedAt) {
			return PlanExecution{}, fmt.Errorf("%w: execution task %d has unordered timestamps", ErrInvalidTransition, task.Sequence)
		}
		previousFinish = task.FinishedAt
		if task.Namespace != "" && !allowedNamespaces[task.Namespace] {
			return PlanExecution{}, fmt.Errorf("%w: execution task %d namespace is not attributable to the Plan", ErrInvalidTransition, task.Sequence)
		}
		if task.InstallationID != "" {
			step, exists := steps[task.InstallationID]
			if !exists || (task.Namespace != "" && task.Namespace != step.Namespace) {
				return PlanExecution{}, fmt.Errorf("%w: execution task %d is not attributable to the Plan", ErrInvalidTransition, task.Sequence)
			}
		}
		if len(task.Resources) > maximumTaskResources {
			return PlanExecution{}, fmt.Errorf("%w: execution task %d has too many resource diagnostics", ErrInvalidTransition, task.Sequence)
		}
		task.Failure = RedactDiagnosticText(task.Failure, maximumDiagnosticFailure)
		task.AgentOutput = RedactDiagnosticText(task.AgentOutput, maximumDiagnosticOutput)
		outputTotal += len(task.AgentOutput)
		diagnosticTotal += len(task.Failure) + len(task.AgentOutput)
		for resourceIndex := range task.Resources {
			resource := &task.Resources[resourceIndex]
			resource.APIVersion = strings.TrimSpace(resource.APIVersion)
			resource.Kind = strings.TrimSpace(resource.Kind)
			resource.Namespace = strings.TrimSpace(resource.Namespace)
			resource.Name = strings.TrimSpace(resource.Name)
			if resource.Kind == "" || resource.Name == "" || len(resource.APIVersion) > maximumResourceIdentifier || len(resource.Kind) > maximumResourceIdentifier || len(resource.Namespace) > maximumResourceIdentifier || len(resource.Name) > maximumResourceIdentifier {
				return PlanExecution{}, fmt.Errorf("%w: execution task %d has an unattributed resource", ErrInvalidTransition, task.Sequence)
			}
			if resource.Namespace != "" && (resource.Namespace != task.Namespace || !allowedNamespaces[resource.Namespace]) {
				return PlanExecution{}, fmt.Errorf("%w: execution task %d resource namespace is not attributable to the task", ErrInvalidTransition, task.Sequence)
			}
			resource.Reason = RedactDiagnosticText(resource.Reason, 256)
			resource.Message = RedactDiagnosticText(resource.Message, maximumResourceMessage)
			diagnosticTotal += len(resource.APIVersion) + len(resource.Kind) + len(resource.Namespace) + len(resource.Name) + len(resource.Reason) + len(resource.Message)
		}
		rollbackStatus := task.Status == ExecutionTaskRolledBack || task.Status == ExecutionTaskRollbackFailed
		if (task.Phase == ExecutionPhaseRollback) != rollbackStatus {
			return PlanExecution{}, fmt.Errorf("%w: execution task %d phase and status disagree", ErrInvalidTransition, task.Sequence)
		}
		if task.Status == ExecutionTaskFailed || task.Status == ExecutionTaskRollbackFailed {
			hasFailure = true
			if task.Failure == "" {
				return PlanExecution{}, fmt.Errorf("%w: failed execution task %d requires a failure", ErrInvalidTransition, task.Sequence)
			}
		}
		if succeeded && (task.Phase == ExecutionPhaseRollback || task.Status == ExecutionTaskRolledBack) {
			return PlanExecution{}, fmt.Errorf("%w: successful execution cannot contain rollback tasks", ErrInvalidTransition)
		}
	}
	if outputTotal > maximumExecutionOutputTotal {
		return PlanExecution{}, fmt.Errorf("%w: execution Agent output exceeds %d bytes", ErrInvalidTransition, maximumExecutionOutputTotal)
	}
	if diagnosticTotal > maximumExecutionDataTotal {
		return PlanExecution{}, fmt.Errorf("%w: execution diagnostics exceed %d bytes", ErrInvalidTransition, maximumExecutionDataTotal)
	}
	if succeeded == hasFailure {
		return PlanExecution{}, fmt.Errorf("%w: execution task outcomes do not match Plan status", ErrInvalidTransition)
	}
	execution.StartedAt = execution.StartedAt.UTC()
	execution.FinishedAt = execution.FinishedAt.UTC()
	return execution, nil
}
