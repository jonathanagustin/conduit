// Package signinhttp is the Hub's browser sign-in: three routes that take a
// person to the identity provider and bring back a session.
//
// The Hub is the OAuth client, not the browser. The authorization code, the
// PKCE verifier, the client secret and every token stay on this side; the
// browser is handed one opaque cookie that names a record here. That is the
// whole reason for the arrangement: a console that held a bearer token would
// hold it where any script on the page could read it.
//
// What each step must check is the domain's. This adapter carries cookies and
// redirects, and logs nothing a person could sign in with: not the session
// identifier, the code, a token, the state or the verifier.
package signinhttp

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

const (
	// LoginPath, CallbackPath and LogoutPath are fixed. The callback is the
	// address registered with the identity provider, so it is not configurable:
	// only the origin in front of it is.
	LoginPath    = "/v1/auth/login"
	CallbackPath = "/v1/auth/callback"
	LogoutPath   = "/v1/auth/logout"

	// FailedReturnPath is where every refused sign-in lands. One address for
	// every reason, so the redirect is not an oracle for which check refused.
	FailedReturnPath = "/console/?sign-in=failed"

	maximumStateBytes = 128
	maximumCodeBytes  = 4096
)

// Config is everything sign-in needs, supplied by the composition root.
type Config struct {
	// PublicOrigin is the https origin browsers reach the console at. It comes
	// from configuration; no request header contributes to it.
	PublicOrigin string
	// Policy is the same normalized policy the bearer path enforces.
	Policy   domain.ClaimPolicy
	Verifier ports.TokenVerifier
	Provider ports.AuthorizationCodeProvider
	Sessions ports.SessionStore
	// SessionLifetime bounds a session; zero means the default.
	SessionLifetime time.Duration
	// Scopes are requested at the provider. "openid" is always among them.
	Scopes []string
	Now    func() time.Time
	Logger *slog.Logger
}

// Handler serves the three routes.
type Handler struct {
	config      Config
	redirectURI string
	cookies     domain.CookiePolicy
}

// New validates the configuration. A sign-in that cannot be stated exactly is
// refused here, at start, rather than discovered by the first person to try it.
func New(config Config) (*Handler, error) {
	origin, err := domain.NormalizePublicOrigin(config.PublicOrigin)
	if err != nil {
		return nil, err
	}
	if origin != config.PublicOrigin {
		return nil, errors.New("the console public origin must be supplied normalized")
	}
	if config.Verifier == nil || config.Provider == nil || config.Sessions == nil {
		return nil, errors.New("interactive sign-in requires a token verifier, a provider and a session store")
	}
	if config.Policy.Issuer == "" || config.Policy.ClientID == "" || config.Policy.MaximumTokenBytes == 0 {
		return nil, errors.New("interactive sign-in requires the normalized OIDC claim policy")
	}
	config.SessionLifetime, err = domain.NormalizeSessionLifetime(config.SessionLifetime)
	if err != nil {
		return nil, err
	}
	scopes := []string{"openid"}
	for _, scope := range config.Scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" || scope == "openid" {
			continue
		}
		if len(scope) > 128 || strings.ContainsAny(scope, " \t\r\n\"\\") {
			return nil, errors.New("an OIDC sign-in scope is not a scope token")
		}
		scopes = append(scopes, scope)
	}
	config.Scopes = scopes
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	return &Handler{
		config: config, redirectURI: origin + CallbackPath,
		cookies: domain.CookiePolicy{PublicOrigin: origin},
	}, nil
}

// CookiePolicy is the rule the session resolver must apply, built from the same
// origin these routes use.
func (handler *Handler) CookiePolicy() domain.CookiePolicy { return handler.cookies }

// RedirectURI is the callback address to register with the identity provider.
func (handler *Handler) RedirectURI() string { return handler.redirectURI }

// Register mounts the routes. A nil handler mounts the same patterns answering
// 404 with a reason, so that a Hub without sign-in says so rather than serving
// whatever lies beneath.
func (handler *Handler) Register(mux *http.ServeMux) {
	if handler == nil {
		for _, pattern := range []string{"GET /v1/auth/login", "GET /v1/auth/callback", "POST /v1/auth/logout"} {
			mux.HandleFunc(pattern, notConfigured)
		}
		return
	}
	mux.HandleFunc("GET /v1/auth/login", handler.login)
	mux.HandleFunc("GET /v1/auth/callback", handler.callback)
	mux.HandleFunc("POST /v1/auth/logout", handler.logout)
}

