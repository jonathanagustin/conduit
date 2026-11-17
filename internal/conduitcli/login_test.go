package conduitcli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/oidcfake"
)

// These run `conduit login` against a real provider: it serves discovery over
// TLS, runs the device grant through its documented answers, and for the code
// grant checks the PKCE verifier and the redirect address itself.

type lockedBuffer struct {
	mutex  sync.Mutex
	buffer bytes.Buffer
}

func (locked *lockedBuffer) Write(contents []byte) (int, error) {
	locked.mutex.Lock()
	defer locked.mutex.Unlock()
	return locked.buffer.Write(contents)
}

func (locked *lockedBuffer) String() string {
	locked.mutex.Lock()
	defer locked.mutex.Unlock()
	return locked.buffer.String()
}

type loginFixture struct {
	t         *testing.T
	provider  *oidcfake.Provider
	directory string
	tokenFile string
	stdout    *lockedBuffer
	stderr    *lockedBuffer
	sleeps    []time.Duration
	sleepErr  func(calls int) error
	mutex     sync.Mutex
}

func newLoginFixture(t *testing.T, options oidcfake.Options) *loginFixture {
	t.Helper()
	options.ClientID = "conduit-cli"
	options.RedirectURIs = []string{"http://127.0.0.1:*"}
	provider, err := oidcfake.Start(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(provider.Close)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "ca.pem"), provider.CertificatePEM(), 0o600); err != nil {
		t.Fatal(err)
	}
	return &loginFixture{
		t: t, provider: provider, directory: directory, tokenFile: filepath.Join(directory, "credentials", "token"),
		stdout: &lockedBuffer{}, stderr: &lockedBuffer{},
	}
}

func (fixture *loginFixture) run(ctx context.Context, arguments ...string) error {
	root := NewRoot(Runtime{
		Stdout: fixture.stdout, Stderr: fixture.stderr,
		Getenv: func(name string) string {
			if name == "CONDUIT_CONFIG_FILE" {
				return filepath.Join(fixture.directory, "config.yaml")
			}
			return ""
		},
		Sleep: func(ctx context.Context, wait time.Duration) error {
			fixture.mutex.Lock()
			fixture.sleeps = append(fixture.sleeps, wait)
			calls := len(fixture.sleeps)
			fixture.mutex.Unlock()
			if fixture.sleepErr != nil {
				return fixture.sleepErr(calls)
			}
			return ctx.Err()
		},
	})
	root.SetArgs(append(arguments,
		"--oidc-issuer", fixture.provider.Issuer(), "--oidc-client-id", "conduit-cli",
		"--oidc-ca-file", filepath.Join(fixture.directory, "ca.pem"), "--bearer-token-file", fixture.tokenFile))
	return root.ExecuteContext(ctx)
}

func (fixture *loginFixture) storedToken() string {
	fixture.t.Helper()
	contents, err := os.ReadFile(fixture.tokenFile)
	if err != nil {
		fixture.t.Fatalf("no token was stored: %v", err)
	}
	return strings.TrimSpace(string(contents))
}

func (fixture *loginFixture) requireNoToken() {
	fixture.t.Helper()
	if _, err := os.Lstat(fixture.tokenFile); !errors.Is(err, os.ErrNotExist) {
		fixture.t.Fatalf("a token file exists after a sign-in that did not complete: %v", err)
	}
	// And no half-written transaction file beside it.
	entries, _ := os.ReadDir(filepath.Dir(fixture.tokenFile))
	for _, entry := range entries {
		fixture.t.Fatalf("left behind %s", entry.Name())
	}
}

func idTokenIssuer(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("the stored token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims struct {
		Issuer string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatal(err)
	}
	return claims.Issuer
}

