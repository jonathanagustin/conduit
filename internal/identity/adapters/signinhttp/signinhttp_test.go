package signinhttp_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/sessionmemory"
	"github.com/jonathanagustin/conduit/internal/identity/adapters/signinhttp"
	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// These tests drive the callback with collaborators that misbehave in ways the
// real ones do not: a verifier that returns claims beside its error, a provider
// that returns a token beside its error, a store that accepts any session. Each
// check in the callback must refuse on its own, not because a later one, or the
// store, happens to refuse too.

const (
	stubOrigin = "https://conduit.example.test"
	stubIssuer = "https://identity.example.test"
	stubClient = "conduit"
)

var stubNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

type stubProvider struct {
	mutex     sync.Mutex
	token     string
	err       error
	exchanges int
}

func (provider *stubProvider) AuthorizationEndpoint() string { return stubIssuer + "/authorize" }
func (provider *stubProvider) EndSessionEndpoint() string    { return "" }
func (provider *stubProvider) Exchange(context.Context, string, string, string) (string, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.exchanges++
	return provider.token, provider.err
}

type stubVerifier struct {
	verify func() (domain.VerifiedToken, map[string]json.RawMessage, error)
	calls  int
}

func (verifier *stubVerifier) Verify(context.Context, string) (domain.VerifiedToken, map[string]json.RawMessage, error) {
	verifier.calls++
	return verifier.verify()
}

// permissiveStore keeps sign-in attempts properly and accepts any session at
// all, so that a session the handler should never have built is visible.
type permissiveStore struct {
	ports.SessionStore
	saved []domain.Session
	// takeFails makes TakeLoginAttempt return the attempt beside an error, as
	// a store that could not complete its delete might.
	takeFails bool
}

func (store *permissiveStore) SaveSession(_ context.Context, session domain.Session) error {
	store.saved = append(store.saved, session)
	return nil
}

func (store *permissiveStore) TakeLoginAttempt(ctx context.Context, stateHash string, now time.Time) (domain.LoginAttempt, error) {
	attempt, err := store.SessionStore.TakeLoginAttempt(ctx, stateHash, now)
	if err == nil && store.takeFails {
		return attempt, errors.New("the store could not be consulted")
	}
	return attempt, err
}

type fixture struct {
	handler  http.Handler
	provider *stubProvider
	verifier *stubVerifier
	store    *permissiveStore
	// state and nonce are what the login redirect carried; cookie is the
	// browser binding it set.
	state, nonce string
	cookie       *http.Cookie
}

// goodClaims is an ID token that answers this sign-in in every respect.
func (f *fixture) goodClaims() map[string]json.RawMessage {
	raw := map[string]json.RawMessage{}
	for name, value := range map[string]any{
		"nonce": f.nonce, "aud": stubClient, "preferred_username": "ada",
		"exp": stubNow.Add(time.Hour).Unix(),
	} {
		encoded, _ := json.Marshal(value)
		raw[name] = encoded
	}
	return raw
}

func goodToken() domain.VerifiedToken {
	return domain.VerifiedToken{Issuer: stubIssuer, Subject: "subject-1", IssuedAt: stubNow.Add(-time.Minute)}
}

func begin(t *testing.T) *fixture {
	t.Helper()
	policy, err := domain.NormalizeClaimPolicy(domain.ClaimPolicy{Issuer: stubIssuer, ClientID: stubClient})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{
		provider: &stubProvider{token: "header.payload.signature"},
		verifier: &stubVerifier{},
		store:    &permissiveStore{SessionStore: sessionmemory.New()},
	}
	f.verifier.verify = func() (domain.VerifiedToken, map[string]json.RawMessage, error) {
		return goodToken(), f.goodClaims(), nil
	}
	handler, err := signinhttp.New(signinhttp.Config{
		PublicOrigin: stubOrigin, Policy: policy, Verifier: f.verifier, Provider: f.provider,
		Sessions: f.store, Now: func() time.Time { return stubNow },
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handler.Register(mux)
	f.handler = mux

	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, signinhttp.LoginPath+"?returnTo=/console/%23/plans", nil))
	if recorder.Code != http.StatusFound {
		t.Fatalf("login answered %d", recorder.Code)
	}
	destination, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	f.state, f.nonce = destination.Query().Get("state"), destination.Query().Get("nonce")
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == domain.LoginCookieName {
			f.cookie = cookie
		}
	}
	if f.state == "" || f.nonce == "" || f.cookie == nil {
		t.Fatalf("login did not carry a state, a nonce and a binding cookie: %s", destination)
	}
	return f
}

