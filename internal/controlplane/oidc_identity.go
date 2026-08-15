package controlplane

import (
	"context"
	"net/http"
	"time"

	httpidentity "github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
	oidcverifier "github.com/jonathanagustin/conduit/internal/identity/adapters/oidcverifier"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
)

// Turning a credential into an operator is Identity's, and this is the wiring
// that is left: the deployment's configuration, a verifier for the issuer, and
// the resolver the HTTP layer already expects.
//
// What a verified token must say to be an operator — the issuer, audience,
// purpose and scope checks, the freshness bound, the claim shapes and their
// limits — is `internal/identity/domain` and is testable against a token
// literal. How keys are fetched and signatures checked is the OIDC adapter. How
// a credential arrives is the HTTP adapter. None of the three can be swapped
// for another by changing this file, which is the point of the split.

// OIDCOperatorIdentityConfig defines a standard OIDC verification boundary.
// Provider-specific administration remains outside Platform.
type OIDCOperatorIdentityConfig struct {
	identity.ClaimPolicy

	// The mechanics the policy deliberately does not carry.
	HTTPClient *http.Client
	Now        func() time.Time
}

// NewOIDCOperatorIdentityResolver validates the configuration, discovers one
// exact issuer, and returns the production request resolver used by the Hub.
func NewOIDCOperatorIdentityResolver(
	ctx context.Context, configuration OIDCOperatorIdentityConfig,
) (OperatorIdentityResolver, error) {
	policy, err := identity.NormalizeClaimPolicy(configuration.ClaimPolicy)
	if err != nil {
		return nil, err
	}
	verifier, err := oidcverifier.New(ctx, policy, configuration.HTTPClient)
	if err != nil {
		return nil, err
	}
	return httpidentity.OperatorResolver(policy, verifier, configuration.Now), nil
}
