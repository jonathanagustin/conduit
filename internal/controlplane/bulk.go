package controlplane

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	BulkSuppress   = "SUPPRESS"
	BulkUnsuppress = "UNSUPPRESS"

	ExecutionRunning   = "RUNNING"
	ExecutionSucceeded = "SUCCEEDED"
	ExecutionFailed    = "FAILED"
)

type FleetActionTarget struct {
	Environment    string `json:"environment"`
	InstallationID string `json:"installationId"`
}

type FleetActionRequest struct {
	Action          string              `json:"action"`
	Targets         []FleetActionTarget `json:"targets"`
	Reason          string              `json:"reason"`
	DurationSeconds int                 `json:"durationSeconds,omitempty"`
}

type FleetActionResult struct {
	Environment   string        `json:"environment"`
	ChangeRequest ChangeRequest `json:"changeRequest"`
}

func validateFleetAction(request FleetActionRequest) error {
	request.Action = strings.ToUpper(strings.TrimSpace(request.Action))
	if request.Action != BulkSuppress && request.Action != BulkUnsuppress {
		return ValidationErrors{{Path: "action", Message: "must be SUPPRESS or UNSUPPRESS"}}
	}
	if len(request.Targets) == 0 || len(request.Targets) > 500 {
		return ValidationErrors{{Path: "targets", Message: "must contain between 1 and 500 installations"}}
	}
	if strings.TrimSpace(request.Reason) == "" {
		return ValidationErrors{{Path: "reason", Message: "must not be empty"}}
	}
	if request.DurationSeconds < 0 || request.DurationSeconds > 30*24*60*60 ||
		(request.DurationSeconds > 0 && request.DurationSeconds < 60) {
		return ValidationErrors{{Path: "durationSeconds", Message: "must be zero or between 60 and 2592000"}}
	}
	seen := map[string]struct{}{}
	for index, target := range request.Targets {
		if !dnsLabelPattern.MatchString(target.Environment) || strings.TrimSpace(target.InstallationID) == "" {
			return ValidationErrors{{Path: fmt.Sprintf("targets[%d]", index), Message: "valid Environment and installationId are required"}}
		}
		key := target.Environment + "\x00" + target.InstallationID
		if _, exists := seen[key]; exists {
			return ValidationErrors{{Path: fmt.Sprintf("targets[%d]", index), Message: "duplicates an earlier target"}}
		}
		seen[key] = struct{}{}
	}
	return nil
}

func fleetActionTargetID(request FleetActionRequest, environment string, targets []FleetActionTarget) string {
	sorted := append([]FleetActionTarget(nil), targets...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].InstallationID < sorted[j].InstallationID })
	payload, _ := json.Marshal(struct {
		Action          string              `json:"action"`
		Environment     string              `json:"environment"`
		Targets         []FleetActionTarget `json:"targets"`
		Reason          string              `json:"reason"`
		DurationSeconds int                 `json:"durationSeconds"`
	}{request.Action, environment, sorted, request.Reason, request.DurationSeconds})
	digest := sha256.Sum256(payload)
	return "blk_" + hex.EncodeToString(digest[:16])
}

func executeApprovedFleetAction(
	ctx context.Context, repository Repository, lifecycle *Lifecycle, governance *GovernanceService, request ChangeRequest,
) (ChangeRequest, error) {
	if request.TargetKind != "FLEET_ACTION" || request.Status != ChangeApproved {
		return request, nil
	}
	request, err := repository.UpdateChangeRequest(ctx, request.ID, func(current *ChangeRequest) error {
		if current.Execution == nil {
			current.Execution = &ChangeExecution{Status: ExecutionRunning, StartedAt: time.Now().UTC()}
		} else if current.Execution.Status != ExecutionSucceeded {
			current.Execution.Status = ExecutionRunning
		}
		return nil
	})
	if err != nil || request.Execution.Status == ExecutionSucceeded {
		return request, err
	}
	for _, change := range request.Changes {
		if executionItemSucceeded(request.Execution, change.ID) {
			continue
		}
		resultID, executionErr := executeFleetChange(ctx, repository, lifecycle, request, change)
		item := ChangeExecutionItem{ChangeID: change.ID, ResourceRID: installationRID(request.Environment, change.InstallationID), Status: ExecutionSucceeded, ResultID: resultID}
		decision, reason := "ALLOW", "bulk action executed"
		if executionErr != nil {
			item.Status, item.Error = ExecutionFailed, executionErr.Error()
			decision, reason = "DENY", executionErr.Error()
		}
		request, err = repository.UpdateChangeRequest(ctx, request.ID, func(current *ChangeRequest) error {
			upsertExecutionItem(current.Execution, item)
			current.UpdatedAt = time.Now().UTC()
			return nil
		})
		governance.audit(ctx, request.Author.Username, "FLEET_BULK_"+change.Type, item.ResourceRID, decision, reason, map[string]any{"changeRequestId": request.ID})
		if err != nil {
			return request, err
		}
	}
	request, err = repository.UpdateChangeRequest(ctx, request.ID, func(current *ChangeRequest) error {
		current.Execution.Status = ExecutionSucceeded
		for _, item := range current.Execution.Items {
			if item.Status != ExecutionSucceeded {
				current.Execution.Status = ExecutionFailed
				break
			}
		}
		now := time.Now().UTC()
		current.Execution.CompletedAt = now
		current.UpdatedAt = now
		return nil
	})
	return request, err
}

func executeFleetChange(ctx context.Context, repository Repository, lifecycle *Lifecycle, request ChangeRequest, change EntityChange) (string, error) {
	reason, _ := change.Request["reason"].(string)
	actor := effectivePrincipal(request.Author).Username
	switch change.Type {
	case BulkSuppress:
		duration, _ := change.Request["durationSeconds"].(float64)
		var endsAt *time.Time
		if duration > 0 {
			end := time.Now().UTC().Add(time.Duration(duration) * time.Second)
			endsAt = &end
		}
		record, err := lifecycle.CreateManualSuppression(ctx, request.Environment, change.InstallationID, reason, actor, time.Time{}, endsAt)
		return record.ID, err
	case BulkUnsuppress:
		records, err := repository.ListSuppressions(ctx, request.Environment)
		if err != nil {
			return "", err
		}
		removed := []string{}
		for _, record := range records {
			if record.InstallationID == change.InstallationID && record.Active(time.Now().UTC()) {
				updated, removeErr := lifecycle.RemoveSuppression(ctx, record.ID, actor)
				if removeErr != nil {
					return strings.Join(removed, ","), removeErr
				}
				removed = append(removed, updated.ID)
			}
		}
		if len(removed) == 0 {
			return "", fmt.Errorf("%w: installation has no active suppression", ErrConflict)
		}
		return strings.Join(removed, ","), nil
	default:
		return "", fmt.Errorf("%w: unsupported fleet action %s", ErrInvalidTransition, change.Type)
	}
}

func executionItemSucceeded(execution *ChangeExecution, changeID string) bool {
	if execution == nil {
		return false
	}
	for _, item := range execution.Items {
		if item.ChangeID == changeID && item.Status == ExecutionSucceeded {
			return true
		}
	}
	return false
}

func upsertExecutionItem(execution *ChangeExecution, item ChangeExecutionItem) {
	for index := range execution.Items {
		if execution.Items[index].ChangeID == item.ChangeID {
			execution.Items[index] = item
			return
		}
	}
	execution.Items = append(execution.Items, item)
	sort.Slice(execution.Items, func(i, j int) bool { return execution.Items[i].ChangeID < execution.Items[j].ChangeID })
}
