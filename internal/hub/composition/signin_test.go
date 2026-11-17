package composition

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/hub/ratelimit"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/sessionmemory"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/signinhttp"
	identitycomposition "github.com/jonathanagustin/conduit/internal/identity/composition"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/oidcfake"
	identityports "github.com/jonathanagustin/conduit/internal/identity/ports"
)

// These drive browser sign-in against the assembled Hub and a real provider.
//
// Nothing between the cookie and the route is mocked: the fake issuer serves
// discovery and keys over TLS and checks PKCE and the client secret itself, the
// Hub is NewHub with the production resolver, and the "browser" is the cookies
// and headers a browser would send. Each test names the check it would stop
// passing without, because every one of them was written by deleting that check
// and watching it fail.

const (
	signInOrigin   = "https://conduit.example.test"
	signInClientID = "conduit-hub"
	signInSecret   = "s3cret/with+reserved&characters"
)

// signInClock is the wall clock plus an offset. The offset moves the Hub's idea
// of now without touching go-oidc's, which reads the real clock.
type signInClock struct {
	mutex  sync.Mutex
	offset time.Duration
}

func (clock *signInClock) Now() time.Time {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	return time.Now().Add(clock.offset)
}

func (clock *signInClock) Advance(by time.Duration) {
	clock.mutex.Lock()
	defer clock.mutex.Unlock()
	clock.offset += by
}

type signInHarness struct {
	t        *testing.T
	provider *oidcfake.Provider
	hub      http.Handler
	options  controlplane.HandlerOptions
	sessions identityports.SessionStore
	clock    *signInClock
	logs     *bytes.Buffer
	writes   atomic.Int64
}

type signInSetup struct {
	policy          identity.ClaimPolicy
	sessionLifetime time.Duration
	tokenLifetime   time.Duration
	publicClient    bool
	wrapSessions    func(identityports.SessionStore) identityports.SessionStore
	withoutEnd      bool
}