func TestDeviceLoginWaitsThroughPendingAndSlowDownThenStoresTheTokenPrivately(t *testing.T) {
	fixture := newLoginFixture(t, oidcfake.Options{})
	fixture.provider.ScriptDevicePolls("authorization_pending", "slow_down", "authorization_pending")
	if err := fixture.run(context.Background(), "login"); err != nil {
		t.Fatalf("login: %v\n%s", err, fixture.stderr.String())
	}
	token := fixture.storedToken()
	if idTokenIssuer(t, token) != fixture.provider.Issuer() {
		t.Fatal("the stored token is not the issuer's ID token")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(fixture.tokenFile)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("token file mode = %v %v, want 0600", info.Mode().Perm(), err)
		}
		directory, err := os.Stat(filepath.Dir(fixture.tokenFile))
		if err != nil || directory.Mode().Perm() != 0o700 {
			t.Fatalf("token directory mode = %v %v, want 0700", directory.Mode().Perm(), err)
		}
	}
	// The file is one the CLI's own bearer reader accepts.
	if read, err := readBearerTokenFile(fixture.tokenFile); err != nil || read != token {
		t.Fatalf("the stored token is not readable as a bearer token file: %v", err)
	}
	// The interval the issuer gave is one second; slow_down adds five, and the
	// longer interval is kept for every poll after it.
	want := []time.Duration{time.Second, time.Second, 6 * time.Second, 6 * time.Second}
	if len(fixture.sleeps) != len(want) {
		t.Fatalf("waited %v, want %v", fixture.sleeps, want)
	}
	for index := range want {
		if fixture.sleeps[index] != want[index] {
			t.Fatalf("waited %v, want %v", fixture.sleeps, want)
		}
	}
	prompt := fixture.stderr.String()
	if !strings.Contains(prompt, "WDJB-MJHT") || !strings.Contains(prompt, fixture.provider.Issuer()+"/activate") {
		t.Fatalf("the person was not told where to go and what to enter: %q", prompt)
	}
	requests := fixture.provider.TokenRequests()
	deviceCode := requests[0].Form.Get("device_code")
	for name, output := range map[string]string{"stdout": fixture.stdout.String(), "stderr": prompt} {
		if strings.Contains(output, token) || strings.Contains(output, deviceCode) || deviceCode == "" {
			t.Errorf("%s carries the token or the device code", name)
		}
	}
	var result loginResult
	if err := json.Unmarshal([]byte(fixture.stdout.String()), &result); err != nil ||
		result.TokenFile != fixture.tokenFile || result.Flow != "device" || result.TokenKind != "id" {
		t.Fatalf("result = %q %v", fixture.stdout.String(), err)
	}
	for _, request := range requests {
		if request.Form.Has("client_secret") || request.BasicPresent {
			t.Fatal("a public client sent a secret")
		}
	}
}

func TestDeviceLoginStoresNothingWhenTheSignInIsRefusedOrRunsOut(t *testing.T) {
	for name, test := range map[string]struct {
		script []string
		want   string
	}{
		"denied":           {[]string{"authorization_pending", "access_denied"}, "the sign-in was denied"},
		"expired":          {[]string{"expired_token"}, "the device code expired"},
		"an unknown error": {[]string{"invalid_scope"}, "invalid_scope"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newLoginFixture(t, oidcfake.Options{})
			fixture.provider.ScriptDevicePolls(test.script...)
			err := fixture.run(context.Background(), "login")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("login answered %v, want an error containing %q", err, test.want)
			}
			fixture.requireNoToken()
			if polls := len(fixture.provider.TokenRequests()); polls != len(test.script) {
				t.Fatalf("polled %d times after a final answer, want %d", polls, len(test.script))
			}
		})
	}
	t.Run("never approved", func(t *testing.T) {
		fixture := newLoginFixture(t, oidcfake.Options{})
		fixture.provider.ScriptDevicePolls(
			"authorization_pending", "authorization_pending", "authorization_pending", "authorization_pending")
		// The login timeout arrives during the third wait.
		fixture.sleepErr = func(calls int) error {
			if calls >= 3 {
				return context.DeadlineExceeded
			}
			return nil
		}
		err := fixture.run(context.Background(), "login")
		if err == nil || !strings.Contains(err.Error(), "not approved before the login timeout") {
			t.Fatalf("login answered %v", err)
		}
		fixture.requireNoToken()
	})
}

