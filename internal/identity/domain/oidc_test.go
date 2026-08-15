package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// The point of splitting the policy out of the verifier is that these rules can
// be checked against a claim literal, with no key, no signature and no clock
// but the one handed in.

var claimFixtureTime = time.Date(2026, 8, 9, 20, 0, 0, 0, time.UTC)

func mustNormalize(t *testing.T, policy ClaimPolicy) ClaimPolicy {
	t.Helper()
	normalized, err := NormalizeClaimPolicy(policy)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	return normalized
}

func basePolicy(t *testing.T) ClaimPolicy {
	t.Helper()
	return mustNormalize(t, ClaimPolicy{
		Issuer: "https://issuer.example.test", ClientID: "conduit",
		UsernameClaim: "email", GroupsClaim: "groups",
		TokenPurposeClaim: "token_use", ExpectedTokenPurpose: "id",
		RequiredScopes: []string{"apollo-api:use-api"},
	})
}

func rawClaims(t *testing.T, values map[string]any) map[string]json.RawMessage {
	t.Helper()
	raw := make(map[string]json.RawMessage, len(values))
	for name, value := range values {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		raw[name] = encoded
	}
	return raw
}

func freshToken(policy ClaimPolicy) VerifiedToken {
	return VerifiedToken{Issuer: policy.Issuer, Subject: "subject-1", IssuedAt: claimFixtureTime.Add(-time.Minute)}
}

func TestNormalizeClaimPolicyFillsDefaultsAndKeepsTheIssuerExact(t *testing.T) {
	t.Parallel()
	policy := mustNormalize(t, ClaimPolicy{
		// OIDC requires byte-for-byte issuer equality, so the trailing slash is
		// part of the identifier and must survive normalization.
		Issuer: "  https://issuer.example.test/  ", ClientID: "conduit",
	})
	if policy.Issuer != "https://issuer.example.test/" {
		t.Fatalf("issuer = %q", policy.Issuer)
	}
	if policy.UsernameClaim != "preferred_username" {
		t.Fatalf("username claim default = %q", policy.UsernameClaim)
	}
	if len(policy.AllowedSigningAlgs) != 1 || policy.AllowedSigningAlgs[0] != SigningRS256 {
		t.Fatalf("signing algorithms = %v", policy.AllowedSigningAlgs)
	}
	if policy.MaximumTokenBytes != DefaultMaximumTokenBytes || policy.MaximumGroups != DefaultMaximumGroups ||
		policy.MaximumTokenAge != DefaultMaximumTokenAge || policy.ClockSkew != DefaultClockSkew {
		t.Fatalf("bounds were not defaulted: %#v", policy)
	}
	// A required scope implies the claim it is read from, because a policy that
	// demanded a scope and looked nowhere for it would silently admit everyone.
	scoped := mustNormalize(t, ClaimPolicy{
		Issuer: "https://issuer.example.test", ClientID: "conduit", RequiredScopes: []string{"a"},
	})
	if scoped.ScopeClaim != "scope" {
		t.Fatalf("scope claim = %q", scoped.ScopeClaim)
	}
}

