// Package nativeauth binds delivery identities to current native operator,
// delegation and grant snapshots. Trusted Hub composition owns all resolution.
package nativeauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"unicode/utf8"

	governance "github.com/jonathanagustin/conduit/internal/governance/domain"
	live "github.com/jonathanagustin/conduit/internal/liveevents/domain"
)

type State struct {
	Principal     governance.Principal
	Grants        []governance.ResourceGrant
	Administrator bool
	Allowed       bool
}
type Config struct {
	// Resolve authenticates the original request again and reads native grants.
	// It must never resolve identity from a cursor, browser state, or event JSON.
	Resolve func(context.Context) (State, error)
	Visible func(context.Context, governance.Principal, live.Subject) (bool, error)
}
type Authorizer struct{ cfg Config }

func New(cfg Config) (*Authorizer, error) {
	if cfg.Resolve == nil || cfg.Visible == nil {
		return nil, live.ErrInvalid
	}
	return &Authorizer{cfg: cfg}, nil
}

func (authorizer *Authorizer) current(ctx context.Context) (State, live.Identity, error) {
	if err := ctx.Err(); err != nil {
		return State{}, live.Identity{}, err
	}
	state, err := authorizer.cfg.Resolve(ctx)
	if err != nil {
		return State{}, live.Identity{}, err
	}
	if state.Principal.Username == "" {
		return State{}, live.Identity{}, live.ErrUnauthorized
	}
	if !state.Allowed {
		return State{}, live.Identity{}, live.ErrForbidden
	}
	principal, ok := normalizePrincipal(state.Principal, 0)
	if !ok {
		return State{}, live.Identity{}, live.ErrInvalid
	}
	state.Principal = principal
	// Normalize copied grant values so storage order and slice aliases do not
	// invent authorization epochs or let a caller mutate a captured stamp.
	grants := make([]governance.ResourceGrant, 0, len(state.Grants))
	for _, grant := range state.Grants {
		if !validStrings([]string{grant.ID, grant.ResourceRID, grant.Team, grant.Relationship}) || !validStrings(grant.Roles) || !validStrings(grant.Operations) {
			return State{}, live.Identity{}, live.ErrInvalid
		}
		grant.Roles = normalized(grant.Roles)
		grant.Operations = normalized(grant.Operations)
		grants = append(grants, grant)
	}
	sort.Slice(grants, func(i, j int) bool {
		if grants[i].ID == grants[j].ID {
			return grants[i].ResourceRID < grants[j].ResourceRID
		}
		return grants[i].ID < grants[j].ID
	})
	state.Grants = grants
	owner := struct {
		Username       string
		ServiceAccount bool
	}{principal.Username, principal.ServiceAccount}
	represented := governance.EffectivePrincipal(principal)
	delegate := struct {
		Username       string
		ServiceAccount bool
	}{represented.Username, represented.ServiceAccount}
	type grantRevision struct {
		ID, ResourceRID, Team, Relationship string
		Roles, Operations                   []string
		Revision                            uint64
	}
	revisions := make([]grantRevision, 0, len(grants))
	for _, grant := range grants {
		revisions = append(revisions, grantRevision{grant.ID, grant.ResourceRID, grant.Team, grant.Relationship, grant.Roles, grant.Operations, grant.Revision})
	}
	revision := struct {
		Policy        string
		Principal     governance.Principal
		Grants        []grantRevision
		Administrator bool
	}{"conduit-native-event-read/v1", principal, revisions, state.Administrator}
	identity := live.Identity{Owner: digest(owner), Represented: digest(delegate), Authorization: digest(revision)}
	return state, identity, nil
}
func (authorizer *Authorizer) Snapshot(ctx context.Context) (live.Identity, error) {
	_, identity, err := authorizer.current(ctx)
	return identity, err
}
func (authorizer *Authorizer) Visible(ctx context.Context, expected live.Identity, subject live.Subject) (bool, error) {
	state, identity, err := authorizer.current(ctx)
	if err != nil {
		return false, err
	}
	if identity != expected {
		return false, live.ErrReset
	}
	return authorizer.cfg.Visible(ctx, state.Principal, subject)
}
func normalized(values []string) []string {
	copied := append([]string(nil), values...)
	sort.Strings(copied)
	return copied
}
func normalizePrincipal(principal governance.Principal, depth int) (governance.Principal, bool) {
	if depth > 8 || principal.Username == "" || !utf8.ValidString(principal.Username) || !validStrings(principal.Groups) || !validStrings(principal.Teams) {
		return governance.Principal{}, false
	}
	principal.Groups = normalized(principal.Groups)
	principal.Teams = normalized(principal.Teams)
	if principal.OnBehalfOf != nil {
		delegate, ok := normalizePrincipal(*principal.OnBehalfOf, depth+1)
		if !ok {
			return governance.Principal{}, false
		}
		principal.OnBehalfOf = &delegate
	}
	return principal, true
}
func digest(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func validStrings(values []string) bool {
	for _, value := range values {
		if !utf8.ValidString(value) {
			return false
		}
	}
	return true
}
