// Package domain is Governance's model: who may do what, and what review a
// change needs before it can happen.
//
// It holds the principals asking, the roles they hold, what each role is
// permitted, how a resource identifier expands into the scopes a grant may have
// been made against, the approval-policy language a change is judged through,
// and the audit contract a decision is recorded under. Everything here is
// decidable from its arguments — a principal, a role table, a resource
// identifier, a policy expression, the facts of a change. Nothing here reads a
// repository, and nothing here knows how a request arrived.
//
// The one lookup a decision needs, which grants exist against a scope, is the
// application layer's business through a port. That split is what lets these
// rules be tested without a store and reused without a transport.
//
// Two of the tables here mean different things and must not be confused. The
// role-to-operation table is Platform's own contract. The Apollo alias map is
// Platform policy about what it is willing to honour when a grant names an
// Apollo role — deliberately conveying less, in places, than Apollo does. The
// captured graph in internal/apollo is the evidence both are checked against;
// neither is derived from it.
//
// This package imports no other context.
package domain

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Operations are the verbs a role can convey.
const (
	OperationActOnBehalf       = "platform:act-on-behalf"
	OperationProposeChange     = "change:propose"
	OperationReadResource      = "resource:read"
	OperationOperate           = "resource:operate"
	OperationImportBundle      = "bundle:import"
	OperationBulkChange        = "fleet:bulk-change"
	OperationManagePermissions = "permissions:manage"
	OperationApplyLabel        = "label:apply"
	OperationCreateArtifact    = "artifact:create"
	OperationCreateProduct     = "product:create"
	OperationCreateRelease     = "product-release:create"
)

// OperationVocabulary is every operation Platform itself can act on: the ones
// an authorization check asks for by name, and therefore the ones the session
// capability projection can meaningfully answer for.
//
// It is NOT the set of strings a grant may carry, and the difference is load
// bearing. ResourceGrant.Operations is deliberately open, because a policy may
// use require_operation to demand an approval token that Platform never
// performs — "environment:approve-change" is one such, and is exercised
// end-to-end by TestCELRoleAndOperationRequirementsUseResourceGrants. Those
// strings are satisfied by matching a grant, not by Platform doing anything, so
// validating them against this list would delete a working feature.
//
// What this list is for is the other direction: enumerating what a principal
// may do, where an operation nothing ever asks for would be noise. Adding a
// constant above without adding it here is caught by
// TestOperationVocabularyCoversEveryOperation rather than being discovered as a
// capability that silently never appears in a session.
// Ordered by the operation's own value rather than by constant name, so the
// sortedness check has something to compare and a duplicate cannot hide.
var OperationVocabulary = []string{
	OperationCreateArtifact,    // artifact:create
	OperationImportBundle,      // bundle:import
	OperationProposeChange,     // change:propose
	OperationBulkChange,        // fleet:bulk-change
	OperationApplyLabel,        // label:apply
	OperationManagePermissions, // permissions:manage
	OperationActOnBehalf,       // platform:act-on-behalf
	OperationCreateRelease,     // product-release:create
	OperationCreateProduct,     // product:create
	OperationOperate,           // resource:operate
	OperationReadResource,      // resource:read
}

// OperationIsKnown reports whether this is an operation Platform performs.
//
// Read the comment on OperationVocabulary before using this to reject
// anything: a false answer means "Platform never does this itself", not "this
// string is invalid". A policy requirement naming an operator-defined approval
// token is legitimate and answers false here.
func OperationIsKnown(operation string) bool {
	return contains(OperationVocabulary, operation)
}

// How a Team is related to a resource, independently of any role it holds.
const (
	RelationshipOwner   = "OWNER"
	RelationshipCoOwner = "CO_OWNER"
)

// What a policy can demand of a reviewer.
const (
	RequirementTeam      = "TEAM"
	RequirementRole      = "ROLE"
	RequirementOperation = "OPERATION"
	RequirementAny       = "ANY"
)

// The shapes a policy variable may take.
const (
	PolicyString     = "STRING"
	PolicyStringList = "STRING_LIST"
)

// Principal is whoever is asking. OnBehalfOf is a delegation: a service account
// acting for a person is judged as that person, which is why EffectivePrincipal
// exists rather than callers reading Username directly.
type Principal struct {
	Username       string     `json:"username"`
	Groups         []string   `json:"groups,omitempty"`
	Teams          []string   `json:"teams,omitempty"`
	ServiceAccount bool       `json:"serviceAccount,omitempty"`
	OnBehalfOf     *Principal `json:"onBehalfOf,omitempty"`
}