func newSignInHarness(t *testing.T, setup signInSetup) *signInHarness {
	t.Helper()
	clock := &signInClock{}
	secret := signInSecret
	if setup.publicClient {
		secret = ""
	}
	provider, err := oidcfake.Start(oidcfake.Options{
		ClientID: signInClientID, ClientSecret: secret,
		RedirectURIs:  []string{signInOrigin + signinhttp.CallbackPath},
		Identity:      oidcfake.Identity{Subject: "subject-ada", Username: "ada", Groups: []string{"reviewers", "operators"}},
		TokenLifetime: setup.tokenLifetime, WithoutEndSession: setup.withoutEnd,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Close)
	policy := setup.policy
	policy.Issuer, policy.ClientID = provider.Issuer(), signInClientID
	if policy.GroupsClaim == "" {
		policy.GroupsClaim = "groups"
	}
	var sessions identityports.SessionStore = sessionmemory.New()
	if setup.wrapSessions != nil {
		sessions = setup.wrapSessions(sessions)
	}
	logs := &bytes.Buffer{}
	resolved, err := identitycomposition.NewOperatorIdentity(context.Background(), identitycomposition.OperatorIdentityConfig{
		Policy: policy, HTTPClient: provider.Client(), Now: clock.Now,
		SignIn: &identitycomposition.OperatorSignInConfig{
			PublicURL: signInOrigin, ClientSecret: secret, SessionLifetime: setup.sessionLifetime,
			Scopes: []string{"profile"}, Sessions: sessions,
			Logger: slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	options := controlplane.HandlerOptions{
		OperatorIdentityResolver: resolved.Resolver, BootstrapAdministrators: []string{"ada"},
		OperatorSignIn: controlplane.OperatorSignIn{
			Issuer: policy.Issuer, ClientID: signInClientID,
			LoginPath: signinhttp.LoginPath, LogoutPath: signinhttp.LogoutPath,
		},
	}
	hub, err := NewHub(controlplane.NewMemoryRepository(), options, Options{SignIn: resolved.SignIn})
	if err != nil {
		t.Fatal(err)
	}
	return &signInHarness{t: t, provider: provider, hub: hub, options: options, sessions: sessions, clock: clock, logs: logs}
}

func (harness *signInHarness) do(request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	harness.hub.ServeHTTP(response, request)
	return response
}

func responseCookie(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// signInTrip is one browser's journey, with everything it saw kept.
type signInTrip struct {
	authorize   *url.URL
	loginCookie *http.Cookie
	callback    string // path and query the provider sent the browser back to
}

// begin asks the Hub to start a sign-in and returns where it sent the browser.
func (harness *signInHarness) begin(returnTo string, edit func(*http.Request)) signInTrip {
	harness.t.Helper()
	target := signinhttp.LoginPath
	if returnTo != "" {
		target += "?returnTo=" + url.QueryEscape(returnTo)
	}
	request := httptest.NewRequest(http.MethodGet, signInOrigin+target, nil)
	if edit != nil {
		edit(request)
	}
	response := harness.do(request)
	if response.Code != http.StatusFound {
		harness.t.Fatalf("login answered %d %s", response.Code, response.Body.String())
	}
	authorize, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		harness.t.Fatal(err)
	}
	cookie := responseCookie(response, identity.LoginCookieName)
	if cookie == nil {
		harness.t.Fatal("login set no binding cookie")
	}
	return signInTrip{authorize: authorize, loginCookie: cookie}
}

// atProvider follows the redirect to the issuer, which signs the person in and
// sends the browser back.
func (harness *signInHarness) atProvider(trip signInTrip) signInTrip {
	harness.t.Helper()
	client := harness.provider.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(trip.authorize.String())
	if err != nil {
		harness.t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		harness.t.Fatalf("the provider answered %d to %s", response.StatusCode, trip.authorize)
	}
	back, err := url.Parse(response.Header.Get("Location"))
	if err != nil {
		harness.t.Fatal(err)
	}
	if back.Scheme+"://"+back.Host != signInOrigin {
		harness.t.Fatalf("the provider sent the browser to %s", back)
	}
	trip.callback = back.RequestURI()
	return trip
}

// finish presents the callback to the Hub with the cookies given.
func (harness *signInHarness) finish(callback string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, signInOrigin+callback, nil)
	for _, cookie := range cookies {
		if cookie != nil {
			request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
		}
	}
	return harness.do(request)
}

// signIn runs the whole journey and returns the session cookie.
func (harness *signInHarness) signIn() *http.Cookie {
	harness.t.Helper()
	trip := harness.atProvider(harness.begin("", nil))
	response := harness.finish(trip.callback, trip.loginCookie)
	session := responseCookie(response, identity.SessionCookieName)
	if response.Code != http.StatusSeeOther || session == nil || session.Value == "" {
		harness.t.Fatalf("sign-in did not produce a session: %d Location=%q logs=%s",
			response.Code, response.Header().Get("Location"), harness.logs.String())
	}
	return session
}

// requireRefused asserts a callback produced no session and landed on the one
// failure address.
func requireRefused(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != signinhttp.FailedReturnPath {
		t.Fatalf("a refused sign-in answered %d Location=%q", response.Code, response.Header().Get("Location"))
	}
	if cookie := responseCookie(response, identity.SessionCookieName); cookie != nil && cookie.Value != "" {
		t.Fatal("a refused sign-in set a session cookie")
	}
}

func (harness *signInHarness) sessionRequest(cookie *http.Cookie, edit func(*http.Request)) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, signInOrigin+"/v1/session", nil)
	if cookie != nil {
		request.AddCookie(&http.Cookie{Name: cookie.Name, Value: cookie.Value})
	}
	if edit != nil {
		edit(request)
	}
	return harness.do(request)
}

// teamWrite is a state-changing request a signed-in administrator may make.
// Each call writes a team of its own, so no call conflicts with an earlier one.
func (harness *signInHarness) teamWrite(edit func(*http.Request)) *httptest.ResponseRecorder {
	name := "team-" + strconv.FormatInt(harness.writes.Add(1), 10)
	request := httptest.NewRequest(http.MethodPut, signInOrigin+"/v1/teams/"+name,
		strings.NewReader(`{"displayName":"Team","adminUsers":["ada"],"memberGroups":["operators"]}`))
	request.Header.Set("Content-Type", "application/json")
	edit(request)
	return harness.do(request)
}

func TestSignInEstablishesASessionWithTheStateNonceAndPKCEARealProviderChecks(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	trip := harness.begin("/console/#/plans/plan-1", nil)

	query := trip.authorize.Query()
	if got := trip.authorize.Scheme + "://" + trip.authorize.Host + trip.authorize.Path; got != harness.provider.Issuer()+"/authorize" {
		t.Fatalf("the browser was sent to %s, not the discovered authorization endpoint", got)
	}
	for name, want := range map[string]string{
		"response_type": "code", "client_id": signInClientID, "code_challenge_method": "S256",
		"redirect_uri": signInOrigin + "/v1/auth/callback", "scope": "openid profile",
	} {
		if query.Get(name) != want {
			t.Errorf("authorization request %s = %q, want %q", name, query.Get(name), want)
		}
	}
	state, nonce, challenge := query.Get("state"), query.Get("nonce"), query.Get("code_challenge")
	if !identity.ValidOpaqueSecret(state) || !identity.ValidOpaqueSecret(nonce) || len(challenge) != 43 || state == nonce {
		t.Fatalf("state, nonce or challenge is not a fresh 256-bit value: %q %q %q", state, nonce, challenge)
	}
	if strings.Contains(trip.authorize.RawQuery, signInSecret) || strings.Contains(trip.authorize.RawQuery, "code_verifier") {
		t.Fatal("the authorization request carried the client secret or the PKCE verifier through the browser")
	}

	// The login cookie binds the attempt to this browser.
	login := trip.loginCookie
	if !login.HttpOnly || !login.Secure || login.SameSite != http.SameSiteLaxMode || login.Path != "/" ||
		login.Domain != "" || login.MaxAge <= 0 || login.MaxAge > 600 || !strings.HasPrefix(login.Name, "__Host-") {
		t.Fatalf("login cookie attributes: %#v", login)
	}
	if login.Value == state || login.Value == nonce {
		t.Fatal("the browser binding repeats a value that travels in the URL")
	}

	trip = harness.atProvider(trip)
	response := harness.finish(trip.callback, trip.loginCookie)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/console/#/plans/plan-1" {
		t.Fatalf("callback answered %d Location=%q; logs: %s", response.Code, response.Header().Get("Location"), harness.logs)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("the callback response may be cached")
	}
	session := responseCookie(response, identity.SessionCookieName)
	if session == nil || !identity.ValidOpaqueSecret(session.Value) {
		t.Fatalf("session cookie: %#v", session)
	}
	// Five-minute token, eight-hour default bound: the token's expiry wins.
	if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" ||
		session.Domain != "" || session.Name != "__Host-conduit-session" || session.MaxAge <= 0 || session.MaxAge > 300 {
		t.Fatalf("session cookie attributes: %#v", session)
	}
	if cleared := responseCookie(response, identity.LoginCookieName); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("the login cookie was not cleared: %#v", cleared)
	}

	// What the provider's token endpoint actually received.
	requests := harness.provider.TokenRequests()
	if len(requests) != 1 || !requests[0].Granted {
		t.Fatalf("token requests: %#v", requests)
	}
	verifier := requests[0].Form.Get("code_verifier")
	sum := sha256.Sum256([]byte(verifier))
	if verifier == "" || base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		t.Fatal("the code verifier sent to the token endpoint does not match the challenge sent through the browser")
	}
	if !requests[0].BasicPresent || requests[0].Form.Get("client_secret") != "" ||
		requests[0].Form.Get("redirect_uri") != signInOrigin+"/v1/auth/callback" {
		t.Fatalf("token request did not authenticate the confidential client as expected: %#v", requests[0])
	}

	// The store holds the hash and never the cookie's value.
	if _, err := harness.sessions.Session(context.Background(), session.Value, harness.clock.Now()); err == nil {
		t.Fatal("the session store is keyed by the cookie's value")
	}
	if _, err := harness.sessions.Session(context.Background(), identity.HashSecret(session.Value), harness.clock.Now()); err != nil {
		t.Fatalf("the session is not stored under its hash: %v", err)
	}

	// Nothing a person could sign in with reached the log.
	logged := harness.logs.String()
	code, _ := url.ParseQuery(strings.SplitN(trip.callback, "?", 2)[1])
	for name, secret := range map[string]string{
		"session identifier": session.Value, "state": state, "nonce": nonce, "PKCE verifier": verifier,
		"authorization code": code.Get("code"), "login binding": login.Value, "client secret": signInSecret,
	} {
		if secret == "" || strings.Contains(logged, secret) {
			t.Errorf("the %s was logged (or was empty)", name)
		}
	}
	if !strings.Contains(logged, "operator signed in") || !strings.Contains(logged, "username=ada") {
		t.Fatalf("the sign-in was not recorded: %s", logged)
	}
}

