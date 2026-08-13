package optimization

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestAdvisoryOutcomePreservesUnknownsUntilAuthoritativeEvidenceExists(t *testing.T) {
	proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, recordedAt := testOptimizerOutcomeChain(t)
	record, err := NewAdvisoryOutcomeRecord(proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, nil, nil, nil, recordedAt)
	if err != nil {
		t.Fatal(err)
	}
	if record.Schedule.Status != OutcomeMeasurementUnknown || record.Cost.Status != OutcomeMeasurementUnknown || record.ValueStatus != OutcomeMeasurementUnknown || len(record.Unknowns) != 3 || record.MutatedState {
		t.Fatalf("unknown outcome = %#v", record)
	}
	if err := VerifyAdvisoryOutcomeRecord(record); err != nil {
		t.Fatal(err)
	}
	store := FileAdvisoryOutcomeStore{Directory: filepath.Join(t.TempDir(), "outcomes")}
	if err := store.Save(record); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Get(record.ID)
	if err != nil || loaded.ID != record.ID {
		t.Fatalf("loaded outcome = %#v, %v", loaded, err)
	}
}

// This is the local Phase 4 shadow-rollout gate. It intentionally exercises
// the complete evidence chain without invoking a control-plane client:
// recommendation/compiler evidence -> immutable proposal -> approval and
// last-safe-point revalidation -> terminal Plan execution evidence -> outcome.
// The test must remain fixture-only; live provider or Plan authority belongs
// to the separately authorized control-plane integration boundary.
func TestAdvisoryShadowRolloutLinksRecommendationThroughOutcome(t *testing.T) {
	proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, recordedAt := testOptimizerOutcomeChain(t)
	if proposal.Mode != "ADVISORY" || proposal.InfluencedPlan || proposal.MutatedState {
		t.Fatalf("proposal crossed advisory boundary: %#v", proposal)
	}
	if approval.Verdict != ProposalRevalidationReady || lastSafePoint.Verdict != ProposalRevalidationReady {
		t.Fatalf("revalidation chain is not approved: approval=%#v lastSafe=%#v", approval, lastSafePoint)
	}
	if approval.AcquisitionReceiptID != approvalAcquisition.ID || lastSafePoint.AcquisitionReceiptID != lastSafePointAcquisition.ID {
		t.Fatal("revalidation records are not bound to their acquisition receipts")
	}
	if execution.Status != "SUCCEEDED" || execution.PlanID == "" {
		t.Fatalf("terminal execution evidence is incomplete: %#v", execution)
	}
	outcome, err := NewAdvisoryOutcomeRecord(
		proposal,
		approval,
		approvalAcquisition,
		lastSafePoint,
		lastSafePointAcquisition,
		execution,
		nil,
		nil,
		nil,
		recordedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyAdvisoryOutcomeRecord(outcome); err != nil {
		t.Fatal(err)
	}
	if outcome.Proposal.ID != proposal.ID || outcome.ApprovalRevalidation.ID != approval.ID || outcome.LastSafePointRevalidation.ID != lastSafePoint.ID || outcome.Execution.PlanID != execution.PlanID {
		t.Fatalf("outcome lost chain linkage: %#v", outcome)
	}
	if outcome.Schedule.Status != OutcomeMeasurementUnknown || outcome.Cost.Status != OutcomeMeasurementUnknown || outcome.ValueStatus != OutcomeMeasurementUnknown {
		t.Fatalf("fixture shadow rollout manufactured realized value: %#v", outcome)
	}
	receipt, err := NewShadowRolloutReceipt(outcome, recordedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyShadowRolloutReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ProposalID != proposal.ID || receipt.ExecutionPlanID != execution.PlanID || receipt.RealizedOutcomeStatus != SolverShadowRealizedUnknown {
		t.Fatalf("shadow rollout linkage = %#v", receipt)
	}
	forged := receipt
	forged.InfluencedPlan = true
	forged.ID = shadowRolloutReceiptID(forged)
	if err := VerifyShadowRolloutReceipt(forged); err == nil {
		t.Fatal("accepted shadow receipt claiming proposal influence")
	}
}

func TestAdvisoryOutcomeMeasuresExactScheduleAndAuthoritativeBilledCost(t *testing.T) {
	proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, recordedAt := testOptimizerOutcomeChain(t)
	_, assignments, err := proposalSchedulePrediction(proposal)
	if err != nil {
		t.Fatal(err)
	}
	schedule := OptimizerScheduleOutcomeEvidence{Source: "platform-plan-execution", Revision: "plan-revision:7", ObservedAt: execution.FinishedAt}
	index := 0
	for workID, target := range assignments {
		start := time.Date(2026, 8, 12, 12, 1, index, 0, time.UTC)
		schedule.Assignments = append(schedule.Assignments, OptimizerObservedAssignment{WorkID: workID, Target: target, StartedAt: start.Format(time.RFC3339Nano), FinishedAt: start.Add(2 * time.Second).Format(time.RFC3339Nano)})
		index++
	}
	fact, err := NewTemporalFact(TemporalFact{
		EntityID: execution.PlanID, Attribute: "cost.realized-total-micros", Value: json.RawMessage(`30`), Unit: "USD",
		Plane: StateBilled, KnowledgeStatus: KnowledgeKnown, ValidFrom: execution.FinishedAt, RecordedAt: recordedAt.Format(time.RFC3339Nano),
		Source: "provider-invoice", SourceRevision: "invoice:17", RequestDigest: digest('a'), ResponseDigest: digest('b'), CollectorIdentity: "collector:billing", Fidelity: FidelityExact,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := NewAdvisoryOutcomeRecord(proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, &schedule, &fact, nil, recordedAt)
	if err != nil {
		t.Fatal(err)
	}
	if record.Schedule.Status != OutcomeMeasurementKnown || record.Cost.Status != OutcomeMeasurementKnown || record.Cost.Realized != "30" || record.ValueStatus != OutcomeMeasurementUnknown || len(record.Unknowns) != 1 {
		t.Fatalf("measured outcome = %#v", record)
	}
	tampered := record
	tampered.Cost.Realized = "0"
	if err := VerifyAdvisoryOutcomeRecord(tampered); err == nil {
		t.Fatal("accepted tampered realized cost")
	}
}

func TestAdvisoryOutcomeRejectsEstimatedCostAsBillingAndGovernanceReplay(t *testing.T) {
	proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, recordedAt := testOptimizerOutcomeChain(t)
	fact, err := NewTemporalFact(TemporalFact{
		EntityID: proposal.Environment, Attribute: "cost.realized-total-micros", Value: json.RawMessage(`30`), Unit: "USD",
		Plane: StateObserved, KnowledgeStatus: KnowledgeKnown, ValidFrom: execution.FinishedAt, RecordedAt: recordedAt.Format(time.RFC3339Nano),
		Source: "opencost", SourceRevision: "allocation:17", RequestDigest: digest('a'), ResponseDigest: digest('b'), CollectorIdentity: "collector:cost", Fidelity: FidelityEstimated, Uncertainty: "modeled allocation",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAdvisoryOutcomeRecord(proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, nil, &fact, nil, recordedAt); err == nil {
		t.Fatal("accepted observed cost estimate as authoritative billing")
	}
	oldEnvironmentTotal, err := NewTemporalFact(TemporalFact{
		EntityID: proposal.Environment, Attribute: "cost.realized-total-micros", Value: json.RawMessage(`30`), Unit: "USD",
		Plane: StateBilled, KnowledgeStatus: KnowledgeKnown, ValidFrom: execution.FinishedAt, RecordedAt: recordedAt.Format(time.RFC3339Nano),
		Source: "provider-invoice", SourceRevision: "invoice:17", RequestDigest: digest('c'), ResponseDigest: digest('d'), CollectorIdentity: "collector:billing", Fidelity: FidelityExact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewAdvisoryOutcomeRecord(proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, nil, &oldEnvironmentTotal, nil, recordedAt); err == nil {
		t.Fatal("accepted an Environment-wide billed total as one Plan's realized cost")
	}
	drift := lastSafePoint
	drift.Verdict = ProposalRevalidationDrift
	if _, err := NewAdvisoryOutcomeRecord(proposal, approval, approvalAcquisition, drift, lastSafePointAcquisition, execution, nil, nil, nil, recordedAt); err == nil {
		t.Fatal("accepted execution linked to tampered revalidation")
	}
	foreignAcquisition := lastSafePointAcquisition
	foreignAcquisition.ID = approvalAcquisition.ID
	if _, err := NewAdvisoryOutcomeRecord(proposal, approval, approvalAcquisition, lastSafePoint, foreignAcquisition, execution, nil, nil, nil, recordedAt); err == nil {
		t.Fatal("accepted execution linked to foreign acquisition receipt")
	}
}

func testOptimizerOutcomeChain(t *testing.T) (AdvisoryPlanProposal, ProposalRevalidation, AcquisitionExecutionReceipt, ProposalRevalidation, AcquisitionExecutionReceipt, OptimizerPlanExecutionEvidence, time.Time) {
	t.Helper()
	proposal, _, facts := testAdvisoryPlanProposal(t)
	return testOptimizerOutcomeChainForProposal(t, proposal, facts)
}

func testOptimizerOutcomeChainForProposal(t *testing.T, proposal AdvisoryPlanProposal, _ []TemporalFact) (AdvisoryPlanProposal, ProposalRevalidation, AcquisitionExecutionReceipt, ProposalRevalidation, AcquisitionExecutionReceipt, OptimizerPlanExecutionEvidence, time.Time) {
	t.Helper()
	validAt := time.Date(2026, 8, 12, 12, 0, 8, 0, time.UTC)
	approvalRevalidator, approvalStore := testProposalRevalidator(t, proposal, validAt)
	approval, err := approvalRevalidator.Execute(context.Background(), proposal, ProposalRevalidationApproval)
	if err != nil {
		t.Fatal(err)
	}
	approvalAcquisition, err := approvalStore.GetAcquisitionReceipt(approval.AcquisitionReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	lastSafeRevalidator, lastSafeStore := testProposalRevalidator(t, proposal, validAt.Add(2*time.Second))
	lastSafePoint, err := lastSafeRevalidator.Execute(context.Background(), proposal, ProposalRevalidationLastSafePoint)
	if err != nil {
		t.Fatal(err)
	}
	lastSafePointAcquisition, err := lastSafeStore.GetAcquisitionReceipt(lastSafePoint.AcquisitionReceiptID)
	if err != nil {
		t.Fatal(err)
	}
	execution := OptimizerPlanExecutionEvidence{
		PlanID: "plan_test", Status: "SUCCEEDED", Revision: 7,
		StartedAt: validAt.Add(4 * time.Second).Format(time.RFC3339Nano), FinishedAt: validAt.Add(2 * time.Minute).Format(time.RFC3339Nano),
		TaskCount: 3, SucceededTaskCount: 3,
	}
	return proposal, approval, approvalAcquisition, lastSafePoint, lastSafePointAcquisition, execution, validAt.Add(3 * time.Minute)
}
