package composition

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/oidcprovider"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/oidcverifier"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/signinhttp"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// OperatorIdentityConfig is one issuer and, optionally, a browser sign-in
// against it.
type OperatorIdentityConfig struct {
	Policy     domain.ClaimPolicy
	HTTPClient *http.Client
	Now        func() time.Time
	// SignIn is nil for a Hub that only verifies bearer tokens, which is every
	// Hub that has not been told the address browsers reach it at.
	SignIn *OperatorSignInConfig
}

// OperatorSignInConfig turns browser sign-in on.
type OperatorSignInConfig struct {
	// PublicURL is the https origin of the console as a browser sees it.
	PublicURL string
	// ClientSecret is empty for a public client.
	ClientSecret    string
	SessionLifetime time.Duration
	Scopes          []string
	Sessions        ports.SessionStore
	Logger          *slog.Logger
}

// OperatorIdentity is what a Hub mounts.
type OperatorIdentity struct {
	// Resolver is the request resolver the Hub's handlers already expect.
	Resolver func(*http.Request) (domain.Claims, error)
	// SignIn is nil when sign-in is off.
	SignIn *signinhttp.Handler
	// PublicOrigin is the normalized origin sign-in was configured with.
	PublicOrigin string
}

// NewOperatorIdentity builds the bearer resolver exactly as it has always been
// built — one normalized policy, one verifier — and, when sign-in is
// configured, lets a session stand in front of it.
//
// The bearer resolver and the sign-in callback share the policy and the
// verifier by construction: there is one of each. A session can therefore only
// ever hold claims the bearer path would have produced from the same token.
func NewOperatorIdentity(ctx context.Context, config OperatorIdentityConfig) (OperatorIdentity, error) {
	policy, err := domain.NormalizeClaimPolicy(config.Policy)
	if err != nil {
		return OperatorIdentity{}, err
	}
	verifier, err := oidcverifier.New(ctx, policy, config.HTTPClient)
	if err != nil {
		return OperatorIdentity{}, err
	}
	bearer := httpidentity.OperatorResolver(policy, verifier, config.Now)
	if config.SignIn == nil {
		return OperatorIdentity{Resolver: bearer}, nil
	}
	if config.SignIn.Sessions == nil {
		return OperatorIdentity{}, errors.New("interactive sign-in requires a session store")
	}
	origin, err := domain.NormalizePublicOrigin(config.SignIn.PublicURL)
	if err != nil {
		return OperatorIdentity{}, err
	}
	provider, err := oidcprovider.New(ctx, oidcprovider.Config{
		Issuer: policy.Issuer, ClientID: policy.ClientID,
		ClientSecret: config.SignIn.ClientSecret, HTTPClient: config.HTTPClient,
	})
	if err != nil {
		return OperatorIdentity{}, err
	}
	handler, err := signinhttp.New(signinhttp.Config{
		PublicOrigin: origin, Policy: policy, Verifier: verifier, Provider: provider,
		Sessions: config.SignIn.Sessions, SessionLifetime: config.SignIn.SessionLifetime,
		Scopes: config.SignIn.Scopes, Now: config.Now, Logger: config.SignIn.Logger,
	})
	if err != nil {
		return OperatorIdentity{}, err
	}
	return OperatorIdentity{
		Resolver:     httpidentity.WithOperatorSessions(bearer, config.SignIn.Sessions, handler.CookiePolicy(), config.Now),
		SignIn:       handler,
		PublicOrigin: origin,
	}, nil
}