func notConfigured(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusNotFound, map[string]string{
		"error": "interactive sign-in is not configured on this Hub", "status": "404",
	})
}

func (handler *Handler) login(writer http.ResponseWriter, request *http.Request) {
	noStore(writer)
	now := handler.config.Now()
	state, stateErr := domain.NewOpaqueSecret()
	nonce, nonceErr := domain.NewOpaqueSecret()
	verifier, verifierErr := domain.NewOpaqueSecret()
	binding, bindingErr := domain.NewOpaqueSecret()
	if err := errors.Join(stateErr, nonceErr, verifierErr, bindingErr); err != nil {
		handler.config.Logger.Error("operator sign-in could not draw randomness")
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "sign-in is unavailable", "status": "503"})
		return
	}
	attempt := domain.LoginAttempt{
		StateHash: domain.HashSecret(state), BindingHash: domain.HashSecret(binding),
		Nonce: nonce, CodeVerifier: verifier,
		ReturnPath: domain.ConsoleReturnPath(request.URL.Query().Get("returnTo")),
		ExpiresAt:  now.Add(domain.LoginAttemptLifetime),
	}
	if err := handler.config.Sessions.SaveLoginAttempt(request.Context(), attempt, now); err != nil {
		handler.config.Logger.Warn("operator sign-in could not record an attempt",
			"storeFull", errors.Is(err, domain.ErrSessionStoreFull))
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "sign-in is unavailable", "status": "503"})
		return
	}
	destination, err := url.Parse(handler.config.Provider.AuthorizationEndpoint())
	if err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "sign-in is unavailable", "status": "503"})
		return
	}
	query := destination.Query()
	query.Set("response_type", "code")
	query.Set("client_id", handler.config.Policy.ClientID)
	// From configuration. The Host header is not consulted anywhere here.
	query.Set("redirect_uri", handler.redirectURI)
	query.Set("scope", strings.Join(handler.config.Scopes, " "))
	query.Set("state", state)
	query.Set("nonce", nonce)
	query.Set("code_challenge", domain.PKCEChallenge(verifier))
	query.Set("code_challenge_method", "S256")
	destination.RawQuery = query.Encode()
	http.SetCookie(writer, hostCookie(domain.LoginCookieName, binding, int(domain.LoginAttemptLifetime/time.Second)))
	http.Redirect(writer, request, destination.String(), http.StatusFound)
}

