package controlplane

import "github.com/jonathanagustin/conduit/internal/governance"

// Who may do what — principals, teams, grants, the role table, and how a
// resource identifier expands into scopes — now lives in internal/governance.
// These aliases keep this package's call sites spelled as they were.
type (
	Principal               = governance.Principal
	Team                    = governance.Team
	ResourceGrant           = governance.ResourceGrant
	PolicyVariable          = governance.PolicyVariable
	ApprovalPolicy          = governance.ApprovalPolicy
	ReviewerRequirement     = governance.ReviewerRequirement
	PolicyEvaluation        = governance.PolicyEvaluation
	AuthorizationAuditEvent = governance.AuthorizationAuditEvent
)

const (
	OperationActOnBehalf       = governance.OperationActOnBehalf
	OperationProposeChange     = governance.OperationProposeChange
	OperationReadResource      = governance.OperationReadResource
	OperationOperate           = governance.OperationOperate
	OperationImportBundle      = governance.OperationImportBundle
	OperationBulkChange        = governance.OperationBulkChange
	OperationManagePermissions = governance.OperationManagePermissions
	OperationApplyLabel        = governance.OperationApplyLabel
	OperationCreateArtifact    = governance.OperationCreateArtifact
	OperationCreateProduct     = governance.OperationCreateProduct
	OperationCreateRelease     = governance.OperationCreateRelease

	RelationshipOwner   = governance.RelationshipOwner
	RelationshipCoOwner = governance.RelationshipCoOwner

	RequirementTeam      = governance.RequirementTeam
	RequirementRole      = governance.RequirementRole
	RequirementOperation = governance.RequirementOperation
	RequirementAny       = governance.RequirementAny

	PolicyString     = governance.PolicyString
	PolicyStringList = governance.PolicyStringList
)

// The two tables are aliased rather than copied so there is exactly one of
// each. A second copy of the role table would be a second answer to "what does
// this role convey", and the two would drift without anything noticing.
var (
	canonicalRoleOperations   = governance.CanonicalRoleOperations
	apolloRolePlatformAliases = governance.ApolloRolePlatformAliases
)

func resolveRole(role string) string { return governance.ResolveRole(role) }

func rolesAllow(roles []string, operation string) bool {
	return governance.RolesAllow(roles, operation)
}

func effectivePrincipal(principal Principal) Principal {
	return governance.EffectivePrincipal(principal)
}

func authorizationScopes(resourceRID string) []string {
	return governance.AuthorizationScopes(resourceRID)
}

func validateCanonicalRoleScope(role, resourceRID string) error {
	return governance.ValidateCanonicalRoleScope(role, resourceRID)
}
