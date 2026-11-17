package repoclone

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const fixtureToken = "tok-3f9a1c7e5b2d4a6f8091"

// counted serves a fixture and counts requests and pack downloads.
type counted struct {
	*httptest.Server
	hits  atomic.Int32
	packs atomic.Int32
	// seen holds every Authorization header value received.
	mu   sync.Mutex
	seen []string
}

func (server *counted) authorizations() []string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]string(nil), server.seen...)
}

func (server *counted) hostPort() string {
	return strings.TrimPrefix(server.URL, "http://")
}

// serveCounted serves projectRoot. A non-empty token makes every request need
// "Authorization: Bearer <token>".
func serveCounted(t *testing.T, projectRoot, token string) *counted {
	t.Helper()
	backend := serveGit(t, projectRoot)
	server := &counted{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		server.hits.Add(1)
		if value := request.Header.Get("Authorization"); value != "" {
			server.mu.Lock()
			server.seen = append(server.seen, value)
			server.mu.Unlock()
		}
		if token != "" && request.Header.Get("Authorization") != "Bearer "+token {
			writer.Header().Set("WWW-Authenticate", `Bearer realm="fixture"`)
			http.Error(writer, "authentication required", http.StatusUnauthorized)
			return
		}
		if request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/git-upload-pack") {
			// A ref listing is also a POST. Only a request for objects counts.
			body, _ := io.ReadAll(request.Body)
			request.Body = io.NopCloser(bytes.NewReader(body))
			if bytes.Contains(body, []byte("command=fetch")) || bytes.Contains(body, []byte("want ")) {
				server.packs.Add(1)
			}
		}
		backend.Config.Handler.ServeHTTP(writer, request)
	}))
	t.Cleanup(server.Close)
	return server
}

func newCloner(t *testing.T, policy Policy) *Cloner {
	t.Helper()
	cloner, err := New(policy)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cloner.Close)
	return cloner
}