// A token or a device code is only ever sent over TLS, whatever the issuer's
// discovery document says. The document is the issuer's word; it is checked.
func TestLoginRefusesAnIssuerWhoseEndpointsAreNotHTTPS(t *testing.T) {
	// plain is a server that really does answer over http, so that a client
	// which went there is seen arriving rather than failing a TLS handshake.
	for name, edit := range map[string]func(plain string, document map[string]any){
		"a plain-http token endpoint": func(plain string, document map[string]any) {
			document["token_endpoint"] = plain + "/token"
		},
		"a plain-http device endpoint": func(plain string, document map[string]any) {
			document["device_authorization_endpoint"] = plain + "/device"
		},
		"another issuer's document": func(_ string, document map[string]any) {
			document["issuer"] = "https://elsewhere.example.test"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var posts atomic.Int32
			plain := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				posts.Add(1)
				writer.WriteHeader(http.StatusBadRequest)
			}))
			t.Cleanup(plain.Close)
			mux := http.NewServeMux()
			server := httptest.NewTLSServer(mux)
			t.Cleanup(server.Close)
			mux.HandleFunc("GET /.well-known/openid-configuration", func(writer http.ResponseWriter, _ *http.Request) {
				document := map[string]any{
					"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize",
					"token_endpoint": server.URL + "/token", "device_authorization_endpoint": server.URL + "/device",
				}
				edit(plain.URL, document)
				_ = json.NewEncoder(writer).Encode(document)
			})
			mux.HandleFunc("POST /", func(http.ResponseWriter, *http.Request) { posts.Add(1) })
			directory := t.TempDir()
			caFile := filepath.Join(directory, "ca.pem")
			certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
			if err := os.WriteFile(caFile, certificate, 0o600); err != nil {
				t.Fatal(err)
			}
			tokenFile := filepath.Join(directory, "credentials", "token")
			root := NewRoot(Runtime{
				Stdout: &lockedBuffer{}, Stderr: &lockedBuffer{},
				Getenv: func(name string) string {
					if name == "CONDUIT_CONFIG_FILE" {
						return filepath.Join(directory, "config.yaml")
					}
					return ""
				},
				Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
			})
			root.SetArgs([]string{"login", "--oidc-issuer", server.URL, "--oidc-client-id", "conduit-cli",
				"--oidc-ca-file", caFile, "--bearer-token-file", tokenFile, "--login-timeout", "2s"})
			if err := root.ExecuteContext(context.Background()); err == nil {
				t.Fatal("login accepted the issuer")
			}
			if posts.Load() != 0 {
				t.Fatal("login sent a request to an endpoint it should have refused")
			}
			if _, err := os.Lstat(tokenFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a token file exists: %v", err)
			}
		})
	}
}

func TestLoginRefusesWhatItCannotDoSafelyBeforeAnybodySignsIn(t *testing.T) {
	t.Run("a token file that is a link", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symbolic links need privilege on Windows")
		}
		fixture := newLoginFixture(t, oidcfake.Options{})
		target := filepath.Join(fixture.directory, "elsewhere")
		if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(fixture.tokenFile), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, fixture.tokenFile); err != nil {
			t.Fatal(err)
		}
		err := fixture.run(context.Background(), "login")
		if err == nil || !strings.Contains(err.Error(), "regular file") {
			t.Fatalf("login answered %v", err)
		}
		if contents, _ := os.ReadFile(target); string(contents) != "untouched" {
			t.Fatal("login wrote through a symbolic link")
		}
		if len(fixture.provider.TokenRequests()) != 0 || strings.Contains(fixture.stderr.String(), "To sign in") {
			t.Fatal("a sign-in was started for a destination that cannot be written")
		}
		// logout does not remove through a link either.
		if err := fixture.run(context.Background(), "logout"); err == nil {
			t.Fatal("logout removed a symbolic link")
		}
	})
	t.Run("a client secret", func(t *testing.T) {
		fixture := newLoginFixture(t, oidcfake.Options{})
		secret := filepath.Join(fixture.directory, "secret")
		if err := os.WriteFile(secret, []byte("s3cret"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := fixture.run(context.Background(), "login", "--oidc-client-secret-file", secret)
		if err == nil || !strings.Contains(err.Error(), "public client") {
			t.Fatalf("login answered %v", err)
		}
	})
	t.Run("the device flow where the issuer has none", func(t *testing.T) {
		fixture := newLoginFixture(t, oidcfake.Options{WithoutDeviceGrant: true})
		err := fixture.run(context.Background(), "login", "--flow", "device")
		if err == nil || !strings.Contains(err.Error(), "does not advertise a device authorization endpoint") {
			t.Fatalf("login answered %v", err)
		}
	})
	for name, arguments := range map[string][]string{
		"an unknown flow":  {"login", "--flow", "implicit"},
		"an unknown token": {"login", "--token", "refresh"},
		"no timeout":       {"login", "--login-timeout", "0"},
		"a day's timeout":  {"login", "--login-timeout", "24h"},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newLoginFixture(t, oidcfake.Options{})
			if err := fixture.run(context.Background(), arguments...); err == nil {
				t.Fatal("accepted")
			}
			fixture.requireNoToken()
		})
	}
}