func (handler *Handler) callback(writer http.ResponseWriter, request *http.Request) {
	noStore(writer)
	// Whatever happens next, this attempt's binding cookie has done its work.
	http.SetCookie(writer, hostCookie(domain.LoginCookieName, "", -1))
	claims, expiry, returnPath, reason := handler.redeem(request)
	if reason != "" {
		handler.config.Logger.Warn("operator sign-in refused", "reason", reason)
		http.Redirect(writer, request, FailedReturnPath, http.StatusSeeOther)
		return
	}
	now := handler.config.Now()
	identifier, err := domain.NewOpaqueSecret()
	if err != nil {
		handler.config.Logger.Error("operator sign-in could not draw randomness")
		http.Redirect(writer, request, FailedReturnPath, http.StatusSeeOther)
		return
	}
	session := domain.Session{IDHash: domain.HashSecret(identifier), Claims: claims, CreatedAt: now.UTC(), ExpiresAt: expiry}
	if err := handler.config.Sessions.SaveSession(request.Context(), session); err != nil {
		handler.config.Logger.Warn("operator sign-in could not record the session",
			"storeFull", errors.Is(err, domain.ErrSessionStoreFull))
		http.Redirect(writer, request, FailedReturnPath, http.StatusSeeOther)
		return
	}
	maxAge := int(expiry.Sub(now) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	http.SetCookie(writer, hostCookie(domain.SessionCookieName, identifier, maxAge))
	handler.config.Logger.Info("operator signed in", "username", claims.Username, "expiresAt", expiry)
	http.Redirect(writer, request, returnPath, http.StatusSeeOther)
}

// redeem runs every check between a callback request and an identity. It
// returns a reason instead of an identity when any of them fails; the reason is
// for the Hub's own log and names a step, never a value.
func (handler *Handler) redeem(request *http.Request) (domain.Claims, time.Time, string, string) {
	refuse := func(reason string) (domain.Claims, time.Time, string, string) {
		return domain.Claims{}, time.Time{}, "", reason
	}
	now := handler.config.Now()
	query := request.URL.Query()
	states := query["state"]
	if len(states) != 1 || states[0] == "" || len(states[0]) > maximumStateBytes {
		return refuse("state missing or malformed")
	}
	// Taken, not read: the state is spent by this request whether or not the
	// rest succeeds, so a replay of the same callback finds nothing.
	attempt, err := handler.config.Sessions.TakeLoginAttempt(request.Context(), domain.HashSecret(states[0]), now)
	if err != nil {
		return refuse("state unknown, expired or already used")
	}
	binding, present := httpidentity.LoginCookie(request)
	if !present || !domain.SecretMatchesHash(binding, attempt.BindingHash) {
		return refuse("the browser did not start this sign-in")
	}
	if query.Has("error") {
		return refuse("the provider reported an error")
	}
	codes := query["code"]
	if len(codes) != 1 || codes[0] == "" || len(codes[0]) > maximumCodeBytes {
		return refuse("code missing or malformed")
	}
	idToken, err := handler.config.Provider.Exchange(request.Context(), codes[0], attempt.CodeVerifier, handler.redirectURI)
	if err != nil {
		return refuse("code exchange failed: " + err.Error())
	}
	if len(idToken) > handler.config.Policy.MaximumTokenBytes {
		return refuse("ID token too large")
	}
	verified, raw, err := handler.config.Verifier.Verify(request.Context(), idToken)
	if err != nil {
		return refuse("ID token did not verify")
	}
	if !domain.IDTokenBoundToLogin(raw, handler.config.Policy.ClientID, attempt.Nonce) {
		return refuse("ID token nonce or audience mismatch")
	}
	claims, err := handler.config.Policy.OperatorClaims(verified, raw, now)
	if err != nil {
		return refuse("ID token refused by the operator claim policy")
	}
	tokenExpiry, present := domain.TokenExpiry(raw)
	if !present {
		return refuse("ID token carries no expiry")
	}
	expiry, valid := domain.SessionExpiry(now, tokenExpiry, handler.config.SessionLifetime)
	if !valid {
		return refuse("ID token already expired")
	}
	return claims, expiry, domain.ConsoleReturnPath(attempt.ReturnPath), ""
}

func (handler *Handler) logout(writer http.ResponseWriter, request *http.Request) {
	noStore(writer)
	identifier, present := httpidentity.SessionCookie(request)
	if present {
		// Signing somebody out from another site is a nuisance rather than a
		// breach, but the rule for cookies is one rule.
		if !handler.cookies.Allows(httpidentity.CookieRequestFrom(request)) {
			writeJSON(writer, http.StatusForbidden, map[string]string{
				"error": "sign-out must come from the console and carry " + domain.CSRFHeaderName, "status": "403",
			})
			return
		}
		if err := handler.config.Sessions.DeleteSession(request.Context(), domain.HashSecret(identifier)); err != nil {
			// The cookie is deliberately left alone: clearing it would tell the
			// person they are signed out while the session still resolves.
			handler.config.Logger.Error("operator sign-out could not delete the session")
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{
				"error": "sign-out could not be completed; try again", "status": "503",
			})
			return
		}
	}
	http.SetCookie(writer, hostCookie(domain.SessionCookieName, "", -1))
	body := map[string]any{"signedOut": true}
	if endpoint := handler.config.Provider.EndSessionEndpoint(); endpoint != "" {
		if destination, err := url.Parse(endpoint); err == nil {
			query := destination.Query()
			query.Set("client_id", handler.config.Policy.ClientID)
			query.Set("post_logout_redirect_uri", handler.cookies.PublicOrigin+domain.DefaultConsoleReturnPath)
			destination.RawQuery = query.Encode()
			body["endSessionUrl"] = destination.String()
		}
	}
	writeJSON(writer, http.StatusOK, body)
}

// hostCookie builds a __Host- cookie: Secure, Path=/, no Domain, unreadable by
// script, and not sent on cross-site subrequests. A negative maxAge deletes it.
func hostCookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge,
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
}

func noStore(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