// Team is a named group that grants are made to.
type Team struct {
	Name         string            `json:"name"`
	DisplayName  string            `json:"displayName"`
	Description  string            `json:"description,omitempty"`
	AdminUsers   []string          `json:"adminUsers,omitempty"`
	AdminGroups  []string          `json:"adminGroups"`
	MemberUsers  []string          `json:"memberUsers,omitempty"`
	MemberGroups []string          `json:"memberGroups"`
	Labels       map[string]string `json:"labels,omitempty"`
	CreatedAt    time.Time         `json:"createdAt"`
	UpdatedAt    time.Time         `json:"updatedAt"`
	Revision     uint64            `json:"revision"`
}

// ResourceGrant gives a Team roles or operations over one resource.
type ResourceGrant struct {
	ID           string    `json:"id"`
	ResourceRID  string    `json:"resourceRid"`
	Team         string    `json:"team"`
	Relationship string    `json:"relationship,omitempty"`
	Roles        []string  `json:"roles,omitempty"`
	Operations   []string  `json:"operations,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Revision     uint64    `json:"revision"`
}

// PolicyVariable is a value a policy expression may refer to.
type PolicyVariable struct {
	Type   string   `json:"type"`
	String string   `json:"string,omitempty"`
	List   []string `json:"list,omitempty"`
}

// ApprovalPolicy is an expression deciding what review a change needs.
type ApprovalPolicy struct {
	ID          string                    `json:"id"`
	DisplayName string                    `json:"displayName"`
	Expression  string                    `json:"expression"`
	Variables   map[string]PolicyVariable `json:"variables,omitempty"`
	Enabled     bool                      `json:"enabled"`
	CreatedAt   time.Time                 `json:"createdAt"`
	UpdatedAt   time.Time                 `json:"updatedAt"`
	Revision    uint64                    `json:"revision"`
}

// ReviewerRequirement is one thing a reviewer must satisfy. Alternatives are
// disjunctive: satisfying any one of them satisfies the requirement.
type ReviewerRequirement struct {
	ID           string                `json:"id"`
	ChangeID     string                `json:"changeId"`
	PolicyID     string                `json:"policyId"`
	Kind         string                `json:"kind"`
	Team         string                `json:"team,omitempty"`
	ResourceRID  string                `json:"resourceRid,omitempty"`
	Role         string                `json:"role,omitempty"`
	Operation    string                `json:"operation,omitempty"`
	Reason       string                `json:"reason"`
	Alternatives []ReviewerRequirement `json:"alternatives,omitempty"`
	SatisfiedBy  []string              `json:"satisfiedBy,omitempty"`
	// Satisfiable is computed at read time and never stored: whether any current
	// grant could satisfy this requirement at all, independent of who is asking.
	// nil means not computed; a pointer to false means no grant conveys it, so
	// the change is unapprovable by anyone until one does. It is a pointer with
	// omitempty for the same reason SatisfiedBy is populated only after creation
	// -- absent at creation, it stays out of the SHA-256 digest that fixes this
	// requirement's identity (see PolicyResult.Requirement).
	Satisfiable *bool `json:"satisfiable,omitempty"`
	// Unsatisfiable explains a false Satisfiable in one line, for the operator
	// looking at a change that will not move and nothing saying why.
	Unsatisfiable string `json:"unsatisfiable,omitempty"`
}

// PolicyEvaluation is what one policy decided about one change.
type PolicyEvaluation struct {
	ChangeID    string               `json:"changeId"`
	PolicyID    string               `json:"policyId"`
	Outcome     string               `json:"outcome"`
	Reason      string               `json:"reason,omitempty"`
	Requirement *ReviewerRequirement `json:"requirement,omitempty"`
}

// AuthorizationAuditEvent records a decision — including the ones that allowed
// something. An audit trail holding only refusals cannot answer who was let in.
type AuthorizationAuditEvent struct {
	ID            string         `json:"id"`
	SchemaVersion string         `json:"schemaVersion"`
	Categories    []string       `json:"categories"`
	TraceID       string         `json:"traceId,omitempty"`
	Subject       string         `json:"subject"`
	Action        string         `json:"action"`
	ResourceRID   string         `json:"resourceRid"`
	Decision      string         `json:"decision"`
	Reason        string         `json:"reason"`
	RequestFields map[string]any `json:"requestFields"`
	ResultFields  map[string]any `json:"resultFields"`
	Context       map[string]any `json:"context,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

// CanonicalRoleOperations is what each Platform role conveys.
//
// Restricted Release Channel contribution is ownership-dependent and is
// evaluated by AuthorizeReleaseChannelContribution, not the generic operation
// expansion. Public role and inheritance contract reviewed 2026-08-02:
// https://www.palantir.com/docs/apollo/core/authorization/
var CanonicalRoleOperations = map[string][]string{
	"permissions:administrator":               {OperationManagePermissions, OperationReadResource},
	"permissions:viewer":                      {OperationReadResource},
	"environment:creator":                     {OperationProposeChange},
	"environment:administrator":               {OperationProposeChange, OperationReadResource, OperationOperate, OperationImportBundle, OperationBulkChange, OperationManagePermissions},
	"environment:editor":                      {OperationProposeChange, OperationReadResource, OperationOperate, OperationImportBundle, OperationBulkChange},
	"environment:operator":                    {OperationReadResource, OperationOperate},
	"environment:viewer":                      {OperationReadResource},
	"entity:administrator":                    {OperationProposeChange, OperationReadResource, OperationOperate, OperationManagePermissions},
	"entity:editor":                           {OperationProposeChange, OperationReadResource, OperationOperate},
	"entity:operator":                         {OperationReadResource, OperationOperate},
	"entity:viewer":                           {OperationReadResource},
	"artifact:creator":                        {OperationCreateArtifact, OperationReadResource},
	"artifact:viewer":                         {OperationReadResource},
	"product:administrator":                   {OperationProposeChange, OperationReadResource, OperationOperate, OperationManagePermissions, OperationCreateProduct, OperationCreateRelease},
	"product:release-creator":                 {OperationProposeChange, OperationReadResource, OperationCreateRelease},
	"product:creator":                         {OperationProposeChange, OperationReadResource, OperationCreateProduct},
	"product:editor":                          {OperationProposeChange, OperationReadResource, OperationOperate},
	"product:viewer":                          {OperationReadResource},
	"release-channel:administrator":           {OperationProposeChange, OperationReadResource, OperationOperate, OperationManagePermissions},
	"release-channel:contributor":             {OperationProposeChange, OperationReadResource},
	"release-channel:restricted-contributor":  {OperationReadResource},
	"release-channel:viewer":                  {OperationReadResource},
	"label:administrator":                     {OperationProposeChange, OperationReadResource, OperationOperate, OperationManagePermissions},
	"label:contributor":                       {OperationReadResource, OperationApplyLabel},
	"label:creator":                           {OperationProposeChange, OperationReadResource},
	"label:viewer":                            {OperationReadResource},
	"module:change-manager":                   {OperationReadResource, OperationOperate},
	"vulnerability-suppression:administrator": {OperationProposeChange, OperationReadResource, OperationOperate, OperationManagePermissions},
	"vulnerability-suppression:approver":      {OperationReadResource, OperationOperate},
	"vulnerability-suppression:viewer":        {OperationReadResource},
	"vulnerability-sla:administrator":         {OperationProposeChange, OperationReadResource, OperationOperate, OperationManagePermissions},
	"vulnerability-sla:approver":              {OperationReadResource, OperationOperate},
	"vulnerability-sla:viewer":                {OperationReadResource},
}

// ApolloRolePlatformAliases resolves an Apollo role identifier onto the
// Platform role whose authority Platform actually implements.
//
// This is a Platform authoring decision, not a recovered fact, and the two must
// not be confused. The captured graph in internal/apollo records what Apollo
// grants; it is evidence. This map records what Platform is willing to honour
// when a grant names an Apollo role; it is policy. Apollo's
// apollo-artifact:creator grants containers:push and repositories:create
// against a container registry Platform does not operate, so the alias
// deliberately conveys less than the Apollo role does. An Apollo role with no
// entry here resolves to no Platform authority at all, which is the fail-closed
// direction.
// Every entry is an exact leaf-name match inside a family Platform actually
// implements. An Apollo role with no entry conveys no Platform authority, which
// is why the omissions matter as much as the entries:
//
//   - terminal-user and terminal-access-approver are refused on purpose. Debug
//     terminal access is the H30 boundary; Platform does not broker it.
//   - deleter, security-administrator, privileged-editor, privileged-viewer and
//     the artifact/module administrator and editor tiers name authority
//     Platform has no equivalent for. Aliasing them onto a weaker Platform role
//     would quietly under-grant; onto a stronger one, quietly over-grant.
//   - the seventeen families with no Platform analogue at all (bundles,
//     pipelines, teams, namespaces, hubs, webhooks, and the rest) are absent
//     for the same reason.
//
// apolloRoleAliasesAreValid pins all of this against the captured graph.
var ApolloRolePlatformAliases = map[string]string{
	"apollo-artifact:creator":           "artifact:creator",
	"apollo-artifact:viewer":            "artifact:viewer",
	"apollo-environments:administrator": "environment:administrator",
	"apollo-environments:creator":       "environment:creator",
	"apollo-environments:editor":        "environment:editor",
	"apollo-environments:operator":      "environment:operator",
	"apollo-environments:viewer":        "environment:viewer",
	"apollo-label:administrator":        "label:administrator",
	"apollo-label:contributor":          "label:contributor",
	"apollo-label:creator":              "label:creator",
	"apollo-label:viewer":               "label:viewer",
	"apollo-module:change-manager":      "module:change-manager",
	"apollo-permissions:administrator":  "permissions:administrator",
	"apollo-permissions:viewer":         "permissions:viewer",
}

// ResolveRole maps an Apollo identifier onto its Platform equivalent, and
// leaves every other role untouched.
func ResolveRole(role string) string {
	if platformRole, aliased := ApolloRolePlatformAliases[role]; aliased {
		return platformRole
	}
	return role
}

// RolesAllow reports whether any of these roles conveys the operation.
func RolesAllow(roles []string, operation string) bool {
	for _, role := range roles {
		if contains(CanonicalRoleOperations[ResolveRole(role)], operation) {
			return true
		}
	}
	return false
}

// EffectivePrincipal is who the request is really being made as. A delegation
// is judged as the person delegated to, not the account carrying it.
func EffectivePrincipal(principal Principal) Principal {
	if principal.OnBehalfOf != nil {
		return *principal.OnBehalfOf
	}
	return principal
}

// AuthorizationScopes expands a resource identifier into every scope a grant
// covering it could have been made against: the resource itself, the
// environment containing it where there is one, and the global scope for its
// family. A grant on the environment therefore reaches its installations
// without having to be restated for each.
func AuthorizationScopes(resourceRID string) []string {
	result := []string{resourceRID}
	parts := strings.Split(resourceRID, "/")
	if len(parts) == 0 {
		return result
	}
	switch parts[0] {
	case "installation":
		if len(parts) >= 3 {
			result = append(result, "environment/"+parts[1])
		}
		result = append(result, "global/installation", "global/environment")
	case "artifact", "environment", "product", "module", "label", "release-channel", "vulnerability-suppression", "vulnerability-sla":
		result = append(result, "global/"+parts[0])
	}
	return normalizedStrings(result)
}

// ValidateCanonicalRoleScope rejects a grant of a canonical role on a resource
// of the wrong kind — an environment role on a product, say. A role Platform
// does not recognise is not this function's business and passes.
func ValidateCanonicalRoleScope(role, resourceRID string) error {
	role = ResolveRole(role)
	if _, canonical := CanonicalRoleOperations[role]; !canonical {
		return nil
	}
	expected := strings.SplitN(role, ":", 2)[0]
	if expected == "permissions" {
		if resourceRID != "platform/root" {
			return fmt.Errorf("role %s requires platform/root", role)
		}
		return nil
	}
	parts := strings.Split(resourceRID, "/")
	resourceType := parts[0]
	if resourceType == "global" && len(parts) == 2 {
		resourceType = parts[1]
	}
	if expected == "entity" {
		expected = "installation"
	}
	if resourceType != expected {
		return fmt.Errorf("role %s cannot be granted on %s", role, resourceRID)
	}
	return nil
}

// contains and normalizedStrings are restated here rather than imported.
// Reaching back to the monolith for a slice search and a trim-dedupe-sort would
// be an edge pointing the wrong way, and the point of this package is that it
// has none.

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func normalizedStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