func TestLoginCanStoreTheAccessTokenAndReplacesAnOlderFilePrivately(t *testing.T) {
	fixture := newLoginFixture(t, oidcfake.Options{})
	if err := os.MkdirAll(filepath.Dir(fixture.tokenFile), 0o700); err != nil {
		t.Fatal(err)
	}
	// An older token left readable by others is replaced, not appended to, and
	// the replacement is private.
	if err := os.WriteFile(fixture.tokenFile, []byte("an-older-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fixture.run(context.Background(), "login", "--token", "access"); err != nil {
		t.Fatal(err)
	}
	token := fixture.storedToken()
	if !strings.HasPrefix(token, "opaque-access-") || strings.Contains(token, "older") {
		t.Fatalf("stored something other than the access token")
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(fixture.tokenFile); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("token file mode = %v %v, want 0600", info.Mode().Perm(), err)
		}
	}
}

func TestLogoutRemovesTheTokenAndIsNotAnErrorTwice(t *testing.T) {
	fixture := newLoginFixture(t, oidcfake.Options{})
	if err := fixture.run(context.Background(), "login"); err != nil {
		t.Fatal(err)
	}
	fixture.storedToken()
	if err := fixture.run(context.Background(), "logout"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(fixture.tokenFile); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the token survived logout: %v", err)
	}
	if !strings.Contains(fixture.stdout.String(), `"removed": true`) {
		t.Fatalf("logout did not report a removal: %s", fixture.stdout.String())
	}
	if err := fixture.run(context.Background(), "logout"); err != nil {
		t.Fatalf("a second logout: %v", err)
	}
	if !strings.Contains(fixture.stdout.String(), `"removed": false`) {
		t.Fatalf("a second logout claimed a removal: %s", fixture.stdout.String())
	}
}

var authorizationAddress = regexp.MustCompile(`(?m)^\s+(https://\S+)$`)

