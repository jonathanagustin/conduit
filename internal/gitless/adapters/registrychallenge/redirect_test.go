package registrychallenge_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/registrychallenge"
)

// request builds the request a client would be about to make, with or without
// the credential on it. via[0] is what the caller actually asked for, and it is
// the only host and scheme this package ever authorised.
func request(t *testing.T, url string, credentialed bool) *http.Request {
	t.Helper()
	built, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build %s: %v", url, err)
	}
	if credentialed {
		built.SetBasicAuth("prod-robot", "SUPER-SECRET-TOKEN")
	}
	return built
}

// The rule in one table. The first row is the one the standard library already
// handled, and it is here because it handled it for a reason outside this
// repository: net/http drops Authorization across domains, keeps it across
// SUBDOMAINS, and never looks at the scheme. If that changes, this row fails
// here rather than silently in production.
//
// `carry` is the assertion, and it is deliberately about the HEADER rather than
// about the error. Every row is followed -- the policy strips instead of
// refusing, because refusing broke credentialed blob reads against every
// CDN-backed registry -- so "was it refused" no longer distinguishes anything.
// What distinguishes the rows is whether the credential is still on the request
// when it goes out, which is the property the package actually promises.
func TestACredentialedRedirectCarriesItsCredentialOnlyToItsOwnHostOverHttps(t *testing.T) {
	for name, hop := range map[string]struct {
		from, to string
		carry    bool
	}{
		"another host entirely":         {"https://registry.example/token", "https://attacker.example/steal", false},
		"a subdomain of the registry":   {"https://registry.example/token", "https://evil.registry.example/steal", false},
		"the same host over plaintext":  {"https://registry.example/token", "http://registry.example/steal", false},
		"the same host on another port": {"https://registry.example/token", "https://registry.example:8443/steal", false},
		"a host the registry name prefixes": {"https://registry.example/token",
			"https://registry.example.attacker.test/steal", false},
		"a non-http scheme":              {"https://registry.example/token", "ftp://registry.example/steal", false},
		"another path on the same host":  {"https://registry.example/token", "https://registry.example/v2/token", true},
		"the same host in another case":  {"https://registry.example/token", "https://REGISTRY.EXAMPLE/v2/token", true},
		"a plaintext mirror staying put": {"http://mirror.example:5000/token", "http://mirror.example:5000/v2/token", true},
		// Not a downgrade: the credential ends up better protected than it
		// started. A disconnected mirror reached over http may legitimately send
		// its token exchange to TLS on the same host.
		"a plaintext mirror upgrading to https": {"http://mirror.example/token", "https://mirror.example/token", true},
	} {
		t.Run(name, func(t *testing.T) {
			original := request(t, hop.from, true)
			redirected := request(t, hop.to, true)
			if err := registrychallenge.CheckRedirect(redirected, []*http.Request{original}); err != nil {
				t.Fatalf("a redirect to %s was refused rather than stripped: %v", hop.to, err)
			}
			carried := redirected.Header.Get("Authorization")
			if hop.carry && carried == "" {
				t.Fatalf("the credential was stripped from a redirect to %s, which stays at its own origin", hop.to)
			}
			if !hop.carry && carried != "" {
				t.Fatalf("a credential was carried to %s: %q", hop.to, carried)
			}
			// Blanked is not deleted. An empty Authorization header still tells
			// the next host that a credential was being carried, and some
			// servers treat a present-but-empty header differently from an
			// absent one.
			if !hop.carry {
				if _, present := redirected.Header["Authorization"]; present {
					t.Errorf("the Authorization header was blanked rather than deleted for %s", hop.to)
				}
			}
			// The original is the caller's request and outlives this call. A
			// policy that stripped it instead of the copy would disarm the
			// retry the adapter is about to make against its own host.
			if original.Header.Get("Authorization") == "" {
				t.Errorf("the caller's own request was stripped on the way to %s", hop.to)
			}
		})
	}
}

// A request with no Authorization header has no credential to lose, and
// registries do redirect uncredentialed traffic to a CDN on another host.
// Constraining it would break pulls and buy nothing.
func TestAnUncredentialedRedirectIsNotConstrained(t *testing.T) {
	original := request(t, "https://registry.example/v2/team/app/blobs/sha256:aa", false)
	for _, target := range []string{
		"https://cdn.example/blobs/sha256:aa",
		"http://cdn.example/blobs/sha256:aa",
	} {
		if err := registrychallenge.CheckRedirect(request(t, target, false), []*http.Request{original}); err != nil {
			t.Errorf("an anonymous redirect to %s was refused: %v", target, err)
		}
	}
}

// Assigning CheckRedirect replaces the default policy, and the default policy
// is what stopped a redirect loop. A same-host https loop is permitted by the
// rule above, so the cap has to be restated or a registry answering its own
// token URL with a redirect to itself spins forever.
func TestARedirectLoopStillStops(t *testing.T) {
	var via []*http.Request
	for range 10 {
		via = append(via, request(t, "https://registry.example/token", true))
	}
	err := registrychallenge.CheckRedirect(request(t, "https://registry.example/token", true), via)
	if err == nil {
		t.Fatal("an unbounded chain of same-host redirects was followed")
	}
	if !strings.Contains(err.Error(), "10 redirects") {
		t.Errorf("the refusal does not say what stopped it: %v", err)
	}
}

// The client belongs to the caller. The composition root hands one to several
// adapters, and an adapter that reaches into it changes something it does not
// own.
func TestTheRedirectPolicyIsAppliedToACopyOfTheCallersClient(t *testing.T) {
	callers := &http.Client{}
	guarded := registrychallenge.WithRedirectPolicy(callers)
	if guarded == callers {
		t.Fatal("the caller's own client was returned")
	}
	if callers.CheckRedirect != nil {
		t.Error("the caller's client was mutated")
	}
	if guarded.CheckRedirect == nil {
		t.Fatal("the copy has no redirect policy")
	}
	// The rest of the caller's client has to survive the copy, or the guard
	// would quietly drop a timeout somebody chose.
	original := &http.Client{Timeout: 42}
	if copied := registrychallenge.WithRedirectPolicy(original); copied.Timeout != 42 {
		t.Errorf("the copy dropped the caller's timeout: %v", copied.Timeout)
	}
	if registrychallenge.WithRedirectPolicy(nil).CheckRedirect == nil {
		t.Error("a nil client produced no redirect policy")
	}
}
