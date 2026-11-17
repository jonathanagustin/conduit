package httpidentity_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/sessionmemory"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

const sessionOrigin = "https://conduit.example.test"

// recordingResolver stands where the bearer and certificate resolver stands,
// and remembers whether it was asked.
type recordingResolver struct {
	calls  int
	claims domain.Claims
	err    error
}

func (resolver *recordingResolver) resolve(*http.Request) (domain.Claims, error) {
	resolver.calls++
	return resolver.claims, resolver.err
}

func sessionFixture(t *testing.T) (*sessionmemory.Store, string, time.Time) {
	t.Helper()
	now := time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)
	store := sessionmemory.New()
	identifier, err := domain.NewOpaqueSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSession(context.Background(), domain.Session{
		IDHash: domain.HashSecret(identifier), Claims: domain.Claims{Username: "ada", Groups: []string{"operators"}},
		CreatedAt: now, ExpiresAt: now.Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	return store, identifier, now
}

func cookieRequest(method, identifier string) *http.Request {
	request := httptest.NewRequest(method, sessionOrigin+"/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: domain.SessionCookieName, Value: identifier})
	return request
}

// The bearer and certificate paths are reached before a cookie is looked at,
// and what they answer is returned untouched -- success and failure alike.
func TestBearerAndCertificateRequestsNeverReachTheSessionStore(t *testing.T) {
	t.Parallel()
	store, identifier, now := sessionFixture(t)
	innerError := errors.New("the inner resolver's own error")
	for name, edit := range map[string]func(*http.Request){
		"a bearer token": func(request *http.Request) { request.Header.Set("Authorization", "Bearer token") },
		"an empty Authorization header": func(request *http.Request) {
			request.Header.Set("Authorization", "")
		},
		"a verified client certificate": func(request *http.Request) {
			request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{}}}}
		},
	} {
		for _, outcome := range []struct {
			claims domain.Claims
			err    error
		}{{claims: domain.Claims{Username: "from-inner", ServiceAccount: true}}, {err: innerError}} {
			inner := &recordingResolver{claims: outcome.claims, err: outcome.err}
			resolve := httpidentity.WithOperatorSessions(inner.resolve, store,
				domain.CookiePolicy{PublicOrigin: sessionOrigin}, func() time.Time { return now })
			// A valid session cookie rides along and must make no difference.
			request := cookieRequest(http.MethodGet, identifier)
			edit(request)
			claims, err := resolve(request)
			if inner.calls != 1 || err != outcome.err || claims.Username != outcome.claims.Username ||
				claims.ServiceAccount != outcome.claims.ServiceAccount {
				t.Errorf("%s: inner calls=%d claims=%#v err=%v, want the inner resolver's answer untouched",
					name, inner.calls, claims, err)
			}
		}
	}
}

func TestARequestWithNoCookieIsTheInnerResolversToAnswer(t *testing.T) {
	t.Parallel()
	store, _, now := sessionFixture(t)
	inner := &recordingResolver{err: domain.ErrUnauthorized}
	resolve := httpidentity.WithOperatorSessions(inner.resolve, store,
		domain.CookiePolicy{PublicOrigin: sessionOrigin}, func() time.Time { return now })
	if _, err := resolve(httptest.NewRequest(http.MethodGet, "/v1/session", nil)); !errors.Is(err, domain.ErrUnauthorized) || inner.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, inner.calls)
	}
}

func TestASessionCookieResolvesToItsStoredClaimsUntilItExpires(t *testing.T) {
	t.Parallel()
	store, identifier, now := sessionFixture(t)
	clock := now
	inner := &recordingResolver{err: domain.ErrUnauthorized}
	resolve := httpidentity.WithOperatorSessions(inner.resolve, store,
		domain.CookiePolicy{PublicOrigin: sessionOrigin}, func() time.Time { return clock })

	claims, err := resolve(cookieRequest(http.MethodGet, identifier))
	if err != nil || claims.Username != "ada" || len(claims.Groups) != 1 || claims.OnBehalfOf != "" || inner.calls != 0 {
		t.Fatalf("claims=%#v err=%v inner calls=%d", claims, err, inner.calls)
	}

	write := cookieRequest(http.MethodPost, identifier)
	write.Header.Set("Origin", sessionOrigin)
	if _, err := resolve(write); !errors.Is(err, domain.ErrUnauthorized) || !errors.Is(err, httpidentity.ErrCookieRequestRefused) {
		t.Fatalf("a write with no CSRF header answered %v", err)
	}
	write.Header.Set(domain.CSRFHeaderName, domain.CSRFHeaderValue)
	if claims, err := resolve(write); err != nil || claims.Username != "ada" {
		t.Fatalf("the console's own write: %#v %v", claims, err)
	}
	write.Header.Add("Origin", "https://evil.example")
	if _, err := resolve(write); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("a write with two Origin headers answered %v", err)
	}

	other, _ := domain.NewOpaqueSecret()
	if _, err := resolve(cookieRequest(http.MethodGet, other)); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("an unknown identifier answered %v", err)
	}
	// The hash is what the store holds. Presenting it is not presenting the cookie.
	if _, err := resolve(cookieRequest(http.MethodGet, domain.HashSecret(identifier))); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("the stored hash was accepted as a session cookie: %v", err)
	}
	clock = now.Add(time.Hour)
	if _, err := resolve(cookieRequest(http.MethodGet, identifier)); !errors.Is(err, domain.ErrUnauthorized) {
		t.Fatalf("an expired session answered %v", err)
	}
}

type failingSessions struct{ *sessionmemory.Store }

func (failingSessions) Session(context.Context, string, time.Time) (domain.Session, error) {
	return domain.Session{Claims: domain.Claims{Username: "ada"}}, errors.New("the database is down")
}

func TestAStoreThatCannotBeReadSignsNobodyIn(t *testing.T) {
	t.Parallel()
	store, identifier, now := sessionFixture(t)
	inner := &recordingResolver{claims: domain.Claims{Username: "from-inner"}}
	resolve := httpidentity.WithOperatorSessions(inner.resolve, failingSessions{store},
		domain.CookiePolicy{PublicOrigin: sessionOrigin}, func() time.Time { return now })
	if claims, err := resolve(cookieRequest(http.MethodGet, identifier)); !errors.Is(err, domain.ErrUnauthorized) || claims.Username != "" {
		t.Fatalf("claims=%#v err=%v", claims, err)
	}
}
