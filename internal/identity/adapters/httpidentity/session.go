package httpidentity

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// ErrCookieRequestRefused is a session cookie that arrived on a request the
// cookie rule does not accept: from another origin, or changing state without
// the custom header. It is ErrUnauthorized to every caller, because a cookie
// that may not be used is no credential; the text says why, since the person
// reading it is a developer whose client forgot the header, and an attacker
// learns nothing from being told a forged request was recognised as one.
var ErrCookieRequestRefused = fmt.Errorf(
	"%w: a session cookie is accepted only from the console's own origin, and a write must carry %s",
	domain.ErrUnauthorized, domain.CSRFHeaderName)

// WithOperatorSessions lets a signed-in browser's session cookie stand where a
// bearer token would, and changes nothing else.
//
// The order is the guarantee. A request carrying an Authorization header, or a
// verified client certificate, is handed to the inner resolver before a cookie
// is looked at, so those two paths behave exactly as they did before sessions
// existed — including when they fail: a bad bearer token is not rescued by a
// good cookie, because a request with two credentials is not offered a choice.
//
// What comes back for a session is the domain.Claims the policy produced from
// the ID token at sign-in: the same value, by the same function, that a bearer
// token carrying those claims yields. Authorization downstream cannot tell the
// two apart, which is the point.
//
// Any failure to read the store is ErrUnauthorized. A store that cannot be
// consulted has not said the person is signed in.
func WithOperatorSessions(
	inner func(*http.Request) (domain.Claims, error),
	sessions ports.SessionStore, policy domain.CookiePolicy, now func() time.Time,
) func(*http.Request) (domain.Claims, error) {
	if now == nil {
		now = time.Now
	}
	return func(request *http.Request) (domain.Claims, error) {
		if request == nil || sessions == nil ||
			len(request.Header.Values("Authorization")) != 0 ||
			(request.TLS != nil && len(request.TLS.VerifiedChains) > 0) {
			return inner(request)
		}
		identifier, present := SessionCookie(request)
		if !present {
			return inner(request)
		}
		if !policy.Allows(CookieRequestFrom(request)) {
			return domain.Claims{}, ErrCookieRequestRefused
		}
		session, err := sessions.Session(request.Context(), domain.HashSecret(identifier), now())
		if err != nil || session.Claims.Username == "" {
			return domain.Claims{}, domain.ErrUnauthorized
		}
		return session.Claims, nil
	}
}

// SessionCookie returns the session identifier a request carries.
//
// Exactly one cookie of the name, shaped like an identifier this Hub issues. A
// request with two is refused rather than resolved by taking the first: which
// one a browser sends first is not something to rest an identity on.
func SessionCookie(request *http.Request) (string, bool) {
	return exactCookie(request, domain.SessionCookieName)
}

// LoginCookie returns the browser binding of a sign-in attempt in flight.
func LoginCookie(request *http.Request) (string, bool) {
	return exactCookie(request, domain.LoginCookieName)
}

func exactCookie(request *http.Request, name string) (string, bool) {
	cookies := request.CookiesNamed(name)
	if len(cookies) != 1 || !domain.ValidOpaqueSecret(cookies[0].Value) {
		return "", false
	}
	return cookies[0].Value, true
}

// CookieRequestFrom reads what the cookie rule needs off a request.
func CookieRequestFrom(request *http.Request) domain.CookieRequest {
	origins := request.Header.Values("Origin")
	description := domain.CookieRequest{
		Method:        request.Method,
		OriginPresent: len(origins) > 0,
		FetchSite:     request.Header.Get("Sec-Fetch-Site"),
		CSRFHeader:    request.Header.Get(domain.CSRFHeaderName),
	}
	// Two Origin headers are not one origin. Left empty, the comparison with
	// the configured origin fails.
	if len(origins) == 1 {
		description.Origin = origins[0]
	}
	if len(request.Header.Values("Sec-Fetch-Site")) > 1 {
		description.FetchSite = "conflicting"
	}
	return description
}
