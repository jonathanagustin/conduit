package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/sessionmemory"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/sessionpostgres"
	identitycomposition "github.com/jonathanagustin/conduit/internal/identity/composition"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	identityports "github.com/jonathanagustin/conduit/internal/identity/ports"
)

// loadOperatorIdentity reads the operator OIDC configuration and, when the Hub
// has also been told the address browsers reach it at, turns on sign-in.
//
// Sign-in is on only when BOTH an issuer and CONSOLE_PUBLIC_URL are configured.
// The public address is never inferred: it becomes the OAuth redirect address
// and the origin a session cookie is honoured from, and the only other place to
// read it would be a request's Host header, which is whatever the caller sent.
//
// A sign-in setting without the things it depends on stops the Hub, on the same
// reasoning as every other half-configured identity mode here.
func loadOperatorIdentity(
	ctx context.Context, getenv func(string) string,
) (identitycomposition.OperatorIdentity, func(), error) {
	none := func() {}
	configuration, err := loadOIDCOperatorIdentityConfig(getenv)
	if err != nil {
		return identitycomposition.OperatorIdentity{}, none, err
	}
	publicURL := strings.TrimSpace(getenv("CONSOLE_PUBLIC_URL"))
	secretFile := strings.TrimSpace(getenv("OPERATOR_OIDC_CLIENT_SECRET_FILE"))
	lifetimeText := strings.TrimSpace(getenv("OPERATOR_SESSION_MAX_LIFETIME"))
	scopesText := strings.TrimSpace(getenv("OPERATOR_OIDC_LOGIN_SCOPES"))
	if configuration == nil {
		// CONSOLE_PUBLIC_URL alone is not an error: a deployment may state its
		// address for other reasons. A session lifetime with no issuer is.
		if lifetimeText != "" {
			return identitycomposition.OperatorIdentity{}, none,
				errors.New("OPERATOR_SESSION_MAX_LIFETIME requires OPERATOR_OIDC_ISSUER")
		}
		return identitycomposition.OperatorIdentity{}, none, nil
	}
	composed := identitycomposition.OperatorIdentityConfig{
		Policy: configuration.ClaimPolicy, HTTPClient: configuration.HTTPClient, Now: configuration.Now,
	}
	if publicURL == "" {
		if secretFile != "" || lifetimeText != "" || scopesText != "" {
			return identitycomposition.OperatorIdentity{}, none, errors.New(
				"OPERATOR_OIDC_CLIENT_SECRET_FILE, OPERATOR_OIDC_LOGIN_SCOPES and OPERATOR_SESSION_MAX_LIFETIME " +
					"configure interactive sign-in and require CONSOLE_PUBLIC_URL")
		}
		resolved, err := identitycomposition.NewOperatorIdentity(ctx, composed)
		return resolved, none, err
	}
	signIn := &identitycomposition.OperatorSignInConfig{PublicURL: publicURL, Logger: slog.Default()}
	if lifetimeText != "" {
		lifetime, err := time.ParseDuration(lifetimeText)
		if err != nil {
			return identitycomposition.OperatorIdentity{}, none, fmt.Errorf("parse OPERATOR_SESSION_MAX_LIFETIME: %w", err)
		}
		// Zero is refused here even though the domain reads it as "default":
		// somebody who wrote 0 did not mean eight hours.
		if lifetime == 0 {
			return identitycomposition.OperatorIdentity{}, none,
				errors.New("OPERATOR_SESSION_MAX_LIFETIME must be between 5m and 24h")
		}
		if signIn.SessionLifetime, err = identity.NormalizeSessionLifetime(lifetime); err != nil {
			return identitycomposition.OperatorIdentity{}, none,
				errors.New("OPERATOR_SESSION_MAX_LIFETIME must be between 5m and 24h")
		}
	}
	signIn.Scopes = []string{"profile", "email"}
	if scopesText != "" {
		signIn.Scopes = strings.Fields(scopesText)
	}
	if secretFile != "" {
		// A confidential client. The secret is read from a mounted file so that
		// it is never in the process environment or a manifest's env list.
		if signIn.ClientSecret, err = readSingleLineSecretFile(secretFile, "OPERATOR_OIDC_CLIENT_SECRET_FILE"); err != nil {
			return identitycomposition.OperatorIdentity{}, none, err
		}
	}
	sessions, closeSessions, err := openOperatorSessions(ctx, getenv)
	if err != nil {
		return identitycomposition.OperatorIdentity{}, none, err
	}
	signIn.Sessions = sessions
	composed.SignIn = signIn
	resolved, err := identitycomposition.NewOperatorIdentity(ctx, composed)
	if err != nil {
		closeSessions()
		return identitycomposition.OperatorIdentity{}, none, err
	}
	if profile := strings.TrimSpace(getenv("OPERATOR_OIDC_PROFILE")); profile == "cognito-access" {
		slog.Warn("interactive sign-in applies the operator claim policy to the ID token; "+
			"the cognito-access profile accepts only access tokens, so browser sign-in will be refused", "profile", profile)
	}
	pruneContext, stopPruning := context.WithCancel(ctx)
	go pruneOperatorSessions(pruneContext, sessions, 5*time.Minute)
	slog.Info("interactive operator sign-in enabled", "publicOrigin", resolved.PublicOrigin,
		"redirectURI", resolved.SignIn.RedirectURI(), "confidentialClient", secretFile != "")
	return resolved, func() { stopPruning(); closeSessions() }, nil
}

// openOperatorSessions picks the session store the repository kind implies.
// PostgreSQL shares sessions between replicas; the file repository is a single
// process and keeps them in memory, where a restart signs everybody out.
func openOperatorSessions(ctx context.Context, getenv func(string) string) (identityports.SessionStore, func(), error) {
	switch strings.TrimSpace(getenv("REPOSITORY_KIND")) {
	case "", "file":
		return sessionmemory.New(), func() {}, nil
	case "postgres":
		dsn, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
		if err != nil {
			return nil, func() {}, err
		}
		store, err := sessionpostgres.New(ctx, dsn)
		if err != nil {
			return nil, func() {}, errors.New("initialize PostgreSQL operator session component failed")
		}
		return store, func() { _ = store.Close() }, nil
	default:
		return nil, func() {}, errors.New("REPOSITORY_KIND must be file or postgres")
	}
}

// pruneOperatorSessions is housekeeping. Expiry is enforced on every read, so
// a pass that fails leaves the store larger and no more permissive.
func pruneOperatorSessions(ctx context.Context, sessions identityports.SessionStore, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := sessions.Prune(ctx, now); err != nil && ctx.Err() == nil {
				slog.Warn("prune expired operator sessions", "error", err)
			}
		}
	}
}