// browser plays the person: it opens the address the command printed, lets the
// provider sign it in, and returns the loopback address it is redirected to.
func (fixture *loginFixture) browser() (authorize, callback *url.URL) {
	fixture.t.Helper()
	var printed string
	deadline := time.Now().Add(10 * time.Second)
	for printed == "" {
		if match := authorizationAddress.FindStringSubmatch(fixture.stderr.String()); match != nil {
			printed = match[1]
			break
		}
		if time.Now().After(deadline) {
			fixture.t.Fatalf("no authorization address was printed: %q", fixture.stderr.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
	authorize, err := url.Parse(printed)
	if err != nil {
		fixture.t.Fatal(err)
	}
	client := fixture.provider.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Get(printed)
	if err != nil {
		fixture.t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusFound {
		fixture.t.Fatalf("the provider answered %d to the authorization request", response.StatusCode)
	}
	callback, err = url.Parse(response.Header.Get("Location"))
	if err != nil {
		fixture.t.Fatal(err)
	}
	return authorize, callback
}

func TestLoopbackLoginUsesPKCEStateAndA127RedirectWhenTheIssuerHasNoDeviceGrant(t *testing.T) {
	fixture := newLoginFixture(t, oidcfake.Options{WithoutDeviceGrant: true})
	done := make(chan error, 1)
	go func() { done <- fixture.run(context.Background(), "login") }()
	authorize, callback := fixture.browser()

	query := authorize.Query()
	redirect, err := url.Parse(query.Get("redirect_uri"))
	if err != nil || redirect.Scheme != "http" || redirect.Hostname() != "127.0.0.1" || redirect.Port() == "" ||
		redirect.Path != "/callback" {
		t.Fatalf("redirect_uri = %q", query.Get("redirect_uri"))
	}
	if query.Get("code_challenge_method") != "S256" || len(query.Get("code_challenge")) != 43 ||
		len(query.Get("state")) != 43 || len(query.Get("nonce")) != 43 || query.Get("response_type") != "code" ||
		query.Has("code_verifier") || query.Has("client_secret") {
		t.Fatalf("authorization request: %v", query)
	}

	// Another local process guesses the port. Without the state it is refused,
	// and the command keeps waiting for the real answer.
	forged := *callback
	forgedQuery := forged.Query()
	forgedQuery.Set("state", "guessed")
	forgedQuery.Set("code", "attacker-code")
	forged.RawQuery = forgedQuery.Encode()
	response, err := http.Get(forged.String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("a callback with the wrong state answered %d", response.StatusCode)
	}
	select {
	case err := <-done:
		t.Fatalf("a forged callback ended the sign-in: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	response, err = http.Get(callback.String())
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("the real callback answered %d", response.StatusCode)
	}
	if err := <-done; err != nil {
		t.Fatalf("login: %v", err)
	}
	token := fixture.storedToken()
	if idTokenIssuer(t, token) != fixture.provider.Issuer() {
		t.Fatal("the stored token is not the issuer's ID token")
	}
	requests := fixture.provider.TokenRequests()
	if len(requests) != 1 || !requests[0].Granted {
		t.Fatalf("token requests: %#v", requests)
	}
	sum := sha256.Sum256([]byte(requests[0].Form.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != query.Get("code_challenge") ||
		requests[0].Form.Get("redirect_uri") != query.Get("redirect_uri") {
		t.Fatal("the verifier or redirect address sent to the token endpoint is not the one sent through the browser")
	}
	for name, output := range map[string]string{"stdout": fixture.stdout.String(), "stderr": fixture.stderr.String()} {
		if strings.Contains(output, token) || strings.Contains(output, requests[0].Form.Get("code_verifier")) ||
			strings.Contains(output, callback.Query().Get("code")) {
			t.Errorf("%s carries the token, the verifier or the code", name)
		}
	}
	if !strings.Contains(fixture.stdout.String(), `"flow": "loopback"`) {
		t.Fatalf("result = %s", fixture.stdout.String())
	}
}

func TestLoopbackLoginStoresNothingWhenTheIDTokenAnswersAnotherRequestOrTheIssuerRefuses(t *testing.T) {
	t.Run("a nonce from another request", func(t *testing.T) {
		fixture := newLoginFixture(t, oidcfake.Options{WithoutDeviceGrant: true})
		fixture.provider.MutateClaims(func(claims map[string]any) { claims["nonce"] = "another-request" })
		done := make(chan error, 1)
		go func() { done <- fixture.run(context.Background(), "login") }()
		_, callback := fixture.browser()
		response, err := http.Get(callback.String())
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if err := <-done; err == nil || !strings.Contains(err.Error(), "does not answer this sign-in") {
			t.Fatalf("login answered %v", err)
		}
		fixture.requireNoToken()
	})
	t.Run("the provider reports an error", func(t *testing.T) {
		fixture := newLoginFixture(t, oidcfake.Options{WithoutDeviceGrant: true})
		done := make(chan error, 1)
		go func() { done <- fixture.run(context.Background(), "login") }()
		authorize, callback := fixture.browser()
		refused := *callback
		refused.RawQuery = url.Values{
			"state": {authorize.Query().Get("state")}, "error": {"access_denied"},
		}.Encode()
		response, err := http.Get(refused.String())
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if err := <-done; err == nil || !strings.Contains(err.Error(), "access_denied") {
			t.Fatalf("login answered %v", err)
		}
		fixture.requireNoToken()
		if len(fixture.provider.TokenRequests()) != 0 {
			t.Fatal("a refused sign-in reached the token endpoint")
		}
	})
	t.Run("nobody signs in", func(t *testing.T) {
		fixture := newLoginFixture(t, oidcfake.Options{WithoutDeviceGrant: true})
		err := fixture.run(context.Background(), "login", "--login-timeout", "150ms")
		if err == nil || !strings.Contains(err.Error(), "did not finish before the login timeout") {
			t.Fatalf("login answered %v", err)
		}
		fixture.requireNoToken()
	})
}

func TestIDTokenCarriesNonce(t *testing.T) {
	encode := func(payload string) string {
		return "e30." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".sig"
	}
	if !idTokenCarriesNonce(encode(`{"nonce":"n-1"}`), "n-1") {
		t.Fatal("a matching nonce was refused")
	}
	for name, token := range map[string]string{
		"another nonce": encode(`{"nonce":"n-2"}`), "no nonce": encode(`{}`), "an empty nonce": encode(`{"nonce":""}`),
		"not a JWT": "opaque", "not base64": "a.!!!.c", "not JSON": encode(`nonce`),
	} {
		if idTokenCarriesNonce(token, "n-1") {
			t.Errorf("%s was accepted", name)
		}
	}
	if idTokenCarriesNonce(encode(`{"nonce":""}`), "") {
		t.Fatal("an empty nonce matched an empty expectation")
	}
}
