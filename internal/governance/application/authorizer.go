// Package application answers Governance's questions against a real grant
// store: may this principal do this, and does this reviewer satisfy what a
// policy demanded.
//
// It is the only place the two halves meet. The rules — how a resource expands
// into scopes, what a role conveys, how a requirement is spelled — live in the
// domain and take no store. The lookup lives behind a port. This layer holds
// them together and holds nothing else, which is why it is short and why the
// rules stay testable without one.
package application

import (
	"context"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/governance/ports"
)

// Authorizer decides authorization questions over a grant source.
type Authorizer struct {
	grants ports.GrantSource
}

// NewAuthorizer binds the rules to a grant source.
func NewAuthorizer(grants ports.GrantSource) *Authorizer {
	return &Authorizer{grants: grants}
}

// EffectiveGrants is every grant that could bear on a resource: the ones made
// against the resource itself, against the environment containing it, and
// against the global scope for its family. A grant on the environment therefore
// reaches its installations without having to be restated for each.
//
// The scopes come from the domain, not from here. An adapter that widened them
// would widen every decision below.
func (authorizer *Authorizer) EffectiveGrants(ctx context.Context, resourceRID string) ([]domain.ResourceGrant, error) {
	result := []domain.ResourceGrant{}
	for _, scope := range domain.AuthorizationScopes(resourceRID) {
		grants, err := authorizer.grants.ResourceGrantsInScope(ctx, scope)
		if err != nil {
			return nil, err
		}
		result = append(result, grants...)
	}
	return result, nil
}

// Allows reports whether the principal may perform the operation on the
// resource, through a grant to one of its teams. A failed lookup is a denial:
// an authorization question that cannot be answered has not been answered
// affirmatively.
func (authorizer *Authorizer) Allows(
	ctx context.Context, principal domain.Principal, resourceRID, operation string,
) bool {
	grants, err := authorizer.EffectiveGrants(ctx, resourceRID)
	if err != nil {
		return false
	}
	teams := teamSet(principal)
	for _, grant := range grants {
		if _, member := teams[grant.Team]; member &&
			(containsString(grant.Operations, operation) || domain.RolesAllow(grant.Roles, operation)) {
			return true
		}
	}
	return false
}

// RequirementSatisfied reports whether this principal meets one reviewer
// requirement. Alternatives are disjunctive, so a satisfied alternative
// satisfies the requirement carrying it.
func (authorizer *Authorizer) RequirementSatisfied(
	ctx context.Context, requirement domain.ReviewerRequirement, principal domain.Principal,
) bool {
	switch requirement.Kind {
	case domain.RequirementTeam:
		return containsString(principal.Teams, requirement.Team)
	case domain.RequirementRole, domain.RequirementOperation:
		grants, err := authorizer.EffectiveGrants(ctx, requirement.ResourceRID)
		if err != nil {
			return false
		}
		teams := teamSet(principal)
		for _, grant := range grants {
			if _, member := teams[grant.Team]; !member {
				continue
			}
			if requirement.Kind == domain.RequirementRole && containsString(grant.Roles, requirement.Role) {
				return true
			}
			if requirement.Kind == domain.RequirementOperation &&
				(containsString(grant.Operations, requirement.Operation) || domain.RolesAllow(grant.Roles, requirement.Operation)) {
				return true
			}
		}
	case domain.RequirementAny:
		for _, candidate := range requirement.Alternatives {
			if authorizer.RequirementSatisfied(ctx, candidate, principal) {
				return true
			}
		}
	}
	return false
}

// PrincipalCanReview reports whether this principal can move any one of the
// outstanding requirements. Reviewing is not all-or-nothing: a reviewer who
// satisfies one requirement may review, and whether that leaves the change
// fully reviewed is domain.AllRequirementsSatisfied's question.
func (authorizer *Authorizer) PrincipalCanReview(
	ctx context.Context, requirements []domain.ReviewerRequirement, principal domain.Principal,
) bool {
	for _, requirement := range requirements {
		if authorizer.RequirementSatisfied(ctx, requirement, principal) {
			return true
		}
	}
	return false
}

func teamSet(principal domain.Principal) map[string]struct{} {
	result := make(map[string]struct{}, len(principal.Teams))
	for _, team := range principal.Teams {
		result[team] = struct{}{}
	}
	return result
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
