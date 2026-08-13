package controlplane

import (
	"context"
	"strings"
	"testing"
)

// The published taxonomy is a contract, not documentation: a policy that guards
// UPDATE_ENTITY by reading referencedResources.installation.rid must be able to
// rely on that key existing, and a change carrying a type outside the taxonomy
// must fail closed rather than slip past every policy that dispatches on type.
func TestPublishedChangeTypesCoverTheDocumentedTaxonomy(t *testing.T) {
	t.Parallel()
	documented := map[string][]string{
		ChangeTypeCreateEntity:                                 {"environment", "product"},
		ChangeTypeUpdateEntity:                                 {"environment", "installation", "product"},
		ChangeTypeUpdateEntitySettings:                         {"environment", "installation", "product"},
		ChangeTypeDeleteEntity:                                 {"environment", "installation", "product"},
		ChangeTypeCreateEntityMaintenanceWindowOverride:        {"environment", "installation", "product"},
		ChangeTypeCreateGenericEntityMaintenanceWindowOverride: {"environment", "installation"},
		ChangeTypeUpdateEnvironmentSettings:                    {"environment"},
		ChangeTypeUpdateEnvironmentConfig:                      {"environment"},
		ChangeTypeUpdateExternalDependencies:                   {"environment"},
		ChangeTypeUpdateEnvironmentProperties:                  {"environment"},
		ChangeTypeDeleteEnvironment:                            {"environment"},
		ChangeTypeExitBootstrapMode:                            {"environment"},
		ChangeTypeModifySpaceSettings:                          nil,
		ChangeTypeModifyResourceOverrideSpaceSettings:          nil,
		ChangeTypeCreatePipeline:                               {"pipelineRoot"},
		ChangeTypeUpdatePipeline:                               {"bundlingpipeline"},
		ChangeTypeUpdatePipelineTransferTarget:                 {"transferTarget", "bundlingpipeline"},
		ChangeTypeDeletePipeline:                               {"bundlingpipeline"},
		ChangeTypeArchivePipeline:                              {"bundlingpipeline"},
		ChangeTypeUnarchivePipeline:                            {"bundlingpipeline"},
		ChangeTypeCreateTeam:                                   {"teamRoot"},
		ChangeTypeUpdateTeam:                                   {"team"},
		ChangeTypeDeleteTeam:                                   {"team"},
		ChangeTypeCreateProductSettings:                        {"product"},
		ChangeTypeUpdateProductSettings:                        {"product"},
		ChangeTypeDeleteProductSettings:                        {"product"},
		ChangeTypeCreateEgressPolicies:                         {"egressPolicyRoot"},
		ChangeTypeDeleteEgressPolicies:                         {"egressPolicyRoot"},
		ChangeTypeCreateTerminalAccessGrant:                    {"environment"},
		ChangeTypeModifyVulnerabilitySuppressions:              {"vulnerabilitySuppression"},
		ChangeTypeRemoveVulnerabilitySuppressions:              {"vulnerabilitySuppression"},
		ChangeTypeCreateManagementOverride:                     nil,
		ChangeTypeUpdateManagementOverride:                     nil,
		ChangeTypeDeleteManagementOverride:                     nil,
	}
	if len(documented) != 34 {
		t.Fatalf("fixture covers %d change types, expected the documented 34", len(documented))
	}
	known := KnownChangeTypes()
	if len(known) != len(documented) {
		t.Fatalf("implementation exposes %d change types, fixture has %d", len(known), len(documented))
	}
	for changeType, resources := range documented {
		contract, ok := ChangeTypeContractFor(changeType)
		if !ok {
			t.Fatalf("change type %s is not implemented", changeType)
		}
		if contract.Type != changeType {
			t.Fatalf("contract for %s reports type %s", changeType, contract.Type)
		}
		if strings.Join(contract.RequiredResources, ",") != strings.Join(resources, ",") {
			t.Fatalf("%s required resources = %v, want %v", changeType, contract.RequiredResources, resources)
		}
	}
}

