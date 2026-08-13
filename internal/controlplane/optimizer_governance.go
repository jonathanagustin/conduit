package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jonathanagustin/conduit/internal/optimization"
)

const (
	optimizerDesiredStateSchema     = "platform.desired-state.v1"
	optimizerRevalidationMaximumAge = 30 * time.Second
)

// ProposeOptimizerPlan is the only optimizer-to-governance translation. It
// imports a strict DesiredState document, supplies the authenticated author in
// the control plane, and delegates to the same Plan and Change Request policy
// path as an operator proposal. The optimizer cannot invoke this method on its
// own because it has no control-plane dependency, identity, or route.
func (service *GovernanceService) ProposeOptimizerPlan(
	ctx context.Context, proposal optimization.AdvisoryPlanProposal, author Principal, title, description string,
) (PlanRecord, ChangeRequest, error) {
	if err := optimization.VerifyAdvisoryPlanProposal(proposal); err != nil {
		return PlanRecord{}, ChangeRequest{}, fmt.Errorf("verify optimizer proposal: %w", err)
	}
	state, err := decodeOptimizerDesiredState(proposal)
	if err != nil {
		return PlanRecord{}, ChangeRequest{}, err
	}
	requestID, receiptID := optimizerSolverCoordinates(proposal.Solver)
	provenance := &OptimizerPlanProvenance{
		ProposalID: proposal.ID, SnapshotID: proposal.Snapshot.ID,
		SolverRequestID: requestID, SolverReceiptID: receiptID, DocumentDigest: proposal.Document.Digest,
	}
	return service.proposePlan(ctx, state, author, title, description, provenance)
}

// ApproveOptimizerProposal requires a fresh READY approval-stage receipt before
// entering the ordinary reviewer/policy path. The receipt satisfies only the
// synthetic optimizer-revalidation requirement; it cannot satisfy any
// team/resource/policy reviewer requirement.
func (service *GovernanceService) ApproveOptimizerProposal(
	ctx context.Context, changeRequestID string, proposal optimization.AdvisoryPlanProposal,
	revalidation optimization.ProposalRevalidation, acquisition optimization.AcquisitionExecutionReceipt,
	principal Principal, rationale string,
) (ChangeRequest, PlanRecord, error) {
	request, err := service.repository.GetChangeRequest(ctx, changeRequestID)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if request.PlanID == "" {
		return ChangeRequest{}, PlanRecord{}, ErrInvalidTransition
	}
	plan, err := service.repository.GetPlan(ctx, request.PlanID)
	if err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if err := verifyOptimizerPlanBinding(plan, proposal); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	if err := verifyOptimizerRevalidation(proposal, revalidation, acquisition, optimization.ProposalRevalidationApproval, service.now().UTC()); err != nil {
		return ChangeRequest{}, PlanRecord{}, err
	}
	evaluatedAt, _ := time.Parse(time.RFC3339Nano, revalidation.EvaluatedAt)
	return service.approve(ctx, changeRequestID, principal, rationale, &optimizerApprovalPermit{
		planID: plan.ID, proposalID: proposal.ID, revalidationID: revalidation.ID, acquisitionID: acquisition.ID,
		evaluatedAt: evaluatedAt,
	})
}

