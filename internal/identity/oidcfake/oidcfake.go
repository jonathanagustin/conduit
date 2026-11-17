// Package oidcfake is an OpenID Connect provider for tests.
//
// It is a real provider in every respect a client can observe: it serves
// discovery and a key set over TLS, signs ID tokens with a real key, binds an
// authorization code to its PKCE challenge and redirect address and refuses a
// code presented with the wrong verifier, authenticates a confidential client,
// and runs the device authorization grant through pending, slow_down, denial
// and success. What it does not have is a login page: whoever reaches the
// authorization endpoint is signed in as Identity. That is the one place it is
// a fake, and it is why this package must never be linked into a Hub.
//
// The checks it performs are the point. A client that forgot to send its PKCE
// verifier passes against a provider that does not look; it fails against this.
package oidcfake

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// Identity is who the provider says a person is.
type Identity struct {
	Subject  string
	Username string
	Groups   []string
}

// Options configure a provider before it starts.
type Options struct {
	ClientID string
	// ClientSecret makes the client confidential. Empty is a public client.
	ClientSecret string
	// RedirectURIs are the registered callback addresses, matched exactly. An
	// entry ending in "*" matches by prefix, for a loopback client whose port
	// is chosen at run time.
	RedirectURIs []string
	Identity     Identity
	// Now is the provider's clock. Nil is the wall clock.
	Now func() time.Time
	// TokenLifetime is how long an ID token is valid for. Zero is five minutes.
	TokenLifetime time.Duration
	// WithoutDeviceGrant leaves device_authorization_endpoint out of discovery.
	WithoutDeviceGrant bool
	// WithoutEndSession leaves end_session_endpoint out of discovery.
	WithoutEndSession bool
}

// TokenRequest is one call the token endpoint received, kept so that a test
// can assert on what a client actually sent.
type TokenRequest struct {
	Form          url.Values
	BasicUsername string
	BasicPassword string
	BasicPresent  bool
	Granted       bool
}

type grant struct {
	challenge, nonce, redirectURI string
}

type deviceGrant struct {
	userCode string
	polls    int
	script   []string
}

// Provider is a running fake.
type Provider struct {
	Server  *httptest.Server
	options Options
	key     *rsa.PrivateKey

	mutex    sync.Mutex
	codes    map[string]grant
	devices  map[string]*deviceGrant
	refresh  map[string]bool
	requests []TokenRequest
	// mutateClaims, when set, edits an ID token's claims before signing.
	mutateClaims func(map[string]any)
	// signingKey, when set, signs instead of the published key.
	signingKey *rsa.PrivateKey
	// deviceScript is what the next device grant's polls answer, in order;
	// once exhausted the grant succeeds.
	deviceScript []string
}

// Start runs a provider on a loopback TLS listener. Stop it with Close.
func Start(options Options) (*Provider, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.TokenLifetime == 0 {
		options.TokenLifetime = 5 * time.Minute
	}
	if options.Identity.Subject == "" {
		options.Identity = Identity{Subject: "subject-ada", Username: "ada", Groups: []string{"operators"}}
	}
	provider := &Provider{
		options: options, key: key,
		codes: map[string]grant{}, devices: map[string]*deviceGrant{}, refresh: map[string]bool{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", provider.discovery)
	mux.HandleFunc("GET /jwks", provider.jwks)
	mux.HandleFunc("GET /authorize", provider.authorize)
	mux.HandleFunc("POST /token", provider.token)
	mux.HandleFunc("POST /device", provider.device)
	mux.HandleFunc("GET /logout", func(writer http.ResponseWriter, request *http.Request) {
		if target := request.URL.Query().Get("post_logout_redirect_uri"); target != "" {
			http.Redirect(writer, request, target, http.StatusFound)
			return
		}
		_, _ = writer.Write([]byte("signed out"))
	})
	provider.Server = httptest.NewTLSServer(mux)
	return provider, nil
}

func (provider *Provider) Close() { provider.Server.Close() }

// Issuer is the provider's issuer identifier, an https URL.
func (provider *Provider) Issuer() string { return provider.Server.URL }

// Client is an HTTP client that trusts the provider's certificate.
func (provider *Provider) Client() *http.Client { return provider.Server.Client() }

// CertificatePEM is the provider's certificate, for a process that must be
// told to trust it through a CA file.
func (provider *Provider) CertificatePEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: provider.Server.Certificate().Raw})
}

