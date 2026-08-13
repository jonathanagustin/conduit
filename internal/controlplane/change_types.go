package controlplane

import (
	"sort"
	"strconv"
	"strings"
)

// Apollo publishes a closed Change Type taxonomy. An approval policy dispatches
// on `type`, and the type fixes which keys the policy may read from
// `referencedResources`, so the set is validated rather than free-form. A
// policy that guards UPDATE_ENTITY by reading
// referencedResources.installation.rid must be able to rely on that key
// existing for every change carrying that type.
//
// The identifiers and their referenced-resource keys follow the public CEL
// policy input reference. Platform emits the subset its own control plane can
// author; the remainder are accepted from adapters so that policies written
// against the public taxonomy evaluate identically here.
//
// Provenance (reviewed 2026-08-02):
// https://www.palantir.com/docs/apollo/cel-policies/input-types/
const (
	ChangeTypeCreateEntity         = "CREATE_ENTITY"
	ChangeTypeUpdateEntity         = "UPDATE_ENTITY"
	ChangeTypeUpdateEntitySettings = "UPDATE_ENTITY_SETTINGS"
	ChangeTypeDeleteEntity         = "DELETE_ENTITY"

	ChangeTypeCreateEntityMaintenanceWindowOverride        = "CREATE_ENTITY_MAINTENANCE_WINDOW_OVERRIDE"
	ChangeTypeCreateGenericEntityMaintenanceWindowOverride = "CREATE_GENERIC_ENTITY_MAINTENANCE_WINDOW_OVERRIDE"

	ChangeTypeUpdateEnvironmentSettings   = "UPDATE_ENVIRONMENT_SETTINGS"
	ChangeTypeUpdateEnvironmentConfig     = "UPDATE_ENVIRONMENT_CONFIG"
	ChangeTypeUpdateExternalDependencies  = "UPDATE_EXTERNAL_DEPENDENCIES"
	ChangeTypeUpdateEnvironmentProperties = "UPDATE_ENVIRONMENT_PROPERTIES"
	ChangeTypeDeleteEnvironment           = "DELETE_ENVIRONMENT"
	ChangeTypeExitBootstrapMode           = "EXIT_BOOTSTRAP_MODE"

	ChangeTypeModifySpaceSettings                 = "MODIFY_SPACE_SETTINGS"
	ChangeTypeModifyResourceOverrideSpaceSettings = "MODIFY_RESOURCE_OVERRIDE_SPACE_SETTINGS"

	ChangeTypeCreatePipeline               = "CREATE_PIPELINE"
	ChangeTypeUpdatePipeline               = "UPDATE_PIPELINE"
	ChangeTypeUpdatePipelineTransferTarget = "UPDATE_PIPELINE_TRANSFER_TARGET"
	ChangeTypeDeletePipeline               = "DELETE_PIPELINE"
	ChangeTypeArchivePipeline              = "ARCHIVE_PIPELINE"
	ChangeTypeUnarchivePipeline            = "UNARCHIVE_PIPELINE"

	ChangeTypeCreateTeam = "CREATE_TEAM"
	ChangeTypeUpdateTeam = "UPDATE_TEAM"
	ChangeTypeDeleteTeam = "DELETE_TEAM"

	ChangeTypeCreateProductSettings = "CREATE_PRODUCT_SETTINGS"
	ChangeTypeUpdateProductSettings = "UPDATE_PRODUCT_SETTINGS"
	ChangeTypeDeleteProductSettings = "DELETE_PRODUCT_SETTINGS"

	ChangeTypeCreateEgressPolicies            = "CREATE_EGRESS_POLICIES"
	ChangeTypeDeleteEgressPolicies            = "DELETE_EGRESS_POLICIES"
	ChangeTypeCreateTerminalAccessGrant       = "CREATE_TERMINAL_ACCESS_GRANT"
	ChangeTypeModifyVulnerabilitySuppressions = "MODIFY_VULNERABILITY_SUPPRESSIONS"
	ChangeTypeRemoveVulnerabilitySuppressions = "REMOVE_VULNERABILITY_SUPPRESSIONS"

	ChangeTypeCreateManagementOverride = "CREATE_MANAGEMENT_OVERRIDE"
	ChangeTypeUpdateManagementOverride = "UPDATE_MANAGEMENT_OVERRIDE"
	ChangeTypeDeleteManagementOverride = "DELETE_MANAGEMENT_OVERRIDE"
)

// ChangeTypeContract records the referenced-resource keys a change of one type
// must carry and the request fields a policy may read from it.
//
// ResourceKeysVary marks the two space-settings types, whose referenced
// resource depends on the setting being modified; those carry no fixed key
// requirement. RequestFields is advisory: it documents the public field names
// so tests can assert a policy written upstream finds what it expects, and it
// does not forbid platform-native additions such as configDigest.
type ChangeTypeContract struct {
	Type              string
	RequiredResources []string
	RequestFields     []string
	ResourceKeysVary  bool
}