// LeaseNextOptimizerPlan is an explicit last-safe-point path. Generic leasing
// skips optimizer-originated Plans, so a missing, foreign, stale, drifted, or
// conflicting receipt can never fall through to ordinary execution.
func (lifecycle *Lifecycle) LeaseNextOptimizerPlan(
	ctx context.Context, planID, agentID string, duration time.Duration,
	proposal optimization.AdvisoryPlanProposal, revalidation optimization.ProposalRevalidation,
	acquisition optimization.AcquisitionExecutionReceipt,
) (PlanRecord, error) {
	plan, err := lifecycle.repository.GetPlan(ctx, planID)
	if err != nil {
		return PlanRecord{}, err
	}
	if err := verifyOptimizerPlanBinding(plan, proposal); err != nil {
		return PlanRecord{}, err
	}
	if plan.Optimizer.ApprovalRevalidationID == "" {
		return PlanRecord{}, fmt.Errorf("%w: optimizer Plan lacks approval revalidation", ErrLeaseRejected)
	}
	if err := verifyOptimizerRevalidation(proposal, revalidation, acquisition, optimization.ProposalRevalidationLastSafePoint, lifecycle.now().UTC()); err != nil {
		return PlanRecord{}, fmt.Errorf("%w: %v", ErrLeaseRejected, err)
	}
	evaluatedAt, _ := time.Parse(time.RFC3339Nano, revalidation.EvaluatedAt)
	leased, err := lifecycle.leaseNextPlan(ctx, plan.Plan.Environment, agentID, duration, &optimizerLeasePermit{
		planID: plan.ID, proposalID: proposal.ID, revalidationID: revalidation.ID, acquisitionID: acquisition.ID,
		evaluatedAt: evaluatedAt,
	})
	if errors.Is(err, ErrNoPlanAvailable) {
		return PlanRecord{}, fmt.Errorf("%w: optimizer Plan was no longer eligible at the lease write", ErrLeaseRejected)
	}
	return leased, err
}

func verifyOptimizerRevalidation(proposal optimization.AdvisoryPlanProposal, record optimization.ProposalRevalidation, acquisition optimization.AcquisitionExecutionReceipt, stage string, now time.Time) error {
	if err := optimization.VerifyAuthoritativeProposalRevalidation(proposal, record, acquisition); err != nil {
		return fmt.Errorf("verify optimizer revalidation: %w", err)
	}
	if record.Stage != stage || record.Verdict != optimization.ProposalRevalidationReady {
		return errors.New("optimizer revalidation is not READY for the required stage")
	}
	if stage == optimization.ProposalRevalidationApproval && record.Leaseable {
		return errors.New("approval revalidation cannot grant leaseability")
	}
	if stage == optimization.ProposalRevalidationLastSafePoint && !record.Leaseable {
		return errors.New("last-safe-point revalidation did not grant leaseability")
	}
	evaluatedAt, err := time.Parse(time.RFC3339Nano, record.EvaluatedAt)
	if err != nil || !optimizerEvidenceFreshAt(evaluatedAt, now) {
		return errors.New("optimizer revalidation is stale or future-dated at the control-plane action")
	}
	return nil
}

func optimizerEvidenceFreshAt(evaluatedAt, now time.Time) bool {
	return !evaluatedAt.IsZero() && !now.IsZero() && !evaluatedAt.After(now) && now.Sub(evaluatedAt) <= optimizerRevalidationMaximumAge
}

func verifyOptimizerPlanBinding(plan PlanRecord, proposal optimization.AdvisoryPlanProposal) error {
	if err := optimization.VerifyAdvisoryPlanProposal(proposal); err != nil {
		return err
	}
	requestID, receiptID := optimizerSolverCoordinates(proposal.Solver)
	want := OptimizerPlanProvenance{
		ProposalID: proposal.ID, SnapshotID: proposal.Snapshot.ID, SolverRequestID: requestID,
		SolverReceiptID: receiptID, DocumentDigest: proposal.Document.Digest,
	}
	if plan.Optimizer == nil || plan.Optimizer.ProposalID != want.ProposalID || plan.Optimizer.SnapshotID != want.SnapshotID ||
		plan.Optimizer.SolverRequestID != want.SolverRequestID || plan.Optimizer.SolverReceiptID != want.SolverReceiptID ||
		plan.Optimizer.DocumentDigest != want.DocumentDigest || plan.DesiredState == nil {
		return errors.New("Plan does not bind the optimizer proposal evidence")
	}
	encoded, err := json.Marshal(plan.DesiredState)
	if err != nil {
		return err
	}
	canonical, err := canonicalOptimizerJSON(encoded)
	if err != nil || !bytes.Equal(canonical, proposal.Document.Value) {
		return errors.New("Plan desired state does not match the optimizer proposal document")
	}
	return nil
}

