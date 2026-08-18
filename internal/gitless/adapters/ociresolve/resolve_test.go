package ociresolve_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ociresolve"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/registrychallenge"
	"github.com/jonathanagustin/conduit/internal/gitless/domain"
)

var digest = "sha256:aa11" + strings.Repeat("0", 60)

// registry stands in for a real one, rewriting the https the resolver builds
// onto the test server.
func registry(t *testing.T, handler http.HandlerFunc) *ociresolve.Resolver {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: rewrite{host: target.Host}}
	return ociresolve.New(client, ociresolve.Anonymous{})
}

type rewrite struct{ host string }

func (transport rewrite) RoundTrip(request *http.Request) (*http.Response, error) {
	request.URL.Scheme, request.URL.Host = "http", transport.host
	return http.DefaultTransport.RoundTrip(request)
}

func channel(name string) domain.Selector {
	return domain.Selector{Kind: domain.ByChannel, Repository: "registry.example/example/runtime", Channel: name}
}

func TestAChannelResolvesToTheDigestTheRegistryReports(t *testing.T) {
	resolver := registry(t, func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v2/example/runtime/manifests/latest-stable" {
			t.Errorf("unexpected path %q", request.URL.Path)
		}
		// Sending every manifest media type matters: a registry given no Accept
		// header can answer with a different manifest, and a different digest,
		// than a normal pull would get.
		if !strings.Contains(request.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") {
			t.Errorf("the resolver did not offer the OCI index media type: %q", request.Header.Get("Accept"))
		}
		writer.Header().Set("Docker-Content-Digest", digest)
		writer.WriteHeader(http.StatusOK)
	})
	pin, err := resolver.Resolve(t.Context(), channel("latest-stable"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pin.Digest != digest || pin.Repository != "registry.example/example/runtime" {
		t.Fatalf("resolved to %s", pin)
	}
}

// A digest selector already names exact bytes. Asking a registry to confirm it
// invites the registry to answer something else.
func TestADigestSelectorNeverReachesTheRegistry(t *testing.T) {
	resolver := registry(t, func(http.ResponseWriter, *http.Request) {
		t.Error("the resolver asked a registry about an exact coordinate")
	})
	pin, err := resolver.Resolve(t.Context(), domain.Selector{
		Kind: domain.ByDigest, Repository: "registry.example/example/runtime", Digest: digest,
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pin.Digest != digest {
		t.Fatalf("resolved to %s", pin)
	}
}

func TestABearerChallengeIsFollowed(t *testing.T) {
	issued := false
	resolver := registry(t, func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/token":
			if request.URL.Query().Get("scope") != "repository:example/runtime:pull" {
				t.Errorf("token scope: %q", request.URL.Query().Get("scope"))
			}
			issued = true
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"token":"a-pull-token"}`))
		case request.Header.Get("Authorization") == "":
			// The realm names the registry itself over https, which is what
			// ghcr.io, Quay, ACR, GCR and zot all issue. The rewrite transport
			// puts the request on the test server regardless; what is being
			// exercised is that the resolver accepts a realm it should.
			writer.Header().Set("WWW-Authenticate",
				`Bearer realm="https://registry.example/token",service="registry.example"`)
			writer.WriteHeader(http.StatusUnauthorized)
		case request.Header.Get("Authorization") == "Bearer a-pull-token":
			writer.Header().Set("Docker-Content-Digest", digest)
			writer.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected Authorization %q", request.Header.Get("Authorization"))
			writer.WriteHeader(http.StatusForbidden)
		}
	})
	pin, err := resolver.Resolve(t.Context(), channel("latest-stable"))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if !issued || pin.Digest != digest {
		t.Fatalf("issued=%v pin=%s", issued, pin)
	}
}

// A registry answering something that is not an exact coordinate must not
// become a pin. Everything downstream treats a pin as immutable bytes.
func TestANonDigestAnswerIsRefused(t *testing.T) {
	for name, answer := range map[string]string{
		"empty":     "",
		"a tag":     "latest-stable",
		"uppercase": "sha256:AA11" + strings.Repeat("0", 60),
		"truncated": "sha256:aa11",
	} {
		resolver := registry(t, func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Docker-Content-Digest", answer)
			writer.WriteHeader(http.StatusOK)
		})
		if _, err := resolver.Resolve(t.Context(), channel("latest-stable")); err == nil {
			t.Errorf("%s answer became a pin", name)
		}
	}
}

func TestARegistryErrorIsReported(t *testing.T) {
	resolver := registry(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	})
	if _, err := resolver.Resolve(t.Context(), channel("nonexistent")); err == nil {
		t.Fatal("a missing channel resolved")
	}
}

func TestAnUnauthenticatedRefusalIsNotRetriedForever(t *testing.T) {
	attempts := 0
	resolver := registry(t, func(writer http.ResponseWriter, _ *http.Request) {
		attempts++
		writer.Header().Set("WWW-Authenticate", `Bearer realm="https://registry.example/token"`)
		writer.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := resolver.Resolve(t.Context(), channel("latest")); err == nil {
		t.Fatal("an unauthorised resolve succeeded")
	}
	if attempts > 2 {
		t.Errorf("the resolver retried %d times", attempts)
	}
}

// --- where a credential may be sent -----------------------------------------
//
// A registry's `WWW-Authenticate` arrives on an UNAUTHENTICATED 401. Before
// this, the realm inside it was taken at its word: the resolver built the token
// URL from it and put the mounted Docker-config credential on that request with
// SetBasicAuth, with no check that the realm was the registry being contacted
// and no requirement that it be https. #220 is what made that reachable -- it
// replaced the hardcoded Anonymous credential source with a real one -- so a
// registry, or anything able to answer for one, could name a host of its
// choosing and be handed a production robot's password.

// stolenUsername and stolenPassword are what a proof of concept saw arrive at an
// unrelated plaintext host as `Basic cHJvZC1yb2JvdDpTVVBFUi1TRUNSRVQtVE9LRU4=`.
const (
	stolenUsername = "prod-robot"
	stolenPassword = "SUPER-SECRET-TOKEN"
)

// recordingCredentials answers like a mounted Docker config and remembers being
// asked. "Never read" is a stronger property than "read but not sent": a realm
// that fails the check costs no credential access at all.
type recordingCredentials struct {
	mutex sync.Mutex
	asked int
}

func (source *recordingCredentials) Basic(context.Context, string) (string, string, error) {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	source.asked++
	return stolenUsername, stolenPassword, nil
}

func (source *recordingCredentials) timesAsked() int {
	source.mutex.Lock()
	defer source.mutex.Unlock()
	return source.asked
}

// listener records every request a host received, so a test can assert that a
// foreign realm received nothing at all rather than merely nothing sensitive.
type listener struct {
	mutex          sync.Mutex
	authorizations []string
}

func (recorder *listener) handler() http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		recorder.mutex.Lock()
		recorder.authorizations = append(recorder.authorizations, request.Header.Get("Authorization"))
		recorder.mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"token":"an-attacker-issued-token"}`))
	}
}

