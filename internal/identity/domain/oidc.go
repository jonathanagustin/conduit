package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultMaximumTokenBytes = 16 * 1024
	DefaultMaximumGroups     = 128
	DefaultMaximumTokenAge   = time.Hour
	DefaultClockSkew         = time.Minute
	MaximumClaimLength       = 256
)

// The signing algorithms a token may be signed with. They are spelled out
// rather than imported from the OIDC library so that the policy this package
// enforces cannot widen because a dependency added an algorithm.
const (
	SigningRS256 = "RS256"
	SigningES256 = "ES256"
	SigningEdDSA = "EdDSA"
)

// ClaimPolicy is what a verified token must say before it becomes an operator.
//
// It is the whole of the identity decision and none of the mechanics: no HTTP
// client, no clock, no key set. Those belong to the adapter that fetches keys
// and verifies signatures, and keeping them out is what lets every rule below
// be checked against a token literal.
//
// AudienceClaim is empty for ordinary ID tokens; Cognito access tokens use
// "client_id" because they do not carry an aud claim.
type ClaimPolicy struct {
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
}

// VerifiedToken is what a verifier established about a token: that its
// signature checked out, and who it says it is about. Everything else a policy
// cares about is in the raw claims, because a claim this package has not been
// told to read is one it must not silently accept.
type VerifiedToken struct {
	Issuer   string
	Subject  string
	IssuedAt time.Time
}

// NormalizeClaimPolicy validates a policy and fills its defaults, or refuses.
//
// It refuses rather than repairing wherever a repair would be a guess: an
// issuer that is not an absolute HTTPS URL, a claim name that is not a claim
// name, a purpose claim configured without the value it must equal. A policy
// that cannot be stated exactly cannot be enforced exactly.
func NormalizeClaimPolicy(policy ClaimPolicy) (ClaimPolicy, error) {
	// The issuer is a protocol identifier, not a display URL. Preserve its
	// trailing slash because OIDC requires byte-for-byte issuer equality.
	policy.Issuer = strings.TrimSpace(policy.Issuer)
	issuer, err := url.Parse(policy.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return ClaimPolicy{}, errors.New("OIDC issuer must be an absolute HTTPS URL without userinfo, query, or fragment")
	}
	policy.ClientID = strings.TrimSpace(policy.ClientID)
	if policy.ClientID == "" || len(policy.ClientID) > MaximumClaimLength {
		return ClaimPolicy{}, errors.New("OIDC client ID is required and must be bounded")
	}
	policy.JWKSURL = strings.TrimSpace(policy.JWKSURL)
	if policy.JWKSURL != "" {
		jwksURL, parseErr := url.Parse(policy.JWKSURL)
		if parseErr != nil || jwksURL.Scheme != "https" || jwksURL.Host == "" || jwksURL.User != nil ||
			jwksURL.RawQuery != "" || jwksURL.Fragment != "" {
			return ClaimPolicy{}, errors.New("OIDC JWKS URL must be an absolute HTTPS URL without userinfo, query, or fragment")
		}
	}
	if policy.UsernameClaim == "" {
		policy.UsernameClaim = "preferred_username"
	}
	claimNames := []string{policy.UsernameClaim, policy.GroupsClaim, policy.ServiceAccountClaim,
		policy.TokenPurposeClaim, policy.AudienceClaim, policy.ScopeClaim}
	for _, claim := range claimNames {
		if claim != "" && !validClaimName(claim) {
			return ClaimPolicy{}, fmt.Errorf("invalid OIDC claim name %q", claim)
		}
	}
	if (policy.TokenPurposeClaim == "") != (strings.TrimSpace(policy.ExpectedTokenPurpose) == "") {
		return ClaimPolicy{}, errors.New("OIDC token-purpose claim and expected value must be configured together")
	}
	policy.ExpectedTokenPurpose = strings.TrimSpace(policy.ExpectedTokenPurpose)
	if len(policy.ExpectedTokenPurpose) > MaximumClaimLength {
		return ClaimPolicy{}, errors.New("OIDC token-purpose value is too long")
	}
	policy.RequiredScopes = normalizedStrings(policy.RequiredScopes)
	if len(policy.RequiredScopes) > 32 {
		return ClaimPolicy{}, errors.New("OIDC required scopes exceed 32")
	}
	if len(policy.RequiredScopes) > 0 && policy.ScopeClaim == "" {
		policy.ScopeClaim = "scope"
	}
	for _, scope := range policy.RequiredScopes {
		if scope == "" || len(scope) > 128 || strings.ContainsAny(scope, " \t\r\n") {
			return ClaimPolicy{}, fmt.Errorf("invalid required OIDC scope %q", scope)
		}
	}
	if len(policy.AllowedSigningAlgs) == 0 {
		policy.AllowedSigningAlgs = []string{SigningRS256}
	}
	policy.AllowedSigningAlgs = normalizedStrings(policy.AllowedSigningAlgs)
	for _, algorithm := range policy.AllowedSigningAlgs {
		if algorithm != SigningRS256 && algorithm != SigningES256 && algorithm != SigningEdDSA {
			return ClaimPolicy{}, fmt.Errorf("OIDC signing algorithm %q is not allowed", algorithm)
		}
	}
	if policy.MaximumTokenBytes == 0 {
		policy.MaximumTokenBytes = DefaultMaximumTokenBytes
	}
	if policy.MaximumTokenBytes < 512 || policy.MaximumTokenBytes > 64*1024 {
		return ClaimPolicy{}, errors.New("OIDC maximum token bytes must be between 512 and 65536")
	}
	if policy.MaximumGroups == 0 {
		policy.MaximumGroups = DefaultMaximumGroups
	}
	if policy.MaximumGroups < 1 || policy.MaximumGroups > 1024 {
		return ClaimPolicy{}, errors.New("OIDC maximum groups must be between 1 and 1024")
	}
	if policy.MaximumTokenAge == 0 {
		policy.MaximumTokenAge = DefaultMaximumTokenAge
	}
	if policy.MaximumTokenAge < time.Minute || policy.MaximumTokenAge > 24*time.Hour {
		return ClaimPolicy{}, errors.New("OIDC maximum token age must be between one minute and 24 hours")
	}
	if policy.ClockSkew == 0 {
		policy.ClockSkew = DefaultClockSkew
	}
	if policy.ClockSkew < 0 || policy.ClockSkew > 5*time.Minute {
		return ClaimPolicy{}, errors.New("OIDC clock skew must be between zero and five minutes")
	}
	return policy, nil
}