func TestNormalizeClaimPolicyRefusesWhatItCannotEnforceExactly(t *testing.T) {
	t.Parallel()
	for name, policy := range map[string]ClaimPolicy{
		"plaintext issuer":         {Issuer: "http://issuer.example.test", ClientID: "conduit"},
		"issuer with query":        {Issuer: "https://issuer.example.test?a=b", ClientID: "conduit"},
		"issuer with userinfo":     {Issuer: "https://user@issuer.example.test", ClientID: "conduit"},
		"missing client":           {Issuer: "https://issuer.example.test"},
		"plaintext JWKS":           {Issuer: "https://issuer.example.test", ClientID: "c", JWKSURL: "http://keys.example.test"},
		"claim name with a space":  {Issuer: "https://issuer.example.test", ClientID: "c", UsernameClaim: "user name"},
		"purpose without a value":  {Issuer: "https://issuer.example.test", ClientID: "c", TokenPurposeClaim: "token_use"},
		"value without a purpose":  {Issuer: "https://issuer.example.test", ClientID: "c", ExpectedTokenPurpose: "id"},
		"scope containing a space": {Issuer: "https://issuer.example.test", ClientID: "c", RequiredScopes: []string{"a b"}},
		"unlisted algorithm":       {Issuer: "https://issuer.example.test", ClientID: "c", AllowedSigningAlgs: []string{"none"}},
		"token bound too small":    {Issuer: "https://issuer.example.test", ClientID: "c", MaximumTokenBytes: 8},
		"token age too long":       {Issuer: "https://issuer.example.test", ClientID: "c", MaximumTokenAge: 48 * time.Hour},
		"skew too generous":        {Issuer: "https://issuer.example.test", ClientID: "c", ClockSkew: time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NormalizeClaimPolicy(policy); err == nil {
				t.Fatal("a policy that cannot be enforced exactly was accepted")
			}
		})
	}
}

func TestOperatorClaimsAcceptsAWellFormedTokenAndNormalizesItsGroups(t *testing.T) {
	t.Parallel()
	policy := basePolicy(t)
	claims, err := policy.OperatorClaims(freshToken(policy), rawClaims(t, map[string]any{
		"email": " operator@example.test ", "groups": []string{"operators", "reviewers", "operators"},
		"token_use": "id", "scope": "openid apollo-api:use-api",
	}), claimFixtureTime)
	if err != nil {
		t.Fatal(err)
	}
	// Two tokens carrying the same groups in a different order, or with a
	// duplicate, must mint the same identity.
	if claims.Username != "operator@example.test" || strings.Join(claims.Groups, ",") != "operators,reviewers" {
		t.Fatalf("claims = %#v", claims)
	}
	if claims.ServiceAccount {
		t.Fatal("a token with no service-account claim minted a service account")
	}
}

func TestOperatorClaimsRefusesEveryWayATokenCanFail(t *testing.T) {
	t.Parallel()
	policy := basePolicy(t)
	valid := map[string]any{"email": "operator@example.test", "groups": []string{"operators"},
		"token_use": "id", "scope": "apollo-api:use-api"}

	for name, testCase := range map[string]struct {
		token VerifiedToken
		extra map[string]any
	}{
		"another issuer": {
			token: VerifiedToken{Issuer: "https://attacker.example", Subject: "s", IssuedAt: claimFixtureTime}, extra: valid,
		},
		"no subject": {
			token: VerifiedToken{Issuer: policy.Issuer, IssuedAt: claimFixtureTime}, extra: valid,
		},
		// A token with no issue time cannot be aged, so it is refused rather
		// than treated as fresh.
		"no issued-at": {
			token: VerifiedToken{Issuer: policy.Issuer, Subject: "s"}, extra: valid,
		},
		// Expiry alone would let a long-lived token be replayed indefinitely.
		"issued too long ago": {
			token: VerifiedToken{Issuer: policy.Issuer, Subject: "s", IssuedAt: claimFixtureTime.Add(-2 * time.Hour)}, extra: valid,
		},
		"issued in the future": {
			token: VerifiedToken{Issuer: policy.Issuer, Subject: "s", IssuedAt: claimFixtureTime.Add(2 * time.Minute)}, extra: valid,
		},
		"missing username": {
			token: freshToken(policy),
			extra: map[string]any{"groups": []string{"operators"}, "token_use": "id", "scope": "apollo-api:use-api"},
		},
		"username of the wrong type": {
			token: freshToken(policy),
			extra: map[string]any{"email": 42, "groups": []string{"operators"}, "token_use": "id", "scope": "apollo-api:use-api"},
		},
		"groups of the wrong type": {
			token: freshToken(policy),
			extra: map[string]any{"email": "operator@example.test", "groups": "operators", "token_use": "id", "scope": "apollo-api:use-api"},
		},
		"missing the required scope": {
			token: freshToken(policy),
			extra: map[string]any{"email": "operator@example.test", "groups": []string{"operators"}, "token_use": "id", "scope": "openid"},
		},
		"wrong token purpose": {
			token: freshToken(policy),
			extra: map[string]any{"email": "operator@example.test", "groups": []string{"operators"}, "token_use": "access", "scope": "apollo-api:use-api"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := policy.OperatorClaims(testCase.token, rawClaims(t, testCase.extra), claimFixtureTime)
			if err == nil {
				t.Fatal("an unacceptable token minted an identity")
			}
			// Every refusal is the same error. Which check refused is a probe of
			// the policy and helps no legitimate caller.
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("refusal leaked which check failed: %v", err)
			}
		})
	}
}

