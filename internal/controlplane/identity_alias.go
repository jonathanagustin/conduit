// Identity aliases.
//
// The translation from credential to principal lives in internal/identity. An
// alias is the same type, so every caller and every JSON shape here is
// unchanged.

package controlplane

import "github.com/jonathanagustin/conduit/internal/identity"

type (
	IdentityClaims = identity.Claims
	AgentIdentity  = identity.AgentIdentity
)
