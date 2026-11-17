package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestConsoleReturnPathNeverLeavesTheConsole(t *testing.T) {
	t.Parallel()
	kept := []string{
		"/console/", "/console/#/plans/plan-1", "/console/?view=fleet", "/console/?a=b#/c",
		"/console/assets", "/console/#/a?b=https://elsewhere.example",
	}
	for _, path := range kept {
		if got := ConsoleReturnPath(path); got != path {
			t.Errorf("ConsoleReturnPath(%q) = %q, want it kept", path, got)
		}
	}
	refused := []string{
		"", "/", "/console", "/consolex/", "/v1/session", "console/",
		"//evil.example", "//evil.example/console/", "///evil.example/console/",
		"https://evil.example", "https://evil.example/console/", "http://conduit.example.test/console/",
		`/\evil.example`, `\/evil.example`, `/console/\evil`, `/console\`,
		"/console/../v1/plans", "/console/./x", "/console/a/../../v1", "/console//evil.example",
		"/console/%2e%2e/v1", "/console/%2E%2E/%2E%2E/v1", "/console/%2f%2fevil.example",
		"/console/\nLocation: https://evil.example", "/console/\r", "/console/\x00", "/console/\t", "/console/\x7f",
		"javascript:alert(1)", "data:text/html,x", " /console/", "/console/" + strings.Repeat("a", 3000),
	}
	for _, path := range refused {
		if got := ConsoleReturnPath(path); got != DefaultConsoleReturnPath {
			t.Errorf("ConsoleReturnPath(%q) = %q, want the default", path, got)
		}
	}
	// Whatever comes out is a path on this origin, for any input.
	for _, path := range append(kept, refused...) {
		got := ConsoleReturnPath(path)
		if !strings.HasPrefix(got, "/console/") || strings.HasPrefix(got, "//") || strings.ContainsAny(got, "\\\r\n") {
			t.Errorf("ConsoleReturnPath(%q) = %q escapes the console", path, got)
		}
	}
}

func TestNormalizePublicOriginAcceptsOnlyAnHTTPSOrigin(t *testing.T) {
	t.Parallel()
	for raw, want := range map[string]string{
		"https://conduit.example.test":      "https://conduit.example.test",
		"https://conduit.example.test/":     "https://conduit.example.test",
		"https://Conduit.Example.Test":      "https://conduit.example.test",
		"https://conduit.example.test:443":  "https://conduit.example.test",
		"https://conduit.example.test:8443": "https://conduit.example.test:8443",
		" https://conduit.example.test ":    "https://conduit.example.test",
	} {
		got, err := NormalizePublicOrigin(raw)
		if err != nil || got != want {
			t.Errorf("NormalizePublicOrigin(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"", "conduit.example.test", "http://conduit.example.test", "https://", "https://conduit.example.test/console/",
		"https://user@conduit.example.test", "https://conduit.example.test?x=1", "https://conduit.example.test#x",
		"https://conduit.example.test\\@evil.example", "//conduit.example.test", "https://a b",
	} {
		if got, err := NormalizePublicOrigin(raw); err == nil {
			t.Errorf("NormalizePublicOrigin(%q) = %q, want it refused", raw, got)
		}
	}
}

func TestSessionExpiryIsTheEarlierOfTheTokenAndTheBound(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	if got, ok := SessionExpiry(now, now.Add(5*time.Minute), 8*time.Hour); !ok || !got.Equal(now.Add(5*time.Minute)) {
		t.Fatalf("token expiry sooner: %v %v", got, ok)
	}
	if got, ok := SessionExpiry(now, now.Add(24*time.Hour), 8*time.Hour); !ok || !got.Equal(now.Add(8*time.Hour)) {
		t.Fatalf("bound sooner: %v %v", got, ok)
	}
	for name, expiry := range map[string]time.Time{"expired": now.Add(-time.Second), "at now": now, "absent": {}} {
		if got, ok := SessionExpiry(now, expiry, 8*time.Hour); ok {
			t.Errorf("%s token yielded a session until %v", name, got)
		}
	}
	if _, ok := SessionExpiry(now, now.Add(time.Hour), 0); ok {
		t.Fatal("a zero bound yielded a session")
	}
	if _, ok := TokenExpiry(map[string]json.RawMessage{}); ok {
		t.Fatal("a token with no exp has an expiry")
	}
	if _, ok := TokenExpiry(map[string]json.RawMessage{"exp": json.RawMessage(`"soon"`)}); ok {
		t.Fatal("a token with a string exp has an expiry")
	}
	if got, ok := TokenExpiry(map[string]json.RawMessage{"exp": json.RawMessage(`1795000000`)}); !ok || got.Unix() != 1795000000 {
		t.Fatalf("exp = %v %v", got, ok)
	}
}

func TestNormalizeSessionLifetimeBounds(t *testing.T) {
	t.Parallel()
	if got, err := NormalizeSessionLifetime(0); err != nil || got != 8*time.Hour {
		t.Fatalf("default = %v %v", got, err)
	}
	for _, lifetime := range []time.Duration{time.Minute, -time.Hour, 25 * time.Hour} {
		if _, err := NormalizeSessionLifetime(lifetime); err == nil {
			t.Errorf("a %v session lifetime was accepted", lifetime)
		}
	}
}

func TestIDTokenBoundToLogin(t *testing.T) {
	t.Parallel()
	claims := func(values map[string]any) map[string]json.RawMessage {
		raw := map[string]json.RawMessage{}
		for name, value := range values {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			raw[name] = encoded
		}
		return raw
	}
	const nonce = "nonce-0123456789abcdef0123456789abcdef"
	accepted := []map[string]any{
		{"nonce": nonce, "aud": "conduit"},
		{"nonce": nonce, "aud": []string{"conduit"}},
		{"nonce": nonce, "aud": []string{"other", "conduit"}, "azp": "conduit"},
	}
	for _, values := range accepted {
		if !IDTokenBoundToLogin(claims(values), "conduit", nonce) {
			t.Errorf("%v was refused", values)
		}
	}
	refused := []map[string]any{
		{"aud": "conduit"},
		{"nonce": "", "aud": "conduit"},
		{"nonce": "another", "aud": "conduit"},
		{"nonce": nonce + "x", "aud": "conduit"},
		{"nonce": 7, "aud": "conduit"},
		{"nonce": nonce},
		{"nonce": nonce, "aud": "another"},
		{"nonce": nonce, "aud": []string{"another"}},
		{"nonce": nonce, "aud": "conduit", "azp": "another"},
		{"nonce": nonce, "aud": []string{"conduit", "another"}, "azp": "another"},
	}
	for _, values := range refused {
		if IDTokenBoundToLogin(claims(values), "conduit", nonce) {
			t.Errorf("%v was accepted", values)
		}
	}
	if IDTokenBoundToLogin(claims(map[string]any{"nonce": "", "aud": "conduit"}), "conduit", "") {
		t.Fatal("an empty nonce matched an empty expectation")
	}
	if IDTokenBoundToLogin(claims(map[string]any{"nonce": nonce, "aud": ""}), "", nonce) {
		t.Fatal("an empty client ID matched an empty audience")
	}
}

func TestCookiePolicy(t *testing.T) {
	t.Parallel()
	const origin = "https://conduit.example.test"
	policy := CookiePolicy{PublicOrigin: origin}
	type request = CookieRequest
	allowed := map[string]request{
		"same-origin read":           {Method: "GET", FetchSite: "same-origin"},
		"same-origin read, Origin":   {Method: "GET", Origin: origin, OriginPresent: true, FetchSite: "same-origin"},
		"typed address":              {Method: "GET", FetchSite: "none"},
		"read, no metadata":          {Method: "GET"},
		"HEAD":                       {Method: "HEAD", FetchSite: "same-origin"},
		"same-origin write":          {Method: "POST", Origin: origin, OriginPresent: true, FetchSite: "same-origin", CSRFHeader: "1"},
		"write, no fetch metadata":   {Method: "DELETE", Origin: origin, OriginPresent: true, CSRFHeader: "1"},
		"same-origin write with PUT": {Method: "PUT", Origin: origin, OriginPresent: true, FetchSite: "same-origin", CSRFHeader: "1"},
	}
	for name, described := range allowed {
		if !policy.Allows(described) {
			t.Errorf("%s was refused", name)
		}
	}
	refused := map[string]request{
		"cross-site read":             {Method: "GET", FetchSite: "cross-site"},
		"same-site read":              {Method: "GET", FetchSite: "same-site"},
		"unknown fetch site":          {Method: "GET", FetchSite: "conflicting"},
		"read with foreign Origin":    {Method: "GET", Origin: "https://evil.example", OriginPresent: true},
		"read with empty Origin":      {Method: "GET", Origin: "", OriginPresent: true},
		"read with null Origin":       {Method: "GET", Origin: "null", OriginPresent: true},
		"write without header":        {Method: "POST", Origin: origin, OriginPresent: true, FetchSite: "same-origin"},
		"write with wrong header":     {Method: "POST", Origin: origin, OriginPresent: true, CSRFHeader: "0"},
		"write without Origin":        {Method: "POST", FetchSite: "same-origin", CSRFHeader: "1"},
		"write with foreign Origin":   {Method: "POST", Origin: "https://evil.example", OriginPresent: true, CSRFHeader: "1"},
		"write with http Origin":      {Method: "POST", Origin: "http://conduit.example.test", OriginPresent: true, CSRFHeader: "1"},
		"write with port Origin":      {Method: "POST", Origin: origin + ":8443", OriginPresent: true, CSRFHeader: "1"},
		"write with suffix Origin":    {Method: "POST", Origin: origin + ".evil.example", OriginPresent: true, CSRFHeader: "1"},
		"write, cross-site metadata":  {Method: "POST", Origin: origin, OriginPresent: true, FetchSite: "cross-site", CSRFHeader: "1"},
		"write, same-site metadata":   {Method: "POST", Origin: origin, OriginPresent: true, FetchSite: "same-site", CSRFHeader: "1"},
		"write, navigation metadata":  {Method: "POST", Origin: origin, OriginPresent: true, FetchSite: "none", CSRFHeader: "1"},
		"PATCH without header":        {Method: "PATCH", Origin: origin, OriginPresent: true},
		"lower-case method is unsafe": {Method: "get", FetchSite: "same-origin"},
		"unknown method without head": {Method: "PROPFIND", FetchSite: "same-origin"},
	}
	for name, described := range refused {
		if policy.Allows(described) {
			t.Errorf("%s was allowed", name)
		}
	}
	// A policy with no origin allows nothing, including what it otherwise would.
	for name, described := range allowed {
		if (CookiePolicy{}).Allows(described) {
			t.Errorf("an unconfigured policy allowed %s", name)
		}
	}
}

func TestOpaqueSecretsAreFreshWellFormedAndComparedByHash(t *testing.T) {
	t.Parallel()
	first, err := NewOpaqueSecret()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewOpaqueSecret()
	if err != nil {
		t.Fatal(err)
	}
	if first == second || len(first) != 43 || !ValidOpaqueSecret(first) {
		t.Fatalf("secrets: %q %q", first, second)
	}
	for _, malformed := range []string{"", "short", first + "a", first[:42] + "!", strings.Repeat("=", 43)} {
		if ValidOpaqueSecret(malformed) {
			t.Errorf("%q was accepted as a secret", malformed)
		}
	}
	if !SecretMatchesHash(first, HashSecret(first)) || SecretMatchesHash(second, HashSecret(first)) ||
		SecretMatchesHash(first, first) || len(HashSecret(first)) != 64 {
		t.Fatal("hash comparison is wrong")
	}
	if EqualSecrets("", "") || !EqualSecrets(first, first) || EqualSecrets(first, second) {
		t.Fatal("secret comparison is wrong")
	}
	// RFC 7636 appendix B.
	if got := PKCEChallenge("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"); got != "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM" {
		t.Fatalf("PKCE challenge = %q", got)
	}
}
