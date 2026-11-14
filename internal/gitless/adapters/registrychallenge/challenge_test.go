package registrychallenge_test

import (
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/gitless/adapters/registrychallenge"
)

const registryHost = "registry.example"

// sameHostOnly is the zero value, and the default an operator who declares
// nothing gets.
var sameHostOnly registrychallenge.Allowlist

// The realms real registries issue on their own host. Every one of these was
// read off a live `GET /v2/` challenge, not off documentation. A rule that
// refused them would pass every negative test below and break every private
// pull, so they lead.
func TestARegistrysOwnHttpsRealmIsAccepted(t *testing.T) {
	for name, realm := range map[string]string{
		"ghcr.io's shape":            "https://registry.example/token",
		"quay.io's shape":            "https://registry.example/v2/auth",
		"gcr.io's shape":             "https://registry.example/v2/token",
		"public.ecr.aws's shape":     "https://registry.example/token/",
		"a realm with its own query": "https://registry.example/token?audience=registry",
		"an uppercase host":          "https://REGISTRY.EXAMPLE/token",
		"surrounding whitespace":     "  https://registry.example/token  ",
	} {
		if _, err := sameHostOnly.TokenEndpoint(realm, registryHost); err != nil {
			t.Errorf("%s was refused: %v", name, err)
		}
	}
}

