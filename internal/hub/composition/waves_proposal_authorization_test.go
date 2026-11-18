package composition

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestGovernedRolloutProposalCoversEveryTargetAuthorityAndPolicy(t *testing.T) {
	handler, repository, governance := driftRepairHub(t)
	for _, user := range []string{"partial-proposer", "full-proposer", "environment-proposer", "release-reviewers", "west-reviewers"} {
		members := []string{user}
		if user == "west-reviewers" {
			members = append(members, "full-proposer")
		}
		if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: user, DisplayName: user, AdminUsers: []string{"root"}, MemberUsers: members}); err != nil {
			t.Fatal(err)
		}
	}
	for _, user := range []string{"partial-proposer", "full-proposer", "environment-proposer"} {
		resources := []string{"product/ledger", "environment/east"}
		if user == "full-proposer" {
			resources = append(resources, "environment/west")
		}
		if user == "environment-proposer" {
			resources = []string{"environment/east", "environment/west"}
		}
		for _, resource := range resources {
			if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{Team: user, ResourceRID: resource, Operations: []string{controlplane.OperationProposeChange}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{ID: "west-only", DisplayName: "West review", Enabled: true, Revision: 1, Expression: `referencedResources.environment.rid == "environment/west" ? require_team("west-reviewers", "west target review") : approve("east target")`}); err != nil {
		t.Fatal(err)
	}
	seed := postAssistant(handler, "root", "", "/v1/rollouts", `{"action":"create","id":"private-rollout","product":"ledger","version":"1","waves":[{"name":"private-wave","environments":["east","west","east"],"gate":"health"}]}`)
	if seed.Code != 200 {
		t.Fatal(seed.Code, seed.Body)
	}
	for _, actor := range []string{"ungranted", "reader", "author", "partial-proposer", "environment-proposer"} {
		response := postAssistant(handler, actor, "", "/v1/rollouts", `{"action":"start","id":"private-rollout","healthy":["east","west"]}`)
		if response.Code != 403 || strings.Contains(response.Body.String(), "private-wave") {
			t.Errorf("%s proposal: %d %s", actor, response.Code, response.Body)
		}
	}
	requests, err := repository.ListChangeRequests(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 0 {
		t.Errorf("refused proposals persisted %d requests", len(requests))
		return
	}
	response := postAssistant(handler, "full-proposer", "", "/v1/rollouts", `{"action":"start","id":"private-rollout","healthy":["east","west"]}`)
	if response.Code != 202 || strings.Contains(response.Body.String(), "private-wave") {
		t.Fatalf("full proposal-only receipt: %d %s", response.Code, response.Body)
	}
	var receipt struct{ ChangeRequestID string }
	if json.Unmarshal(response.Body.Bytes(), &receipt) != nil || receipt.ChangeRequestID == "" {
		t.Fatal(response.Body)
	}
	request, err := repository.GetChangeRequest(t.Context(), receipt.ChangeRequestID)
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]bool{}
	for _, change := range request.Changes {
		targets[change.ReferencedResources["environment"].RID] = true
	}
	if len(request.Changes) != 2 || !targets["environment/east"] || !targets["environment/west"] {
		t.Fatalf("targets not exact/distinct: %#v", request.Changes)
	}
	westRequired := false
	for _, requirement := range request.Requirements {
		if requirement.Team == "west-reviewers" {
			westRequired = true
		}
	}
	if !westRequired {
		t.Fatal("second Environment policy omitted", request.Requirements)
	}
	body := fmt.Sprintf(`{"action":"approve","changeRequestId":%q,"rationale":"reviewed targets"}`, receipt.ChangeRequestID)
	self := postAssistant(handler, "full-proposer", "", "/v1/rollouts", body)
	if self.Code != 409 {
		t.Errorf("eligible author approved own proposal: %d %s", self.Code, self.Body)
	}
	wrong := postAssistant(handler, "release-reviewers", "", "/v1/rollouts", body)
	if wrong.Code != 403 {
		t.Errorf("wrong target reviewer: %d %s", wrong.Code, wrong.Body)
	}
	approved := postAssistant(handler, "west-reviewers", "", "/v1/rollouts", body)
	if approved.Code != 200 || strings.Contains(approved.Body.String(), "private-wave") {
		t.Errorf("eligible review without aggregate read: %d %s", approved.Code, approved.Body)
	}
}

func TestGovernedRolloutProposalRetainsOriginalCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for i, tc := range []struct {
		actor, represented string
		status             int
	}{{"alice", "", 202}, {"alice", "bob", 202}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403}} {
		id := fmt.Sprintf("carrier-%d", i)
		seed := postAssistant(handler, "alice", "", "/v1/rollouts", fmt.Sprintf(`{"action":"create","id":%q,"product":"private-product","version":"1","waves":[{"name":"first","environments":["private-environment"],"gate":"health"}]}`, id))
		if seed.Code != 200 {
			t.Fatal(seed.Code, seed.Body)
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/rollouts", strings.NewReader(fmt.Sprintf(`{"action":"start","id":%q}`, id)))
		request.Header.Set("X-Platform-Subject", tc.actor)
		if tc.represented != "" {
			request.Header.Set("X-Platform-Service-Account", "true")
			request.Header.Set("X-Platform-On-Behalf-Of", tc.represented)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.status {
			t.Errorf("%s as %s: %d %s", tc.actor, tc.represented, response.Code, response.Body)
		}
	}
}