func (recorder *listener) received() []string {
	recorder.mutex.Lock()
	defer recorder.mutex.Unlock()
	return append([]string(nil), recorder.authorizations...)
}

// routed sends each logical registry host to its own test server, so "which
// host got the credential" is an observable fact rather than an assumption.
type routed struct{ hosts map[string]string }

func (transport routed) RoundTrip(request *http.Request) (*http.Response, error) {
	target, known := transport.hosts[request.URL.Host]
	if !known {
		return nil, fmt.Errorf("no test server stands in for %s", request.URL.Host)
	}
	request.URL.Scheme, request.URL.Host = "http", target
	return http.DefaultTransport.RoundTrip(request)
}

// challenged builds a resolver against a registry that answers every request
// with a 401 naming realm, with a separate server standing in for every other
// host so "which host got the credential" is observable.
func challenged(t *testing.T, realm string) (*ociresolve.Resolver, *listener, *recordingCredentials) {
	t.Helper()
	attacker := &listener{}
	attackerServer := httptest.NewServer(attacker.handler())
	t.Cleanup(attackerServer.Close)
	registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("WWW-Authenticate", `Bearer realm="`+realm+`",service="registry.example"`)
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registryServer.Close)
	attackerTarget, err := url.Parse(attackerServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	registryTarget, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	credentials := &recordingCredentials{}
	return ociresolve.New(&http.Client{Transport: routed{hosts: map[string]string{
		"registry.example":               registryTarget.Host,
		"attacker.example":               attackerTarget.Host,
		"evil.registry.example":          attackerTarget.Host,
		"registry.example.attacker.test": attackerTarget.Host,
	}}}, credentials), attacker, credentials
}