var changeTypeContracts = map[string]ChangeTypeContract{
	ChangeTypeCreateEntity: {
		RequiredResources: []string{"environment", "product"},
		RequestFields:     []string{"apolloEntityId", "apolloEntityType", "releaseChannel", "configOverrides", "entity", "asset"},
	},
	ChangeTypeUpdateEntity: {
		RequiredResources: []string{"environment", "installation", "product"},
		RequestFields:     []string{"apolloEntityId", "apolloEntityType", "releaseChannel", "configOverrides", "entity", "asset"},
	},
	ChangeTypeUpdateEntitySettings: {
		RequiredResources: []string{"environment", "installation", "product"},
		RequestFields:     []string{"apolloEntityId", "apolloEntityType", "releaseChannel", "configOverrides", "entity", "asset"},
	},
	ChangeTypeDeleteEntity: {
		RequiredResources: []string{"environment", "installation", "product"},
		RequestFields:     []string{"apolloEntityId", "apolloEntityType"},
	},
	ChangeTypeCreateEntityMaintenanceWindowOverride: {
		RequiredResources: []string{"environment", "installation", "product"},
		RequestFields:     []string{"entity", "startTime", "endTime", "type"},
	},
	ChangeTypeCreateGenericEntityMaintenanceWindowOverride: {
		RequiredResources: []string{"environment", "installation"},
		RequestFields:     []string{"entity", "startTime", "endTime", "type"},
	},
	ChangeTypeUpdateEnvironmentSettings: {
		RequiredResources: []string{"environment"},
		RequestFields:     []string{"deploymentId", "settings"},
	},
	ChangeTypeUpdateEnvironmentConfig: {
		RequiredResources: []string{"environment"},
		RequestFields:     []string{"config"},
	},
	ChangeTypeUpdateExternalDependencies: {
		RequiredResources: []string{"environment"},
		RequestFields:     []string{"config"},
	},
	ChangeTypeUpdateEnvironmentProperties: {
		RequiredResources: []string{"environment"},
		RequestFields: []string{
			"deploymentId", "owner", "enableOperationalResponsibility", "coOwner",
			"accreditation", "registerToNamespaces", "supportOverride",
		},
	},
	ChangeTypeDeleteEnvironment: {
		RequiredResources: []string{"environment"},
		RequestFields:     []string{"deployment", "namespaces"},
	},
	ChangeTypeExitBootstrapMode: {
		RequiredResources: []string{"environment"},
	},
	ChangeTypeModifySpaceSettings: {
		RequestFields: []string{"settingName"}, ResourceKeysVary: true,
	},
	ChangeTypeModifyResourceOverrideSpaceSettings: {
		RequestFields: []string{"settingName"}, ResourceKeysVary: true,
	},
	ChangeTypeCreatePipeline: {
		RequiredResources: []string{"pipelineRoot"},
		RequestFields:     []string{"settings"},
	},
	ChangeTypeUpdatePipeline: {
		RequiredResources: []string{"bundlingpipeline"},
		RequestFields:     []string{"pipelineRid", "settings"},
	},
	ChangeTypeUpdatePipelineTransferTarget: {
		RequiredResources: []string{"transferTarget", "bundlingpipeline"},
	},
	ChangeTypeDeletePipeline: {
		RequiredResources: []string{"bundlingpipeline"}, RequestFields: []string{"pipelineRid"},
	},
	ChangeTypeArchivePipeline: {
		RequiredResources: []string{"bundlingpipeline"}, RequestFields: []string{"pipelineRid"},
	},
	ChangeTypeUnarchivePipeline: {
		RequiredResources: []string{"bundlingpipeline"}, RequestFields: []string{"pipelineRid"},
	},
	ChangeTypeCreateTeam: {
		RequiredResources: []string{"teamRoot"},
		RequestFields:     []string{"name", "admins", "members", "adminUsers", "memberUsers"},
	},
	ChangeTypeUpdateTeam: {
		RequiredResources: []string{"team"},
		RequestFields:     []string{"teamRid", "admins", "members", "adminUsers", "memberUsers"},
	},
	ChangeTypeDeleteTeam: {
		RequiredResources: []string{"team"}, RequestFields: []string{"teamRid"},
	},
	ChangeTypeCreateProductSettings: {
		RequiredResources: []string{"product"},
		RequestFields:     []string{"productId", "orTeam", "operationalResponsibilityExcludedEnvironments"},
	},
	ChangeTypeUpdateProductSettings: {
		RequiredResources: []string{"product"},
		RequestFields:     []string{"productId", "orTeam", "operationalResponsibilityExcludedEnvironments"},
	},
	ChangeTypeDeleteProductSettings: {
		RequiredResources: []string{"product"}, RequestFields: []string{"product"},
	},
	ChangeTypeCreateEgressPolicies: {
		RequiredResources: []string{"egressPolicyRoot"}, RequestFields: []string{"data"},
	},
	ChangeTypeDeleteEgressPolicies: {
		RequiredResources: []string{"egressPolicyRoot"}, RequestFields: []string{"data"},
	},
	ChangeTypeCreateTerminalAccessGrant: {
		RequiredResources: []string{"environment"}, RequestFields: []string{"environment", "data"},
	},
	ChangeTypeModifyVulnerabilitySuppressions: {
		RequiredResources: []string{"vulnerabilitySuppression"},
		RequestFields:     vulnerabilitySuppressionRequestFields,
	},
	ChangeTypeRemoveVulnerabilitySuppressions: {
		RequiredResources: []string{"vulnerabilitySuppression"},
		RequestFields:     vulnerabilitySuppressionRequestFields,
	},
	ChangeTypeCreateManagementOverride: {
		RequestFields: []string{"name", "owner", "products", "services"},
	},
	ChangeTypeUpdateManagementOverride: {
		RequestFields: []string{"name", "owner", "products", "services"},
	},
	ChangeTypeDeleteManagementOverride: {
		RequestFields: []string{"name"},
	},
}

