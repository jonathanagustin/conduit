package domain

import (
	"fmt"
	"reflect"
)

// PromotionLifecycle tracks an append-only run independently of eligibility.
// CONDUIT-INVENTED: catalog.PromotionLifecycle
// Displaced-by: A published Apollo promotion lifecycle API schema and transition contract, including cancellation reasons.
// Eligibility remains a separate, reproducible verdict. The lifecycle is an
// append-only run of evaluations: continuation names its exact predecessor.
type PromotionLifecycle string

const (
	PromotionRunning           PromotionLifecycle = "RUNNING"
	PromotionSucceeded         PromotionLifecycle = "SUCCEEDED"
	PromotionAborted           PromotionLifecycle = "ABORTED"
	PromotionCancelled         PromotionLifecycle = "CANCELLED"
	PromotionBlockerTerminated                    = "PROMOTION_TERMINATED"
)

func PromotionLifecycleFor(status PromotionStatus, blockers []PromotionBlocker) (PromotionLifecycle, string) {
	if status == PromotionPassed {
		return PromotionSucceeded, ""
	}
	for _, blocker := range blockers {
		if blocker.Kind == PromotionBlockerEvaluationTimedOut || blocker.Kind == PromotionBlockerCanaryRecallTriggered {
			return PromotionAborted, blocker.Message
		}
	}
	return PromotionRunning, ""
}

func ValidatePromotionContinuation(record PromotionRecord, existing []PromotionRecord) error {
	if record.RunID == "" {
		if record.PreviousEvaluationID != "" || record.Lifecycle != "" || record.LifecycleReason != "" || record.ValueTier != "" {
			return fmt.Errorf("lifecycle fields require a run")
		}
		return nil
	} // Legacy immutable decisions predate runs.
	if record.Lifecycle != PromotionRunning && record.Lifecycle != PromotionSucceeded && record.Lifecycle != PromotionAborted && record.Lifecycle != PromotionCancelled {
		return fmt.Errorf("invalid promotion lifecycle")
	}
	if (record.Lifecycle == PromotionSucceeded) != (record.Status == PromotionPassed) {
		return fmt.Errorf("lifecycle must agree with eligibility")
	}
	if (record.Lifecycle == PromotionCancelled || record.Lifecycle == PromotionAborted) && record.LifecycleReason == "" {
		return fmt.Errorf("terminal promotion needs a reason")
	}
	if record.PreviousEvaluationID == "" {
		for _, prior := range existing {
			if prior.RunID == record.RunID {
				return fmt.Errorf("promotion run already exists")
			}
		}
		return nil
	}
	var previous *PromotionRecord
	for index := range existing {
		prior := &existing[index]
		if prior.ID == record.PreviousEvaluationID {
			previous = prior
		}
		if prior.RunID == record.RunID && prior.PreviousEvaluationID == record.PreviousEvaluationID {
			return fmt.Errorf("promotion already continued from this evaluation")
		}
	}
	if previous == nil || previous.RunID != record.RunID {
		return fmt.Errorf("promotion predecessor not found in run")
	}
	if previous.Lifecycle != PromotionRunning {
		return fmt.Errorf("promotion run is already terminal")
	}
	if previous.Product != record.Product || previous.Version != record.Version || previous.FromChannel != record.FromChannel || previous.ToChannel != record.ToChannel || !previous.EvaluationStartedAt.Equal(record.EvaluationStartedAt) {
		return fmt.Errorf("promotion continuation must preserve run coordinates and start time")
	}
	if !reflect.DeepEqual(previous.Policy, record.Policy) {
		return fmt.Errorf("promotion policy changed within a run")
	}
	if record.EvaluatedAt.Before(previous.EvaluatedAt) {
		return fmt.Errorf("promotion evaluation predates predecessor")
	}
	return nil
}