func TestASessionCookieYieldsThePrincipalABearerTokenWithTheSameClaimsDoes(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	session := harness.signIn()

	token, err := harness.provider.IDToken("")
	if err != nil {
		t.Fatal(err)
	}
	// A team the token's "operators" group belongs to, so that the projection
	// below shows whether the groups survived, not only the username.
	if seeded := harness.teamWrite(func(request *http.Request) {
		request.Header.Set("Authorization", "Bearer "+token)
	}); seeded.Code != http.StatusOK {
		t.Fatalf("seeding a team: %d %s", seeded.Code, seeded.Body)
	}
	byCookie := harness.sessionRequest(session, nil)
	byBearer := harness.sessionRequest(nil, func(request *http.Request) {
		request.Header.Set("Authorization", "Bearer "+token)
	})
	if byCookie.Code != http.StatusOK || byBearer.Code != http.StatusOK {
		t.Fatalf("cookie=%d %s bearer=%d %s", byCookie.Code, byCookie.Body, byBearer.Code, byBearer.Body)
	}
	if byCookie.Body.String() != byBearer.Body.String() {
		t.Fatalf("the session projection differs:\ncookie: %s\nbearer: %s", byCookie.Body, byBearer.Body)
	}
	var projection struct {
		Actor struct {
			Username       string   `json:"username"`
			Teams          []string `json:"teams"`
			ServiceAccount bool     `json:"serviceAccount"`
		} `json:"actor"`
		Delegated bool `json:"delegated"`
	}
	if err := json.Unmarshal(byCookie.Body.Bytes(), &projection); err != nil {
		t.Fatal(err)
	}
	if projection.Actor.Username != "ada" || len(projection.Actor.Teams) != 1 || projection.Actor.Teams[0] != "team-1" ||
		projection.Actor.ServiceAccount || projection.Delegated {
		t.Fatalf("principal = %#v in %s", projection, byCookie.Body)
	}

	// And the same authorization: the bootstrap administrator may write a team
	// either way, and nobody may with no credential.
	cookieWrite := harness.teamWrite(func(request *http.Request) {
		request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
		request.Header.Set("Origin", signInOrigin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set(identity.CSRFHeaderName, identity.CSRFHeaderValue)
	})
	bearerWrite := harness.teamWrite(func(request *http.Request) { request.Header.Set("Authorization", "Bearer "+token) })
	anonymous := harness.teamWrite(func(*http.Request) {})
	if cookieWrite.Code != http.StatusOK || bearerWrite.Code != http.StatusOK || anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("team write: cookie=%d %s bearer=%d %s anonymous=%d",
			cookieWrite.Code, cookieWrite.Body, bearerWrite.Code, bearerWrite.Body, anonymous.Code)
	}
}