func TestChangeTypeValidationFailsClosed(t *testing.T) {
	t.Parallel()
	for name, testCase := range map[string]struct {
		change    EntityChange
		wantError bool
	}{
		"unknown type is rejected": {
			change:    EntityChange{Type: "UPDATE_EVERYTHING"},
			wantError: true,
		},
		"empty type is rejected": {
			change:    EntityChange{Type: "  "},
			wantError: true,
		},
		"missing required resource is rejected": {
			change: EntityChange{Type: ChangeTypeUpdateEntity, ReferencedResources: map[string]ReferencedResource{
				"environment": {RID: "ri.env.1"}, "product": {RID: "ri.product.1"},
			}},
			wantError: true,
		},
		"blank rid on a required resource is rejected": {
			change: EntityChange{Type: ChangeTypeUpdateEntity, ReferencedResources: map[string]ReferencedResource{
				"environment": {RID: "ri.env.1"}, "product": {RID: "ri.product.1"}, "installation": {RID: " "},
			}},
			wantError: true,
		},
		"complete change is accepted": {
			change: EntityChange{Type: ChangeTypeUpdateEntity, ReferencedResources: map[string]ReferencedResource{
				"environment": {RID: "ri.env.1"}, "product": {RID: "ri.product.1"}, "installation": {RID: "ri.install.1"},
			}},
		},
		"space settings carry no fixed resource key": {
			change: EntityChange{Type: ChangeTypeModifySpaceSettings},
		},
		"bundle import is namespaced outside the taxonomy": {
			change: EntityChange{Type: importChangeTypePrefix + "ENVIRONMENT"},
		},
		"unknown bundle import type is rejected": {
			change: EntityChange{Type: importChangeTypePrefix + "EVERYTHING"}, wantError: true,
		},
		"platform-native bulk action is enumerated": {
			change: EntityChange{Type: BulkSuppress, ReferencedResources: map[string]ReferencedResource{
				"environment": {RID: "ri.env.1"}, "product": {RID: "ri.product.1"}, "installation": {RID: "ri.install.1"},
			}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			errs := validateChangeType(0, testCase.change)
			if testCase.wantError != (len(errs) > 0) {
				t.Fatalf("errors = %v, wantError = %v", errs, testCase.wantError)
			}
		})
	}
}

// Ordinary desired-state removal is a deletion, so it must be reviewable under
// the same policies that guard installation and upgrade rather than executing
// as an unnamed side effect of the entity disappearing from desired state.
func TestDesiredStateRemovalProposesDeleteEntity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	repository := NewMemoryRepository()
	service := NewGovernanceService(repository)
	if _, err := service.PutPolicy(ctx, ApprovalPolicy{
		ID: "review-all", DisplayName: "Review all", Enabled: true,
		Expression: `approve("fixture approval")`,
	}); err != nil {
		t.Fatal(err)
	}
	state := foundation()
	author, err := service.ResolvePrincipal(ctx, IdentityClaims{Username: "author@example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.ProposePlan(ctx, state, author, "Initial rollout", ""); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(state)
	if err != nil {
		t.Fatal(err)
	}
	reported := EnvironmentReport{Environment: state.Environment.ID, AgentID: "agent-1"}
	for _, step := range plan.Steps {
		reported.Entities = append(reported.Entities, ReportedEntity{
			InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
			ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
			Health: HealthHealthy,
		})
	}
	if _, err := repository.PutEnvironmentReport(ctx, reported); err != nil {
		t.Fatal(err)
	}

	trimmed := foundation()
	trimmed.Entities = nil
	trimmed.Environment.Generation = "generation-2"
	_, request, err := service.ProposePlan(ctx, trimmed, author, "Remove every entity", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(request.Changes) != len(reported.Entities) {
		t.Fatalf("changes = %d, want %d", len(request.Changes), len(reported.Entities))
	}
	for _, change := range request.Changes {
		if change.Type != ChangeTypeDeleteEntity {
			t.Fatalf("change %s type = %s, want %s", change.ID, change.Type, ChangeTypeDeleteEntity)
		}
		if _, ok := change.ReferencedResources["installation"]; !ok {
			t.Fatalf("change %s omits the installation resource policies dispatch on", change.ID)
		}
		if change.Request["apolloEntityId"] != change.InstallationID {
			t.Fatalf("change %s request = %#v", change.ID, change.Request)
		}
	}
}