// A token with no issue time cannot be aged. The age check happens to refuse it
// too, because the zero time is very old — but only while the clock is sane. If
// now is also zero the age is zero, and without the explicit guard a token
// carrying no issue time at all would be minted into an identity. Failing
// closed on a broken clock is the property; this is what makes the guard
// load-bearing rather than decorative.
func TestOperatorClaimsRefusesATokenWithNoIssueTimeEvenIfTheClockIsBroken(t *testing.T) {
	t.Parallel()
	policy := basePolicy(t)
	token := VerifiedToken{Issuer: policy.Issuer, Subject: "subject-1"}
	claims := rawClaims(t, map[string]any{"email": "operator@example.test",
		"groups": []string{"operators"}, "token_use": "id", "scope": "apollo-api:use-api"})
	if _, err := policy.OperatorClaims(token, claims, time.Time{}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("a token with no issue time was accepted against a zero clock: %v", err)
	}
}

func TestOperatorClaimsEnforcesTheGroupBoundAndTheNamedAudienceClaim(t *testing.T) {
	t.Parallel()
	bounded := basePolicy(t)
	bounded.MaximumGroups = 1
	if _, err := bounded.OperatorClaims(freshToken(bounded), rawClaims(t, map[string]any{
		"email": "operator@example.test", "groups": []string{"one", "two"},
		"token_use": "id", "scope": "apollo-api:use-api",
	}), claimFixtureTime); err == nil {
		t.Fatal("a token exceeding the group bound was accepted")
	}
	// The bound is on what the token carries, not on what survives
	// de-duplication. Two identical groups collapse to one, which would satisfy
	// a bound of one — a token padded with thousands of repeats would otherwise
	// be parsed in full before being found acceptable.
	if _, err := bounded.OperatorClaims(freshToken(bounded), rawClaims(t, map[string]any{
		"email": "operator@example.test", "groups": []string{"one", "one"},
		"token_use": "id", "scope": "apollo-api:use-api",
	}), claimFixtureTime); err == nil {
		t.Fatal("a token padded with duplicate groups was accepted because they de-duplicated")
	}

	// Cognito access tokens carry no aud claim, so the policy names the claim
	// that stands in for it and checks it itself.
	cognito := basePolicy(t)
	cognito.AudienceClaim = "client_id"
	cognito.ExpectedTokenPurpose = "access"
	accepted := map[string]any{"client_id": cognito.ClientID, "email": "operator@example.test",
		"groups": []string{"operators"}, "token_use": "access", "scope": "apollo-api:use-api"}
	if _, err := cognito.OperatorClaims(freshToken(cognito), rawClaims(t, accepted), claimFixtureTime); err != nil {
		t.Fatalf("a token naming this client in the configured audience claim was refused: %v", err)
	}
	accepted["client_id"] = "another-client"
	if _, err := cognito.OperatorClaims(freshToken(cognito), rawClaims(t, accepted), claimFixtureTime); err == nil {
		t.Fatal("a token issued for another client was accepted")
	}
	// An audience claim carrying a list satisfies the check by containing the
	// client, which is what a multi-audience token looks like.
	accepted["client_id"] = []string{"another-client", cognito.ClientID}
	if _, err := cognito.OperatorClaims(freshToken(cognito), rawClaims(t, accepted), claimFixtureTime); err != nil {
		t.Fatalf("a multi-audience token including this client was refused: %v", err)
	}
}
