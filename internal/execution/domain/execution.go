package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// The bounds an Agent's report must fit inside. They are exported where a
// caller has to admit work against the same limit -- refusing a Plan that could
// never report inside them is better done before it runs than after.
const (
	MaximumExecutionTasks       = 512
	MaximumTaskResources        = 64
	MaximumDiagnosticFailure    = 2048
	MaximumDiagnosticOutput     = 4096
	MaximumResourceMessage      = 1024
	MaximumExecutionOutputTotal = 32 * 1024
	MaximumExecutionDataTotal   = 256 * 1024
	MaximumResourceIdentifier   = 253
)

// PlanExecution is the bounded, attributable task timeline reported by the
// Agent. Provenance (reviewed 2026-08-02):
// https://www.palantir.com/docs/apollo/core/plans-and-constraints/
type PlanExecution struct {
	Attempt    int                 `json:"attempt"`
	AgentID    string              `json:"agentId"`
	StartedAt  time.Time           `json:"startedAt"`
	FinishedAt time.Time           `json:"finishedAt"`
	Tasks      []PlanExecutionTask `json:"tasks"`
}

type PlanExecutionTask struct {
	Sequence       int                  `json:"sequence"`
	Phase          string               `json:"phase"`
	Status         string               `json:"status"`
	InstallationID string               `json:"installationId,omitempty"`
	Namespace      string               `json:"namespace,omitempty"`
	StartedAt      time.Time            `json:"startedAt"`
	FinishedAt     time.Time            `json:"finishedAt"`
	Failure        string               `json:"failure,omitempty"`
	AgentOutput    string               `json:"agentOutput,omitempty"`
	Resources      []ResourceDiagnostic `json:"resources,omitempty"`
}

type ResourceDiagnostic struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
	Reason     string `json:"reason,omitempty"`
	Message    string `json:"message,omitempty"`
}

const (
	ExecutionPhaseValidate       = "VALIDATE"
	ExecutionPhaseLease          = "LEASE"
	ExecutionPhaseLegacy         = "LEGACY"
	ExecutionPhaseSecretMutation = "SECRET_MUTATION"
	ExecutionPhaseApply          = "APPLY"
	ExecutionPhaseObserve        = "OBSERVE"
	ExecutionPhaseRollback       = "ROLLBACK"

	ExecutionTaskSucceeded      = "SUCCEEDED"
	ExecutionTaskFailed         = "FAILED"
	ExecutionTaskRolledBack     = "ROLLED_BACK"
	ExecutionTaskRollbackFailed = "ROLLBACK_FAILED"
)

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

// Permitted is what the Plan allows an execution to be attributed to: the
// namespaces its steps and secret mutations touch, and the namespace each
// installation is expected in.
//
// It is this rather than the Plan because attribution is the only thing the
// rules need, and taking the Plan would make Execution depend on Delivery to
// answer a question about an Agent's report.
type Permitted struct {
	Namespaces              map[string]bool
	NamespaceByInstallation map[string]string
}

