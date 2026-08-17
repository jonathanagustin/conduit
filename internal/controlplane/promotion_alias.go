package controlplane

import (
	"fmt"
	"time"

	catalog "github.com/jonathanagustin/conduit/internal/catalog/domain"
)

// Deciding a promotion is Catalog's. What is left here is everything that had
// to touch a store: loading the policy and the evidence, refusing a paused
// stage, minting the record and persisting it.
//
// The split follows what each half depends on. Whether a Release satisfies its
// label contract, which health evidence a stage will accept, how long a state
// must have held, and what a canary set has to show are decidable from a policy
// and a slice of evidence, and are now testable without a repository. Whether
// the operator has paused this channel edge is a fact about persisted state and
// stays where the state is.
type (
	PromotionStatus             = catalog.PromotionStatus
	PromotionStageControlStatus = catalog.PromotionStageControlStatus
	PromotionStageControl       = catalog.PromotionStageControl
	PromotionPolicy             = catalog.PromotionPolicy
	PromotionHealthEvidence     = catalog.PromotionHealthEvidence
	PromotionEvaluationRequest  = catalog.PromotionEvaluationRequest
	PromotionBlocker            = catalog.PromotionBlocker
	PromotionRecord             = catalog.PromotionRecord
	PromotionLabelRequirement   = catalog.PromotionLabelRequirement
	PromotionEntityFilter       = catalog.PromotionEntityFilter
	PromotionCondition          = catalog.PromotionCondition
)

const (
	PromotionPassed  = catalog.PromotionPassed
	PromotionBlocked = catalog.PromotionBlocked

	PromotionStageActive = catalog.PromotionStageActive
	PromotionStagePaused = catalog.PromotionStagePaused

	SecurityScannedLabel     = catalog.SecurityScannedLabel
	SecurityScanOutcomeLabel = catalog.SecurityScanOutcomeLabel

	PromotionTimed  = catalog.PromotionTimed
	PromotionCanary = catalog.PromotionCanary

	PromoteWhenHealthy  = catalog.PromoteWhenHealthy
	RecallWhenUnhealthy = catalog.RecallWhenUnhealthy

	PromotionBlockerRequiredLabelMissing    = catalog.PromotionBlockerRequiredLabelMissing
	PromotionBlockerRequiredLabelNotAllowed = catalog.PromotionBlockerRequiredLabelNotAllowed

	PromotionBlockerEvidenceDuplicate           = catalog.PromotionBlockerEvidenceDuplicate
	PromotionBlockerEvidenceInvalid             = catalog.PromotionBlockerEvidenceInvalid
	PromotionBlockerEvidenceStale               = catalog.PromotionBlockerEvidenceStale
	PromotionBlockerEvidenceHealthySinceInvalid = catalog.PromotionBlockerEvidenceHealthySinceInvalid
	PromotionBlockerEvidenceHealthUnknown       = catalog.PromotionBlockerEvidenceHealthUnknown

	PromotionBlockerEnvironmentCannotVouch = catalog.PromotionBlockerEnvironmentCannotVouch
	PromotionBlockerEvidenceBoundConflict  = catalog.PromotionBlockerEvidenceBoundConflict

	PromotionBlockerEvaluationWindowInvalid = catalog.PromotionBlockerEvaluationWindowInvalid
	PromotionBlockerEvaluationTimedOut      = catalog.PromotionBlockerEvaluationTimedOut
	PromotionBlockerHealthyBelowMinimum     = catalog.PromotionBlockerHealthyBelowMinimum
	PromotionBlockerUnhealthyAboveMaximum   = catalog.PromotionBlockerUnhealthyAboveMaximum

	PromotionBlockerCanaryNoConditions        = catalog.PromotionBlockerCanaryNoConditions
	PromotionBlockerCanaryNoEvidence          = catalog.PromotionBlockerCanaryNoEvidence
	PromotionBlockerCanaryTooFewEntities      = catalog.PromotionBlockerCanaryTooFewEntities
	PromotionBlockerCanaryShareBelowThreshold = catalog.PromotionBlockerCanaryShareBelowThreshold
	PromotionBlockerCanaryRecallTriggered     = catalog.PromotionBlockerCanaryRecallTriggered
	PromotionBlockerCanaryUnsupportedAction   = catalog.PromotionBlockerCanaryUnsupportedAction
	PromotionBlockerCanaryNoConditionMet      = catalog.PromotionBlockerCanaryNoConditionMet
)

func sortBlockers(blockers []PromotionBlocker) { catalog.SortBlockers(blockers) }

func blockersOfKind(blockers []PromotionBlocker, kind string) []PromotionBlocker {
	return catalog.BlockersOfKind(blockers, kind)
}

func promotionBlockersEqual(left, right []PromotionBlocker) bool {
	return catalog.PromotionBlockersEqual(left, right)
}

func blockerMessages(blockers []PromotionBlocker) []string { return catalog.BlockerMessages(blockers) }

func evaluatePromotion(request PromotionEvaluationRequest, now time.Time) []PromotionBlocker {
	return catalog.EvaluatePromotion(request, now)
}

func promotionChannelReachable(promotions []PromotionRecord, product, version, from, to string) bool {
	return catalog.PromotionChannelReachable(promotions, product, version, from, to)
}

func promotionIdentity(record PromotionRecord) (string, error) {
	return catalog.PromotionIdentity(record)
}

func defaultPromotionStageControl(product, fromChannel, toChannel string) PromotionStageControl {
	return catalog.DefaultPromotionStageControl(product, fromChannel, toChannel)
}

func promotionStageControlID(product, fromChannel, toChannel string) string {
	return catalog.PromotionStageControlID(product, fromChannel, toChannel)
}

func stageType(policy PromotionPolicy) string { return catalog.StageType(policy) }

// validatePromotionStage wraps Catalog's reason in this package's vocabulary.
// Catalog returns the reason alone, deliberately: which error class an invalid
// stage belongs to is a caller's decision, and out here it has always been an
// invalid transition and a 409. The rendered message is unchanged.
func validatePromotionStage(policy PromotionPolicy) error {
	if err := catalog.ValidateStage(policy); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidTransition, err)
	}
	return nil
}
