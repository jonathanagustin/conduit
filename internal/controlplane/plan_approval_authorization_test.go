package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A Plan with no Change Request is approved by Lifecycle.ApprovePlan, which
// performs no eligibility check of any kind -- no requirement, no grant, only a
// per-subject dedupe -- and then flips the Plan to APPROVED once the approval
// count is reached. Authentication alone was the whole gate.
//
// Such Plans are not broken records to be refused: the reconciliation planner
// and the command path both create them legitimately. They are Plans governance
// never saw, which is exactly why the route has to ask who is approving.

// planWithoutAChangeRequest stores a Plan the way the ungoverned creators do,
// straight through the repository with no Change Request attached.
func planWithoutAChangeRequest(t *testing.T, repository Repository) PlanRecord {
	t.Helper()
	record, err := NewLifecycle(repository).CreatePlan(context.Background(), foundation())
	if err != nil {
		t.Fatalf("create an ungoverned plan: %v", err)
	}
	if record.ChangeRequestID != "" {
		t.Fatalf("this fixture is meant to have no Change Request, got %q", record.ChangeRequestID)
	}
	return record
}

func approvePlanRequest(t *testing.T, handler http.Handler, subject, planID string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"rationale": "approving"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/plans/"+planID+"/approvals", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform-Subject", subject)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

// TestApprovingAnUngovernedPlanRequiresAuthorityOverItsEnvironment is the
// defect: any authenticated operator could approve, in any Environment,
// including one they hold nothing on.
func TestApprovingAnUngovernedPlanRequiresAuthorityOverItsEnvironment(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	record := planWithoutAChangeRequest(t, repository)
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})

	response := approvePlanRequest(t, handler, "someone", record.ID)

	if response.Code != http.StatusForbidden {
		t.Fatalf("an operator with no grant approved an ungoverned Plan: %d %s",
			response.Code, response.Body.String())
	}
	// The refusal has to have refused, not merely answered 403 on the way to
	// approving anyway.
	stored, err := repository.GetPlan(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == PlanApproved || len(stored.Approvals) != 0 {
		t.Fatalf("the refused approval still landed: status=%s approvals=%#v", stored.Status, stored.Approvals)
	}
}

// And the route still works for an operator who does hold the Environment, so
// the check took nothing away from the reconciliation planner and the command
// path that create these Plans.
func TestApprovingAnUngovernedPlanSucceedsForAGrantedOperator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	record := planWithoutAChangeRequest(t, repository)

	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, Team{
		Name: "environment-operators", DisplayName: "Environment operators",
		AdminUsers: []string{"owner"}, MemberUsers: []string{"someone"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("dev-use2"),
		Team:        "environment-operators",
		Operations:  []string{OperationOperate},
	}); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := approvePlanRequest(t, handler, "someone", record.ID)

	if response.Code != http.StatusOK {
		t.Fatalf("a granted operator could not approve: %d %s", response.Code, response.Body.String())
	}
	stored, err := repository.GetPlan(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Approvals) != 1 || stored.Approvals[0].Subject != "someone" {
		t.Fatalf("the approval was not recorded: %#v", stored.Approvals)
	}
}

// Reading a grant on the wrong Environment must not admit an approval. Without
// this the previous test passes against a check that only asks whether the
// principal holds Operate anywhere at all.
func TestApprovingAnUngovernedPlanRefusesAGrantOnAnotherEnvironment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	record := planWithoutAChangeRequest(t, repository)

	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, Team{
		Name: "other-operators", DisplayName: "Other operators",
		AdminUsers: []string{"owner"}, MemberUsers: []string{"someone"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID("some-other-environment"),
		Team:        "other-operators",
		Operations:  []string{OperationOperate},
	}); err != nil {
		t.Fatal(err)
	}

	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := approvePlanRequest(t, handler, "someone", record.ID)

	if response.Code != http.StatusForbidden {
		t.Fatalf("a grant on a different Environment approved this one: %d %s",
			response.Code, response.Body.String())
	}
}
