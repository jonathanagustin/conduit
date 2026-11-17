package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"
)

// Interactive sign-in: the rules, without the transport.
//
// A browser cannot hold a bearer token safely, so the Hub holds the verified
// identity for it and hands the browser an opaque name for that record. Every
// decision about that arrangement that can be made without a socket is made
// here: what a session may outlive, where a browser may be sent afterwards,
// and when a cookie counts as a credential at all. The HTTP adapter carries
// the answers; it does not have opinions of its own.

const (
	// SessionCookieName and LoginCookieName carry the __Host- prefix, which a
	// browser honours only for a cookie that is Secure, has Path=/ and names no
	// Domain. That is what stops a sibling subdomain from planting either one.
	SessionCookieName = "__Host-conduit-session"
	LoginCookieName   = "__Host-conduit-login"

	// CSRFHeaderName is the header a cookie-authenticated write must carry. A
	// cross-site form cannot set a custom header, and a cross-site script can
	// only do so after a CORS preflight the Hub never answers.
	CSRFHeaderName  = "X-Conduit-CSRF"
	CSRFHeaderValue = "1"

	// DefaultSessionLifetime bounds a session whose ID token would allow more.
	DefaultSessionLifetime = 8 * time.Hour
	MinimumSessionLifetime = 5 * time.Minute
	MaximumSessionLifetime = 24 * time.Hour

	// LoginAttemptLifetime is how long a person has to finish at the provider.
	LoginAttemptLifetime = 10 * time.Minute

	// DefaultConsoleReturnPath is where sign-in ends when nothing better was
	// asked for, and what every refused return address collapses to.
	DefaultConsoleReturnPath = "/console/"

	maximumReturnPathBytes = 2048
	opaqueSecretBytes      = 32
)

// ErrSessionNotFound is the one answer for a session or sign-in attempt that is
// absent, expired, or already used. A caller is never told which.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionStoreFull is a store refusing to grow. It is a refusal rather than
// an eviction because evicting would let an anonymous caller sign people out.
var ErrSessionStoreFull = errors.New("session store is full")

// Session is a signed-in browser as the Hub remembers it. IDHash is the SHA-256
// of the cookie's value: the value itself is never stored, so reading the store
// does not yield a credential.
type Session struct {
	IDHash    string
	Claims    Claims
	CreatedAt time.Time
	ExpiresAt time.Time
}

// LoginAttempt is one trip to the provider that has not come back yet.
//
// StateHash names it. BindingHash ties it to the browser that started it: the
// callback must present the cookie whose hash this is, which is what stops an
// attacker from finishing their own sign-in in somebody else's browser.
type LoginAttempt struct {
	StateHash    string
	BindingHash  string
	Nonce        string
	CodeVerifier string
	ReturnPath   string
	ExpiresAt    time.Time
}