func TestACallbackIsRefusedUnlessItsStateIsKnownUnusedAndFromTheSameBrowser(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})

	t.Run("no state", func(t *testing.T) {
		trip := harness.atProvider(harness.begin("", nil))
		values, _ := url.ParseQuery(strings.SplitN(trip.callback, "?", 2)[1])
		requireRefused(t, harness.finish("/v1/auth/callback?code="+url.QueryEscape(values.Get("code")), trip.loginCookie))
	})
	t.Run("a state this Hub never issued", func(t *testing.T) {
		trip := harness.atProvider(harness.begin("", nil))
		values, _ := url.ParseQuery(strings.SplitN(trip.callback, "?", 2)[1])
		forged, _ := identity.NewOpaqueSecret()
		requireRefused(t, harness.finish("/v1/auth/callback?code="+url.QueryEscape(values.Get("code"))+"&state="+forged, trip.loginCookie))
	})
	t.Run("another attempt's state", func(t *testing.T) {
		// Two attempts in two browsers. The first browser presents its own
		// cookie with the second's callback: the state is real, and not its.
		first := harness.begin("", nil)
		second := harness.atProvider(harness.begin("", nil))
		requireRefused(t, harness.finish(second.callback, first.loginCookie))
	})
	t.Run("the browser that did not start it", func(t *testing.T) {
		// Login CSRF: an attacker finishes their own sign-in in a victim's
		// browser, which holds no binding cookie.
		trip := harness.atProvider(harness.begin("", nil))
		requireRefused(t, harness.finish(trip.callback))
	})
	t.Run("a replayed callback", func(t *testing.T) {
		trip := harness.atProvider(harness.begin("", nil))
		first := harness.finish(trip.callback, trip.loginCookie)
		if responseCookie(first, identity.SessionCookieName) == nil {
			t.Fatalf("the first callback did not sign in: %s", harness.logs)
		}
		before := len(harness.provider.TokenRequests())
		requireRefused(t, harness.finish(trip.callback, trip.loginCookie))
		if after := len(harness.provider.TokenRequests()); after != before {
			t.Fatal("a replayed state reached the token endpoint")
		}
	})
	t.Run("an attempt left too long", func(t *testing.T) {
		expiring := newSignInHarness(t, signInSetup{})
		trip := expiring.atProvider(expiring.begin("", nil))
		expiring.clock.Advance(identity.LoginAttemptLifetime + time.Second)
		requireRefused(t, expiring.finish(trip.callback, trip.loginCookie))
	})
	t.Run("the provider reported an error", func(t *testing.T) {
		trip := harness.begin("", nil)
		state := trip.authorize.Query().Get("state")
		requireRefused(t, harness.finish("/v1/auth/callback?error=access_denied&state="+state, trip.loginCookie))
	})
}

func TestAnIDTokenThatDoesNotAnswerThisSignInOpensNoSession(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		setup  signInSetup
		mutate func(map[string]any)
		forge  bool
	}{
		"nonce of another request": {mutate: func(claims map[string]any) { claims["nonce"] = "nonce-of-another-request-0123456789abcdef" }},
		"no nonce":                 {mutate: func(claims map[string]any) { delete(claims, "nonce") }},
		"audience of another client": {mutate: func(claims map[string]any) {
			claims["aud"] = "another-client"
		}},
		// With a policy that reads its audience from another claim, go-oidc's
		// own audience check is off. The ID token's aud is still checked.
		"audience of another client under an audience-claim policy": {
			setup: signInSetup{policy: identity.ClaimPolicy{AudienceClaim: "client_id"}},
			mutate: func(claims map[string]any) {
				claims["client_id"], claims["aud"] = signInClientID, "another-client"
			},
		},
		"authorized party of another client": {mutate: func(claims map[string]any) {
			claims["aud"], claims["azp"] = []string{signInClientID, "another-client"}, "another-client"
		}},
		"another issuer": {mutate: func(claims map[string]any) { claims["iss"] = "https://attacker.example" }},
		"already expired": {mutate: func(claims map[string]any) {
			claims["exp"] = time.Now().Add(-time.Minute).Unix()
		}},
		"signed by a key the issuer does not publish": {forge: true},
		"no username": {mutate: func(claims map[string]any) { delete(claims, "preferred_username") }},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			harness := newSignInHarness(t, test.setup)
			if test.setup.policy.AudienceClaim != "" {
				// The control: this policy does sign in when aud is right.
				harness.provider.MutateClaims(func(claims map[string]any) { claims["client_id"] = signInClientID })
				harness.signIn()
			}
			harness.provider.MutateClaims(test.mutate)
			if test.forge {
				if err := harness.provider.SignWithUnpublishedKey(); err != nil {
					t.Fatal(err)
				}
			}
			trip := harness.atProvider(harness.begin("", nil))
			response := harness.finish(trip.callback, trip.loginCookie)
			requireRefused(t, response)
			// The provider did issue a token: the refusal is the Hub's.
			requests := harness.provider.TokenRequests()
			if len(requests) == 0 || !requests[len(requests)-1].Granted {
				t.Fatalf("the provider did not grant a token, so this did not test the Hub: %#v", requests)
			}
		})
	}
}

