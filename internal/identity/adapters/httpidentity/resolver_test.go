package httpidentity_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/oidcverifier"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// These exercise the whole inbound path against real signed tokens: the HTTP
// adapter finds the credential, the OIDC adapter checks the signature against a
// local key set, and the domain policy decides. Nothing is mocked, because the
// interesting failures -- a token signed by the wrong key, one that is not yet
// valid, one carrying the audience of another client -- only exist when a real
// verifier is doing the verifying.

// resolver wires the three layers the way the Hub does.
func resolver(policy domain.ClaimPolicy, publicKey *rsa.PublicKey, now time.Time) func(*http.Request) (domain.Claims, error) {
	clock := func() time.Time { return now }
	keySet := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{publicKey}}
	return httpidentity.OperatorResolver(policy, oidcverifier.NewWithKeySet(policy, keySet, clock), clock)
}

func TestOIDCOperatorIdentityResolverVerifiesAndBoundsClaims(t *testing.T) {
	now := time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	policy := testPolicy(t)
	resolve := resolver(policy, &privateKey.PublicKey, now)
	token := signOIDCTestToken(t, privateKey, jose.RS256, oidcTestClaims(now, policy), map[string]any{
		"email": " operator@example.test ", "groups": []string{"operators", "reviewers", "operators"},
		"token_use": "id", "scope": "openid apollo-api:use-api",
	})
	request := newOIDCTestRequest(token)
	claims, err := resolve(request)
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
	policy := testPolicy(t)
	resolve := resolver(policy, &privateKey.PublicKey, now)
	baseClaims := oidcTestClaims(now, policy)
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
			if _, err := resolve(request); err == nil {
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
	for name, test := range map[string]struct {
		bound func(*domain.ClaimPolicy)
		extra map[string]any
	}{
		"too many groups": {
			bound: func(policy *domain.ClaimPolicy) { policy.MaximumGroups = 1 },
			extra: map[string]any{"email": "operator@example.test", "groups": []string{"one", "two"},
				"token_use": "id", "scope": "apollo-api:use-api"},
		},
		"oversized token": {
			bound: func(policy *domain.ClaimPolicy) { policy.MaximumTokenBytes = 512 },
			extra: map[string]any{"email": "operator@example.test", "groups": []string{"one"},
				"token_use": "id", "scope": "apollo-api:use-api", "padding": strings.Repeat("x", 1024)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy := testPolicy(t)
			test.bound(&policy)
			resolve := resolver(policy, &privateKey.PublicKey, now)
			token := signOIDCTestToken(t, privateKey, jose.RS256, oidcTestClaims(now, policy), test.extra)
			if _, err := resolve(newOIDCTestRequest(token)); err == nil {
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
	// Cognito access tokens carry no aud claim, so the policy names the claim
	// that stands in for it and checks it itself.
	policy := testPolicy(t)
	policy.AudienceClaim = "client_id"
	policy.UsernameClaim = "username"
	policy.GroupsClaim = "cognito:groups"
	policy.ExpectedTokenPurpose = "access"
	resolve := resolver(policy, &privateKey.PublicKey, now)

	claims := oidcTestClaims(now, policy)
	claims.Audience = nil
	token := signOIDCTestToken(t, privateKey, jose.RS256, claims, map[string]any{
		"client_id": policy.ClientID, "username": "operator", "cognito:groups": []string{"operators"},
		"token_use": "access", "scope": "apollo-api:use-api",
	})
	if identity, err := resolve(newOIDCTestRequest(token)); err != nil || identity.Username != "operator" {
		t.Fatalf("Cognito-shaped access token was not accepted: identity=%#v error=%v", identity, err)
	}
	wrongAudienceToken := signOIDCTestToken(t, privateKey, jose.RS256, claims, map[string]any{
		"client_id": "another-client", "username": "operator", "cognito:groups": []string{"operators"},
		"token_use": "access", "scope": "apollo-api:use-api",
	})
	if _, err := resolve(newOIDCTestRequest(wrongAudienceToken)); err == nil {
		t.Fatal("Cognito-shaped access token for another client was accepted")
	}
}

func testPolicy(t *testing.T) domain.ClaimPolicy {
	t.Helper()
	policy, err := domain.NormalizeClaimPolicy(domain.ClaimPolicy{
		Issuer: "https://multipass.example.test", ClientID: "conduit",
		UsernameClaim: "email", GroupsClaim: "groups",
		TokenPurposeClaim: "token_use", ExpectedTokenPurpose: "id",
		RequiredScopes: []string{"apollo-api:use-api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func oidcTestClaims(now time.Time, policy domain.ClaimPolicy) jwt.Claims {
	return jwt.Claims{Issuer: policy.Issuer, Subject: "subject-1", Audience: jwt.Audience{policy.ClientID},
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

func TestAgentIdentityFromRequestDoesNotFallbackToHeadersWhenCertificateVerificationFails(t *testing.T) {
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://hub.example.test/v1/agent/report", nil)
	request.Header.Set("X-Platform-Environment-ID", "prod")
	request.Header.Set("X-Platform-Agent-ID", "agent-1")
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{}},
	}

	identity, err := httpidentity.AgentIdentityFromRequest(request, true)
	if err == nil {
		t.Fatalf("unverified certificate fell back to development headers: identity=%#v", identity)
	}
}

func TestAgentIdentityFromRequestUsesHeadersOnlyWhenNoCertificateWasPresented(t *testing.T) {
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://hub.example.test/v1/agent/report", nil)
	request.Header.Set("X-Platform-Environment-ID", "prod")
	request.Header.Set("X-Platform-Agent-ID", "agent-1")

	identity, err := httpidentity.AgentIdentityFromRequest(request, true)
	if err != nil {
		t.Fatalf("development headers were not accepted without a certificate: %v", err)
	}
	if identity.Environment != "prod" || identity.AgentID != "agent-1" {
		t.Fatalf("unexpected identity from development headers: %#v", identity)
	}
}