// OperatorClaims decides whether a verified token becomes an operator identity.
//
// Every check fails to the same ErrUnauthorized. That is deliberate: telling a
// caller which of the issuer, audience, purpose, scope, username or groups
// checks refused them is a probe of the policy, and none of the answers help a
// legitimate caller who simply needs a correct token.
func (policy ClaimPolicy) OperatorClaims(
	token VerifiedToken, raw map[string]json.RawMessage, now time.Time,
) (Claims, error) {
	if token.Issuer != policy.Issuer || token.Subject == "" {
		return Claims{}, ErrUnauthorized
	}
	// A token with no issue time, one issued in the future beyond the tolerated
	// skew, or one older than the policy allows, is refused. Expiry alone is not
	// enough: a long-lived token replayed later is the case this bounds.
	if token.IssuedAt.IsZero() || token.IssuedAt.After(now.Add(policy.ClockSkew)) ||
		now.Sub(token.IssuedAt) > policy.MaximumTokenAge+policy.ClockSkew {
		return Claims{}, ErrUnauthorized
	}
	if policy.AudienceClaim != "" &&
		!claimContainsExactString(raw[policy.AudienceClaim], policy.ClientID) {
		return Claims{}, ErrUnauthorized
	}
	if policy.TokenPurposeClaim != "" &&
		!claimIsExactString(raw[policy.TokenPurposeClaim], policy.ExpectedTokenPurpose) {
		return Claims{}, ErrUnauthorized
	}
	if !claimContainsRequiredScopes(raw[policy.ScopeClaim], policy.RequiredScopes) {
		return Claims{}, ErrUnauthorized
	}
	username, ok := requiredBoundedStringClaim(raw, policy.UsernameClaim)
	if !ok {
		return Claims{}, ErrUnauthorized
	}
	groups, ok := boundedStringListClaim(raw, policy.GroupsClaim, policy.MaximumGroups)
	if !ok {
		return Claims{}, ErrUnauthorized
	}
	serviceAccount := false
	if policy.ServiceAccountClaim != "" {
		value, exists := raw[policy.ServiceAccountClaim]
		if exists && json.Unmarshal(value, &serviceAccount) != nil {
			return Claims{}, ErrUnauthorized
		}
	}
	return Claims{Username: username, Groups: groups, ServiceAccount: serviceAccount}, nil
}

func requiredBoundedStringClaim(claims map[string]json.RawMessage, name string) (string, bool) {
	var value string
	if json.Unmarshal(claims[name], &value) != nil {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != "" && len(value) <= MaximumClaimLength
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
		if strings.TrimSpace(value) == "" || len(strings.TrimSpace(value)) > MaximumClaimLength {
			return nil, false
		}
	}
	// The bound is checked above, against what the token carried. Re-checking
	// after normalization would be dead: normalizedStrings only ever removes
	// entries, and it already returns them ordered.
	return normalizedStrings(values), true
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
	available := make(map[string]struct{}, len(scopes))
	for _, scope := range scopes {
		available[scope] = struct{}{}
	}
	for _, scope := range required {
		if _, exists := available[scope]; !exists {
			return false
		}
	}
	return true
}

func validClaimName(value string) bool {
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
