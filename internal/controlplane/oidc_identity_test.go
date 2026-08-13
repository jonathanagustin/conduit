package controlplane

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

func TestOIDCOperatorIdentityResolverVerifiesAndBoundsClaims(t *testing.T) {
	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	configuration := normalizedOIDCTestConfiguration(t, now)
	resolver := testOIDCResolver(t, configuration, &privateKey.PublicKey)
	token := signOIDCTestToken(t, privateKey, jose.RS256, oidcTestClaims(now, configuration), map[string]any{
		"email": " operator@example.test ", "groups": []string{"operators", "reviewers", "operators"},
		"token_use": "id", "scope": "openid apollo-api:use-api",
	})
	request := newOIDCTestRequest(token)
	claims, err := resolver(request)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Username != "operator@example.test" || strings.Join(claims.Groups, ",") != "operators,reviewers" || claims.ServiceAccount {
		t.Fatalf("unexpected verified claims: %#v", claims)
	}
}

func TestOIDCOperatorIdentityResolverRejectsInvalidTokensAndClaims(t *testing.T) {
	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	configuration := normalizedOIDCTestConfiguration(t, now)
	resolver := testOIDCResolver(t, configuration, &privateKey.PublicKey)
	baseClaims := oidcTestClaims(now, configuration)
	validExtra := map[string]any{"email": "operator@example.test", "groups": []string{"operators"},
		"token_use": "id", "scope": "openid apollo-api:use-api"}

	tests := []struct {
		name    string
		key     *rsa.PrivateKey
		claims  jwt.Claims
		extra   map[string]any
		request func(string) *http.Request
	}{
		{name: "wrong signature", key: otherKey, claims: baseClaims, extra: validExtra},
		{name: "missing subject", key: privateKey, claims: withOIDCSubject(baseClaims, ""), extra: validExtra},
		{name: "wrong issuer", key: privateKey, claims: withOIDCIssuer(baseClaims, "https://attacker.example"), extra: validExtra},
		{name: "wrong audience", key: privateKey, claims: withOIDCAudience(baseClaims, "another-client"), extra: validExtra},
		{name: "expired", key: privateKey, claims: withOIDCExpiry(baseClaims, now.Add(-time.Second)), extra: validExtra},
		{name: "not yet valid", key: privateKey, claims: withOIDCNotBefore(baseClaims, now.Add(10*time.Minute)), extra: validExtra},
		{name: "stale issued at", key: privateKey, claims: withOIDCIssuedAt(baseClaims, now.Add(-2*time.Hour)), extra: validExtra},
		{name: "future issued at", key: privateKey, claims: withOIDCIssuedAt(baseClaims, now.Add(2*time.Minute)), extra: validExtra},
		{name: "missing username", key: privateKey, claims: baseClaims,
			extra: map[string]any{"groups": []string{"operators"}, "token_use": "id", "scope": "apollo-api:use-api"}},
		{name: "wrong group type", key: privateKey, claims: baseClaims,
			extra: map[string]any{"email": "operator@example.test", "groups": "operators", "token_use": "id", "scope": "apollo-api:use-api"}},
		{name: "missing required scope", key: privateKey, claims: baseClaims,
			extra: map[string]any{"email": "operator@example.test", "groups": []string{"operators"}, "token_use": "id", "scope": "openid"}},
		{name: "wrong token purpose", key: privateKey, claims: baseClaims,
			extra: map[string]any{"email": "operator@example.test", "groups": []string{"operators"}, "token_use": "access", "scope": "apollo-api:use-api"}},
		{name: "duplicate authorization", key: privateKey, claims: baseClaims, extra: validExtra, request: func(token string) *http.Request {
			request := newOIDCTestRequest(token)
			request.Header.Add("Authorization", "Bearer "+token)
			return request
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token := signOIDCTestToken(t, test.key, jose.RS256, test.claims, test.extra)
			request := newOIDCTestRequest(token)
			if test.request != nil {
				request = test.request(token)
			}
			if _, err := resolver(request); err == nil {
				t.Fatal("invalid OIDC identity accepted")
			}
		})
	}
}

func TestOIDCOperatorIdentityResolverRejectsOversizedTokenAndGroupSet(t *testing.T) {
	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	configuration := normalizedOIDCTestConfiguration(t, now)
	claims := oidcTestClaims(now, configuration)
	for name, test := range map[string]struct {
		configure func(*OIDCOperatorIdentityConfig)
		extra     map[string]any
	}{
		"too many groups": {configure: func(configuration *OIDCOperatorIdentityConfig) { configuration.MaximumGroups = 1 }, extra: map[string]any{"email": "operator@example.test", "groups": []string{"one", "two"}, "token_use": "id", "scope": "apollo-api:use-api"}},
		"oversized token": {configure: func(configuration *OIDCOperatorIdentityConfig) { configuration.MaximumTokenBytes = 512 }, extra: map[string]any{"email": "operator@example.test", "groups": []string{"one"}, "token_use": "id", "scope": "apollo-api:use-api", "padding": strings.Repeat("x", 1024)}},
	} {
		t.Run(name, func(t *testing.T) {
			boundedConfiguration := configuration
			test.configure(&boundedConfiguration)
			resolver := testOIDCResolver(t, boundedConfiguration, &privateKey.PublicKey)
			token := signOIDCTestToken(t, privateKey, jose.RS256, claims, test.extra)
			if _, err := resolver(newOIDCTestRequest(token)); err == nil {
				t.Fatal("bounded OIDC identity contract accepted invalid input")
			}
		})
	}
}

