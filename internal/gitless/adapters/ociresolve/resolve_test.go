package ociresolve_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ociresolve"
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
			writer.Header().Set("WWW-Authenticate",
				`Bearer realm="http://`+request.Host+`/token",service="registry.example"`)
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
		writer.Header().Set("WWW-Authenticate", `Bearer realm="http://example.invalid/token"`)
		writer.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := resolver.Resolve(t.Context(), channel("latest")); err == nil {
		t.Fatal("an unauthorised resolve succeeded")
	}
	if attempts > 2 {
		t.Errorf("the resolver retried %d times", attempts)
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
