package controlplane

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func cancelPlanRequest(t *testing.T, handler http.Handler, subject, planID string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"reason": "stopping"})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/plans/"+planID+"/cancel", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Platform-Subject", subject)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func grantOperateOnEnvironment(t *testing.T, repository Repository, username, environment string) {
	t.Helper()
	ctx := context.Background()
	governance := NewGovernanceService(repository)
	team := "operators-" + environment
	if _, err := governance.PutTeam(ctx, Team{
		Name: team, DisplayName: team,
		AdminUsers: []string{"owner"}, MemberUsers: []string{username},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: environmentRID(environment),
		Team:        team,
		Operations:  []string{OperationOperate},
	}); err != nil {
		t.Fatal(err)
	}
}

// TestCancellingAnUngovernedPlanRequiresAuthorityOverItsEnvironment is the
// defect: any authenticated operator could cancel, in any Environment,
// including one they hold nothing on. The approval route for the same
// ungoverned Plans already required Operate; cancel did not.
func TestCancellingAnUngovernedPlanRequiresAuthorityOverItsEnvironment(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	record := planWithoutAChangeRequest(t, repository)
	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})

	response := cancelPlanRequest(t, handler, "someone", record.ID)

	if response.Code != http.StatusForbidden {
		t.Fatalf("an operator with no grant cancelled an ungoverned Plan: %d %s",
			response.Code, response.Body.String())
	}
	stored, err := repository.GetPlan(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == PlanCancelled || stored.Cancellation != nil {
		t.Fatalf("the refused cancellation still landed: status=%s cancellation=%#v", stored.Status, stored.Cancellation)
	}
}

// And the route still works for an operator who does hold the Environment, so
// the check took nothing away from the reconciliation planner and the command
// path that create these Plans.
func TestCancellingAnUngovernedPlanSucceedsForAGrantedOperator(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	record := planWithoutAChangeRequest(t, repository)
	grantOperateOnEnvironment(t, repository, "someone", "dev-use2")

	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := cancelPlanRequest(t, handler, "someone", record.ID)

	if response.Code != http.StatusOK {
		t.Fatalf("a granted operator could not cancel: %d %s", response.Code, response.Body.String())
	}
	stored, err := repository.GetPlan(ctx, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != PlanCancelled || stored.Cancellation == nil || stored.Cancellation.Subject != "someone" {
		t.Fatalf("the cancellation was not recorded: status=%s cancellation=%#v", stored.Status, stored.Cancellation)
	}
}

// Reading a grant on the wrong Environment must not admit a cancellation.
// Without this the previous test passes against a check that only asks whether
// the principal holds Operate anywhere at all.
func TestCancellingAnUngovernedPlanRefusesAGrantOnAnotherEnvironment(t *testing.T) {
	t.Parallel()
	repository := NewMemoryRepository()
	record := planWithoutAChangeRequest(t, repository)
	grantOperateOnEnvironment(t, repository, "someone", "some-other-environment")

	handler := NewHandler(repository, HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := cancelPlanRequest(t, handler, "someone", record.ID)

	if response.Code != http.StatusForbidden {
		t.Fatalf("a grant on a different Environment cancelled this one: %d %s",
			response.Code, response.Body.String())
	}
	stored, err := repository.GetPlan(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status == PlanCancelled {
		t.Fatalf("the refused cancellation still landed: %#v", stored)
	}
}