func TestOIDCOperatorIdentityResolverSupportsExplicitCognitoAccessTokenClaims(t *testing.T) {
	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	configuration := normalizedOIDCTestConfiguration(t, now)
	configuration.AudienceClaim = "client_id"
	configuration.UsernameClaim = "username"
	configuration.GroupsClaim = "cognito:groups"
	configuration.ExpectedTokenPurpose = "access"
	verifier := oidc.NewVerifier(configuration.Issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&privateKey.PublicKey}}, &oidc.Config{
		SkipClientIDCheck: true, SupportedSigningAlgs: configuration.AllowedSigningAlgs, Now: configuration.Now,
	})
	resolver := newOIDCOperatorIdentityResolver(configuration, verifier)
	claims := oidcTestClaims(now, configuration)
	claims.Audience = nil
	token := signOIDCTestToken(t, privateKey, jose.RS256, claims, map[string]any{
		"client_id": configuration.ClientID, "username": "operator", "cognito:groups": []string{"operators"},
		"token_use": "access", "scope": "apollo-api:use-api",
	})
	if identity, err := resolver(newOIDCTestRequest(token)); err != nil || identity.Username != "operator" {
		t.Fatalf("Cognito-shaped access token was not accepted: identity=%#v error=%v", identity, err)
	}
	wrongAudienceToken := signOIDCTestToken(t, privateKey, jose.RS256, claims, map[string]any{
		"client_id": "another-client", "username": "operator", "cognito:groups": []string{"operators"},
		"token_use": "access", "scope": "apollo-api:use-api",
	})
	if _, err := resolver(newOIDCTestRequest(wrongAudienceToken)); err == nil {
		t.Fatal("Cognito-shaped access token for another client was accepted")
	}
}

func normalizedOIDCTestConfiguration(t *testing.T, now time.Time) OIDCOperatorIdentityConfig {
	t.Helper()
	configuration, err := normalizeOIDCOperatorIdentityConfig(OIDCOperatorIdentityConfig{
		Issuer: "https://multipass.example.test", ClientID: "conduit", UsernameClaim: "email", GroupsClaim: "groups",
		TokenPurposeClaim: "token_use", ExpectedTokenPurpose: "id", RequiredScopes: []string{"apollo-api:use-api"}, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	return configuration
}

func testOIDCResolver(t *testing.T, configuration OIDCOperatorIdentityConfig, publicKey *rsa.PublicKey) OperatorIdentityResolver {
	t.Helper()
	verifier := oidc.NewVerifier(configuration.Issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{publicKey}}, &oidc.Config{
		ClientID: configuration.ClientID, SupportedSigningAlgs: configuration.AllowedSigningAlgs, Now: configuration.Now,
	})
	return newOIDCOperatorIdentityResolver(configuration, verifier)
}

func oidcTestClaims(now time.Time, configuration OIDCOperatorIdentityConfig) jwt.Claims {
	return jwt.Claims{Issuer: configuration.Issuer, Subject: "subject-1", Audience: jwt.Audience{configuration.ClientID},
		IssuedAt: jwt.NewNumericDate(now.Add(-time.Minute)), NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)), Expiry: jwt.NewNumericDate(now.Add(time.Hour))}
}

func signOIDCTestToken(t *testing.T, privateKey *rsa.PrivateKey, algorithm jose.SignatureAlgorithm, claims jwt.Claims, extra map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: algorithm, Key: privateKey}, nil)
	if err != nil {
		t.Fatal(err)
	}
	token, err := jwt.Signed(signer).Claims(claims).Claims(extra).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func newOIDCTestRequest(token string) *http.Request {
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://hub.example.test/v1/environments", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func withOIDCIssuer(claims jwt.Claims, issuer string) jwt.Claims {
	claims.Issuer = issuer
	return claims
}
func withOIDCSubject(claims jwt.Claims, subject string) jwt.Claims {
	claims.Subject = subject
	return claims
}
func withOIDCAudience(claims jwt.Claims, audience string) jwt.Claims {
	claims.Audience = jwt.Audience{audience}
	return claims
}
func withOIDCExpiry(claims jwt.Claims, expiry time.Time) jwt.Claims {
	claims.Expiry = jwt.NewNumericDate(expiry)
	return claims
}
func withOIDCNotBefore(claims jwt.Claims, notBefore time.Time) jwt.Claims {
	claims.NotBefore = jwt.NewNumericDate(notBefore)
	return claims
}
func withOIDCIssuedAt(claims jwt.Claims, issuedAt time.Time) jwt.Claims {
	claims.IssuedAt = jwt.NewNumericDate(issuedAt)
	return claims
}
