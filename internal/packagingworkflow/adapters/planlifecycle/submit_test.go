package planlifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/adapters"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

func TestSubmitProposesOneUnauthoredPlan(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	ledger := openLedger(t)
	submitter := New(repository, ledger)
	request := sampleRequest()
	first, err := submitter.Submit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := submitter.Submit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.PlanID == "" || second.PlanID != first.PlanID || !second.Reconciled {
		t.Fatalf("first = %#v second = %#v", first, second)
	}
	plans, err := repository.ListPlans(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 {
		t.Fatalf("plans = %d", len(plans))
	}
	plan := plans[0]
	if plan.Status != controlplane.PlanAwaitingApproval || plan.Lease != nil || len(plan.Approvals) != 0 || plan.Execution != nil {
		t.Fatalf("plan was approved or leased: %#v", plan)
	}
	if plan.DesiredState == nil || plan.DesiredState.Releases[0].Chart.Digest != request.ArtifactDigest {
		t.Fatalf("plan does not carry the proposal digest: %#v", plan.DesiredState)
	}
}

func TestASecondRunReusesTheSamePlan(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	submitter := New(repository, openLedger(t))
	first, err := submitter.Submit(context.Background(), sampleRequest())
	if err != nil {
		t.Fatal(err)
	}
	again := sampleRequest()
	again.RunID = "sample-app-2"
	second, err := submitter.Submit(context.Background(), again)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := repository.ListPlans(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || second.PlanID == "" || second.PlanID != first.PlanID {
		t.Fatalf("plans = %d first = %#v second = %#v", len(plans), first, second)
	}
	if plans[0].Status != controlplane.PlanAwaitingApproval || plans[0].Lease != nil || len(plans[0].Approvals) != 0 {
		t.Fatalf("reused plan changed state: %#v", plans[0])
	}
}

func TestSuppressionBlocksBeforeAPlanExists(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	now := time.Now().UTC().Add(-time.Minute)
	if _, err := repository.CreateSuppression(context.Background(), controlplane.SuppressionRecord{
		ID: "sup-env-dev", Environment: "env-dev", Scope: controlplane.SuppressionEnvironment,
		Source: controlplane.SuppressionManual, Reason: "maintenance", CreatedBy: "operator",
		StartsAt: now, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	submitter := New(repository, openLedger(t))
	if _, err := submitter.Submit(context.Background(), sampleRequest()); !errors.Is(err, adapters.ErrDenied) {
		t.Fatalf("error = %v", err)
	}
	plans, err := repository.ListPlans(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 0 {
		t.Fatalf("suppression still created %d plans", len(plans))
	}
}

func TestLostAcknowledgementRetriesTheSamePlan(t *testing.T) {
	repository := controlplane.NewMemoryRepository()
	submitter := New(repository, openLedger(t))
	submitter.FailAfterSubmit = true
	request := sampleRequest()
	if _, err := submitter.Submit(context.Background(), request); err == nil {
		t.Fatal("expected a lost acknowledgement")
	}
	receipt, err := submitter.Submit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !receipt.Reconciled {
		t.Fatalf("retry did not reconcile: %#v", receipt)
	}
	plans, err := repository.ListPlans(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || plans[0].ID != receipt.PlanID {
		t.Fatalf("plans = %#v receipt = %#v", plans, receipt)
	}
}

func sampleRequest() domain.HandoffRequest {
	return domain.HandoffRequest{
		RunID: "sample-app-1", ProposalRevision: "rev-1",
		ArtifactDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TargetPlanRef:  "env-dev/sample-app",
	}
}

func openLedger(t *testing.T) *adapters.Ledger {
	t.Helper()
	ledger, err := adapters.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}
