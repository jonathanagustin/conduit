// Package oidcprovider is the identity provider's side of a browser sign-in:
// the endpoints discovery advertises, and the one back-channel call that
// redeems an authorization code.
//
// It verifies nothing. The ID token it returns is an opaque string until the
// TokenVerifier has checked its signature and the policy has read its claims;
// an adapter that trusted its own HTTP response would be trusting whoever
// answered.
//
// There is deliberately no insecure TLS mode, and every endpoint must be https.
package oidcprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
)

// maximumTokenResponseBytes bounds a token response. An ID token is refused by
// the policy long before this, at MaximumTokenBytes; this only stops a hostile
// endpoint from being read forever.
const maximumTokenResponseBytes = 1 << 20

// Config names one confidential or public client at one issuer.
type Config struct {
	Issuer   string
	ClientID string
	// ClientSecret is empty for a public client, which then relies on PKCE
	// alone. It is read from a mounted file by the caller and never logged.
	ClientSecret string
	HTTPClient   *http.Client
}

// Provider satisfies Identity's AuthorizationCodeProvider port.
type Provider struct {
	clientID, clientSecret string
	authorizationEndpoint  string
	tokenEndpoint          string
	endSessionEndpoint     string
	secretInBody           bool
	client                 *http.Client
}

type metadata struct {
	EndSessionEndpoint   string   `json:"end_session_endpoint"`
	TokenAuthMethods     []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethods []string `json:"code_challenge_methods_supported"`
}

// New discovers the issuer and refuses one a browser sign-in cannot safely use.
func New(ctx context.Context, config Config) (*Provider, error) {
	if strings.TrimSpace(config.ClientID) == "" {
		return nil, errors.New("interactive sign-in requires an OIDC client ID")
	}
	client := &http.Client{}
	if config.HTTPClient != nil {
		copied := *config.HTTPClient
		client = &copied
	}
	// A redirect from the token endpoint is not followed: following one would
	// carry the client secret and the authorization code to wherever it points.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	discovered, err := oidc.NewProvider(oidc.ClientContext(ctx, client), config.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC issuer: %w", err)
	}
	var extra metadata
	if err := discovered.Claims(&extra); err != nil {
		return nil, fmt.Errorf("read OIDC discovery document: %w", err)
	}
	endpoints := discovered.Endpoint()
	for name, endpoint := range map[string]string{
		"authorization_endpoint": endpoints.AuthURL, "token_endpoint": endpoints.TokenURL,
	} {
		if !httpsEndpoint(endpoint) {
			return nil, fmt.Errorf("the OIDC issuer's %s must be an absolute https URL", name)
		}
	}
	if extra.EndSessionEndpoint != "" && !httpsEndpoint(extra.EndSessionEndpoint) {
		return nil, errors.New("the OIDC issuer's end_session_endpoint must be an absolute https URL")
	}
	// An issuer that lists its PKCE methods and omits S256 would ignore the
	// challenge. Sign-in is refused rather than run without it.
	if len(extra.CodeChallengeMethods) > 0 && !contains(extra.CodeChallengeMethods, "S256") {
		return nil, errors.New("the OIDC issuer does not support PKCE with S256")
	}
	return &Provider{
		clientID: strings.TrimSpace(config.ClientID), clientSecret: config.ClientSecret,
		authorizationEndpoint: endpoints.AuthURL, tokenEndpoint: endpoints.TokenURL,
		endSessionEndpoint: extra.EndSessionEndpoint,
		// client_secret_basic is the default every provider must support; the
		// body is used only when discovery says basic is not offered.
		secretInBody: len(extra.TokenAuthMethods) > 0 && !contains(extra.TokenAuthMethods, "client_secret_basic") &&
			contains(extra.TokenAuthMethods, "client_secret_post"),
		client: client,
	}, nil
}

func (provider *Provider) AuthorizationEndpoint() string { return provider.authorizationEndpoint }
func (provider *Provider) EndSessionEndpoint() string    { return provider.endSessionEndpoint }

// Exchange redeems one code. Its errors name the step and the status and
// nothing the provider sent back, so nothing it returns can carry a token into
// a log line.
func (provider *Provider) Exchange(ctx context.Context, code, codeVerifier, redirectURI string) (string, error) {
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"code_verifier": {codeVerifier}, "redirect_uri": {redirectURI},
	}
	if provider.clientSecret == "" || provider.secretInBody {
		form.Set("client_id", provider.clientID)
	}
	if provider.clientSecret != "" && provider.secretInBody {
		form.Set("client_secret", provider.clientSecret)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, provider.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", errors.New("build the token request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if provider.clientSecret != "" && !provider.secretInBody {
		// RFC 6749 section 2.3.1: both halves are form-encoded before Basic.
		request.SetBasicAuth(url.QueryEscape(provider.clientID), url.QueryEscape(provider.clientSecret))
	}
	response, err := provider.client.Do(request)
	if err != nil {
		return "", errors.New("the token endpoint could not be reached")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, maximumTokenResponseBytes+1))
	if err != nil || len(body) > maximumTokenResponseBytes {
		return "", errors.New("the token response could not be read")
	}
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the token endpoint answered %d", response.StatusCode)
	}
	var tokens struct {
		IDToken string `json:"id_token"`
	}
	if json.Unmarshal(body, &tokens) != nil || tokens.IDToken == "" {
		return "", errors.New("the token response carried no ID token")
	}
	return tokens.IDToken, nil
}

func httpsEndpoint(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil && parsed.Fragment == ""
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
