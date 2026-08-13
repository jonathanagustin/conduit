package controlplane

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestAdaptedPlanIngressUsesNativeGovernanceAndLifecycle(t *testing.T) {
	called := false
	adapter := func(mode string, payload json.RawMessage, keys map[string][]string) (
		*ManagedInstallationConfiguration, *UniversalInstallationConfiguration, []SecretRequirement, error,
	) {
		called = true
		if mode != "managed-v0" || string(payload) != `{"inputMapping":{}}` || len(keys["credential"]) != 1 {
			t.Fatalf("adapter input = mode %q payload %s keys %#v", mode, payload, keys)
		}
		return &ManagedInstallationConfiguration{InputMapping: map[string]json.RawMessage{}}, nil,
			[]SecretRequirement{{Name: "credential", Secret: "runtime", Keys: []string{"username"}}}, nil
	}
	state := foundation()
	state.Environment.Secrets = []ExternalSecretAssignment{{
		Name: "runtime", Revision: 1, ProviderRef: "vault://runtime", KubernetesSecretName: "runtime", Keys: []string{"username"},
	}}
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		EntityConfigurationAdapters:          map[string]EntityConfigurationAdapter{"fixture": adapter},
	})
	response := requestJSON(t, handler, http.MethodPost, "/v1/adapted-plans/fixture", map[string]any{
		"desiredState": state,
		"configurations": []map[string]any{{
			"entity": "example-api", "mode": "managed-v0",
			"payload":            json.RawMessage(`{"inputMapping":{}}`),
			"requiredSecretKeys": map[string][]string{"credential": {"username"}},
		}},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("adapted Plan = %d %s", response.Code, response.Body.String())
	}
	if !called || response.Header().Get("X-Change-Request-ID") == "" {
		t.Fatalf("adapter called=%v change request=%q", called, response.Header().Get("X-Change-Request-ID"))
	}
	var record PlanRecord
	decodeResponse(t, response, http.StatusCreated, &record)
	if record.Status != PlanAwaitingApproval || record.DesiredState == nil ||
		len(record.DesiredState.Entities[1].SecretRequirements) != 1 {
		t.Fatalf("adapted Plan record = %#v", record)
	}
}

func TestAdaptedPlanIngressRejectsUnknownAdapter(t *testing.T) {
	handler := NewHandler(NewMemoryRepository(), HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	response := requestJSON(t, handler, http.MethodPost, "/v1/adapted-plans/missing", map[string]any{})
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown adapter = %d %s", response.Code, response.Body.String())
	}
}
