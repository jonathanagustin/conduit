// Package oidcverifier establishes that a bearer token is authentic, against a
// real OIDC issuer.
//
// This is the outbound adapter: discovery, key retrieval and signature checking
// live here and nowhere else. It answers one question — is this token genuinely
// from the issuer — and hands back the claims exactly as carried, because
// deciding which of them matter is the policy's job.
//
// There is deliberately no insecure TLS mode.
package oidcverifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// Verifier satisfies Identity's TokenVerifier port over go-oidc.
type Verifier struct {
	issuer   string
	verifier interface {
		Verify(context.Context, string) (*oidc.IDToken, error)
	}
}

// New discovers one exact issuer and returns a verifier for it.
//
// A policy with a JWKS URL skips discovery and uses that key set directly; a
// policy without one discovers the issuer's metadata. Either way the issuer the
// token must name is pinned from the policy, not read back from the provider.
func New(ctx context.Context, policy domain.ClaimPolicy, client *http.Client) (*Verifier, error) {
	discoveryContext := ctx
	if client != nil {
		discoveryContext = oidc.ClientContext(ctx, client)
	}
	configuration := &oidc.Config{
		ClientID:             policy.ClientID,
		SupportedSigningAlgs: append([]string(nil), policy.AllowedSigningAlgs...),
	}
	// A policy naming its own audience claim checks the audience itself, so the
	// library's client-ID check is turned off rather than applied twice against
	// a claim the token does not carry.
	if policy.AudienceClaim != "" {
		configuration.ClientID = ""
		configuration.SkipClientIDCheck = true
	}
	if policy.JWKSURL != "" {
		return &Verifier{issuer: policy.Issuer, verifier: oidc.NewVerifier(
			policy.Issuer, oidc.NewRemoteKeySet(discoveryContext, policy.JWKSURL), configuration,
		)}, nil
	}
	provider, err := oidc.NewProvider(discoveryContext, policy.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC issuer: %w", err)
	}
	return &Verifier{issuer: policy.Issuer, verifier: provider.Verifier(configuration)}, nil
}

// NewWithKeySet builds a verifier over a key set the caller already holds,
// skipping discovery. Production uses New; this exists so that the verification
// path — signature, issuer, audience, expiry and not-before — can be exercised
// end to end against a locally signed token rather than mocked away.
//
// Now is the clock the token's expiry and not-before are judged against.
func NewWithKeySet(policy domain.ClaimPolicy, keySet oidc.KeySet, now func() time.Time) *Verifier {
	configuration := &oidc.Config{
		ClientID:             policy.ClientID,
		SupportedSigningAlgs: append([]string(nil), policy.AllowedSigningAlgs...),
		Now:                  now,
	}
	if policy.AudienceClaim != "" {
		configuration.ClientID = ""
		configuration.SkipClientIDCheck = true
	}
	return &Verifier{issuer: policy.Issuer, verifier: oidc.NewVerifier(policy.Issuer, keySet, configuration)}
}

// Verify establishes authenticity and returns the claims undecoded.
func (adapter *Verifier) Verify(
	ctx context.Context, token string,
) (domain.VerifiedToken, map[string]json.RawMessage, error) {
	if adapter == nil || adapter.verifier == nil {
		return domain.VerifiedToken{}, nil, domain.ErrUnauthorized
	}
	verified, err := adapter.verifier.Verify(ctx, token)
	if err != nil || verified == nil {
		return domain.VerifiedToken{}, nil, domain.ErrUnauthorized
	}
	var raw map[string]json.RawMessage
	if err := verified.Claims(&raw); err != nil {
		return domain.VerifiedToken{}, nil, domain.ErrUnauthorized
	}
	return domain.VerifiedToken{
		Issuer: verified.Issuer, Subject: verified.Subject, IssuedAt: verified.IssuedAt,
	}, raw, nil
}