// NormalizeExecution checks that a reported task timeline is one this Plan
// could have produced, redacts every Agent-supplied string in it, and returns
// the normalized result.
//
// It returns the reason without a sentinel: which error class a rejected report
// belongs to is the caller's vocabulary, and out here it has always been an
// invalid transition.
func NormalizeExecution(permitted Permitted, execution PlanExecution, succeeded bool) (PlanExecution, error) {
	if execution.StartedAt.IsZero() || execution.FinishedAt.IsZero() || execution.FinishedAt.Before(execution.StartedAt) {
		return PlanExecution{}, errors.New("execution requires ordered start and finish timestamps")
	}
	if len(execution.Tasks) == 0 || len(execution.Tasks) > MaximumExecutionTasks {
		return PlanExecution{}, fmt.Errorf("execution must contain between 1 and %d tasks", MaximumExecutionTasks)
	}
	steps := permitted.NamespaceByInstallation
	allowedNamespaces := permitted.Namespaces
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
			return PlanExecution{}, fmt.Errorf("execution task %d has invalid sequence, phase, or status", index+1)
		}
		if task.StartedAt.IsZero() || task.FinishedAt.IsZero() || task.StartedAt.Before(previousFinish) || task.FinishedAt.Before(task.StartedAt) || task.FinishedAt.After(execution.FinishedAt) {
			return PlanExecution{}, fmt.Errorf("execution task %d has unordered timestamps", task.Sequence)
		}
		previousFinish = task.FinishedAt
		if task.Namespace != "" && !allowedNamespaces[task.Namespace] {
			return PlanExecution{}, fmt.Errorf("execution task %d namespace is not attributable to the Plan", task.Sequence)
		}
		if task.InstallationID != "" {
			stepNamespace, exists := steps[task.InstallationID]
			if !exists || (task.Namespace != "" && task.Namespace != stepNamespace) {
				return PlanExecution{}, fmt.Errorf("execution task %d is not attributable to the Plan", task.Sequence)
			}
		}
		if len(task.Resources) > MaximumTaskResources {
			return PlanExecution{}, fmt.Errorf("execution task %d has too many resource diagnostics", task.Sequence)
		}
		task.Failure = RedactDiagnosticText(task.Failure, MaximumDiagnosticFailure)
		task.AgentOutput = RedactDiagnosticText(task.AgentOutput, MaximumDiagnosticOutput)
		outputTotal += len(task.AgentOutput)
		diagnosticTotal += len(task.Failure) + len(task.AgentOutput)
		for resourceIndex := range task.Resources {
			resource := &task.Resources[resourceIndex]
			resource.APIVersion = strings.TrimSpace(resource.APIVersion)
			resource.Kind = strings.TrimSpace(resource.Kind)
			resource.Namespace = strings.TrimSpace(resource.Namespace)
			resource.Name = strings.TrimSpace(resource.Name)
			if resource.Kind == "" || resource.Name == "" || len(resource.APIVersion) > MaximumResourceIdentifier || len(resource.Kind) > MaximumResourceIdentifier || len(resource.Namespace) > MaximumResourceIdentifier || len(resource.Name) > MaximumResourceIdentifier {
				return PlanExecution{}, fmt.Errorf("execution task %d has an unattributed resource", task.Sequence)
			}
			if resource.Namespace != "" && (resource.Namespace != task.Namespace || !allowedNamespaces[resource.Namespace]) {
				return PlanExecution{}, fmt.Errorf("execution task %d resource namespace is not attributable to the task", task.Sequence)
			}
			resource.Reason = RedactDiagnosticText(resource.Reason, 256)
			resource.Message = RedactDiagnosticText(resource.Message, MaximumResourceMessage)
			diagnosticTotal += len(resource.APIVersion) + len(resource.Kind) + len(resource.Namespace) + len(resource.Name) + len(resource.Reason) + len(resource.Message)
		}
		rollbackStatus := task.Status == ExecutionTaskRolledBack || task.Status == ExecutionTaskRollbackFailed
		if (task.Phase == ExecutionPhaseRollback) != rollbackStatus {
			return PlanExecution{}, fmt.Errorf("execution task %d phase and status disagree", task.Sequence)
		}
		if task.Status == ExecutionTaskFailed || task.Status == ExecutionTaskRollbackFailed {
			hasFailure = true
			if task.Failure == "" {
				return PlanExecution{}, fmt.Errorf("failed execution task %d requires a failure", task.Sequence)
			}
		}
		if succeeded && (task.Phase == ExecutionPhaseRollback || task.Status == ExecutionTaskRolledBack) {
			return PlanExecution{}, errors.New("successful execution cannot contain rollback tasks")
		}
	}
	if outputTotal > MaximumExecutionOutputTotal {
		return PlanExecution{}, fmt.Errorf("execution Agent output exceeds %d bytes", MaximumExecutionOutputTotal)
	}
	if diagnosticTotal > MaximumExecutionDataTotal {
		return PlanExecution{}, fmt.Errorf("execution diagnostics exceed %d bytes", MaximumExecutionDataTotal)
	}
	if succeeded == hasFailure {
		return PlanExecution{}, errors.New("execution task outcomes do not match Plan status")
	}
	execution.StartedAt = execution.StartedAt.UTC()
	execution.FinishedAt = execution.FinishedAt.UTC()
	return execution, nil
}
