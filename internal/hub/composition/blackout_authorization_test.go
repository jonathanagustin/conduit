package composition

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func TestBlackoutAuthorityRetainsOriginalDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented string
		status             int
	}{{"alice", "", 201}, {"alice", "bob", 201}, {"robot", "alice", 403}, {"bob", "alice", 403}, {"bob", "", 403}} {
		request := httptest.NewRequest(http.MethodPost, "/v1/blackouts", strings.NewReader(`{"action":"declare","environment":"east","reason":"reviewed freeze","startsAt":"2026-11-18T12:00:00Z","endsAt":"2026-11-18T13:00:00Z","confirm":true}`))
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
		response = as(t, handler, "/v1/blackouts?environment=east", tc.actor, tc.represented)
		want := 403
		if tc.status == 201 {
			want = 200
		}
		if response.Code != want {
			t.Errorf("%s as %s read: %d %s", tc.actor, tc.represented, response.Code, response.Body)
		}
	}
	secure := NewHubHandler(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"alice"}})
	for _, actor := range []string{"", "alice"} {
		response := as(t, secure, "/v1/blackouts?environment=east", actor, "")
		if response.Code != 401 {
			t.Fatalf("untrusted identity %q: %d %s", actor, response.Code, response.Body)
		}
	}
}

func TestBlackoutReadsAndDeclarationsRequireEnvironmentAuthority(t *testing.T) {
	handler, repository, governance := driftRepairHub(t)
	if _, err := governance.PutTeam(t.Context(), controlplane.Team{Name: "maintainer", DisplayName: "Maintainer", AdminUsers: []string{"root"}, MemberUsers: []string{"maintainer"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(t.Context(), controlplane.ResourceGrant{ResourceRID: "environment/east", Team: "maintainer", Operations: []string{controlplane.OperationOperate}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, query := range []string{"", "?environment=", "?environment=east&environment=west", "?environment=%zz"} {
		response := as(t, handler, "/v1/blackouts"+query, "root", "")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid target %q: %d %s", query, response.Code, response.Body)
		}
	}
	body := func(environment, reason string) string {
		b, err := json.Marshal(map[string]any{"action": "declare", "environment": environment, "reason": reason, "startsAt": now.Add(-time.Minute), "endsAt": now.Add(time.Hour), "confirm": true})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, environment := range []string{"east", "west"} {
		response := postAssistant(handler, "root", "", "/v1/blackouts", body(environment, "private-"+environment))
		if response.Code != http.StatusCreated {
			t.Fatalf("seed: %d %s", response.Code, response.Body)
		}
	}
	for _, tc := range []struct {
		actor, environment string
		status             int
	}{{"reader", "east", 200}, {"reader", "west", 403}, {"ungranted", "east", 403}} {
		response := as(t, handler, "/v1/blackouts?environment="+tc.environment+"&includeIntervals=true", tc.actor, "")
		if response.Code != tc.status || (tc.status != 200 && strings.Contains(response.Body.String(), "private-")) {
			t.Errorf("%s read %s: %d %s", tc.actor, tc.environment, response.Code, response.Body)
		}
	}
	before, err := repository.ListAuthorizationAudit(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"ungranted", "reader", "author"} {
		response := postAssistant(handler, actor, "", "/v1/blackouts", body("east", "unauthorized-declaration"))
		if response.Code != http.StatusForbidden {
			t.Errorf("%s declaration: %d %s", actor, response.Code, response.Body)
		}
	}
	after, err := repository.ListAuthorizationAudit(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range after[len(before):] {
		if event.Action == "blackout.decision" && event.Decision == "ALLOW" {
			t.Error("refused declaration produced a mutation success receipt")
		}
	}
	unchanged := as(t, handler, "/v1/blackouts?environment=east&includeIntervals=true", "root", "")
	if strings.Contains(unchanged.Body.String(), "unauthorized-declaration") {
		t.Fatal("refused declaration mutated intervals")
	}
	response := postAssistant(handler, "maintainer", "", "/v1/blackouts", body("east", "authorized-declaration"))
	if response.Code != http.StatusCreated || strings.Contains(response.Body.String(), "private-") {
		t.Errorf("authorized target receipt leaked foreign blackout: %d %s", response.Code, response.Body)
	}
}
