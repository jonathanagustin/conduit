package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/identity"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

const (
	defaultOIDCMaximumTokenBytes = 16 * 1024
	defaultOIDCMaximumGroups     = 128
	defaultOIDCMaximumTokenAge   = time.Hour
	defaultOIDCClockSkew         = time.Minute
	maximumOIDCClaimLength       = 256
)

// OIDCOperatorIdentityConfig defines a standard OIDC verification boundary.
// Provider-specific administration remains outside Platform. AudienceClaim is
// empty for ordinary ID tokens; Cognito access tokens use "client_id" because
// they do not carry an aud claim.
type OIDCOperatorIdentityConfig struct {
	Issuer               string
	JWKSURL              string
	ClientID             string
	AudienceClaim        string
	UsernameClaim        string
	GroupsClaim          string
	ServiceAccountClaim  string
	TokenPurposeClaim    string
	ExpectedTokenPurpose string
	ScopeClaim           string
	RequiredScopes       []string
	AllowedSigningAlgs   []string
	MaximumTokenBytes    int
	MaximumGroups        int
	MaximumTokenAge      time.Duration
	ClockSkew            time.Duration
	HTTPClient           *http.Client
	Now                  func() time.Time
}

type oidcTokenVerifier interface {
	Verify(context.Context, string) (*oidc.IDToken, error)
}

// NewOIDCOperatorIdentityResolver discovers one exact issuer and returns the
// production request resolver used by the Hub. Discovery and JWKS retrieval use
// the supplied bounded HTTP client; there is deliberately no insecure TLS mode.
func NewOIDCOperatorIdentityResolver(
	ctx context.Context, configuration OIDCOperatorIdentityConfig,
) (OperatorIdentityResolver, error) {
	configuration, err := normalizeOIDCOperatorIdentityConfig(configuration)
	if err != nil {
		return nil, err
	}
	discoveryContext := ctx
	if configuration.HTTPClient != nil {
		discoveryContext = oidc.ClientContext(ctx, configuration.HTTPClient)
	}
	verifierConfiguration := &oidc.Config{
		ClientID:             configuration.ClientID,
		SupportedSigningAlgs: append([]string(nil), configuration.AllowedSigningAlgs...),
		Now:                  configuration.Now,
	}
	if configuration.AudienceClaim != "" {
		verifierConfiguration.ClientID = ""
		verifierConfiguration.SkipClientIDCheck = true
	}
	if configuration.JWKSURL != "" {
		return newOIDCOperatorIdentityResolver(configuration, oidc.NewVerifier(
			configuration.Issuer, oidc.NewRemoteKeySet(discoveryContext, configuration.JWKSURL), verifierConfiguration,
		)), nil
	}
	provider, err := oidc.NewProvider(discoveryContext, configuration.Issuer)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC issuer: %w", err)
	}
	return newOIDCOperatorIdentityResolver(configuration, provider.Verifier(verifierConfiguration)), nil
}

