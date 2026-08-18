package ocipublish_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/ocipublish"
	"github.com/jonathanagustin/conduit/internal/gitless/adapters/registrychallenge"
)

var digest = "sha256:aa11" + strings.Repeat("0", 60)

func manifests(sourceDigest string) []byte {
	return []byte(`apiVersion: source.toolkit.fluxcd.io/v1
kind: OCIRepository
metadata:
  name: conduit-prod
  namespace: flux-system
spec:
  ref:
    digest: ` + sourceDigest + `
  url: oci://ghcr.io/example/platform-runtime
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: conduit-prod
  namespace: flux-system
spec:
  prune: true
`)
}

// requireRegistry needs a real one. The push and pull flows are a sequence of
// status codes, headers and redirects that a hand-written fake would model
// exactly as I imagined them, which is the same reason the cosign adapter is
// tested against the real binary.
func requireRegistry(t *testing.T) string {
	t.Helper()
	registry := os.Getenv("OCI_TEST_REGISTRY")
	if registry == "" {
		t.Skip("set OCI_TEST_REGISTRY to a host:port running an OCI registry")
	}
	return registry
}

func newPublisher(t *testing.T, repository string) *ocipublish.Publisher {
	t.Helper()
	publisher, err := ocipublish.New(repository, nil, ocipublish.Anonymous{}, true)
	if err != nil {
		t.Fatalf("build publisher: %v", err)
	}
	return publisher
}

