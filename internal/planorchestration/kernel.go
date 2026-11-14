package planorchestration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

// LifecycleKernel binds workflow activities to kernel methods. Confirm and
// wave execution read the Plan. Compensate cancels through the lifecycle.
// None of these methods approve or write reported state.
type LifecycleKernel struct {
	Lifecycle  *controlplane.Lifecycle
	Repository controlplane.Repository
}

func (kernel LifecycleKernel) ConfirmApproval(ctx context.Context, planID, approvalID string) error {
	key := auditID("confirm", planID, approvalID, 0)
	if done, err := kernel.alreadyAllowed(ctx, key); err != nil || done {
		return err
	}
	record, err := kernel.plan(ctx, planID)
	if err != nil {
		return err
	}
	for _, approval := range record.Approvals {
		if approval.Subject == approvalID {
			return kernel.record(ctx, key, "plan.confirm", planID, governance.DecisionAllow, "governance record matches the plan")
		}
	}
	return fmt.Errorf("governance record %s is not on plan %s", approvalID, planID)
}

func (kernel LifecycleKernel) ExecuteWave(ctx context.Context, request WaveRequest) error {
	if request.PlanID == "" || request.Wave == "" || request.Attempt < 1 {
		return fmt.Errorf("wave key must include plan, wave, and attempt")
	}
	key := auditID("wave", request.PlanID, request.Wave, request.Attempt)
	if done, err := kernel.alreadyAllowed(ctx, key); err != nil || done {
		return err
	}
	record, err := kernel.plan(ctx, request.PlanID)
	if err != nil {
		return err
	}
	switch record.Status {
	case controlplane.PlanApproved, controlplane.PlanLeased, controlplane.PlanRunning:
		return kernel.record(ctx, key, "plan.wave", request.PlanID, governance.DecisionAllow, "wave "+request.Wave+" attempt "+strconv.Itoa(request.Attempt))
	default:
		return fmt.Errorf("plan %s is %s and wave %s attempt %d cannot run", request.PlanID, record.Status, request.Wave, request.Attempt)
	}
}

func (kernel LifecycleKernel) Compensate(ctx context.Context, planID, reason string) error {
	if kernel.Lifecycle == nil {
		return fmt.Errorf("lifecycle is required")
	}
	key := auditID("compensate", planID, "", 0)
	if done, err := kernel.alreadyAllowed(ctx, key); err != nil || done {
		return err
	}
	record, err := kernel.plan(ctx, planID)
	if err != nil {
		return err
	}
	if record.Status != controlplane.PlanCancelled {
		if _, err := kernel.Lifecycle.CancelPlan(ctx, planID, "plan-orchestrator", reason); err != nil {
			return err
		}
	}
	return kernel.record(ctx, key, "plan.compensate", planID, governance.DecisionAllow, reason)
}

func auditID(action, planID, wave string, attempt int) string {
	sum := sha256.Sum256([]byte(action + "\x00" + planID + "\x00" + wave + "\x00" + strconv.Itoa(attempt)))
	return "aud_plan_" + hex.EncodeToString(sum[:16])
}

func (kernel LifecycleKernel) alreadyAllowed(ctx context.Context, id string) (bool, error) {
	if kernel.Repository == nil {
		return false, fmt.Errorf("repository is required")
	}
	events, err := kernel.Repository.ListAuthorizationAudit(ctx, "")
	if err != nil {
		return false, err
	}
	for _, event := range events {
		if event.ID == id && event.Decision == governance.DecisionAllow {
			return true, nil
		}
	}
	return false, nil
}

func (kernel LifecycleKernel) record(ctx context.Context, id, action, planID, decision, reason string) error {
	if kernel.Repository == nil {
		return fmt.Errorf("repository is required")
	}
	resource := "plan/" + planID
	event := governance.AuthorizationAuditEvent{
		ID: id, SchemaVersion: governance.AuditSchemaV3,
		Categories: []string{governance.AuditCategoryAuthorizationCheck},
		Subject:    "plan-orchestrator", Action: action, ResourceRID: resource,
		Decision: decision, Reason: reason, CreatedAt: time.Unix(0, 0).UTC(),
		RequestFields: map[string]any{
			"authorizationCheckTargets":    []string{resource},
			"authorizationCheckOperations": []string{action},
		},
		ResultFields: map[string]any{
			"authorizationCheckSucceededTargets": []string{resource},
			"authorizationCheckFailedTargets":    []string{},
			"authorizationCheckResultMessage":    reason,
		},
	}
	if decision != governance.DecisionAllow {
		event.ResultFields["authorizationCheckSucceededTargets"] = []string{}
		event.ResultFields["authorizationCheckFailedTargets"] = []string{resource}
	}
	_, err := kernel.Repository.AppendAuthorizationAudit(ctx, event)
	return err
}

func (kernel LifecycleKernel) plan(ctx context.Context, planID string) (controlplane.PlanRecord, error) {
	if kernel.Repository == nil {
		return controlplane.PlanRecord{}, fmt.Errorf("repository is required")
	}
	return kernel.Repository.GetPlan(ctx, planID)
}