func newOIDCOperatorIdentityResolver(
	configuration OIDCOperatorIdentityConfig, verifier oidcTokenVerifier,
) OperatorIdentityResolver {
	return func(request *http.Request) (IdentityClaims, error) {
		if request == nil || verifier == nil {
			return IdentityClaims{}, ErrUnauthorized
		}
		if len(request.Header.Values("Authorization")) == 0 {
			if request.TLS != nil && len(request.TLS.VerifiedChains) > 0 && len(request.TLS.VerifiedChains[0]) > 0 {
				return identity.OperatorIdentityFromCertificate(request.TLS.VerifiedChains[0][0])
			}
			return IdentityClaims{}, ErrUnauthorized
		}
		token, err := exactBearerToken(request.Header)
		if err != nil || len(token) > configuration.MaximumTokenBytes {
			return IdentityClaims{}, ErrUnauthorized
		}
		verified, err := verifier.Verify(request.Context(), token)
		if err != nil || verified == nil || verified.Issuer != configuration.Issuer || verified.Subject == "" {
			return IdentityClaims{}, ErrUnauthorized
		}
		now := configuration.Now()
		if verified.IssuedAt.IsZero() || verified.IssuedAt.After(now.Add(configuration.ClockSkew)) ||
			now.Sub(verified.IssuedAt) > configuration.MaximumTokenAge+configuration.ClockSkew {
			return IdentityClaims{}, ErrUnauthorized
		}
		var rawClaims map[string]json.RawMessage
		if err := verified.Claims(&rawClaims); err != nil {
			return IdentityClaims{}, ErrUnauthorized
		}
		if configuration.AudienceClaim != "" &&
			!claimContainsExactString(rawClaims[configuration.AudienceClaim], configuration.ClientID) {
			return IdentityClaims{}, ErrUnauthorized
		}
		if configuration.TokenPurposeClaim != "" &&
			!claimIsExactString(rawClaims[configuration.TokenPurposeClaim], configuration.ExpectedTokenPurpose) {
			return IdentityClaims{}, ErrUnauthorized
		}
		if !claimContainsRequiredScopes(rawClaims[configuration.ScopeClaim], configuration.RequiredScopes) {
			return IdentityClaims{}, ErrUnauthorized
		}
		username, ok := requiredBoundedStringClaim(rawClaims, configuration.UsernameClaim)
		if !ok {
			return IdentityClaims{}, ErrUnauthorized
		}
		groups, ok := boundedStringListClaim(rawClaims, configuration.GroupsClaim, configuration.MaximumGroups)
		if !ok {
			return IdentityClaims{}, ErrUnauthorized
		}
		serviceAccount := false
		if configuration.ServiceAccountClaim != "" {
			value, exists := rawClaims[configuration.ServiceAccountClaim]
			if exists && json.Unmarshal(value, &serviceAccount) != nil {
				return IdentityClaims{}, ErrUnauthorized
			}
		}
		return IdentityClaims{Username: username, Groups: groups, ServiceAccount: serviceAccount}, nil
	}
}

func normalizeOIDCOperatorIdentityConfig(configuration OIDCOperatorIdentityConfig) (OIDCOperatorIdentityConfig, error) {
	// The issuer is a protocol identifier, not a display URL. Preserve its
	// trailing slash because OIDC requires byte-for-byte issuer equality.
	configuration.Issuer = strings.TrimSpace(configuration.Issuer)
	issuer, err := url.Parse(configuration.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC issuer must be an absolute HTTPS URL without userinfo, query, or fragment")
	}
	configuration.ClientID = strings.TrimSpace(configuration.ClientID)
	if configuration.ClientID == "" || len(configuration.ClientID) > maximumOIDCClaimLength {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC client ID is required and must be bounded")
	}
	configuration.JWKSURL = strings.TrimSpace(configuration.JWKSURL)
	if configuration.JWKSURL != "" {
		jwksURL, parseErr := url.Parse(configuration.JWKSURL)
		if parseErr != nil || jwksURL.Scheme != "https" || jwksURL.Host == "" || jwksURL.User != nil ||
			jwksURL.RawQuery != "" || jwksURL.Fragment != "" {
			return OIDCOperatorIdentityConfig{}, errors.New("OIDC JWKS URL must be an absolute HTTPS URL without userinfo, query, or fragment")
		}
	}
	if configuration.UsernameClaim == "" {
		configuration.UsernameClaim = "preferred_username"
	}
	claimNames := []string{configuration.UsernameClaim, configuration.GroupsClaim, configuration.ServiceAccountClaim,
		configuration.TokenPurposeClaim, configuration.AudienceClaim, configuration.ScopeClaim}
	for _, claim := range claimNames {
		if claim != "" && !validOIDCClaimName(claim) {
			return OIDCOperatorIdentityConfig{}, fmt.Errorf("invalid OIDC claim name %q", claim)
		}
	}
	if (configuration.TokenPurposeClaim == "") != (strings.TrimSpace(configuration.ExpectedTokenPurpose) == "") {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC token-purpose claim and expected value must be configured together")
	}
	configuration.ExpectedTokenPurpose = strings.TrimSpace(configuration.ExpectedTokenPurpose)
	if len(configuration.ExpectedTokenPurpose) > maximumOIDCClaimLength {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC token-purpose value is too long")
	}
	configuration.RequiredScopes = normalizedStrings(configuration.RequiredScopes)
	if len(configuration.RequiredScopes) > 32 {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC required scopes exceed 32")
	}
	if len(configuration.RequiredScopes) > 0 && configuration.ScopeClaim == "" {
		configuration.ScopeClaim = "scope"
	}
	for _, scope := range configuration.RequiredScopes {
		if scope == "" || len(scope) > 128 || strings.ContainsAny(scope, " \t\r\n") {
			return OIDCOperatorIdentityConfig{}, fmt.Errorf("invalid required OIDC scope %q", scope)
		}
	}
	if len(configuration.AllowedSigningAlgs) == 0 {
		configuration.AllowedSigningAlgs = []string{oidc.RS256}
	}
	configuration.AllowedSigningAlgs = normalizedStrings(configuration.AllowedSigningAlgs)
	for _, algorithm := range configuration.AllowedSigningAlgs {
		if algorithm != oidc.RS256 && algorithm != oidc.ES256 && algorithm != oidc.EdDSA {
			return OIDCOperatorIdentityConfig{}, fmt.Errorf("OIDC signing algorithm %q is not allowed", algorithm)
		}
	}
	if configuration.MaximumTokenBytes == 0 {
		configuration.MaximumTokenBytes = defaultOIDCMaximumTokenBytes
	}
	if configuration.MaximumTokenBytes < 512 || configuration.MaximumTokenBytes > 64*1024 {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC maximum token bytes must be between 512 and 65536")
	}
	if configuration.MaximumGroups == 0 {
		configuration.MaximumGroups = defaultOIDCMaximumGroups
	}
	if configuration.MaximumGroups < 1 || configuration.MaximumGroups > 1024 {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC maximum groups must be between 1 and 1024")
	}
	if configuration.MaximumTokenAge == 0 {
		configuration.MaximumTokenAge = defaultOIDCMaximumTokenAge
	}
	if configuration.MaximumTokenAge < time.Minute || configuration.MaximumTokenAge > 24*time.Hour {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC maximum token age must be between one minute and 24 hours")
	}
	if configuration.ClockSkew == 0 {
		configuration.ClockSkew = defaultOIDCClockSkew
	}
	if configuration.ClockSkew < 0 || configuration.ClockSkew > 5*time.Minute {
		return OIDCOperatorIdentityConfig{}, errors.New("OIDC clock skew must be between zero and five minutes")
	}
	if configuration.Now == nil {
		configuration.Now = time.Now
	}
	return configuration, nil
}

