package optimization

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const ShadowRolloutAPIVersion = "optimization.platform.example/shadow-rollout/v1alpha1"

// ShadowRolloutReceipt is an immutable, evidence-only join over the governed
// proposal path. It records linkage, not authority: it cannot create a Plan,
// approve a Change Request, lease work, or assert realized value.
type ShadowRolloutReceipt struct {
	APIVersion                 string `json:"apiVersion"`
	Kind                       string `json:"kind"`
	ID                         string `json:"id"`
	Mode                       string `json:"mode"`
	ProposalID                 string `json:"proposalId"`
	ApprovalRevalidationID     string `json:"approvalRevalidationId"`
	ApprovalAcquisitionID      string `json:"approvalAcquisitionId"`
	LastSafePointID            string `json:"lastSafePointId"`
	LastSafePointAcquisitionID string `json:"lastSafePointAcquisitionId"`
	ExecutionPlanID            string `json:"executionPlanId"`
	OutcomeID                  string `json:"outcomeId"`
	InfluencedPlan             bool   `json:"influencedPlan"`
	MutatedState               bool   `json:"mutatedState"`
	RealizedOutcomeStatus      string `json:"realizedOutcomeStatus"`
	RecordedAt                 string `json:"recordedAt"`
}

func NewShadowRolloutReceipt(outcome AdvisoryOutcomeRecord, recordedAt time.Time) (ShadowRolloutReceipt, error) {
	if err := VerifyAdvisoryOutcomeRecord(outcome); err != nil {
		return ShadowRolloutReceipt{}, fmt.Errorf("shadow rollout outcome: %w", err)
	}
	receipt := ShadowRolloutReceipt{
		APIVersion: ShadowRolloutAPIVersion, Kind: "ShadowRolloutReceipt", Mode: "SHADOW",
		ProposalID: outcome.Proposal.ID, ApprovalRevalidationID: outcome.ApprovalRevalidation.ID,
		ApprovalAcquisitionID: outcome.ApprovalAcquisition.ID, LastSafePointID: outcome.LastSafePointRevalidation.ID,
		LastSafePointAcquisitionID: outcome.LastSafePointAcquisition.ID, ExecutionPlanID: outcome.Execution.PlanID,
		OutcomeID: outcome.ID, InfluencedPlan: false, MutatedState: false,
		RealizedOutcomeStatus: SolverShadowRealizedUnknown, RecordedAt: recordedAt.UTC().Format(time.RFC3339Nano),
	}
	if err := validateShadowRolloutReceipt(receipt); err != nil {
		return ShadowRolloutReceipt{}, err
	}
	receipt.ID = shadowRolloutReceiptID(receipt)
	return receipt, nil
}

func VerifyShadowRolloutReceipt(receipt ShadowRolloutReceipt) error {
	if err := validateShadowRolloutReceipt(receipt); err != nil {
		return err
	}
	if shadowRolloutReceiptID(receipt) != receipt.ID {
		return errors.New("shadow rollout receipt id does not match content")
	}
	return nil
}

func validateShadowRolloutReceipt(receipt ShadowRolloutReceipt) error {
	if receipt.APIVersion != ShadowRolloutAPIVersion || receipt.Kind != "ShadowRolloutReceipt" || (receipt.ID != "" && !validContentID(receipt.ID, "shroll_")) || receipt.Mode != "SHADOW" || receipt.InfluencedPlan || receipt.MutatedState || receipt.RealizedOutcomeStatus != SolverShadowRealizedUnknown {
		return errors.New("unsupported or unsafe shadow rollout receipt")
	}
	for label, value := range map[string]string{"proposal": receipt.ProposalID, "approval revalidation": receipt.ApprovalRevalidationID, "approval acquisition": receipt.ApprovalAcquisitionID, "last-safe-point": receipt.LastSafePointID, "last-safe-point acquisition": receipt.LastSafePointAcquisitionID, "outcome": receipt.OutcomeID} {
		prefix := "optprop_"
		switch label {
		case "approval revalidation", "last-safe-point":
			prefix = "optrev_"
		case "approval acquisition", "last-safe-point acquisition":
			prefix = "acq_"
		case "outcome":
			prefix = "optout_"
		}
		if !validContentID(value, prefix) {
			return fmt.Errorf("shadow rollout has invalid %s identity", label)
		}
	}
	if !validObservationText(receipt.ExecutionPlanID) {
		return errors.New("shadow rollout has invalid execution Plan identity")
	}
	if _, err := time.Parse(time.RFC3339Nano, receipt.RecordedAt); err != nil {
		return errors.New("shadow rollout recordedAt is invalid")
	}
	return nil
}

func shadowRolloutReceiptID(receipt ShadowRolloutReceipt) string {
	receipt.ID = ""
	encoded, _ := json.Marshal(receipt)
	digest := sha256.Sum256(encoded)
	return "shroll_" + hex.EncodeToString(digest[:])
}

type FileShadowRolloutStore struct{ Directory string }

func (store FileShadowRolloutStore) Save(receipt ShadowRolloutReceipt) error {
	if err := VerifyShadowRolloutReceipt(receipt); err != nil {
		return err
	}
	return FileTwinStore(store).save("shadow-rollouts", receipt.ID, receipt)
}

func (store FileShadowRolloutStore) Get(id string) (ShadowRolloutReceipt, error) {
	var receipt ShadowRolloutReceipt
	if !validContentID(id, "shroll_") {
		return receipt, errors.New("invalid shadow rollout receipt id")
	}
	if err := FileTwinStore(store).get("shadow-rollouts", id, &receipt); err != nil {
		return receipt, err
	}
	return receipt, VerifyShadowRolloutReceipt(receipt)
}