// PublicKey is the key ID tokens are signed with.
func (provider *Provider) PublicKey() *rsa.PublicKey { return &provider.key.PublicKey }

// MutateClaims edits every ID token's claims before it is signed. Nil stops.
func (provider *Provider) MutateClaims(mutate func(map[string]any)) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.mutateClaims = mutate
}

// SignWithUnpublishedKey makes the provider sign with a key its key set does
// not contain, which is what a forged token looks like to a verifier.
func (provider *Provider) SignWithUnpublishedKey() error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.signingKey = key
	return nil
}

// ScriptDevicePolls sets what the next device grant answers to each poll before
// it succeeds: any of "authorization_pending", "slow_down", "access_denied",
// "expired_token".
func (provider *Provider) ScriptDevicePolls(answers ...string) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.deviceScript = append([]string(nil), answers...)
}

// TokenRequests returns every call the token endpoint has received.
func (provider *Provider) TokenRequests() []TokenRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]TokenRequest(nil), provider.requests...)
}

// IDToken signs an ID token directly, without the code exchange, for tests
// that compare a bearer token with a session built from the same claims.
func (provider *Provider) IDToken(nonce string) (string, error) { return provider.signIDToken(nonce) }

func (provider *Provider) discovery(writer http.ResponseWriter, _ *http.Request) {
	issuer := provider.Issuer()
	document := map[string]any{
		"issuer": issuer, "authorization_endpoint": issuer + "/authorize",
		"token_endpoint": issuer + "/token", "jwks_uri": issuer + "/jwks",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
	}
	if !provider.options.WithoutDeviceGrant {
		document["device_authorization_endpoint"] = issuer + "/device"
	}
	if !provider.options.WithoutEndSession {
		document["end_session_endpoint"] = issuer + "/logout"
	}
	writeJSON(writer, http.StatusOK, document)
}

func (provider *Provider) jwks(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &provider.key.PublicKey, KeyID: "fake-1", Algorithm: "RS256", Use: "sig",
	}}})
}

func (provider *Provider) redirectRegistered(candidate string) bool {
	for _, registered := range provider.options.RedirectURIs {
		if prefix, wildcard := strings.CutSuffix(registered, "*"); wildcard {
			if strings.HasPrefix(candidate, prefix) {
				return true
			}
			continue
		}
		if candidate == registered {
			return true
		}
	}
	return false
}

func (provider *Provider) authorize(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	redirectURI := query.Get("redirect_uri")
	// As a real provider does: an unregistered redirect address is refused on
	// the provider's own page and never redirected to.
	if query.Get("client_id") != provider.options.ClientID || !provider.redirectRegistered(redirectURI) {
		http.Error(writer, "unknown client or unregistered redirect_uri", http.StatusBadRequest)
		return
	}
	if query.Get("response_type") != "code" || query.Get("code_challenge_method") != "S256" ||
		query.Get("code_challenge") == "" || !strings.Contains(" "+query.Get("scope")+" ", " openid ") {
		http.Error(writer, "a code flow with an S256 challenge and the openid scope is required", http.StatusBadRequest)
		return
	}
	code := randomToken()
	provider.mutex.Lock()
	provider.codes[code] = grant{
		challenge: query.Get("code_challenge"), nonce: query.Get("nonce"), redirectURI: redirectURI,
	}
	provider.mutex.Unlock()
	destination, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(writer, "malformed redirect_uri", http.StatusBadRequest)
		return
	}
	values := destination.Query()
	values.Set("code", code)
	if state := query.Get("state"); state != "" {
		values.Set("state", state)
	}
	destination.RawQuery = values.Encode()
	http.Redirect(writer, request, destination.String(), http.StatusFound)
}

func (provider *Provider) device(writer http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil || request.PostForm.Get("client_id") != provider.options.ClientID {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid_client"})
		return
	}
	deviceCode, userCode := randomToken(), "WDJB-MJHT"
	provider.mutex.Lock()
	provider.devices[deviceCode] = &deviceGrant{userCode: userCode, script: provider.deviceScript}
	provider.deviceScript = nil
	provider.mutex.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{
		"device_code": deviceCode, "user_code": userCode,
		"verification_uri":          provider.Issuer() + "/activate",
		"verification_uri_complete": provider.Issuer() + "/activate?user_code=" + userCode,
		"expires_in":                600, "interval": 1,
	})
}