// tamperingSessions hands back a sign-in attempt whose PKCE verifier is not the
// one the challenge was made from, which is what a Hub with a broken verifier
// would send.
type tamperingSessions struct{ identityports.SessionStore }

func (store tamperingSessions) TakeLoginAttempt(ctx context.Context, hash string, now time.Time) (identity.LoginAttempt, error) {
	attempt, err := store.SessionStore.TakeLoginAttempt(ctx, hash, now)
	attempt.CodeVerifier, _ = identity.NewOpaqueSecret()
	return attempt, err
}

func TestTheProviderRefusesACodePresentedWithTheWrongVerifierAndNoSessionOpens(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{wrapSessions: func(inner identityports.SessionStore) identityports.SessionStore {
		return tamperingSessions{inner}
	}})
	trip := harness.atProvider(harness.begin("", nil))
	requireRefused(t, harness.finish(trip.callback, trip.loginCookie))
	requests := harness.provider.TokenRequests()
	if len(requests) != 1 || requests[0].Granted || requests[0].Form.Get("code_verifier") == "" {
		t.Fatalf("the provider should have received and refused a wrong verifier: %#v", requests)
	}
}

func TestAPublicClientSignsInWithPKCEAndNoSecret(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{publicClient: true})
	harness.signIn()
	requests := harness.provider.TokenRequests()
	if len(requests) != 1 || requests[0].BasicPresent || requests[0].Form.Get("client_id") != signInClientID ||
		requests[0].Form.Has("client_secret") || requests[0].Form.Get("code_verifier") == "" {
		t.Fatalf("public client token request: %#v", requests)
	}
}

func TestASessionEndsAtTheEarlierOfTheTokensExpiryAndTheConfiguredBound(t *testing.T) {
	t.Parallel()
	t.Run("the ID token's expiry", func(t *testing.T) {
		harness := newSignInHarness(t, signInSetup{tokenLifetime: 5 * time.Minute, sessionLifetime: 8 * time.Hour})
		session := harness.signIn()
		harness.clock.Advance(4 * time.Minute)
		if response := harness.sessionRequest(session, nil); response.Code != http.StatusOK {
			t.Fatalf("a live session answered %d", response.Code)
		}
		harness.clock.Advance(90 * time.Second)
		if response := harness.sessionRequest(session, nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("a session outlived its ID token: %d", response.Code)
		}
	})
	t.Run("the configured bound", func(t *testing.T) {
		harness := newSignInHarness(t, signInSetup{tokenLifetime: time.Hour, sessionLifetime: 10 * time.Minute})
		trip := harness.atProvider(harness.begin("", nil))
		response := harness.finish(trip.callback, trip.loginCookie)
		session := responseCookie(response, identity.SessionCookieName)
		if session == nil || session.MaxAge > 600 || session.MaxAge < 590 {
			t.Fatalf("session cookie Max-Age = %#v, want the ten-minute bound", session)
		}
		harness.clock.Advance(9 * time.Minute)
		if response := harness.sessionRequest(session, nil); response.Code != http.StatusOK {
			t.Fatalf("a live session answered %d", response.Code)
		}
		harness.clock.Advance(2 * time.Minute)
		if response := harness.sessionRequest(session, nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("a session outlived the configured bound: %d", response.Code)
		}
	})
}

