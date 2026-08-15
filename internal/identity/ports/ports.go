// Package ports declares what Identity needs in order to turn a bearer token
// into an operator, and nothing more.
//
// There is one port and it does one thing: establish that a token is
// authentic. What the token then has to *say* is the policy's business, and
// keeping the two apart is the point — a verifier that also decided who was
// allowed in would put the identity rules behind a network call and out of
// reach of a test.
package ports

import (
	"context"
	"encoding/json"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// TokenVerifier checks a token's signature against an issuer's keys and
// returns what it established, plus the claims exactly as they were carried.
//
// The raw claims are handed back undecoded on purpose. A verifier that decoded
// them would be choosing which ones matter, and that choice belongs to the
// policy — a claim nobody was told to read must not be silently accepted.
//
// An error means the token is not authentic. It never means the token is
// authentic but unwelcome; that answer comes from the policy.
type TokenVerifier interface {
	Verify(ctx context.Context, token string) (domain.VerifiedToken, map[string]json.RawMessage, error)
}