func writeToken(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(fixtureToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDefaultPolicyRefusesLoopbackPrivateLinkLocalAndMetadata(t *testing.T) {
	t.Parallel()
	fixture, _ := makeFixture(t)
	server := serveCounted(t, fixture, "")
	cloner := newCloner(t, Policy{})

	_, cleanup, err := cloner.Clone(context.Background(), server.URL+"/src.git", "main")
	cleanup()
	if !errors.Is(err, ErrAddressRefused) {
		t.Fatalf("loopback clone under the default policy: err = %v, want ErrAddressRefused", err)
	}
	if server.hits.Load() != 0 {
		t.Fatalf("loopback server was contacted %d times", server.hits.Load())
	}
	for _, host := range []string{
		"10.0.0.1", "172.16.5.4", "192.168.1.1", "100.64.0.1", "169.254.169.254", "169.254.1.1",
		"[::1]", "[fe80::1]", "[fd00::1]", "[fd00:ec2::254]", "0.0.0.0", "[::ffff:127.0.0.1]", "100.100.100.200",
	} {
		_, cleanup, err := cloner.Clone(context.Background(), "http://"+host+"/src.git", "main")
		cleanup()
		if !errors.Is(err, ErrAddressRefused) {
			t.Errorf("%s: err = %v, want ErrAddressRefused", host, err)
		}
	}
}

func TestAddressRule(t *testing.T) {
	t.Parallel()
	cloner := newCloner(t, Policy{})
	for _, item := range []struct {
		address string
		exact   bool
		allowed bool
	}{
		{"140.82.112.3", false, true},
		{"2606:50c0:8000::154", false, true},
		{"127.0.0.1", false, false},
		{"127.0.0.1", true, true},
		{"10.1.2.3", false, false},
		{"10.1.2.3", true, true},
		{"fd12:3456::1", false, false},
		{"fd12:3456::1", true, true},
		// An exact entry never unlocks link-local or metadata addresses.
		{"169.254.169.254", true, false},
		{"fe80::1", true, false},
		{"fd00:ec2::254", true, false},
		{"0.0.0.0", true, false},
		{"224.0.0.1", true, false},
	} {
		err := cloner.checkAddress(netip.MustParseAddr(item.address), item.exact)
		if (err == nil) != item.allowed {
			t.Errorf("%s exact=%v: err = %v, want allowed = %v", item.address, item.exact, err, item.allowed)
		}
	}
}

// A name passes the name check, because it is not an address. The address it
// resolves to is checked when the connection is made.
func TestAddressIsCheckedAtConnectTime(t *testing.T) {
	t.Parallel()
	if addresses, err := net.LookupHost("localhost"); err != nil || len(addresses) == 0 {
		t.Skip("localhost does not resolve here")
	}
	fixture, _ := makeFixture(t)
	server := serveCounted(t, fixture, "")
	_, port, _ := net.SplitHostPort(server.hostPort())
	cloner := newCloner(t, Policy{})
	repo, _ := url.Parse("http://localhost:" + port + "/src.git")
	if err := cloner.checkName(repo); err != nil {
		t.Fatalf("name check refused a host name: %v", err)
	}
	_, cleanup, err := cloner.Clone(context.Background(), repo.String(), "main")
	cleanup()
	if !errors.Is(err, ErrAddressRefused) {
		t.Fatalf("err = %v, want ErrAddressRefused", err)
	}
	if server.hits.Load() != 0 {
		t.Fatalf("a name resolving to loopback reached the server %d times", server.hits.Load())
	}
}

func TestAllowlistPermitsAnExactPrivateHostAndRefusesOthers(t *testing.T) {
	t.Parallel()
	fixture, sha := makeFixture(t)
	server := serveCounted(t, fixture, "")
	_, port, _ := net.SplitHostPort(server.hostPort())

	exact := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1"}})
	got, cleanup, err := exact.Clone(context.Background(), server.URL+"/src.git", "main")
	defer cleanup()
	if err != nil || got.SHA != sha {
		t.Fatalf("allowlisted loopback clone: sha = %q err = %v", got.SHA, err)
	}
	_, cleanup, err = exact.Clone(context.Background(), "http://localhost:"+port+"/src.git", "main")
	cleanup()
	if !errors.Is(err, ErrHostRefused) {
		t.Fatalf("unlisted host: err = %v, want ErrHostRefused", err)
	}

	otherPort := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1:1"}})
	_, cleanup, err = otherPort.Clone(context.Background(), server.URL+"/src.git", "main")
	cleanup()
	if !errors.Is(err, ErrHostRefused) {
		t.Fatalf("entry for another port: err = %v, want ErrHostRefused", err)
	}

	// A suffix admits the name. It does not let the name sit on a private address.
	before := server.hits.Load()
	suffix := newCloner(t, Policy{AllowedHosts: []string{"*.0.0.1"}})
	_, cleanup, err = suffix.Clone(context.Background(), server.URL+"/src.git", "main")
	cleanup()
	if !errors.Is(err, ErrAddressRefused) {
		t.Fatalf("suffix entry on loopback: err = %v, want ErrAddressRefused", err)
	}
	if server.hits.Load() != before {
		t.Fatal("suffix entry reached a loopback server")
	}
}

func TestSourceConnectionAuthenticatesALoopbackServer(t *testing.T) {
	t.Parallel()
	fixture, sha := makeFixture(t)
	server := serveCounted(t, fixture, fixtureToken)

	anonymous := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1"}})
	_, cleanup, err := anonymous.Clone(context.Background(), server.URL+"/src.git", "main")
	cleanup()
	if err == nil {
		t.Fatal("clone with no connection read a repository that needs a token")
	}

	var events []Event
	var argvs, envs [][]string
	cloner := newCloner(t, Policy{
		Connections: []Connection{{ID: "fixture-git", Host: server.hostPort(), TokenFile: writeToken(t), AllowHTTP: true}},
		OnCheckout:  func(event Event) { events = append(events, event) },
	})
	cloner.observe = func(argv, env []string) {
		argvs = append(argvs, append([]string(nil), argv...))
		envs = append(envs, append([]string(nil), env...))
	}
	got, cleanup, err := cloner.Clone(context.Background(), server.URL+"/src.git", "main")
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA != sha || got.ConnectionID != "fixture-git" {
		t.Fatalf("checkout = %+v", got)
	}
	if body, err := os.ReadFile(filepath.Join(got.Dir, "README.md")); err != nil || strings.TrimSpace(string(body)) != "fixture readme" {
		t.Fatalf("worktree = %q %v", body, err)
	}
	if len(events) != 1 || events[0].ConnectionID != "fixture-git" || events[0].Commit != sha || events[0].Error != "" {
		t.Fatalf("events = %+v", events)
	}
	if strings.Contains(strings.Join([]string{events[0].Host, events[0].Commit, events[0].ConnectionID, events[0].Error}, " "), fixtureToken) {
		t.Fatal("audit event carries the token")
	}
	if len(argvs) == 0 {
		t.Fatal("no git process was observed")
	}
	wantKey := "http." + server.URL + "/.extraHeader"
	for index, argv := range argvs {
		if strings.Contains(strings.Join(argv, "\x00"), fixtureToken) {
			t.Fatalf("token is on the git command line: %q", argv)
		}
		var key, value string
		for _, item := range envs[index] {
			name, text, _ := strings.Cut(item, "=")
			switch name {
			case "GIT_CONFIG_KEY_0":
				key = text
			case "GIT_CONFIG_VALUE_0":
				value = text
			default:
				if strings.Contains(text, fixtureToken) {
					t.Fatalf("token is in environment variable %s", name)
				}
			}
		}
		if key != wantKey || value != "Authorization: Bearer "+fixtureToken {
			t.Fatalf("credential config = %q, want key %q scoped to the connection's origin", key, wantKey)
		}
	}
}

func TestTokenIsSentOnlyToItsOwnHost(t *testing.T) {
	t.Parallel()
	fixture, _ := makeFixture(t)
	server := serveCounted(t, fixture, fixtureToken)
	other := serveCounted(t, fixture, "")
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, other.URL+request.URL.RequestURI(), http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	tokenFile := writeToken(t)
	cloner := newCloner(t, Policy{
		AllowedHosts: []string{"127.0.0.1"},
		Connections: []Connection{
			{ID: "fixture-git", Host: server.hostPort(), TokenFile: tokenFile, AllowHTTP: true},
			{ID: "redirecting", Host: strings.TrimPrefix(redirect.URL, "http://"), TokenFile: tokenFile, AllowHTTP: true},
		},
	})

	// Another host on the allowlist is fetched anonymously.
	got, cleanup, err := cloner.Clone(context.Background(), other.URL+"/src.git", "main")
	cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if got.ConnectionID != "" {
		t.Fatalf("connection %q was used for a host it does not name", got.ConnectionID)
	}
	if seen := other.authorizations(); len(seen) != 0 {
		t.Fatalf("other host received Authorization %d times", len(seen))
	}

	// A host that holds a connection redirects to another host. The redirect
	// is not followed, so the other host receives neither request nor token.
	before := other.hits.Load()
	_, cleanup, err = cloner.Clone(context.Background(), redirect.URL+"/src.git", "main")
	cleanup()
	if err == nil {
		t.Fatal("clone followed a redirect to another host")
	}
	if other.hits.Load() != before || len(other.authorizations()) != 0 {
		t.Fatalf("redirect target was contacted %d times with %d credentials", other.hits.Load()-before, len(other.authorizations()))
	}
	if strings.Contains(err.Error(), fixtureToken) {
		t.Fatalf("error carries the token: %v", err)
	}
}

func TestTokenIsRedactedFromGitOutput(t *testing.T) {
	t.Parallel()
	// The server echoes the credential back in an error body, which git prints.
	echo := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte("refused " + request.Header.Get("Authorization") + "\n"))
	}))
	t.Cleanup(echo.Close)
	cloner := newCloner(t, Policy{Connections: []Connection{{
		ID: "echo", Host: strings.TrimPrefix(echo.URL, "http://"), TokenFile: writeToken(t), AllowHTTP: true,
	}}})
	_, cleanup, err := cloner.Clone(context.Background(), echo.URL+"/src.git", "main")
	cleanup()
	if err == nil {
		t.Fatal("clone of a refusing server succeeded")
	}
	if strings.Contains(err.Error(), fixtureToken) {
		t.Fatalf("error carries the token: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Fatalf("the echoed credential did not reach the error, so this test proves nothing: %v", err)
	}
}

