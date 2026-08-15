// Identity aliases.
//
// The translation from credential to principal lives in internal/identity. An
// alias is the same type, so every caller and every JSON shape here is
// unchanged.

package controlplane

import identity "github.com/jonathanagustin/conduit/internal/identity/domain"

type (
	IdentityClaims = identity.Claims
	AgentIdentity  = identity.AgentIdentity
)