// NewOpaqueSecret returns 256 bits from the system generator, URL-safe. It is
// long enough to be a PKCE verifier (RFC 7636 asks for 43 to 128 characters).
func NewOpaqueSecret() (string, error) {
	buffer := make([]byte, opaqueSecretBytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

// ValidOpaqueSecret reports whether a value has the shape NewOpaqueSecret
// produces. A cookie that does not is refused before any store is consulted.
func ValidOpaqueSecret(value string) bool {
	if len(value) != base64.RawURLEncoding.EncodedLen(opaqueSecretBytes) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == opaqueSecretBytes
}

// HashSecret is how a secret is named in a store.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// SecretMatchesHash compares in constant time.
func SecretMatchesHash(secret, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(HashSecret(secret)), []byte(hash)) == 1
}

// EqualSecrets compares two secrets in constant time, and never equates empty.
func EqualSecrets(left, right string) bool {
	return left != "" && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

// PKCEChallenge is the S256 transform of RFC 7636.
func PKCEChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// NormalizePublicOrigin reads the configured address browsers reach the console
// at and returns its origin.
//
// It must be an https origin and nothing else. This value becomes the OAuth
// redirect address and the origin every cookie-authenticated write is compared
// with, so it is taken from configuration and never from a request: a Host
// header is whatever the caller typed.
func NormalizePublicOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" ||
		(parsed.Path != "" && parsed.Path != "/") || strings.ContainsAny(raw, " \t\r\n\\") {
		return "", errors.New("the console public URL must be an https origin with no path, query, fragment or userinfo")
	}
	host := strings.TrimSuffix(strings.ToLower(parsed.Host), ":443")
	return "https://" + host, nil
}

// NormalizeSessionLifetime fills the default and refuses a bound outside the
// range this package is willing to promise.
func NormalizeSessionLifetime(lifetime time.Duration) (time.Duration, error) {
	if lifetime == 0 {
		return DefaultSessionLifetime, nil
	}
	if lifetime < MinimumSessionLifetime || lifetime > MaximumSessionLifetime {
		return 0, errors.New("the operator session lifetime must be between five minutes and 24 hours")
	}
	return lifetime, nil
}

// ConsoleReturnPath decides where a browser goes after signing in.
//
// The answer is always a path on this origin beneath /console/. Anything else —
// an absolute URL, a scheme-relative //host, a backslash a browser would read
// as a slash, a control character, a dot segment — collapses to the console's
// front page rather than being repaired, because a repaired redirect is a
// redirect somebody else chose.
func ConsoleReturnPath(raw string) string {
	if raw == "" || len(raw) > maximumReturnPathBytes || !strings.HasPrefix(raw, DefaultConsoleReturnPath) {
		return DefaultConsoleReturnPath
	}
	for _, character := range raw {
		if character < 0x20 || character == 0x7f || character == '\\' {
			return DefaultConsoleReturnPath
		}
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" ||
		!strings.HasPrefix(parsed.Path, DefaultConsoleReturnPath) {
		return DefaultConsoleReturnPath
	}
	for _, segment := range strings.Split(parsed.Path, "/")[1:] {
		if segment == "." || segment == ".." {
			return DefaultConsoleReturnPath
		}
	}
	if strings.Contains(parsed.Path, "//") {
		return DefaultConsoleReturnPath
	}
	// Rebuilt from its parts, so what is sent is what was checked.
	rebuilt := (&url.URL{Path: parsed.Path, RawQuery: parsed.RawQuery, Fragment: parsed.Fragment}).String()
	if !strings.HasPrefix(rebuilt, DefaultConsoleReturnPath) {
		return DefaultConsoleReturnPath
	}
	return rebuilt
}

// SessionExpiry is the moment a new session ends: the earlier of the ID token's
// own expiry and the configured bound. A token that has already expired, or
// that carries no expiry, yields no session.
func SessionExpiry(now, tokenExpiry time.Time, maximum time.Duration) (time.Time, bool) {
	if tokenExpiry.IsZero() || !tokenExpiry.After(now) || maximum <= 0 {
		return time.Time{}, false
	}
	expiry := now.Add(maximum)
	if tokenExpiry.Before(expiry) {
		expiry = tokenExpiry
	}
	return expiry.UTC(), true
}

// TokenExpiry reads the exp claim. go-oidc has already refused an expired
// token; this is read again because the session must not outlive it.
func TokenExpiry(raw map[string]json.RawMessage) (time.Time, bool) {
	var seconds float64
	if json.Unmarshal(raw["exp"], &seconds) != nil || seconds <= 0 || seconds > 1e11 {
		return time.Time{}, false
	}
	return time.Unix(int64(seconds), 0).UTC(), true
}

// IDTokenBoundToLogin checks the two things an ID token returned by a code
// exchange must say beyond being authentic: that it answers the request this
// browser made (nonce), and that it was issued to this client (aud, and azp
// when present). The audience is checked here even when the bearer policy
// reads its audience from another claim, because an ID token always carries
// aud and one minted for another client must never open a session here.
func IDTokenBoundToLogin(raw map[string]json.RawMessage, clientID, expectedNonce string) bool {
	var nonce string
	if json.Unmarshal(raw["nonce"], &nonce) != nil || !EqualSecrets(nonce, expectedNonce) {
		return false
	}
	if clientID == "" || !claimContainsExactString(raw["aud"], clientID) {
		return false
	}
	if authorizedParty, present := raw["azp"]; present && !claimIsExactString(authorizedParty, clientID) {
		return false
	}
	return true
}

// CookieRequest is what the cookie rule needs to know about a request.
type CookieRequest struct {
	Method string
	// Origin is the Origin header and OriginPresent whether one was sent; an
	// absent header and an empty one are different facts.
	Origin        string
	OriginPresent bool
	// FetchSite is Sec-Fetch-Site, empty when the browser sent none.
	FetchSite string
	// CSRFHeader is the value of CSRFHeaderName.
	CSRFHeader string
}

// CookiePolicy decides when a session cookie counts as a credential.
//
// A cookie is sent by the browser whether or not the page that caused the
// request is the console, so presenting one proves less than presenting a
// bearer token does. The rule:
//
//   - If the browser says where the request came from, it must have come from
//     this origin. That holds for reads too: it is what refuses a sibling
//     subdomain, which SameSite treats as the same site.
//   - A request that changes state must additionally say where it came from
//     and carry the custom header.
//
// Bearer tokens and client certificates are not cookies and never reach this.
type CookiePolicy struct {
	PublicOrigin string
}

// Allows reports whether the cookie on this request may be used.
func (policy CookiePolicy) Allows(request CookieRequest) bool {
	if policy.PublicOrigin == "" {
		return false
	}
	safe := request.Method == "GET" || request.Method == "HEAD" || request.Method == "OPTIONS"
	if request.OriginPresent && request.Origin != policy.PublicOrigin {
		return false
	}
	switch request.FetchSite {
	case "", "same-origin":
	case "none":
		// A request the person made themselves — a typed address. No page
		// caused it, so there is no page to distrust, but it can only be a read.
		if !safe {
			return false
		}
	default:
		return false
	}
	if safe {
		return true
	}
	return request.OriginPresent && request.CSRFHeader == CSRFHeaderValue
}
