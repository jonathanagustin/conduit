package controlplane

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestModuleAutoUpdatePolicyRequiresChangeManagerRole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	modules := NewModuleService(repository)
	definition := ModuleDefinition{Name: "managed-module", Entities: []ModuleEntityDefinition{{
		Type: "helmChart", HelmChart: ModuleHelmChartEntity{ProductID: "com.example:api", KubernetesNamespace: "apps"},
	}}}
	if _, err := modules.PublishBatch(ctx, []ModuleDefinition{definition}); err != nil {
		t.Fatal(err)
	}
	governance := NewGovernanceService(repository)
	if _, err := governance.PutTeam(ctx, Team{
		Name: "module-change-managers", DisplayName: "Module change managers",
		AdminUsers: []string{"change-manager"}, MemberUsers: []string{"change-manager"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := governance.PutGrant(ctx, ResourceGrant{
		ResourceRID: moduleRID(definition.Name), Team: "module-change-managers", Roles: []string{"module:change-manager"},
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewHandler(repository, HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true, BootstrapAdministrators: []string{"bootstrap"},
	})
	path := "/v1/modules/" + definition.Name + "/auto-update-policy"
	denied := governanceRequest(t, handler, http.MethodPut, path, "outsider", "", map[string]any{
		"enabled": true, "rationale": "compliance approved", "revision": 0,
	})
	if denied.Code != http.StatusForbidden {
		t.Fatalf("outsider policy update = %d %s", denied.Code, denied.Body.String())
	}
	allowed := governanceRequest(t, handler, http.MethodPut, path, "change-manager", "", map[string]any{
		"enabled": true, "rationale": "compliance approved", "revision": 0,
	})
	if allowed.Code != http.StatusOK {
		t.Fatalf("change manager policy update = %d %s", allowed.Code, allowed.Body.String())
	}
	read := governanceRequest(t, handler, http.MethodGet, path, "change-manager", "", nil)
	if read.Code != http.StatusOK {
		t.Fatalf("change manager policy read = %d %s", read.Code, read.Body.String())
	}
}

func TestModuleHTTPPublicationInstallationUpdateAndUnlink(t *testing.T) {
	handler := Handler()
	definitions := readExampleModuleDefinitions(t)
	response := requestJSON(t, handler, http.MethodPost, "/v1/modules/releases", map[string]any{"definitions": definitions})
	if response.Code != http.StatusCreated {
		t.Fatalf("publish = %d %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodGet, "/v1/modules/example-foundation/releases", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("list = %d %s", response.Code, response.Body.String())
	}

	response = requestJSON(t, handler, http.MethodPost, "/v1/environments/aws-dev/module-installations", map[string]any{
		"module": "example-foundation", "version": "latest", "variables": map[string]string{"databaseCidr": "10.70.0.0/24"},
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("install = %d %s", response.Code, response.Body.String())
	}
	var installation ModuleInstallation
	if err := json.Unmarshal(response.Body.Bytes(), &installation); err != nil {
		t.Fatal(err)
	}
	if len(installation.Entities) != 2 || len(installation.Entities[0].SecretRequirements)+len(installation.Entities[1].SecretRequirements) == 0 {
		t.Fatalf("installation = %#v", installation)
	}

	response = requestJSON(t, handler, http.MethodGet, "/v1/module-installations/"+installation.ID+"/update", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("proposal = %d %s", response.Code, response.Body.String())
	}
	response = requestJSON(t, handler, http.MethodPost, "/v1/module-installations/"+installation.ID+"/unlink", nil)
	if response.Code != http.StatusOK {
		t.Fatalf("unlink = %d %s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &installation); err != nil {
		t.Fatal(err)
	}
	if installation.Status != "UNLINKED" || len(installation.Entities) != 2 {
		t.Fatalf("unlink deleted Entities: %#v", installation)
	}
}

func TestModuleInstallChannelInheritsEnvironmentDefault(t *testing.T) {
	t.Parallel()
	plans := []PlanRecord{{
		ID:           "plan-1",
		Plan:         Plan{Environment: "env-a"},
		DesiredState: &DesiredState{Environment: Environment{ID: "env-a", DefaultReleaseChannel: "RELEASE_CANDIDATE"}},
	}}
	// An explicit request wins over the Environment default.
	if got := moduleInstallChannel("DEV", plans, "env-a"); got != "DEV" {
		t.Errorf("requested channel = %q, want DEV", got)
	}
	// No request inherits the Environment's default Release Channel, not a
	// hardcoded RELEASE.
	if got := moduleInstallChannel("", plans, "env-a"); got != "RELEASE_CANDIDATE" {
		t.Errorf("inherited channel = %q, want RELEASE_CANDIDATE", got)
	}
	// An unknown Environment leaves "" so InstallTrackingChannel keeps its
	// RELEASE fallback.
	if got := moduleInstallChannel("", plans, "env-unknown"); got != "" {
		t.Errorf("unknown Environment channel = %q, want empty", got)
	}
	// An Environment with no default also falls back.
	noDefault := []PlanRecord{{ID: "plan-2", Plan: Plan{Environment: "env-b"}, DesiredState: &DesiredState{Environment: Environment{ID: "env-b"}}}}
	if got := moduleInstallChannel("", noDefault, "env-b"); got != "" {
		t.Errorf("no-default Environment channel = %q, want empty", got)
	}
}