func TestTokenIsNotSentOverPlainHTTPUnlessAllowed(t *testing.T) {
	t.Parallel()
	fixture, _ := makeFixture(t)
	server := serveCounted(t, fixture, fixtureToken)
	cloner := newCloner(t, Policy{Connections: []Connection{{ID: "fixture-git", Host: server.hostPort(), TokenFile: writeToken(t)}}})
	_, cleanup, err := cloner.Clone(context.Background(), server.URL+"/src.git", "main")
	cleanup()
	if err == nil || !strings.Contains(err.Error(), "https only") {
		t.Fatalf("err = %v, want an https-only refusal", err)
	}
	if server.hits.Load() != 0 {
		t.Fatalf("server was contacted %d times", server.hits.Load())
	}
}

func TestCloneStopsAtTheSizeLimit(t *testing.T) {
	t.Parallel()
	fixture, _ := makeFixture(t)
	noise := make([]byte, 2<<20)
	if _, err := rand.Read(noise); err != nil {
		t.Fatal(err)
	}
	commitToFixture(t, fixture, "big.bin", noise)
	server := serveCounted(t, fixture, "")

	limited := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1"}, MaxBytes: 256 << 10})
	got, cleanup, err := limited.Clone(context.Background(), server.URL+"/src.git", "main")
	if !errors.Is(err, ErrTooLarge) {
		cleanup()
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	cleanup()
	if got.Dir != "" {
		t.Fatalf("an oversized checkout was returned at %s", got.Dir)
	}

	roomy := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1"}, MaxBytes: 64 << 20})
	_, cleanup, err = roomy.Clone(context.Background(), server.URL+"/src.git", "main")
	cleanup()
	if err != nil {
		t.Fatalf("clone under a limit the repository fits: %v", err)
	}
}

func TestCloneStopsAtTheTimeLimit(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// The server gives up on its own, so a clone with no time limit ends
		// with a different error and the test fails without hanging.
		select {
		case <-release:
		case <-request.Context().Done():
		case <-time.After(15 * time.Second):
		}
	}))
	t.Cleanup(stalled.Close)
	t.Cleanup(func() { close(release) })
	cloner := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1"}, Timeout: 400 * time.Millisecond})
	started := time.Now()
	_, cleanup, err := cloner.Clone(context.Background(), stalled.URL+"/src.git", "main")
	cleanup()
	if !errors.Is(err, ErrTimeLimit) {
		t.Fatalf("err = %v, want ErrTimeLimit", err)
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("clone took %s to stop at a 400ms limit", elapsed)
	}
}

