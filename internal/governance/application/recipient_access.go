package application

import (
	"context"
	"errors"
	"strings"

	"github.com/jonathanagustin/conduit/internal/governance/domain"
	"github.com/jonathanagustin/conduit/internal/governance/ports"
)

// ErrRecipientGroupsUnavailable means an external group could convey access,
// but no trusted current directory is available to establish that membership.
// Reusing groups captured at subscription time would retain revoked authority.
var ErrRecipientGroupsUnavailable = errors.New("current external group membership is unavailable for notification recipient")

// RecipientAccess checks stored owner identifiers against current native
// authority. It does not turn those identifiers into authenticated principals,
// accept delegated identities, or cache request-time claims.
type RecipientAccess struct {
	teams      ports.TeamSource
	authorizer *Authorizer
	policy     AdministratorPolicy
}

func NewRecipientAccess(teams ports.TeamSource, grants ports.GrantSource, policy AdministratorPolicy) (*RecipientAccess, error) {
	if teams == nil || grants == nil {
		return nil, errors.New("current team and grant sources are required for notification recipient authorization")
	}
	policy.BootstrapAdministrators = append([]string(nil), policy.BootstrapAdministrators...)
	return &RecipientAccess{teams: teams, authorizer: NewAuthorizer(grants), policy: policy}, nil
}

// UserCanRead resolves only native direct-user membership. External groups are
// neither guessed from a username nor replayed from an old token. A standing
// bootstrap decision or a current native allow is sufficient without them.
func (access *RecipientAccess) UserCanRead(ctx context.Context, username, resourceRID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !exactIdentifier(username) || !exactIdentifier(resourceRID) {
		return false, nil
	}
	if access.policy.BootstrapAdministrator(username) {
		return true, nil
	}
	teams, allowedTeams, err := access.current(ctx, resourceRID)
	if err != nil {
		return false, err
	}
	externalCouldAllow := false
	for _, team := range teams {
		if !allowedTeams[team.Name] {
			continue
		}
		if containsString(team.AdminUsers, username) || containsString(team.MemberUsers, username) {
			return true, nil
		}
		externalCouldAllow = externalCouldAllow || len(team.AdminGroups) > 0 || len(team.MemberGroups) > 0
	}
	if externalCouldAllow {
		return false, ErrRecipientGroupsUnavailable
	}
	return false, nil
}

// TeamCanRead checks the named team's existence and grants. Team members may
// change, but the team owner's authority is exactly its own current grant set;
// a username bootstrap policy never applies to a team identifier.
func (access *RecipientAccess) TeamCanRead(ctx context.Context, teamID, resourceRID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if !exactIdentifier(teamID) || !exactIdentifier(resourceRID) {
		return false, nil
	}
	teams, allowedTeams, err := access.current(ctx, resourceRID)
	if err != nil {
		return false, err
	}
	for _, team := range teams {
		if team.Name == teamID {
			return allowedTeams[teamID], nil
		}
	}
	return false, nil
}

func (access *RecipientAccess) current(ctx context.Context, resourceRID string) ([]domain.Team, map[string]bool, error) {
	teams, err := access.teams.ListTeams(ctx)
	if err != nil {
		return nil, nil, err
	}
	grants, err := access.authorizer.EffectiveGrants(ctx, resourceRID)
	if err != nil {
		return nil, nil, err
	}
	administrators, err := access.authorizer.EffectiveGrants(ctx, platformRoot)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	allowed := make(map[string]bool)
	for _, grant := range grants {
		if grantConveys(grant, domain.OperationReadResource) {
			allowed[grant.Team] = true
		}
	}
	for _, grant := range administrators {
		if grantConveys(grant, domain.OperationManagePermissions) {
			allowed[grant.Team] = true
		}
	}
	return teams, allowed, nil
}

func exactIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value)
}
