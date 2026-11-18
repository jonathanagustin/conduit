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

func TestDirectRolloutMutationsRequireAllTargetAuthority(t *testing.T) {
	handler, _, governance := driftRepairHub(t)
	for _, user := range []string{"partial", "complete", "environment-only", "product-only"} {
		if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: user, DisplayName: user, AdminUsers: []string{"root"}, MemberUsers: []string{user}}); err != nil {
			t.Fatal(err)
		}
		resources := []string{"product/ledger", "environment/east"}
		if user == "complete" || user == "environment-only" {
			resources = append(resources, "environment/west")
		}
		if user == "environment-only" {
			resources = []string{"environment/east", "environment/west"}
		}
		if user == "product-only" {
			resources = []string{"product/ledger"}
		}
		for _, resource := range resources {
			if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: resource, Team: user, Operations: []string{controlplane.OperationOperate}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	body := func(id string) string {
		b, err := json.Marshal(map[string]any{"action": "create", "id": id, "product": "ledger", "version": "1", "waves": []map[string]any{{"name": "private-wave", "environments": []string{"east", "west"}, "gate": "health"}}})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	seed := postAssistant(handler, "root", "", "/v1/rollouts", body("private-rollout"))
	if seed.Code != 200 {
		t.Fatal(seed.Code, seed.Body)
	}
	for _, actor := range []string{"ungranted", "reader", "author", "partial", "environment-only", "product-only"} {
		response := postAssistant(handler, actor, "", "/v1/rollouts", body("refused-"+actor))
		if response.Code != http.StatusForbidden {
			t.Errorf("%s create: %d %s", actor, response.Code, response.Body)
		}
		response = postAssistant(handler, actor, "", "/v1/rollouts", `{"action":"pause","id":"private-rollout","confirm":true}`)
		if response.Code != http.StatusForbidden || strings.Contains(response.Body.String(), "private-wave") {
			t.Errorf("%s pause: %d %s", actor, response.Code, response.Body)
		}
	}
	stored := as(t, handler, "/v1/rollouts", "root", "")
	if strings.Contains(stored.Body.String(), "refused-") {
		t.Error("ungranted draft persisted", stored.Body)
	}
	created := postAssistant(handler, "complete", "", "/v1/rollouts", body("authorized-draft"))
	if created.Code != 200 {
		t.Fatal("all-target operator refused", created.Code, created.Body)
	}
	paused := postAssistant(handler, "complete", "", "/v1/rollouts", `{"action":"pause","id":"private-rollout","confirm":true}`)
	if paused.Code != 409 || strings.Contains(paused.Body.String(), "private-wave") {
		t.Errorf("operate-only pause should retain draft conflict without historical aggregate: %d %s", paused.Code, paused.Body)
	}
}

func TestDirectRolloutMutationsRetainOriginalCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for i, tc := range []struct {
		actor, represented string
		status             int
	}{{"alice", "", 200}, {"alice", "bob", 200}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403}} {
		body, err := json.Marshal(map[string]any{"action": "create", "id": fmt.Sprintf("carrier-%d", i), "product": "private-product", "version": "1", "waves": []map[string]any{{"name": "first", "environments": []string{"private-environment"}, "gate": "health"}}})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/rollouts", strings.NewReader(string(body)))
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
	secure := NewHubHandler(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"alice"}})
	for _, actor := range []string{"", "alice"} {
		response := postAssistant(secure, actor, "", "/v1/rollouts", `{"action":"create"}`)
		if response.Code != 401 {
			t.Fatalf("untrusted identity %q: %d %s", actor, response.Code, response.Body)
		}
	}
}
