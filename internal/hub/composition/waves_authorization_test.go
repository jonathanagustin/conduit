package composition

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
)

func TestRolloutListRequiresProductAndEveryEnvironmentRead(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	for _, body := range []string{
		`{"action":"create","id":"visible","product":"com.example:product-1","version":"1","waves":[{"name":"first","environments":["a-one"],"gate":"health"}]}`,
		`{"action":"create","id":"mixed","product":"com.example:product-1","version":"1","waves":[{"name":"first","environments":["a-one","b-two"],"gate":"health"}]}`,
		`{"action":"create","id":"foreign-product","product":"com.example:product-0","version":"1","waves":[{"name":"first","environments":["a-one"],"gate":"health"}]}`,
	} {
		response := postAssistant(f.handler, "admin", "", "/v1/rollouts", body)
		if response.Code != 200 {
			t.Fatalf("seed: %d %s", response.Code, response.Body)
		}
	}
	for _, tc := range []struct {
		actor string
		count int
	}{{"admin", 3}, {"reader", 1}, {"outsider", 0}} {
		response := as(t, f.handler, "/v1/rollouts", tc.actor, "")
		var body struct {
			Rollouts []struct {
				ID string `json:"id"`
			} `json:"rollouts"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Rollouts) != tc.count {
			t.Errorf("%s: %d %s; want %d", tc.actor, response.Code, response.Body, tc.count)
		}
		if tc.actor == "reader" && len(body.Rollouts) > 0 && body.Rollouts[0].ID != "visible" {
			t.Error("mixed visibility or foreign Product disclosed")
		}
	}
}

func TestRolloutListRetainsOriginalDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	seed := postAssistant(handler, "alice", "", "/v1/rollouts", `{"action":"create","id":"private-rollout","product":"private-product","version":"1","waves":[{"name":"first","environments":["private-environment"],"gate":"health"}]}`)
	if seed.Code != 200 {
		t.Fatal(seed.Code, seed.Body)
	}
	for _, tc := range []struct {
		actor, represented string
		count, status      int
	}{{"alice", "", 1, 200}, {"alice", "bob", 1, 200}, {"robot", "alice", 0, 200}, {"bob", "alice", 0, 403}, {"bob", "", 0, 200}} {
		response := as(t, handler, "/v1/rollouts", tc.actor, tc.represented)
		var body struct {
			Rollouts []json.RawMessage `json:"rollouts"`
		}
		if response.Code != tc.status || (tc.status == 200 && (json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Rollouts) != tc.count)) {
			t.Errorf("%s as %s: %d %s", tc.actor, tc.represented, response.Code, response.Body)
		}
	}
	secure := NewHubHandler(repository, controlplane.HandlerOptions{BootstrapAdministrators: []string{"alice"}})
	for _, actor := range []string{"", "alice"} {
		response := as(t, secure, "/v1/rollouts", actor, "")
		if response.Code != 401 {
			t.Fatalf("untrusted identity %q: %d %s", actor, response.Code, response.Body)
		}
	}
}