func TestCheckoutIsReusedForTheSameCommitAndNotAcrossCommits(t *testing.T) {
	t.Parallel()
	fixture, sha := makeFixture(t)
	server := serveCounted(t, fixture, "")
	cloner := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1"}, CacheEntries: 4})
	repo := server.URL + "/src.git"

	first, cleanFirst, err := cloner.Clone(context.Background(), repo, "main")
	defer cleanFirst()
	if err != nil {
		t.Fatal(err)
	}
	// One fetch can be more than one object request. Reuse means no more of them.
	fetched := server.packs.Load()
	if first.Cached || fetched == 0 {
		t.Fatalf("first clone: cached = %v packs = %d", first.Cached, fetched)
	}
	// A caller that writes into its checkout must not change the next one.
	if err := os.WriteFile(filepath.Join(first.Dir, "README.md"), []byte("scribbled\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	second, cleanSecond, err := cloner.Clone(context.Background(), repo, "main")
	defer cleanSecond()
	if err != nil {
		t.Fatal(err)
	}
	if !second.Cached || second.SHA != sha || server.packs.Load() != fetched {
		t.Fatalf("second clone: cached = %v sha = %q packs = %d, want a reuse with no download", second.Cached, second.SHA, server.packs.Load())
	}
	if second.Dir == first.Dir {
		t.Fatal("two callers share one directory")
	}
	if body, _ := os.ReadFile(filepath.Join(second.Dir, "README.md")); strings.TrimSpace(string(body)) != "fixture readme" {
		t.Fatalf("reused checkout holds another caller's write: %q", body)
	}
	bySHA, cleanSHA, err := cloner.Clone(context.Background(), repo, sha)
	cleanSHA()
	if err != nil || !bySHA.Cached || server.packs.Load() != fetched {
		t.Fatalf("clone by full commit: cached = %v err = %v packs = %d", bySHA.Cached, err, server.packs.Load())
	}
	short, cleanShort, err := cloner.Clone(context.Background(), repo, sha[:12])
	cleanShort()
	if err != nil || !short.Cached || short.SHA != sha {
		t.Fatalf("clone by abbreviated commit: cached = %v sha = %q err = %v", short.Cached, short.SHA, err)
	}
	cleanSecond()
	if _, err := os.Stat(second.Dir); !os.IsNotExist(err) {
		t.Fatalf("cleanup left the checkout: %v", err)
	}

	// The branch moves. The same ref is now another commit and another entry.
	moved := commitToFixture(t, fixture, "README.md", []byte("moved readme\n"))
	third, cleanThird, err := cloner.Clone(context.Background(), repo, "main")
	defer cleanThird()
	if err != nil {
		t.Fatal(err)
	}
	if third.Cached || third.SHA != moved {
		t.Fatalf("clone after the branch moved: cached = %v sha = %q, want a fetch of %q", third.Cached, third.SHA, moved)
	}
	if body, _ := os.ReadFile(filepath.Join(third.Dir, "README.md")); strings.TrimSpace(string(body)) != "moved readme" {
		t.Fatalf("clone after the branch moved read stale files: %q", body)
	}
}

func TestCacheIsBoundedAndKeyedByConnection(t *testing.T) {
	t.Parallel()
	fixtureA, _ := makeFixture(t)
	fixtureB, _ := makeFixture(t)
	serverA := serveCounted(t, fixtureA, "")
	serverB := serveCounted(t, fixtureB, "")
	cloner := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1"}, CacheEntries: 1})
	clone := func(server *counted) Checkout {
		t.Helper()
		got, cleanup, err := cloner.Clone(context.Background(), server.URL+"/src.git", "main")
		cleanup()
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	clone(serverA)
	if !clone(serverA).Cached {
		t.Fatal("second clone of A was not reused")
	}
	clone(serverB)
	if cloner.cache.acquire(cacheKey{}) != nil || len(cloner.cache.entries) != 1 {
		t.Fatalf("cache holds %d entries, want 1", len(cloner.cache.entries))
	}
	if clone(serverA).Cached {
		t.Fatal("A was served from a cache bounded to one entry after B replaced it")
	}

	store := newCache(2)
	dir := t.TempDir()
	store.add(cacheKey{url: "u", sha: "s", connection: "one"}, dir)
	if store.acquire(cacheKey{url: "u", sha: "s", connection: "two"}) != nil {
		t.Fatal("a checkout fetched with one connection was offered to another")
	}
	if store.acquire(cacheKey{url: "u", sha: "s"}) != nil {
		t.Fatal("a checkout fetched with a connection was offered to an anonymous clone")
	}
}

func TestCacheIsSafeForConcurrentClones(t *testing.T) {
	t.Parallel()
	fixture, sha := makeFixture(t)
	server := serveCounted(t, fixture, "")
	cloner := newCloner(t, Policy{AllowedHosts: []string{"127.0.0.1"}, CacheEntries: 2})
	var group sync.WaitGroup
	failures := make(chan string, 8)
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			got, cleanup, err := cloner.Clone(context.Background(), server.URL+"/src.git", "main")
			defer cleanup()
			if err != nil {
				failures <- err.Error()
				return
			}
			body, _ := os.ReadFile(filepath.Join(got.Dir, "README.md"))
			if got.SHA != sha || strings.TrimSpace(string(body)) != "fixture readme" {
				failures <- "wrong checkout " + got.SHA
			}
		}()
	}
	group.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
}

func TestPolicyFromEnv(t *testing.T) {
	t.Parallel()
	tokenFile := writeToken(t)
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	policy, err := PolicyFromEnv(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.AllowedHosts) != 0 || policy.Timeout != DefaultTimeout || policy.MaxBytes != DefaultMaxBytes || policy.CacheEntries != DefaultCacheEntries {
		t.Fatalf("defaults = %+v", policy)
	}
	policy, err = PolicyFromEnv(env(map[string]string{
		EnvAllowedHosts: " github.com , .corp.example ,git.svc.cluster.local:3000",
		EnvConnections:  `[{"id":"corp","host":"git.svc.cluster.local:3000","tokenFile":"` + tokenFile + `","scheme":"basic"}]`,
		EnvTimeout:      "30s", EnvMaxBytes: "1048576", EnvCacheEntries: "0",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.AllowedHosts) != 3 || len(policy.Connections) != 1 || policy.Timeout != 30*time.Second || policy.MaxBytes != 1<<20 || policy.CacheEntries != 0 {
		t.Fatalf("policy = %+v", policy)
	}
	if _, err := New(policy); err != nil {
		t.Fatal(err)
	}
	for name, values := range map[string]map[string]string{
		"inline token":     {EnvConnections: `[{"id":"corp","host":"git.example","token":"` + fixtureToken + `"}]`},
		"missing file":     {EnvConnections: `[{"id":"corp","host":"git.example","tokenFile":"/nonexistent/token"}]`},
		"bad timeout":      {EnvTimeout: "soon"},
		"bad size":         {EnvMaxBytes: "-1"},
		"bad cache":        {EnvCacheEntries: "many"},
		"connections text": {EnvConnections: "git.example=token"},
	} {
		if _, err := PolicyFromEnv(env(values)); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), fixtureToken) {
			t.Errorf("%s: error repeats the inline secret: %v", name, err)
		}
	}
	for name, policy := range map[string]Policy{
		"url as host":     {AllowedHosts: []string{"https://github.com"}},
		"pattern host":    {Connections: []Connection{{ID: "a", Host: ".example.com", TokenFile: tokenFile}}},
		"duplicate id":    {Connections: []Connection{{ID: "a", Host: "a.example", TokenFile: tokenFile}, {ID: "a", Host: "b.example", TokenFile: tokenFile}}},
		"duplicate host":  {Connections: []Connection{{ID: "a", Host: "a.example", TokenFile: tokenFile}, {ID: "b", Host: "a.example", TokenFile: tokenFile}}},
		"unknown scheme":  {Connections: []Connection{{ID: "a", Host: "a.example", TokenFile: tokenFile, Scheme: "digest"}}},
		"empty":           {Connections: []Connection{{ID: "a", Host: "a.example"}}},
		"uppercase id":    {Connections: []Connection{{ID: "Corp", Host: "a.example", TokenFile: tokenFile}}},
		"wildcard inside": {AllowedHosts: []string{"git.*.example"}},
	} {
		if _, err := New(policy); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// commitToFixture commits one file to the fixture's main branch and returns
// the new commit.
func commitToFixture(t *testing.T, fixture, name string, body []byte) string {
	t.Helper()
	src := filepath.Join(fixture, "src")
	if err := os.WriteFile(filepath.Join(src, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run(src, "add", ".")
	run(src, "commit", "--quiet", "-m", "change "+name)
	sha := strings.TrimSpace(run(src, "rev-parse", "HEAD"))
	run(src, "push", "--quiet", filepath.Join(fixture, "src.git"), "HEAD:refs/heads/main")
	run(filepath.Join(fixture, "src.git"), "update-server-info")
	return sha
}