func TestAnUndeclaredTokenRealmNeverReceivesTheCredential(t *testing.T) {
	for name, realm := range map[string]string{
		"an unrelated host":        "https://attacker.example/token",
		"a plaintext foreign host": "http://attacker.example/token",
		// A suffix of the registry host is not the registry host. This is the
		// shape that defeats a host rule written with HasSuffix.
		"a host the registry name is a suffix of": "https://evil.registry.example/token",
		// Nor is a host that merely starts with it.
		"a host that starts with the registry name": "https://registry.example.attacker.test/token",
	} {
		t.Run(name, func(t *testing.T) {
			resolver, attacker, credentials := challenged(t, realm)
			_, err := resolver.Resolve(t.Context(), channel("latest-stable"))
			if err == nil {
				t.Fatal("an undeclared token realm produced a pin")
			}
			if got := attacker.received(); len(got) != 0 {
				t.Fatalf("the foreign realm was contacted at all, carrying %q", got)
			}
			if credentials.timesAsked() != 0 {
				t.Errorf("the credential was read %d time(s) for a realm it could never be sent to",
					credentials.timesAsked())
			}
			// An error is where a stolen credential leaks a second time. Logs
			// travel further than the request that produced them.
			if strings.Contains(err.Error(), stolenPassword) || strings.Contains(err.Error(), stolenUsername) {
				t.Errorf("the refusal names the credential: %v", err)
			}
		})
	}
}

// The other direction. Docker Hub's registry is registry-1.docker.io and its
// realm is auth.docker.io; once an operator declares that, the exchange has to
// work, or the fix has made every private pull from Docker Hub impossible.
func TestADeclaredCrossHostRealmIsPaidWithTheCredential(t *testing.T) {
	realmHost := &listener{}
	realmServer := httptest.NewServer(realmHost.handler())
	t.Cleanup(realmServer.Close)
	registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") == "Bearer an-attacker-issued-token" {
			writer.Header().Set("Docker-Content-Digest", digest)
			writer.WriteHeader(http.StatusOK)
			return
		}
		writer.Header().Set("WWW-Authenticate",
			`Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`)
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(registryServer.Close)
	realmTarget, err := url.Parse(realmServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	registryTarget, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	declared, err := registrychallenge.ParseAllowlist([]string{"registry-1.docker.io=auth.docker.io"})
	if err != nil {
		t.Fatal(err)
	}
	credentials := &recordingCredentials{}
	resolver := ociresolve.New(&http.Client{Transport: routed{hosts: map[string]string{
		"registry-1.docker.io": registryTarget.Host,
		"auth.docker.io":       realmTarget.Host,
	}}}, credentials, ociresolve.WithTokenRealms(declared))

	pin, err := resolver.Resolve(t.Context(), domain.Selector{
		Kind: domain.ByChannel, Repository: "registry-1.docker.io/example/runtime", Channel: "latest-stable",
	})
	if err != nil {
		t.Fatalf("a declared cross-host realm was refused: %v", err)
	}
	if pin.Digest != digest {
		t.Fatalf("resolved to %s", pin)
	}
	got := realmHost.received()
	if len(got) != 1 || got[0] == "" {
		t.Fatalf("the declared realm was paid %q, want one request carrying the credential", got)
	}
	if credentials.timesAsked() == 0 {
		t.Error("no credential was read for a declared realm")
	}
}

// Same host, wrong scheme. A credential is exchanged over https or not at all;
// nothing in this adapter's configuration relaxes that.
func TestAPlaintextTokenRealmIsRefusedEvenOnTheRegistrysOwnHost(t *testing.T) {
	resolver, _, credentials := challenged(t, "http://registry.example/token")
	if _, err := resolver.Resolve(t.Context(), channel("latest-stable")); err == nil {
		t.Fatal("a plaintext token realm produced a pin")
	}
	if credentials.timesAsked() != 0 {
		t.Errorf("the credential was read for a plaintext realm")
	}
}

func TestARepositoryWithNoRegistryHostIsRefused(t *testing.T) {
	resolver := ociresolve.New(nil, nil)
	_, err := resolver.Resolve(context.Background(), domain.Selector{
		Kind: domain.ByChannel, Repository: "example/runtime", Channel: "latest",
	})
	if err == nil {
		t.Fatal("a repository with no registry host was resolved")
	}
}

// --- against a real registry ------------------------------------------------

// The check the fakes cannot make: that a real registry's anonymous bearer flow
// and digest header still work the way this adapter assumes. Skips without
// network.
func TestARealPublicChannelResolves(t *testing.T) {
	if testing.Short() {
		t.Skip("network test")
	}
	resolver := ociresolve.New(nil, ociresolve.Anonymous{})
	pin, err := resolver.Resolve(t.Context(), domain.Selector{
		Kind: domain.ByChannel, Repository: "ghcr.io/fluxcd/source-controller", Channel: "v1.7.1",
	})
	if err != nil {
		t.Skipf("could not reach the registry: %v", err)
	}
	if err := pin.Validate(); err != nil {
		t.Fatalf("a real registry produced an invalid pin %s: %v", pin, err)
	}
	t.Logf("ghcr.io/fluxcd/source-controller:v1.7.1 resolves to %s", pin.Digest)
}