var vulnerabilitySuppressionRequestFields = []string{
	"spaceRid", "vulnerabilitySuppressions", "scannerSuppressions",
	"imageNameVulnerabilitySuppressions", "imagePrefixVulnerabilitySuppressions",
	"productIdVulnerabilitySuppressions", "imageEndOfSupportLifeSuppressions",
}

// importChangeTypePrefix marks Bundle import changes. Import is a
// platform-native governance surface rather than a published Change Type, so it
// is namespaced instead of colliding with the public taxonomy.
const importChangeTypePrefix = "IMPORT_"

var bundleImportChangeTypes = map[string]struct{}{
	importChangeTypePrefix + BundleSelectEnvironment: {},
	importChangeTypePrefix + "MODULE_RELEASE":        {},
	importChangeTypePrefix + "PRODUCT_RELEASE":       {},
	importChangeTypePrefix + BundleSelectTeam:        {},
}

// platformChangeTypes are governance surfaces Platform reviews that Apollo does
// not publish as Change Types. They are enumerated rather than accepted
// implicitly so that a typo in a published identifier still fails closed, and so
// the divergence from the public taxonomy stays auditable in one place.
var platformChangeTypes = map[string][]string{
	BulkSuppress:                       {"environment", "product", "installation"},
	BulkUnsuppress:                     {"environment", "product", "installation"},
	ModuleUpdateChangeType:             {"module", "installation", "environment"},
	ChangeTypeRetireProduct:            {"product"},
	ChangeTypeRetireProductRelease:     {"product", "release"},
	ChangeTypeCreateLabelDefinition:    {"label"},
	ChangeTypeUpdateLabelDefinition:    {"label"},
	ChangeTypeTombstoneLabelDefinition: {"label"},
}

const ModuleUpdateChangeType = "MODULE_UPDATE"

// ChangeTypeContractFor returns the published contract for a Change Type.
func ChangeTypeContractFor(changeType string) (ChangeTypeContract, bool) {
	contract, ok := changeTypeContracts[changeType]
	if !ok {
		return ChangeTypeContract{}, false
	}
	contract.Type = changeType
	return contract, true
}

// KnownChangeTypes lists the published taxonomy in a stable order.
func KnownChangeTypes() []string {
	types := make([]string, 0, len(changeTypeContracts))
	for changeType := range changeTypeContracts {
		types = append(types, changeType)
	}
	sort.Strings(types)
	return types
}

// validateChangeType rejects a change whose type is outside the published
// taxonomy, or which omits a referenced resource that policies written against
// that type are entitled to read. Import changes are namespaced and exempt.
func validateChangeType(index int, change EntityChange) []ValidationError {
	path := "changes[" + strconv.Itoa(index) + "]"
	changeType := strings.TrimSpace(change.Type)
	if changeType == "" {
		return []ValidationError{{Path: path + ".type", Message: "change type is required"}}
	}
	if strings.HasPrefix(changeType, importChangeTypePrefix) {
		if _, known := bundleImportChangeTypes[changeType]; known {
			return nil
		}
		return []ValidationError{{Path: path + ".type", Message: "unsupported Bundle import change type " + changeType}}
	}
	contract, known := ChangeTypeContractFor(changeType)
	if !known {
		required, native := platformChangeTypes[changeType]
		if !native {
			return []ValidationError{{
				Path:    path + ".type",
				Message: "unsupported change type " + changeType,
			}}
		}
		contract = ChangeTypeContract{Type: changeType, RequiredResources: required}
	}
	if contract.ResourceKeysVary {
		return nil
	}
	errs := []ValidationError{}
	for _, key := range contract.RequiredResources {
		resource, present := change.ReferencedResources[key]
		if !present {
			errs = append(errs, ValidationError{
				Path:    path + ".referencedResources." + key,
				Message: changeType + " requires referenced resource " + key,
			})
			continue
		}
		if strings.TrimSpace(resource.RID) == "" {
			errs = append(errs, ValidationError{
				Path:    path + ".referencedResources." + key + ".rid",
				Message: "referenced resource requires a rid",
			})
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}
