package conduitcli

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/jonathanagustin/conduit/internal/installoverride/ports"
)

func TestModuleOverrideCLIRequiresSeparateReviewAndPreservesMetadataScope(t *testing.T) {
	root := filepath.Join(t.TempDir(), "core")
	repository, err := controlplane.NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	governance := controlplane.NewGovernanceService(repository)
	for _, team := range []controlplane.Team{{Name: "operators", DisplayName: "Operators", AdminUsers: []string{"operator"}, MemberUsers: []string{"operator"}}, {Name: "reviewers", DisplayName: "Reviewers", AdminUsers: []string{"reviewer"}, MemberUsers: []string{"reviewer"}}} {
		if _, err := governance.PutTeam(t.Context(), team); err != nil {
			t.Fatal(err)
		}
	}
	for _, grant := range []controlplane.ResourceGrant{{ResourceRID: "environment/prod", Team: "operators", Roles: []string{"environment:operator"}}, {ResourceRID: "environment/prod", Team: "reviewers", Roles: []string{"environment:module-override-reviewer"}}} {
		if _, err := governance.PutGrant(t.Context(), grant); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := governance.PutPolicy(t.Context(), controlplane.ApprovalPolicy{ID: "normal", DisplayName: "Normal", Enabled: true, Expression: `approve("ordinary changes")`}); err != nil {
		t.Fatal(err)
	}
	modules := controlplane.NewModuleService(repository)
	published, err := modules.PublishBatch(t.Context(), []controlplane.ModuleDefinition{{Name: "reviewed-module", Entities: []controlplane.ModuleEntityDefinition{{Type: "helmChart", HelmChart: controlplane.ModuleHelmChartEntity{ProductID: "example:api", KubernetesNamespace: "apps", SecretRequirements: map[string]any{"database": map[string]any{"type": "single"}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := modules.RecallRelease(t.Context(), published[0].Name, published[0].Version); err != nil {
		t.Fatal(err)
	}
	handler, err := composition.NewHub(repository, controlplane.HandlerOptions{AllowInsecureOperatorIdentityHeaders: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	run := func(subject string, args ...string) (string, error) {
		return runMembershipCLI(t, server.URL, "", subject, args...)
	}
	base := []string{"module", "install", "reviewed-module:0.1.0", "--environment", "prod", "--ignore-recalls", "--ignore-secret-requirements"}
	if _, err := run("operator", base...); err == nil {
		t.Fatal("missing rationale admitted")
	}
	args := append(append([]string{}, base...), "--rationale", "CAB-305 metadata review")
	if _, err := run("outsider", args...); err == nil {
		t.Fatal("unauthorized proposal")
	}
	invalid := append(append([]string{}, args...), "--output", "table")
	if _, err := run("operator", invalid...); err == nil {
		t.Fatal("unsupported output mutated")
	}
	output, err := run("operator", args...)
	if err != nil {
		t.Fatal(output, err)
	}
	var review ports.ModuleReview
	if err := json.Unmarshal([]byte(output), &review); err != nil {
		t.Fatal(err, output)
	}
	if review.Status != controlplane.ChangePendingApprovals || review.SecretRequirementsEffect != "INTENT_ONLY" {
		t.Fatal(output)
	}
	installations, err := repository.ListModuleInstallations(t.Context(), "prod")
	if err != nil || len(installations) != 0 {
		t.Fatal("proposal installed metadata", err)
	}
	apply := []string{"module", "override", "apply", review.ReviewID}
	if _, err := run("operator", apply...); err == nil {
		t.Fatal("pending review applied")
	}
	if _, err := run("operator", "change-request", "approve", review.ReviewID, "--rationale", "self"); err == nil {
		t.Fatal("self approval accepted")
	}
	if output, err := run("reviewer", "change-request", "approve", review.ReviewID, "--rationale", "Independent CAB-305 review"); err != nil {
		t.Fatal(output, err)
	}
	output, err = run("operator", apply...)
	if err != nil {
		t.Fatal(output, err)
	}
	var admission ports.ModuleAdmission
	if err := json.Unmarshal([]byte(output), &admission); err != nil {
		t.Fatal(err)
	}
	if admission.Status != "APPLIED" || admission.SecretRequirementsEffect != "INTENT_ONLY" {
		t.Fatal(output)
	}
	stored, err := repository.GetModuleInstallation(t.Context(), admission.InstallationID)
	if err != nil || len(stored.Entities) != 1 || stored.Entities[0].SecretRequirements["database"] == nil {
		t.Fatal("lost declaration", stored, err)
	}
	plans, err := repository.ListPlans(t.Context(), "prod")
	if err != nil || len(plans) != 0 {
		t.Fatal("invented workload execution", plans, err)
	}
	reopened, err := controlplane.NewFileRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	request, err := reopened.GetChangeRequest(t.Context(), review.ReviewID)
	if err != nil || request.Execution == nil || request.Execution.Status != controlplane.ExecutionSucceeded {
		t.Fatal("lost receipt", err)
	}
	output, err = run("operator", apply...)
	if err != nil || !strings.Contains(output, "ALREADY_APPLIED") {
		t.Fatal(output, err)
	}
	reloaded, err := repository.GetModuleInstallation(t.Context(), admission.InstallationID)
	if err != nil || reloaded.Revision != stored.Revision {
		t.Fatal("retry repeated write", err)
	}
}