func exactBearerToken(header http.Header) (string, error) {
	values := header.Values("Authorization")
	if len(values) != 1 {
		return "", ErrUnauthorized
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.ContainsAny(parts[1], ",\r\n") {
		return "", ErrUnauthorized
	}
	return parts[1], nil
}

func requiredBoundedStringClaim(claims map[string]json.RawMessage, name string) (string, bool) {
	var value string
	if json.Unmarshal(claims[name], &value) != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != "" && len(value) <= maximumOIDCClaimLength
}

func boundedStringListClaim(claims map[string]json.RawMessage, name string, maximum int) ([]string, bool) {
	if name == "" {
		return nil, true
	}
	raw, exists := claims[name]
	if !exists {
		return nil, true
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil || len(values) > maximum {
		return nil, false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len(strings.TrimSpace(value)) > maximumOIDCClaimLength {
			return nil, false
		}
	}
	values = normalizedStrings(values)
	if len(values) > maximum {
		return nil, false
	}
	sort.Strings(values)
	return values, true
}

func claimIsExactString(raw json.RawMessage, expected string) bool {
	var value string
	return json.Unmarshal(raw, &value) == nil && value == expected
}

func claimContainsExactString(raw json.RawMessage, expected string) bool {
	if claimIsExactString(raw, expected) {
		return true
	}
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return false
	}
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func claimContainsRequiredScopes(raw json.RawMessage, required []string) bool {
	if len(required) == 0 {
		return true
	}
	var scopes []string
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		scopes = strings.Fields(encoded)
	} else if json.Unmarshal(raw, &scopes) != nil {
		return false
	}
	available := stringSet(scopes)
	for _, scope := range required {
		if _, exists := available[scope]; !exists {
			return false
		}
	}
	return true
}

func validOIDCClaimName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._:-/", character) {
			continue
		}
		return false
	}
	return true
}