func decodeOptimizerDesiredState(proposal optimization.AdvisoryPlanProposal) (DesiredState, error) {
	if proposal.Document.Schema != optimizerDesiredStateSchema {
		return DesiredState{}, errors.New("optimizer proposal uses an unsupported desired-state schema")
	}
	decoder := json.NewDecoder(bytes.NewReader(proposal.Document.Value))
	decoder.DisallowUnknownFields()
	var state DesiredState
	if err := decoder.Decode(&state); err != nil {
		return DesiredState{}, fmt.Errorf("decode optimizer desired state: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return DesiredState{}, errors.New("optimizer desired state has trailing input")
	}
	if state.Environment.ID != proposal.Environment {
		return DesiredState{}, errors.New("optimizer desired state targets a foreign Environment")
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return DesiredState{}, err
	}
	canonical, err := canonicalOptimizerJSON(encoded)
	if err != nil || !bytes.Equal(canonical, proposal.Document.Value) {
		return DesiredState{}, errors.New("optimizer desired state is not an exact known-schema document")
	}
	return state, nil
}

func canonicalOptimizerJSON(value []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("JSON has trailing input")
	}
	return json.Marshal(decoded)
}

func optimizerSolverCoordinates(evidence optimization.ProposalSolverEvidence) (string, string) {
	if evidence.Discrete != nil {
		return evidence.Discrete.Request.ID, evidence.Discrete.Receipt.ID
	}
	return "", ""
}

// BuildOptimizerOutcomeRecord is the read-only control-plane adapter from a
// terminal Plan record into immutable optimizer outcome evidence. It derives
// lifecycle status and task counts from the authoritative Plan; optional
// schedule, billing, and value evidence remains independently verified by the
// optimization domain.
func BuildOptimizerOutcomeRecord(
	plan PlanRecord,
	proposal optimization.AdvisoryPlanProposal,
	approval optimization.ProposalRevalidation,
	approvalAcquisition optimization.AcquisitionExecutionReceipt,
	lastSafePoint optimization.ProposalRevalidation,
	lastSafePointAcquisition optimization.AcquisitionExecutionReceipt,
	schedule *optimization.OptimizerScheduleOutcomeEvidence,
	billedCost *optimization.TemporalFact,
	value *optimization.OptimizerValueOutcomeEvidence,
	recordedAt time.Time,
) (optimization.AdvisoryOutcomeRecord, error) {
	if err := verifyOptimizerPlanBinding(plan, proposal); err != nil {
		return optimization.AdvisoryOutcomeRecord{}, err
	}
	if plan.Optimizer.ApprovalRevalidationID != approval.ID || plan.Optimizer.ApprovalAcquisitionID != approvalAcquisition.ID ||
		plan.Optimizer.LastSafePointRevalidationID != lastSafePoint.ID || plan.Optimizer.LastSafePointAcquisitionID != lastSafePointAcquisition.ID {
		return optimization.AdvisoryOutcomeRecord{}, errors.New("Plan does not bind the supplied optimizer revalidations")
	}
	if plan.Status != PlanSucceeded && plan.Status != PlanFailed && plan.Status != PlanCancelled {
		return optimization.AdvisoryOutcomeRecord{}, errors.New("optimizer outcome requires a terminal Plan")
	}
	if plan.Execution == nil {
		return optimization.AdvisoryOutcomeRecord{}, errors.New("optimizer outcome requires Plan execution evidence")
	}
	execution := optimization.OptimizerPlanExecutionEvidence{
		PlanID: plan.ID, Status: string(plan.Status), Revision: plan.Revision,
		StartedAt: plan.Execution.StartedAt.UTC().Format(time.RFC3339Nano), FinishedAt: plan.Execution.FinishedAt.UTC().Format(time.RFC3339Nano),
		TaskCount: len(plan.Execution.Tasks),
	}
	for _, task := range plan.Execution.Tasks {
		switch task.Status {
		case ExecutionTaskSucceeded:
			execution.SucceededTaskCount++
		case ExecutionTaskFailed, ExecutionTaskRollbackFailed:
			execution.FailedTaskCount++
		}
	}
	return optimization.NewAdvisoryOutcomeRecord(proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, schedule, billedCost, value, recordedAt)
}
