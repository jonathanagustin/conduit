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

// ErrUnauthorized is Identity's error, not a second one with the same text.
//
// It used to be declared here separately. The production resolver returns
// Identity's, every handler compared against this package's, and the two never
// matched: a Hub verifying real tokens answered a missing or refused credential
// with 500 rather than 401. Nothing showed it because the header-based test
// mode never produces the error at all. One value, so a refused credential is
// a 401 whichever layer refused it.
var ErrUnauthorized = identity.ErrUnauthorized