func (f *fixture) callback(query string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, signinhttp.CallbackPath+"?"+query, nil)
	request.AddCookie(&http.Cookie{Name: f.cookie.Name, Value: f.cookie.Value})
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder
}

func (f *fixture) goodQuery() string {
	return url.Values{"state": {f.state}, "code": {"the-code"}}.Encode()
}

func sessionCookieSet(recorder *httptest.ResponseRecorder) bool {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == domain.SessionCookieName && cookie.Value != "" {
			return true
		}
	}
	return false
}

func (f *fixture) mustRefuse(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != signinhttp.FailedReturnPath {
		t.Errorf("answered %d to %q, want a redirect to the one failure address", recorder.Code, recorder.Header().Get("Location"))
	}
	if len(f.store.saved) != 0 {
		t.Errorf("a session was handed to the store: %#v", f.store.saved)
	}
	if sessionCookieSet(recorder) {
		t.Error("a session cookie was set")
	}
}

// The baseline: with collaborators that behave, the same fixture signs in. The
// refusals below mean nothing without it.
func TestTheStubbedCallbackSignsInWhenNothingIsWrong(t *testing.T) {
	t.Parallel()
	f := begin(t)
	recorder := f.callback(f.goodQuery())
	if recorder.Code != http.StatusSeeOther || recorder.Header().Get("Location") != "/console/#/plans" {
		t.Fatalf("answered %d to %q", recorder.Code, recorder.Header().Get("Location"))
	}
	if len(f.store.saved) != 1 || f.store.saved[0].Claims.Username != "ada" || !sessionCookieSet(recorder) {
		t.Fatalf("no session for ada: %#v", f.store.saved)
	}
	if want := stubNow.Add(time.Hour); !f.store.saved[0].ExpiresAt.Equal(want) {
		t.Fatalf("the session ends at %s, want the token's expiry %s", f.store.saved[0].ExpiresAt, want)
	}
	if strings.Contains(f.store.saved[0].IDHash, "-") || len(f.store.saved[0].IDHash) != 64 {
		t.Fatalf("the store was handed %q, not a SHA-256", f.store.saved[0].IDHash)
	}
}

func TestATokenTheVerifierRefusedOpensNoSessionWhateverElseTheVerifierReturned(t *testing.T) {
	t.Parallel()
	f := begin(t)
	f.verifier.verify = func() (domain.VerifiedToken, map[string]json.RawMessage, error) {
		// Everything a later check reads is in order. Only the verdict is not.
		return goodToken(), f.goodClaims(), errors.New("signature, issuer or expiry refused")
	}
	f.mustRefuse(t, f.callback(f.goodQuery()))
}

func TestAFailedCodeExchangeOpensNoSessionEvenIfATokenCameBack(t *testing.T) {
	t.Parallel()
	f := begin(t)
	f.provider.err = errors.New("the token endpoint answered 400")
	f.mustRefuse(t, f.callback(f.goodQuery()))
	if f.verifier.calls != 0 {
		t.Error("a token from a failed exchange reached the verifier")
	}
}

func TestATokenTheClaimPolicyRefusesOpensNoSession(t *testing.T) {
	t.Parallel()
	for name, token := range map[string]domain.VerifiedToken{
		"another issuer":  {Issuer: "https://elsewhere.example.test", Subject: "subject-1", IssuedAt: stubNow.Add(-time.Minute)},
		"no issue time":   {Issuer: stubIssuer, Subject: "subject-1"},
		"no subject":      {Issuer: stubIssuer, IssuedAt: stubNow.Add(-time.Minute)},
		"issued tomorrow": {Issuer: stubIssuer, Subject: "subject-1", IssuedAt: stubNow.Add(24 * time.Hour)},
	} {
		f := begin(t)
		f.verifier.verify = func() (domain.VerifiedToken, map[string]json.RawMessage, error) {
			return token, f.goodClaims(), nil
		}
		recorder := f.callback(f.goodQuery())
		if recorder.Header().Get("Location") != signinhttp.FailedReturnPath || len(f.store.saved) != 0 || sessionCookieSet(recorder) {
			t.Errorf("%s: answered %q and saved %#v", name, recorder.Header().Get("Location"), f.store.saved)
		}
	}
}

