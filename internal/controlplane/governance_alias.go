package controlplane

import (
	"context"
	"time"

	governanceapp "github.com/jonathanagustin/conduit/internal/governance/application"
	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
)

// Governance is a bounded context, and this file is all that is left of it
// here: type aliases and one-line delegations that keep this package's call
// sites spelled as they were.
//
// What moved is the deciding. Who may do what, what a role conveys, how a
// resource expands into scopes, the approval-policy language and its published
// predicates, and the audit contract a decision is filed under are all in
// internal/governance now — the domain for the rules, a port for the one lookup
// they need, and an application service that holds the two together. This
// package keeps the Change Request lifecycle that *asks* those questions, and
// asks them through the service rather than answering them itself.
type (
	Principal               = governance.Principal
	Team                    = governance.Team
	ResourceGrant           = governance.ResourceGrant
	PolicyVariable          = governance.PolicyVariable
	ApprovalPolicy          = governance.ApprovalPolicy
	ReviewerRequirement     = governance.ReviewerRequirement
	PolicyEvaluation        = governance.PolicyEvaluation
	AuthorizationAuditEvent = governance.AuthorizationAuditEvent

	ChangeDiffEntry    = governance.ChangeDiffEntry
	ReferencedResource = governance.ReferencedResource

	PolicyTrust           = governance.PolicyTrust
	TrustedServiceAccount = governance.TrustedServiceAccount
	policyResult          = governance.PolicyResult
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

	AuditSchemaV3 = governance.AuditSchemaV3

	AuditCategoryAPIGatewayRequest   = governance.AuditCategoryAPIGatewayRequest
	AuditCategoryAuthenticationCheck = governance.AuditCategoryAuthenticationCheck
	AuditCategoryAuthorizationCheck  = governance.AuditCategoryAuthorizationCheck
	AuditCategoryRequestCreate       = governance.AuditCategoryRequestCreate
	AuditCategoryRequestApprove      = governance.AuditCategoryRequestApprove
	AuditCategoryRequestDisapprove   = governance.AuditCategoryRequestDisapprove
	AuditCategoryRequestCancel       = governance.AuditCategoryRequestCancel
	AuditCategoryRequestExecute      = governance.AuditCategoryRequestExecute
	AuditCategoryRequestUpdate       = governance.AuditCategoryRequestUpdate
	AuditCategoryCreateInfra         = governance.AuditCategoryCreateInfra
	AuditCategoryConfigureInfra      = governance.AuditCategoryConfigureInfra
	AuditCategoryUpgradeInfra        = governance.AuditCategoryUpgradeInfra

	ChangeSourceModuleInstallation = governance.ChangeSourceModuleInstallation
	ChangeSourceEnvironmentImport  = governance.ChangeSourceEnvironmentImport

	MetadataChangeSource = governance.MetadataChangeSource
	MetadataModuleID     = governance.MetadataModuleID
	MetadataBundleID     = governance.MetadataBundleID
)

// The two tables are aliased rather than copied so there is exactly one of
// each. A second copy of the role table would be a second answer to "what does
// this role convey", and the two would drift without anything noticing.
var (
	canonicalRoleOperations = governance.CanonicalRoleOperations
)

// ValidateAuditCategoryEvent enforces the public audit.3 field contract. It is
// re-exported because the repository writes audit events and must reject an
// invalid one at the same boundary the service does.
func ValidateAuditCategoryEvent(event AuthorizationAuditEvent) error {
	return governance.ValidateAuditCategoryEvent(event)
}

func normalizedAuditCategories(categories []string) []string {
	return governance.NormalizedAuditCategories(categories)
}

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

func evaluatePolicy(policy ApprovalPolicy, input map[string]any, trust PolicyTrust) (policyResult, error) {
	return governance.EvaluatePolicy(policy, input, trust)
}

func allRequirementsSatisfied(requirements []ReviewerRequirement) bool {
	return governance.AllRequirementsSatisfied(requirements)
}

// grantScopeAdapter satisfies Governance's one port over the legacy repository.
// It answers for exactly the scope it is given; the expansion from a resource
// into its scopes is the domain's, and doing any of it here would be a second
// answer to the same question.
type grantScopeAdapter struct {
	repository GovernanceRepository
}

func (adapter grantScopeAdapter) ResourceGrantsInScope(
	ctx context.Context, scope string,
) ([]ResourceGrant, error) {
	return adapter.repository.ListResourceGrants(ctx, scope)
}

func governanceAuthorizer(repository GovernanceRepository) *governanceapp.Authorizer {
	return governanceapp.NewAuthorizer(grantScopeAdapter{repository: repository})
}

func effectiveResourceGrants(
	ctx context.Context, repository GovernanceRepository, resourceRID string,
) ([]ResourceGrant, error) {
	return governanceAuthorizer(repository).EffectiveGrants(ctx, resourceRID)
}

func requirementSatisfied(
	ctx context.Context, repository Repository, requirement ReviewerRequirement, principal Principal,
) bool {
	return governanceAuthorizer(repository).RequirementSatisfied(ctx, requirement, principal)
}

func principalCanReview(
	ctx context.Context, repository Repository, requirements []ReviewerRequirement, principal Principal,
) bool {
	return governanceAuthorizer(repository).PrincipalCanReview(ctx, requirements, principal)
}

// policyInput projects an entity change onto the facts a policy may read. The
// projection is the boundary: the identifiers and installation coordinates on
// EntityChange stay on this side of it.
func policyInput(change EntityChange, author Principal, editors []string, now time.Time) map[string]any {
	return governance.PolicyInput(governance.ChangeFacts{
		Type:                change.Type,
		Request:             change.Request,
		Metadata:            change.Metadata,
		Diff:                change.Diff,
		ReferencedResources: change.ReferencedResources,
	}, author, editors, now)
}