// The other direction, and the one a naive same-host rule breaks. Docker Hub
// and GitLab genuinely issue a realm on another host; declaring it must make it
// work, or the fix is a fix that makes the feature unusable.
func TestADeclaredCrossHostRealmIsFollowed(t *testing.T) {
	declared, err := registrychallenge.ParseAllowlist([]string{
		"registry-1.docker.io=auth.docker.io",
		"registry.gitlab.com=gitlab.com",
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, shape := range []struct{ realm, registry string }{
		{"https://auth.docker.io/token", "registry-1.docker.io"},
		{"https://gitlab.com/jwt/auth", "registry.gitlab.com"},
		// Its own host still works once something is declared for it.
		{"https://registry-1.docker.io/token", "registry-1.docker.io"},
	} {
		if _, err := declared.TokenEndpoint(shape.realm, shape.registry); err != nil {
			t.Errorf("%s issuing %s was refused: %v", shape.registry, shape.realm, err)
		}
	}
	// A declaration is per registry, not global. Declaring auth.docker.io for
	// Docker Hub must not let some other registry direct credentials there.
	if _, err := declared.TokenEndpoint("https://auth.docker.io/token", registryHost); err == nil {
		t.Error("a realm declared for one registry was accepted for another")
	}
	// And the declaration does not become a general permission to roam.
	if _, err := declared.TokenEndpoint("https://attacker.example/token", "registry-1.docker.io"); err == nil {
		t.Error("declaring one cross-host realm admitted every other host")
	}
}

// The vulnerability, as a unit. Every one of these is a host nobody declared,
// including the two that a host rule written with a substring or suffix
// comparison would wave through.
func TestAnUndeclaredRealmOnAnotherHostIsRefused(t *testing.T) {
	for name, realm := range map[string]string{
		"an unrelated host":                        "https://attacker.example/token",
		"the Docker Hub realm, undeclared":         "https://auth.docker.io/token",
		"a subdomain of the registry":              "https://evil.registry.example/token",
		"a host the registry name prefixes":        "https://registry.example.attacker.test/token",
		"a host that contains the registry name":   "https://x-registry.example-y/token",
		"the registry on another port":             "https://registry.example:8443/token",
		"a userinfo-carrying lookalike":            "https://registry.example@attacker.example/token",
		"a path that only looks like the registry": "https://attacker.example/registry.example/token",
		"an uppercase unrelated host":              "https://ATTACKER.EXAMPLE/token",
	} {
		if _, err := sameHostOnly.TokenEndpoint(realm, registryHost); err == nil {
			t.Errorf("%s was accepted as somewhere this registry's credentials may go", name)
		}
	}
}

func TestARealmThatIsNotAnAbsoluteHttpsUrlIsRefused(t *testing.T) {
	for name, realm := range map[string]string{
		"empty":             "",
		"a bare path":       "/token",
		"a bare host":       "registry.example/token",
		"scheme only":       "https://",
		"an unusable URL":   "https://registry.example/token\x7f",
		"a non-http scheme": "file:///etc/shadow",
		"a data URL":        "data:text/plain,token",
	} {
		if _, err := sameHostOnly.TokenEndpoint(realm, registryHost); err == nil {
			t.Errorf("%s was accepted as a token realm", name)
		}
	}
}

// https is required whoever the host is, and nothing relaxes it. A deployment
// may be configured to REACH a registry over http -- a disconnected mirror
// terminating TLS elsewhere -- but that is not a statement that a credential may
// be posted in the clear, and there is no parameter here that could be read as
// one.
func TestAPlaintextRealmIsAlwaysRefused(t *testing.T) {
	declared, err := registrychallenge.ParseAllowlist([]string{"registry.example=auth.registry.example"})
	if err != nil {
		t.Fatal(err)
	}
	for name, realm := range map[string]string{
		"on the registry's own host": "http://registry.example/token",
		"on a declared host":         "http://auth.registry.example/token",
	} {
		if _, err := declared.TokenEndpoint(realm, registryHost); err == nil {
			t.Errorf("a plaintext realm %s was accepted", name)
		}
	}
}

// A registry carrying a port is a normal thing -- a local zot, a disconnected
// mirror -- and its realm carries the same port.
func TestAHostWithAPortMatchesOnlyItself(t *testing.T) {
	if _, err := sameHostOnly.TokenEndpoint("https://mirror.example:5000/token", "mirror.example:5000"); err != nil {
		t.Fatalf("a realm on the registry's own host:port was refused: %v", err)
	}
	if _, err := sameHostOnly.TokenEndpoint("https://mirror.example/token", "mirror.example:5000"); err == nil {
		t.Fatal("a realm on another port was accepted")
	}
	// And a declaration may carry a port too.
	declared, err := registrychallenge.ParseAllowlist([]string{"mirror.example:5000=auth.mirror.example:8443"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := declared.TokenEndpoint("https://auth.mirror.example:8443/token", "mirror.example:5000"); err != nil {
		t.Errorf("a declared host:port realm was refused: %v", err)
	}
}

func TestAnAllowlistEntryMustNameTwoBareHosts(t *testing.T) {
	for name, entries := range map[string][]string{
		"no separator":        {"registry-1.docker.io"},
		"no realm":            {"registry-1.docker.io="},
		"no registry":         {"=auth.docker.io"},
		"a URL":               {"registry-1.docker.io=https://auth.docker.io/token"},
		"a wildcard realm":    {"registry-1.docker.io=*"},
		"a wildcard subtree":  {"registry-1.docker.io=*.docker.io"},
		"a wildcard registry": {"*=auth.docker.io"},
		"a path":              {"registry-1.docker.io=auth.docker.io/token"},
	} {
		if _, err := registrychallenge.ParseAllowlist(entries); err == nil {
			t.Errorf("%s was accepted as a token realm declaration", name)
		}
	}
}

func TestAnEmptyDeclarationIsSameHostOnly(t *testing.T) {
	allowed, err := registrychallenge.ParseAllowlist([]string{"", "   "})
	if err != nil {
		t.Fatalf("an empty declaration was an error: %v", err)
	}
	if allowed != nil {
		t.Fatalf("an empty declaration produced %v rather than the same-host default", allowed)
	}
	if _, err := allowed.TokenEndpoint("https://attacker.example/token", registryHost); err == nil {
		t.Fatal("the same-host default admitted a foreign realm")
	}
}

// The refusal has to tell an operator what to do about a legitimate cross-host
// registry, or it reads as a bug rather than as a decision.
func TestTheRefusalNamesTheDeclarationThatWouldPermitIt(t *testing.T) {
	_, err := sameHostOnly.TokenEndpoint("https://auth.docker.io/token", "registry-1.docker.io")
	if err == nil {
		t.Fatal("an undeclared cross-host realm was accepted")
	}
	for _, want := range []string{
		"GITLESS_REGISTRY_TOKEN_REALMS",
		"registry-1.docker.io=auth.docker.io",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %q: %v", want, err)
		}
	}
}

// Scope is added to the realm's query rather than concatenated onto its string.
// `realm + "?scope=..."` produces a URL naming neither when the realm already
// carries a query, which is a legal thing for a realm to do.
func TestScopeIsAddedToARealmThatAlreadyCarriesAQuery(t *testing.T) {
	endpoint, err := sameHostOnly.TokenEndpoint("https://registry.example/token?audience=registry", registryHost)
	if err != nil {
		t.Fatal(err)
	}
	built := registrychallenge.WithScope(endpoint, "repository:team/manifests:pull,push", "registry.example")
	for _, want := range []string{
		"audience=registry",
		"scope=repository%3Ateam%2Fmanifests%3Apull%2Cpush",
		"service=registry.example",
	} {
		if !strings.Contains(built, want) {
			t.Errorf("the token URL %q does not carry %q", built, want)
		}
	}
	if strings.Count(built, "?") != 1 {
		t.Errorf("the token URL has more than one query: %q", built)
	}
}

func TestAnAbsentServiceIsNotSent(t *testing.T) {
	endpoint, err := sameHostOnly.TokenEndpoint("https://registry.example/token", registryHost)
	if err != nil {
		t.Fatal(err)
	}
	if built := registrychallenge.WithScope(endpoint, "repository:team/x:pull", ""); strings.Contains(built, "service=") {
		t.Errorf("an empty service was sent: %q", built)
	}
}