func TestSignOutDestroysTheSessionOnTheServer(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	session := harness.signIn()
	logout := func(edit func(*http.Request)) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, signInOrigin+signinhttp.LogoutPath, nil)
		request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
		edit(request)
		return harness.do(request)
	}
	// A cross-site page cannot sign somebody out.
	for name, edit := range map[string]func(*http.Request){
		"no CSRF header": func(request *http.Request) { request.Header.Set("Origin", signInOrigin) },
		"cross-site origin": func(request *http.Request) {
			request.Header.Set("Origin", "https://evil.example")
			request.Header.Set(identity.CSRFHeaderName, identity.CSRFHeaderValue)
		},
	} {
		if response := logout(edit); response.Code != http.StatusForbidden {
			t.Fatalf("sign-out with %s answered %d", name, response.Code)
		}
		if response := harness.sessionRequest(session, nil); response.Code != http.StatusOK {
			t.Fatalf("a refused sign-out (%s) ended the session", name)
		}
	}
	response := logout(func(request *http.Request) {
		request.Header.Set("Origin", signInOrigin)
		request.Header.Set(identity.CSRFHeaderName, identity.CSRFHeaderValue)
	})
	if response.Code != http.StatusOK {
		t.Fatalf("sign-out answered %d %s", response.Code, response.Body)
	}
	cleared := responseCookie(response, identity.SessionCookieName)
	if cleared == nil || cleared.MaxAge >= 0 || cleared.Value != "" || !cleared.Secure || !cleared.HttpOnly || cleared.Path != "/" {
		t.Fatalf("sign-out did not clear the cookie: %#v", cleared)
	}
	var body struct {
		SignedOut     bool   `json:"signedOut"`
		EndSessionURL string `json:"endSessionUrl"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	end, err := url.Parse(body.EndSessionURL)
	if err != nil || !body.SignedOut || end.Scheme+"://"+end.Host+end.Path != harness.provider.Issuer()+"/logout" ||
		end.Query().Get("client_id") != signInClientID ||
		end.Query().Get("post_logout_redirect_uri") != signInOrigin+"/console/" {
		t.Fatalf("sign-out body: %s", response.Body)
	}
	// The cookie the browser was told to drop is replayed by somebody who kept it.
	if replay := harness.sessionRequest(session, nil); replay.Code != http.StatusUnauthorized {
		t.Fatalf("a signed-out session still resolves: %d %s", replay.Code, replay.Body)
	}
	// Signing out with no session is not an error and discloses nothing.
	request := httptest.NewRequest(http.MethodPost, signInOrigin+signinhttp.LogoutPath, nil)
	if again := harness.do(request); again.Code != http.StatusOK {
		t.Fatalf("sign-out with no cookie answered %d", again.Code)
	}

	withoutEnd := newSignInHarness(t, signInSetup{withoutEnd: true})
	other := withoutEnd.signIn()
	request = httptest.NewRequest(http.MethodPost, signInOrigin+signinhttp.LogoutPath, nil)
	request.AddCookie(&http.Cookie{Name: other.Name, Value: other.Value})
	request.Header.Set("Origin", signInOrigin)
	request.Header.Set(identity.CSRFHeaderName, identity.CSRFHeaderValue)
	if response := withoutEnd.do(request); response.Code != http.StatusOK || strings.Contains(response.Body.String(), "endSessionUrl") {
		t.Fatalf("an issuer with no end-session endpoint: %d %s", response.Code, response.Body)
	}
}

func TestSignInOnlyEverReturnsTheBrowserToTheConsole(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	for returnTo, want := range map[string]string{
		"":                                "/console/",
		"/console/":                       "/console/",
		"/console/#/environments/prod":    "/console/#/environments/prod",
		"/console/?view=fleet#/overview":  "/console/?view=fleet#/overview",
		"//evil.example":                  "/console/",
		"//evil.example/console/":         "/console/",
		"https://evil.example":            "/console/",
		"https://evil.example/console/":   "/console/",
		`/\evil.example`:                  "/console/",
		`/console/\..\..\evil`:            "/console/",
		"/console/../v1/auth/logout":      "/console/",
		"/console//evil.example":          "/console/",
		"/v1/session":                     "/console/",
		"/consolex/":                      "/console/",
		"javascript:alert(1)":             "/console/",
		"/console/\r\nSet-Cookie: a=b":    "/console/",
		"/console/%2e%2e/%2e%2e/v1/plans": "/console/",
	} {
		trip := harness.atProvider(harness.begin(returnTo, nil))
		response := harness.finish(trip.callback, trip.loginCookie)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != want {
			t.Errorf("returnTo=%q sent the browser to %q, want %q", returnTo, response.Header().Get("Location"), want)
		}
	}
}

func TestTheRedirectAddressComesFromConfigurationAndNeverFromTheRequest(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	trip := harness.begin("", func(request *http.Request) {
		request.Host = "evil.example"
		request.Header.Set("X-Forwarded-Host", "evil.example")
		request.Header.Set("X-Forwarded-Proto", "http")
		request.Header.Set("Forwarded", "host=evil.example;proto=http")
		request.Header.Set("Origin", "https://evil.example")
	})
	if got := trip.authorize.Query().Get("redirect_uri"); got != signInOrigin+"/v1/auth/callback" {
		t.Fatalf("redirect_uri = %q", got)
	}
	if strings.Contains(trip.authorize.String(), "evil.example") {
		t.Fatalf("a request header reached the authorization request: %s", trip.authorize)
	}
	// And the same at the token endpoint.
	trip = harness.atProvider(trip)
	request := httptest.NewRequest(http.MethodGet, signInOrigin+trip.callback, nil)
	request.Host = "evil.example"
	request.Header.Set("X-Forwarded-Host", "evil.example")
	request.AddCookie(&http.Cookie{Name: trip.loginCookie.Name, Value: trip.loginCookie.Value})
	if response := harness.do(request); responseCookie(response, identity.SessionCookieName) == nil {
		t.Fatalf("sign-in failed: %s", harness.logs)
	}
	if got := harness.provider.TokenRequests()[0].Form.Get("redirect_uri"); got != signInOrigin+"/v1/auth/callback" {
		t.Fatalf("token request redirect_uri = %q", got)
	}
}

func TestACookieIsACredentialOnlyFromTheConsolesOwnOriginAndAWriteNeedsTheHeader(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	session := harness.signIn()
	token, err := harness.provider.IDToken("")
	if err != nil {
		t.Fatal(err)
	}
	withCookie := func(edit func(*http.Request)) func(*http.Request) {
		return func(request *http.Request) {
			request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
			edit(request)
		}
	}
	sameOrigin := func(request *http.Request) {
		request.Header.Set("Origin", signInOrigin)
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.Header.Set(identity.CSRFHeaderName, identity.CSRFHeaderValue)
	}

	writes := map[string]struct {
		edit func(*http.Request)
		want int
	}{
		"the console's own write": {withCookie(sameOrigin), http.StatusOK},
		"no CSRF header": {withCookie(func(request *http.Request) {
			sameOrigin(request)
			request.Header.Del(identity.CSRFHeaderName)
		}), http.StatusUnauthorized},
		"a CSRF header with another value": {withCookie(func(request *http.Request) {
			sameOrigin(request)
			request.Header.Set(identity.CSRFHeaderName, "true")
		}), http.StatusUnauthorized},
		"a cross-site Origin": {withCookie(func(request *http.Request) {
			sameOrigin(request)
			request.Header.Set("Origin", "https://evil.example")
		}), http.StatusUnauthorized},
		"a sibling subdomain, which SameSite calls the same site": {withCookie(func(request *http.Request) {
			sameOrigin(request)
			request.Header.Set("Origin", "https://evil.example.test")
			request.Header.Set("Sec-Fetch-Site", "same-site")
		}), http.StatusUnauthorized},
		"the right Origin over http": {withCookie(func(request *http.Request) {
			sameOrigin(request)
			request.Header.Set("Origin", "http://conduit.example.test")
		}), http.StatusUnauthorized},
		"an opaque Origin": {withCookie(func(request *http.Request) {
			sameOrigin(request)
			request.Header.Set("Origin", "null")
		}), http.StatusUnauthorized},
		"no Origin at all": {withCookie(func(request *http.Request) {
			request.Header.Set(identity.CSRFHeaderName, identity.CSRFHeaderValue)
		}), http.StatusUnauthorized},
		"Sec-Fetch-Site cross-site with a forged Origin": {withCookie(func(request *http.Request) {
			sameOrigin(request)
			request.Header.Set("Sec-Fetch-Site", "cross-site")
		}), http.StatusUnauthorized},
		// Bearer tokens are not cookies. None of the above applies to them.
		"a bearer token with no CSRF header": {func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
		}, http.StatusOK},
		"a bearer token from another origin": {func(request *http.Request) {
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Origin", "https://evil.example")
			request.Header.Set("Sec-Fetch-Site", "cross-site")
		}, http.StatusOK},
		// Two credentials are not a choice: a bad token is not rescued by a
		// good cookie, exactly as before sessions existed.
		"a bad bearer token beside a good cookie": {withCookie(func(request *http.Request) {
			sameOrigin(request)
			request.Header.Set("Authorization", "Bearer not-a-token")
		}), http.StatusUnauthorized},
	}
	for name, test := range writes {
		if response := harness.teamWrite(test.edit); response.Code != test.want {
			t.Errorf("write with %s answered %d, want %d (%s)", name, response.Code, test.want, response.Body)
		}
	}

	reads := map[string]struct {
		edit func(*http.Request)
		want int
	}{
		"a same-origin fetch": {func(request *http.Request) {
			request.Header.Set("Sec-Fetch-Site", "same-origin")
		}, http.StatusOK},
		"an address typed by the person": {func(request *http.Request) {
			request.Header.Set("Sec-Fetch-Site", "none")
		}, http.StatusOK},
		"a browser that sends neither header": {func(*http.Request) {}, http.StatusOK},
		"a cross-site page": {func(request *http.Request) {
			request.Header.Set("Sec-Fetch-Site", "cross-site")
		}, http.StatusUnauthorized},
		"a sibling subdomain": {func(request *http.Request) {
			request.Header.Set("Origin", "https://evil.example.test")
			request.Header.Set("Sec-Fetch-Site", "same-site")
		}, http.StatusUnauthorized},
		"a cross-origin Origin with no fetch metadata": {func(request *http.Request) {
			request.Header.Set("Origin", "https://evil.example")
		}, http.StatusUnauthorized},
	}
	for name, test := range reads {
		if response := harness.sessionRequest(session, test.edit); response.Code != test.want {
			t.Errorf("read from %s answered %d, want %d", name, response.Code, test.want)
		}
	}

	// A cookie that is not one this Hub issued, and two cookies of the name.
	forged, _ := identity.NewOpaqueSecret()
	if response := harness.sessionRequest(&http.Cookie{Name: session.Name, Value: forged}, nil); response.Code != http.StatusUnauthorized {
		t.Fatalf("an unknown session identifier answered %d", response.Code)
	}
	if response := harness.sessionRequest(session, func(request *http.Request) {
		request.AddCookie(&http.Cookie{Name: session.Name, Value: forged})
	}); response.Code != http.StatusUnauthorized {
		t.Fatalf("two session cookies answered %d", response.Code)
	}
}

func TestTheHubSendsNoCrossOriginResourceSharingHeaders(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	session := harness.signIn()
	for _, target := range []struct{ method, path string }{
		{http.MethodOptions, "/v1/teams/platform"}, {http.MethodGet, "/v1/session"},
		{http.MethodGet, "/v1/operator-identity"}, {http.MethodPost, signinhttp.LogoutPath},
	} {
		request := httptest.NewRequest(target.method, signInOrigin+target.path, nil)
		request.Header.Set("Origin", "https://evil.example")
		request.Header.Set("Access-Control-Request-Method", "PUT")
		request.Header.Set("Access-Control-Request-Headers", identity.CSRFHeaderName)
		request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
		response := harness.do(request)
		for header := range response.Header() {
			if strings.HasPrefix(strings.ToLower(header), "access-control-") {
				t.Errorf("%s %s answered %s: %s", target.method, target.path, header, response.Header().Get(header))
			}
		}
	}
}

func TestSignInRoutesAreAnonymousToTheRateLimiterAndSessionsSpendTheOperatorsBudget(t *testing.T) {
	t.Parallel()
	harness := newSignInHarness(t, signInSetup{})
	session := harness.signIn()
	routes := http.NewServeMux()
	for _, route := range HubRouteAuthorization() {
		routes.Handle(route.Pattern, routeDecision(route.Decision))
	}
	for _, target := range []struct{ method, path string }{
		{http.MethodGet, signinhttp.LoginPath}, {http.MethodGet, signinhttp.CallbackPath}, {http.MethodPost, signinhttp.LogoutPath},
	} {
		request := httptest.NewRequest(target.method, signInOrigin+target.path, nil)
		request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
		if class, _, exempt := rateLimitSubject(routes, request, harness.options); class != ratelimit.ClassAnonymous || exempt {
			t.Errorf("%s %s is limited as %q (exempt=%v), want anonymous", target.method, target.path, class, exempt)
		}
	}
	request := httptest.NewRequest(http.MethodGet, signInOrigin+"/v1/session", nil)
	request.AddCookie(&http.Cookie{Name: session.Name, Value: session.Value})
	if class, key, _ := rateLimitSubject(routes, request, harness.options); class != ratelimit.ClassOperator || key != "user:ada" {
		t.Fatalf("a signed-in browser is limited as %q %q, want the operator ada", class, key)
	}
}

func TestAHubWithoutSignInAnswersItsRoutesNotFoundAndPublishesNoLoginPath(t *testing.T) {
	t.Parallel()
	hub, err := NewHub(controlplane.NewMemoryRepository(), controlplane.HandlerOptions{
		OperatorSignIn: controlplane.OperatorSignIn{Issuer: "https://identity.example.test", ClientID: "conduit"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []struct{ method, path string }{
		{http.MethodGet, signinhttp.LoginPath}, {http.MethodGet, signinhttp.CallbackPath + "?code=a&state=b"},
		{http.MethodPost, signinhttp.LogoutPath},
	} {
		response := httptest.NewRecorder()
		hub.ServeHTTP(response, httptest.NewRequest(target.method, target.path, nil))
		if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "not configured") ||
			len(response.Result().Cookies()) != 0 {
			t.Errorf("%s %s answered %d %s", target.method, target.path, response.Code, response.Body)
		}
	}
	response := httptest.NewRecorder()
	hub.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/operator-identity", nil))
	if strings.Contains(response.Body.String(), "signIn") || strings.Contains(response.Body.String(), "loginPath") {
		t.Fatalf("a Hub without sign-in publishes one: %s", response.Body)
	}

	harness := newSignInHarness(t, signInSetup{})
	published := harness.do(httptest.NewRequest(http.MethodGet, "/v1/operator-identity", nil))
	if !strings.Contains(published.Body.String(), `"loginPath":"/v1/auth/login"`) ||
		!strings.Contains(published.Body.String(), `"logoutPath":"/v1/auth/logout"`) ||
		strings.Contains(published.Body.String(), signInSecret) {
		t.Fatalf("operator identity document: %s", published.Body)
	}
}