func TestPublishedManifestsAreReadBackAsTheirSourceDigest(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/roundtrip")

	if current, err := publisher.CurrentDigest(t.Context(), "prod"); err != nil || current != "" {
		t.Fatalf("an unpublished environment reported %q (%v)", current, err)
	}
	if err := publisher.Put(t.Context(), "prod", manifests(digest)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	current, err := publisher.CurrentDigest(t.Context(), "prod")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// The answer is the SOURCE digest inside the manifests, not the digest of
	// the artifact carrying them. Confusing the two would make every republish
	// look like a source change.
	if current != digest {
		t.Fatalf("read back %q, want the source digest %q", current, digest)
	}
}

func TestRepublishingADifferentSourceChangesWhatIsReadBack(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/republish")
	second := "sha256:bb22" + strings.Repeat("0", 60)

	if err := publisher.Put(t.Context(), "prod", manifests(digest)); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := publisher.Put(t.Context(), "prod", manifests(second)); err != nil {
		t.Fatalf("second publish: %v", err)
	}
	current, err := publisher.CurrentDigest(t.Context(), "prod")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if current != second {
		t.Fatalf("read back %q after republishing, want %q", current, second)
	}
}

// Each environment is its own tag. One environment's deployment must not be
// readable as another's, or a fleet would converge on whichever published last.
func TestEnvironmentsDoNotShareATag(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/environments")
	staging := "sha256:cc33" + strings.Repeat("0", 60)

	if err := publisher.Put(t.Context(), "prod", manifests(digest)); err != nil {
		t.Fatalf("publish prod: %v", err)
	}
	if err := publisher.Put(t.Context(), "staging", manifests(staging)); err != nil {
		t.Fatalf("publish staging: %v", err)
	}
	for environment, want := range map[string]string{"prod": digest, "staging": staging} {
		got, err := publisher.CurrentDigest(t.Context(), environment)
		if err != nil {
			t.Fatalf("%s: %v", environment, err)
		}
		if got != want {
			t.Errorf("%s read back %q, want %q", environment, got, want)
		}
	}
}

// Republishing identical bytes must not re-upload them. A five-minute reconcile
// loop would otherwise be a five-minute upload loop.
func TestRepublishingIdenticalBytesIsAccepted(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/idempotent")
	for range 3 {
		if err := publisher.Put(t.Context(), "prod", manifests(digest)); err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	current, err := publisher.CurrentDigest(t.Context(), "prod")
	if err != nil || current != digest {
		t.Fatalf("read back %q (%v)", current, err)
	}
}

func TestManifestsWithNoOCIRepositoryAreRefusedOnReadBack(t *testing.T) {
	registry := requireRegistry(t)
	publisher := newPublisher(t, registry+"/conduit/notasource")
	if err := publisher.Put(t.Context(), "prod", []byte("apiVersion: v1\nkind: ConfigMap\n")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := publisher.CurrentDigest(t.Context(), "prod"); err == nil {
		t.Fatal("manifests declaring no source digest were read back as a digest")
	}
}

// --- where a credential may be sent -----------------------------------------
//
// The publisher's challenge handling had the same defect as the resolver's, and
// a worse blast radius: its credential carries push scope, and
// GITLESS_FLUX_INSECURE_REGISTRY=true makes the whole exchange plaintext. The
// realm came out of an unauthenticated 401 and was used to build the token URL
// with SetBasicAuth on it, unchecked. These are hermetic on purpose -- the
// round-trip tests above need a real registry and skip without one, which is
// exactly how a challenge-handling defect stayed invisible.

const (
	stolenUsername = "prod-robot"
	stolenPassword = "SUPER-SECRET-TOKEN"
)

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

// routed sends each logical host to its own server, so which host received the
// credential is observable.
type routed struct {
	scheme string
	hosts  map[string]string
}

// RoundTrip rewrites a COPY, both because a RoundTripper may not modify the
// request it is given and because the redirect tests below depend on it: the
// client resolves a Location against request.URL and hands that URL to
// CheckRedirect, so rewriting the original in place would make every redirect
// look like one from 127.0.0.1 to 127.0.0.1.
func (transport routed) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != transport.scheme {
		return nil, fmt.Errorf("the publisher spoke %s where %s was configured", request.URL.Scheme, transport.scheme)
	}
	target, known := transport.hosts[request.URL.Host]
	if !known {
		return nil, fmt.Errorf("no test server stands in for %s", request.URL.Host)
	}
	proxied := request.Clone(request.Context())
	proxied.URL.Scheme, proxied.URL.Host = "http", target
	return http.DefaultTransport.RoundTrip(proxied)
}

func challenged(t *testing.T, realm string, insecure bool,
	options ...ocipublish.Option) (*ocipublish.Publisher, *listener, *recordingCredentials) {
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
	scheme := "https"
	if insecure {
		scheme = "http"
	}
	credentials := &recordingCredentials{}
	publisher, err := ocipublish.New("registry.example/team/manifests", &http.Client{Transport: routed{
		scheme: scheme,
		hosts: map[string]string{
			"registry.example":               registryTarget.Host,
			"attacker.example":               attackerTarget.Host,
			"auth.registry.example":          attackerTarget.Host,
			"evil.registry.example":          attackerTarget.Host,
			"registry.example.attacker.test": attackerTarget.Host,
		},
	}}, credentials, insecure, options...)
	if err != nil {
		t.Fatal(err)
	}
	return publisher, attacker, credentials
}

func TestAnUndeclaredTokenRealmNeverReceivesThePushCredential(t *testing.T) {
	for name, realm := range map[string]string{
		"an unrelated host":                         "https://attacker.example/token",
		"a plaintext foreign host":                  "http://attacker.example/token",
		"a host the registry name is a suffix of":   "https://evil.registry.example/token",
		"a host that starts with the registry name": "https://registry.example.attacker.test/token",
	} {
		t.Run(name, func(t *testing.T) {
			publisher, attacker, credentials := challenged(t, realm, false)
			err := publisher.Put(t.Context(), "prod", manifests(digest))
			if err == nil {
				t.Fatal("an undeclared token realm was followed and the manifests published")
			}
			if got := attacker.received(); len(got) != 0 {
				t.Fatalf("the foreign realm was contacted at all, carrying %q", got)
			}
			if credentials.timesAsked() != 0 {
				t.Errorf("the push credential was read %d time(s) for a realm it could never be sent to",
					credentials.timesAsked())
			}
			if strings.Contains(err.Error(), stolenPassword) || strings.Contains(err.Error(), stolenUsername) {
				t.Errorf("the refusal names the credential: %v", err)
			}
		})
	}
}

// The plaintext switch is about REACHING this registry. It is not a statement
// that a push credential may be posted in the clear, and it does not widen which
// host may be paid either.
func TestThePlaintextRegistrySwitchDoesNotReachTheTokenExchange(t *testing.T) {
	for name, realm := range map[string]string{
		"a foreign plaintext realm":          "http://attacker.example/token",
		"the registry's own plaintext realm": "http://registry.example/token",
		"a declared host over plaintext":     "http://auth.registry.example/token",
	} {
		declared, err := registrychallenge.ParseAllowlist([]string{"registry.example=auth.registry.example"})
		if err != nil {
			t.Fatal(err)
		}
		publisher, attacker, credentials := challenged(t, realm, true, ocipublish.WithTokenRealms(declared))
		if err := publisher.Put(t.Context(), "prod", manifests(digest)); err == nil {
			t.Errorf("%s was followed by an insecure publisher", name)
		}
		if got := attacker.received(); len(got) != 0 {
			t.Errorf("%s received %q", name, got)
		}
		if credentials.timesAsked() != 0 {
			t.Errorf("the push credential was read for %s", name)
		}
	}
}

// A declared cross-host realm is paid, over https, even for an otherwise
// plaintext registry. Declaring it is the operator's decision; believing the
// header is not.
func TestADeclaredCrossHostRealmIsPaidWithThePushCredential(t *testing.T) {
	declared, err := registrychallenge.ParseAllowlist([]string{"registry.example=auth.registry.example"})
	if err != nil {
		t.Fatal(err)
	}
	publisher, realmHost, credentials := challenged(t,
		"https://auth.registry.example/token", false, ocipublish.WithTokenRealms(declared))
	// The stub realm issues a token and the stub registry then 401s again, so
	// the publish does not complete. What is asserted is that the declared realm
	// was paid at all.
	_ = publisher.Put(t.Context(), "prod", manifests(digest))
	got := realmHost.received()
	if len(got) == 0 {
		t.Fatal("a declared cross-host realm was never contacted")
	}
	if got[0] == "" {
		t.Fatal("a declared cross-host realm was contacted without the credential it asked for")
	}
	if credentials.timesAsked() == 0 {
		t.Error("no credential was read for a declared realm")
	}
}

// And the realm a real registry issues is still followed. A rule that refused
// everything would pass the tests above and break every private push.
func TestTheRegistrysOwnRealmIsFollowedWithTheScopeThePushNeeds(t *testing.T) {
	var (
		mutex  sync.Mutex
		scopes []string
		auths  []string
	)
	registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/token" {
			mutex.Lock()
			scopes = append(scopes, request.URL.Query().Get("scope"))
			auths = append(auths, request.Header.Get("Authorization"))
			mutex.Unlock()
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"token":"a-push-token"}`))
			return
		}
		if request.Header.Get("Authorization") == "" {
			writer.Header().Set("WWW-Authenticate",
				`Bearer realm="https://registry.example/token",service="registry.example"`)
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(registryServer.Close)
	target, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	credentials := &recordingCredentials{}
	publisher, err := ocipublish.New("registry.example/team/manifests",
		&http.Client{Transport: routed{scheme: "https", hosts: map[string]string{"registry.example": target.Host}}},
		credentials, false)
	if err != nil {
		t.Fatal(err)
	}
	// The upload cannot complete against this stub, and does not need to. What
	// is being asserted is that the registry's own realm was paid, with the
	// push scope, over https.
	_ = publisher.Put(t.Context(), "prod", manifests(digest))

	mutex.Lock()
	defer mutex.Unlock()
	if len(scopes) == 0 {
		t.Fatal("the registry's own token realm was never contacted")
	}
	if scopes[0] != "repository:team/manifests:pull,push" {
		t.Errorf("token scope: %q", scopes[0])
	}
	if auths[0] == "" {
		t.Error("the registry's own realm was contacted without the credential it asked for")
	}
	if credentials.timesAsked() == 0 {
		t.Error("no credential was read for the registry's own realm")
	}
}

// --- where a redirect may take a credential ---------------------------------
//
// The realm check above says which host may be paid. It said nothing about
// where that host may then redirect, and the publisher's client followed a 302
// carrying whatever net/http was willing to carry: Authorization survives a
// redirect to the same domain or a SUBDOMAIN of it, with the scheme ignored. A
// push credential is the worst one to move -- it can write the artifact the
// fleet converges on -- so #244 is fixed here as well as in the resolver, from
// the one policy in registrychallenge.

// anyScheme is routed without its scheme assertion. The plaintext-downgrade row
// has to be able to REACH a server over http, or the harness would refuse the
// hop and the test would pass without the policy ever being consulted.
type anyScheme struct{ hosts map[string]string }

func (transport anyScheme) RoundTrip(request *http.Request) (*http.Response, error) {
	target, known := transport.hosts[request.URL.Host]
	if !known {
		return nil, fmt.Errorf("no test server stands in for %s", request.URL.Host)
	}
	proxied := request.Clone(request.Context())
	proxied.URL.Scheme, proxied.URL.Host = "http", target
	return http.DefaultTransport.RoundTrip(proxied)
}

// redirectedRealm builds a publisher whose registry issues its own permitted
// https realm and whose token endpoint answers with a 302 to target. The
// foreign hosts and the registry's own /steal path share one recorder, so the
// credential's arrival is observed wherever it lands.
func redirectedRealm(t *testing.T, target string) (*ocipublish.Publisher, *listener, *recordingCredentials) {
	t.Helper()
	arrivals := &listener{}
	attackerServer := httptest.NewServer(arrivals.handler())
	t.Cleanup(attackerServer.Close)
	registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			http.Redirect(writer, request, target, http.StatusFound)
		case "/steal":
			arrivals.handler()(writer, request)
		default:
			writer.Header().Set("WWW-Authenticate",
				`Bearer realm="https://registry.example/token",service="registry.example"`)
			writer.WriteHeader(http.StatusUnauthorized)
		}
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
	publisher, err := ocipublish.New("registry.example/team/manifests", &http.Client{Transport: anyScheme{
		hosts: map[string]string{
			"registry.example":      registryTarget.Host,
			"attacker.example":      attackerTarget.Host,
			"evil.registry.example": attackerTarget.Host,
		},
	}}, credentials, false)
	if err != nil {
		t.Fatal(err)
	}
	return publisher, arrivals, credentials
}

func TestAPermittedRealmCannotRedirectThePushCredentialElsewhere(t *testing.T) {
	for name, target := range map[string]string{
		"to another host entirely":       "https://attacker.example/steal",
		"to a subdomain of the registry": "https://evil.registry.example/steal",
		"to its own host over plaintext": "http://registry.example/steal",
	} {
		t.Run(name, func(t *testing.T) {
			publisher, arrivals, credentials := redirectedRealm(t, target)
			err := publisher.Put(t.Context(), "prod", manifests(digest))
			if err == nil {
				t.Fatal("a redirect away from the permitted realm published the manifests")
			}
			if got := arrivals.received(); len(got) != 0 {
				t.Fatalf("the redirect target was contacted, carrying %q", got)
			}
			if credentials.timesAsked() == 0 {
				t.Error("the realm was permitted, so the credential should have been read for it")
			}
			if strings.Contains(err.Error(), stolenPassword) || strings.Contains(err.Error(), stolenUsername) {
				t.Errorf("the refusal names the credential: %v", err)
			}
		})
	}
}

// The redirect that must keep working. A token service that answers on another
// path of its own host over https is ordinary, and refusing it would break
// every private push to a registry that does it.
func TestASameHostHttpsRedirectStillCarriesThePushCredential(t *testing.T) {
	arrivals := &listener{}
	registryServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/token":
			http.Redirect(writer, request, "https://registry.example/v2/token?"+request.URL.RawQuery,
				http.StatusFound)
		case "/v2/token":
			arrivals.handler()(writer, request)
		default:
			writer.Header().Set("WWW-Authenticate",
				`Bearer realm="https://registry.example/token",service="registry.example"`)
			writer.WriteHeader(http.StatusUnauthorized)
		}
	}))
	t.Cleanup(registryServer.Close)
	registryTarget, err := url.Parse(registryServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	credentials := &recordingCredentials{}
	publisher, err := ocipublish.New("registry.example/team/manifests", &http.Client{Transport: anyScheme{
		hosts: map[string]string{"registry.example": registryTarget.Host},
	}}, credentials, false)
	if err != nil {
		t.Fatal(err)
	}
	// The stub 401s again after issuing the token, so the push does not
	// complete, exactly as in the declared-realm test above. What is asserted
	// is that the redirected token endpoint was paid.
	_ = publisher.Put(t.Context(), "prod", manifests(digest))

	got := arrivals.received()
	if len(got) == 0 {
		t.Fatal("a same-host https redirect was refused: the token endpoint was never reached")
	}
	if !strings.HasPrefix(got[0], "Basic ") {
		t.Fatalf("the redirected token endpoint received %q, want the credential", got[0])
	}
}

// --- construction -----------------------------------------------------------

func TestARepositoryWithNoRegistryHostIsRefused(t *testing.T) {
	if _, err := ocipublish.New("example/runtime", nil, nil, false); err == nil {
		t.Fatal("a repository with no registry host was accepted")
	}
}

func TestAnUnnamedEnvironmentIsRefused(t *testing.T) {
	publisher := newPublisher(t, "registry.example/team/manifests")
	if err := publisher.Put(t.Context(), "  ", manifests(digest)); err == nil {
		t.Fatal("manifests were published for an unnamed environment")
	}
}

// A registry may not name where the push credential goes.
//
// The token realm was hardened first, but it was not the only destination a
// registry could choose: a blob upload answers with a Location header, and an
// absolute Location was followed verbatim. So a registry could reply
// `Location: http://attacker.example/hijacked`, answer the PUT with a 401, and
// be handed the push robot's password -- in cleartext, on a host that never had
// it, bypassing the realm constraint entirely. On a Bearer registry that is
// strictly worse than it sounds: the registry itself never sees the Basic
// credential, so this extracts one it could not otherwise read.
func TestPublisherRefusesToSendCredentialsToAHostTheRegistryNamed(t *testing.T) {
	t.Parallel()
	var attackerSawAuthorization string
	attacker := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if header := request.Header.Get("Authorization"); header != "" {
			attackerSawAuthorization = header
		}
		writer.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer attacker.Close()

	registry := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodHead:
			writer.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(request.URL.Path, "/blobs/uploads/"):
			// The whole attack, in one header.
			writer.Header().Set("Location", attacker.URL+"/hijacked/upload")
			writer.WriteHeader(http.StatusAccepted)
		default:
			writer.WriteHeader(http.StatusCreated)
		}
	}))
	defer registry.Close()

	host := strings.TrimPrefix(registry.URL, "http://")
	publisher, err := ocipublish.New(host+"/team/manifests", registry.Client(), &recordingCredentials{}, true)
	if err != nil {
		t.Fatal(err)
	}
	err = publisher.Put(t.Context(), "prod", []byte("manifests"))
	if err == nil {
		t.Fatal("publishing followed the registry's Location to another host and reported success")
	}
	if attackerSawAuthorization != "" {
		t.Fatalf("the push credential reached a host the registry named: %q", attackerSawAuthorization)
	}
	if !strings.Contains(err.Error(), "another host") {
		t.Fatalf("error does not explain the refusal: %v", err)
	}
}