func TestATokenWithNoExpiryOrOneAlreadyPastOpensNoSession(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(map[string]json.RawMessage){
		"no exp":         func(raw map[string]json.RawMessage) { delete(raw, "exp") },
		"exp not a time": func(raw map[string]json.RawMessage) { raw["exp"] = json.RawMessage(`"tomorrow"`) },
		"exp in the past": func(raw map[string]json.RawMessage) {
			raw["exp"], _ = json.Marshal(stubNow.Add(-time.Second).Unix())
		},
		"exp now": func(raw map[string]json.RawMessage) { raw["exp"], _ = json.Marshal(stubNow.Unix()) },
	} {
		f := begin(t)
		f.verifier.verify = func() (domain.VerifiedToken, map[string]json.RawMessage, error) {
			raw := f.goodClaims()
			change(raw)
			return goodToken(), raw, nil
		}
		recorder := f.callback(f.goodQuery())
		if recorder.Header().Get("Location") != signinhttp.FailedReturnPath || len(f.store.saved) != 0 || sessionCookieSet(recorder) {
			t.Errorf("%s: answered %q and saved %#v", name, recorder.Header().Get("Location"), f.store.saved)
		}
	}
}

func TestAProviderErrorIsNotRedeemedEvenWhenACodeRidesWithIt(t *testing.T) {
	t.Parallel()
	f := begin(t)
	f.mustRefuse(t, f.callback(url.Values{"state": {f.state}, "code": {"the-code"}, "error": {"access_denied"}}.Encode()))
	if f.provider.exchanges != 0 {
		t.Error("a code was exchanged on a callback that reported an error")
	}
}

func TestACallbackWithTwoStatesOrTwoCodesIsRefusedWithoutAnExchange(t *testing.T) {
	t.Parallel()
	for name, query := range map[string]func(*fixture) url.Values{
		"the real state first":  func(f *fixture) url.Values { return url.Values{"state": {f.state, "another"}, "code": {"the-code"}} },
		"the real state second": func(f *fixture) url.Values { return url.Values{"state": {"another", f.state}, "code": {"the-code"}} },
		"two codes":             func(f *fixture) url.Values { return url.Values{"state": {f.state}, "code": {"the-code", "another"}} },
		"an empty code":         func(f *fixture) url.Values { return url.Values{"state": {f.state}, "code": {""}} },
		"no code":               func(f *fixture) url.Values { return url.Values{"state": {f.state}} },
		"an oversized code": func(f *fixture) url.Values {
			return url.Values{"state": {f.state}, "code": {strings.Repeat("c", 4097)}}
		},
		"an oversized state": func(f *fixture) url.Values {
			return url.Values{"state": {strings.Repeat("s", 129)}, "code": {"the-code"}}
		},
	} {
		f := begin(t)
		recorder := f.callback(query(f).Encode())
		if recorder.Header().Get("Location") != signinhttp.FailedReturnPath || len(f.store.saved) != 0 ||
			sessionCookieSet(recorder) || f.provider.exchanges != 0 {
			t.Errorf("%s: answered %q, saved %d and exchanged %d", name,
				recorder.Header().Get("Location"), len(f.store.saved), f.provider.exchanges)
		}
	}
}

func TestAStoreThatCouldNotRedeemTheStateEndsTheSignIn(t *testing.T) {
	t.Parallel()
	f := begin(t)
	f.store.takeFails = true
	f.mustRefuse(t, f.callback(f.goodQuery()))
	if f.provider.exchanges != 0 {
		t.Error("a code was exchanged for a state the store did not vouch for")
	}
}

func TestACallbackWithoutTheBrowserBindingIsRefusedWithoutAnExchange(t *testing.T) {
	t.Parallel()
	f := begin(t)
	other, err := domain.NewOpaqueSecret()
	if err != nil {
		t.Fatal(err)
	}
	f.cookie = &http.Cookie{Name: domain.LoginCookieName, Value: other}
	f.mustRefuse(t, f.callback(f.goodQuery()))
	if f.provider.exchanges != 0 {
		t.Error("a code was exchanged for a browser that did not start the sign-in")
	}
}
