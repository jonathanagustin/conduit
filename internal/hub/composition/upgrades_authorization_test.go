package composition

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	upgradeapp "github.com/jonathanagustin/conduit/internal/delivery/upgrades/application"
	spacememory "github.com/jonathanagustin/conduit/internal/spaces/adapters/memory"
)

func TestUpgradeReadsPreserveNativeEnvironmentAndProductAuthority(t *testing.T) {
	f := newSpaceIndexFixture(t, controlplane.NewMemoryRepository(), spacememory.New())
	for i, environment := range []string{"a-one", "b-two", "c-three", "d-four"} {
		if _, err := f.repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: environment, Entities: []controlplane.ReportedEntity{{InstallationID: "install-" + environment, Product: fmt.Sprintf("com.example:product-%d", i), Namespace: environment, Version: "0.9.0"}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		actor string
		count int
	}{{"admin", 4}, {"reader", 1}, {"outsider", 0}} {
		response := spaceRequest(f.handler, tc.actor, http.MethodGet, "/v1/fleet/upgrades", "")
		var body struct {
			Upgrades []upgradeapp.Upgrade `json:"upgrades"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Upgrades) != tc.count {
			t.Errorf("%s upgrade visibility: %d %s, want %d rows", tc.actor, response.Code, response.Body, tc.count)
		}
		if tc.actor == "reader" && len(body.Upgrades) > 0 && body.Upgrades[0].Environment != "a-one" {
			t.Error("foreign Environment disclosed")
		}
	}
	for _, tc := range []struct {
		actor, product string
		status         int
	}{{"admin", "com.example:product-0", 200}, {"reader", "com.example:product-1", 200}, {"reader", "com.example:product-0", 403}, {"outsider", "com.example:product-1", 403}} {
		response := spaceRequest(f.handler, tc.actor, http.MethodGet, "/v1/products/"+url.PathEscape(tc.product)+"/dependencies/1.0.0", "")
		if response.Code != tc.status {
			t.Errorf("%s product %s read=%d %s; want %d", tc.actor, tc.product, response.Code, response.Body, tc.status)
		}
	}
}

func TestUpgradeReadsPreserveDelegatingCarrier(t *testing.T) {
	_, repository, _ := delegationHub(t)
	state := controlplane.DesiredState{Environment: controlplane.Environment{ID: "private-environment"}}
	if _, err := repository.CreatePlan(t.Context(), controlplane.PlanRecord{ID: "private-plan", DesiredState: &state, Plan: controlplane.Plan{Environment: "private-environment", Steps: []controlplane.PlanStep{{InstallationID: "private-install", Product: "private-product", Namespace: "private", Version: "2.0.0"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.PutEnvironmentReport(t.Context(), controlplane.EnvironmentReport{Environment: "private-environment", Entities: []controlplane.ReportedEntity{{InstallationID: "private-install", Product: "private-product", Namespace: "private", Version: "1.0.0"}}}); err != nil {
		t.Fatal(err)
	}
	handler := NewHubHandler(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"alice"}})
	for _, tc := range []struct {
		actor, represented              string
		count, status, dependencyStatus int
	}{{"alice", "", 1, 200, 200}, {"alice", "bob", 1, 200, 200}, {"robot", "alice", 0, 200, 403}, {"bob", "alice", 0, 403, 403}, {"bob", "", 0, 200, 403}} {
		response := as(t, handler, "/v1/fleet/upgrades", tc.actor, tc.represented)
		var body struct {
			Upgrades []upgradeapp.Upgrade `json:"upgrades"`
		}
		if response.Code != tc.status || (response.Code == http.StatusOK && (json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Upgrades) != tc.count)) {
			t.Errorf("%s as %s: upgrade status=%d body=%s", tc.actor, tc.represented, response.Code, response.Body)
		}
		response = as(t, handler, "/v1/products/private-product/dependencies/2.0.0", tc.actor, tc.represented)
		if response.Code != tc.dependencyStatus {
			t.Errorf("%s as %s: dependencies status=%d body=%s", tc.actor, tc.represented, response.Code, response.Body)
		}
	}
}

func TestUpgradeReadsRejectAnonymousAndUntrustedIdentityHeaders(t *testing.T) {
	handler := NewHubHandler(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{BootstrapAdministrators: []string{"alice"}})
	for _, path := range []string{"/v1/fleet/upgrades", "/v1/products/private-product/dependencies/2.0.0"} {
		for _, subject := range []string{"", "alice"} {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("X-Platform-Subject", subject)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("untrusted identity %q on %s: %d %s", subject, path, response.Code, response.Body)
			}
		}
	}
}