func (provider *Provider) token(writer http.ResponseWriter, request *http.Request) {
	if err := request.ParseForm(); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	record := TokenRequest{Form: request.PostForm}
	record.BasicUsername, record.BasicPassword, record.BasicPresent = request.BasicAuth()
	status, body := provider.grant(request, record)
	record.Granted = status == http.StatusOK
	provider.mutex.Lock()
	provider.requests = append(provider.requests, record)
	provider.mutex.Unlock()
	writeJSON(writer, status, body)
}

func (provider *Provider) grant(request *http.Request, record TokenRequest) (int, map[string]any) {
	refuse := func(code string) (int, map[string]any) {
		return http.StatusBadRequest, map[string]any{"error": code}
	}
	form := request.PostForm
	clientID, secret := form.Get("client_id"), form.Get("client_secret")
	if record.BasicPresent {
		// RFC 6749 section 2.3.1: the client form-encodes both halves.
		decodedID, idErr := url.QueryUnescape(record.BasicUsername)
		decodedSecret, secretErr := url.QueryUnescape(record.BasicPassword)
		if idErr != nil || secretErr != nil {
			return http.StatusUnauthorized, map[string]any{"error": "invalid_client"}
		}
		clientID, secret = decodedID, decodedSecret
	}
	if clientID != provider.options.ClientID ||
		subtle.ConstantTimeCompare([]byte(secret), []byte(provider.options.ClientSecret)) != 1 {
		return http.StatusUnauthorized, map[string]any{"error": "invalid_client"}
	}
	switch form.Get("grant_type") {
	case "authorization_code":
		provider.mutex.Lock()
		granted, known := provider.codes[form.Get("code")]
		// Single use, whatever happens next.
		delete(provider.codes, form.Get("code"))
		provider.mutex.Unlock()
		if !known || granted.redirectURI != form.Get("redirect_uri") {
			return refuse("invalid_grant")
		}
		sum := sha256.Sum256([]byte(form.Get("code_verifier")))
		if form.Get("code_verifier") == "" ||
			base64.RawURLEncoding.EncodeToString(sum[:]) != granted.challenge {
			return refuse("invalid_grant")
		}
		return provider.tokens(granted.nonce)
	case "urn:ietf:params:oauth:grant-type:device_code":
		provider.mutex.Lock()
		pending, known := provider.devices[form.Get("device_code")]
		answer := ""
		if known {
			if pending.polls < len(pending.script) {
				answer = pending.script[pending.polls]
			}
			pending.polls++
			if answer == "" || answer == "access_denied" || answer == "expired_token" {
				delete(provider.devices, form.Get("device_code"))
			}
		}
		provider.mutex.Unlock()
		if !known {
			return refuse("invalid_grant")
		}
		if answer != "" {
			return refuse(answer)
		}
		return provider.tokens("")
	case "refresh_token":
		provider.mutex.Lock()
		known := provider.refresh[form.Get("refresh_token")]
		delete(provider.refresh, form.Get("refresh_token"))
		provider.mutex.Unlock()
		if !known {
			return refuse("invalid_grant")
		}
		return provider.tokens("")
	}
	return refuse("unsupported_grant_type")
}

func (provider *Provider) tokens(nonce string) (int, map[string]any) {
	idToken, err := provider.signIDToken(nonce)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": "server_error"}
	}
	refreshToken := randomToken()
	provider.mutex.Lock()
	provider.refresh[refreshToken] = true
	provider.mutex.Unlock()
	return http.StatusOK, map[string]any{
		"token_type": "Bearer", "expires_in": int(provider.options.TokenLifetime / time.Second),
		"id_token": idToken, "access_token": "opaque-access-" + randomToken(), "refresh_token": refreshToken,
	}
}

func (provider *Provider) signIDToken(nonce string) (string, error) {
	now := provider.options.Now()
	claims := map[string]any{
		"iss": provider.Issuer(), "sub": provider.options.Identity.Subject, "aud": provider.options.ClientID,
		"iat": now.Unix(), "exp": now.Add(provider.options.TokenLifetime).Unix(),
		"preferred_username": provider.options.Identity.Username, "groups": provider.options.Identity.Groups,
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	provider.mutex.Lock()
	mutate, key := provider.mutateClaims, provider.key
	if provider.signingKey != nil {
		key = provider.signingKey
	}
	provider.mutex.Unlock()
	if mutate != nil {
		mutate(claims)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "fake-1"))
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return signed.CompactSerialize()
}

func randomToken() string {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(body)
}
