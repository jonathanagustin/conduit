// Package registrychallenge decides where a registry's authentication
// challenge may send a credential.
//
// It exists because of the one thing that is easy to miss about the OCI
// distribution token flow: the realm a client exchanges its credentials at is
// read out of a `WWW-Authenticate` header on an UNAUTHENTICATED 401. Nothing
// about that response is trusted yet. A client that takes the realm at its word
// and offers Basic credentials there will hand a registry robot's password to
// whatever host the header named -- and because the same header can then issue
// a token that works, the pull succeeds and nothing surfaces the theft.
//
// The rule here is: a realm may name the registry's own host, or a host the
// OPERATOR declared for that registry, over https, and nothing else.
//
// A same-host rule alone would be wrong, and wrong in the direction that looks
// correct in tests. Two of the seven registries this was measured against issue
// a realm on another host, by design:
//
//	ghcr.io               Bearer realm="https://ghcr.io/token"
//	quay.io               Bearer realm="https://quay.io/v2/auth"
//	gcr.io                Bearer realm="https://gcr.io/v2/token"
//	public.ecr.aws        Bearer realm="https://public.ecr.aws/token/"
//	registry-1.docker.io  Bearer realm="https://auth.docker.io/token"     <- cross-host
//	registry.gitlab.com   Bearer realm="https://gitlab.com/jwt/auth"      <- cross-host
//
// So cross-host has to be reachable. It just must not be reachable by the
// header asking for it. The declaration lives with the operator, who can be
// asked to review it, rather than with the response, which cannot.
//
// The nearest well-reviewed implementation is go-containerregistry's
// pkg/v1/remote/transport/bearer.go (validateRealmURL, added for issue #2258).
// It constrains the scheme the same way, always allows the registry's own
// host:port, and for any other host rejects only realms whose host is a private
// or link-local IP LITERAL -- explicitly leaving DNS-named cross-host realms
// permitted and DNS-based SSRF out of scope. containerd's
// core/remotes/docker/auth/fetch.go constrains nothing at all: it parses the
// realm and posts to it. This package is deliberately stricter than both, since
// a Hub reaches a small set of registries an operator already had to name.
//
// It is a leaf package with no dependencies precisely so the resolver and the
// publisher cannot drift apart: a rule about where a credential may go,
// implemented twice, gets fixed once.
package registrychallenge

import (
	"fmt"
	"net/url"
	"strings"
)

// Allowlist maps a registry host to the additional hosts whose token realms
// that registry may direct credentials to.
//
// The zero value -- nil -- is the safe one: a registry's own host and nothing
// else. That is the right default because it is the shape most registries
// already have, and because the alternative default is "believe the header".
type Allowlist map[string][]string

// ParseAllowlist reads "registry-host=realm-host" entries.
//
// Deliberately hosts and not URLs on both sides. A URL invites a path, and a
// path in an allowlist reads as though it constrains something while the token
// endpoint's path is chosen by the realm regardless. The scheme is not
// configurable either: it is https.
func ParseAllowlist(entries []string) (Allowlist, error) {
	allowed := Allowlist{}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		registryHost, realmHost, found := strings.Cut(entry, "=")
		registryHost, realmHost = normalize(registryHost), normalize(realmHost)
		if !found || registryHost == "" || realmHost == "" {
			return nil, fmt.Errorf(
				"token realm %q must be registry-host=realm-host, for example registry-1.docker.io=auth.docker.io",
				entry)
		}
		for _, host := range []string{registryHost, realmHost} {
			// A port is legitimate -- a mirror on 127.0.0.1:5000 is a real
			// deployment. A slash means somebody wrote a URL and believes the
			// path is being checked, and a wildcard means somebody wrote
			// "anything", which is the state this package exists to leave.
			if strings.ContainsAny(host, "/*? @") {
				return nil, fmt.Errorf(
					"token realm entry %q names %q, which is not a bare host; write hosts, not URLs or patterns",
					entry, host)
			}
		}
		allowed[registryHost] = append(allowed[registryHost], realmHost)
	}
	if len(allowed) == 0 {
		return nil, nil
	}
	return allowed, nil
}

// TokenEndpoint reports the URL a Bearer challenge's credentials may be sent
// to, or refuses the challenge.
//
// registryHost is the host:port actually being contacted -- the one the caller
// chose, not one the response supplied.
//
// https is required whoever the host is, and no configuration relaxes it. The
// plaintext switch a deployment may set is about REACHING a registry that
// terminates TLS elsewhere; it is not a statement that a credential may be
// posted in the clear, and reading it as one is how a flag meant for an
// disconnected mirror ends up authorising a plaintext credential exchange.
func (allowed Allowlist) TokenEndpoint(realm, registryHost string) (*url.URL, error) {
	trimmed := strings.TrimSpace(realm)
	endpoint, err := url.Parse(trimmed)
	if err != nil || !endpoint.IsAbs() || endpoint.Host == "" {
		return nil, fmt.Errorf(
			"%s issued a token realm that is not an absolute URL (%q); refusing to send credentials to it",
			registryHost, trimmed)
	}
	if endpoint.User != nil {
		return nil, fmt.Errorf(
			"%s issued a token realm carrying its own credentials (%q); refusing to send credentials to it",
			registryHost, endpoint.Redacted())
	}
	if !strings.EqualFold(endpoint.Scheme, "https") {
		return nil, fmt.Errorf(
			"%s issued a token realm with scheme %q; credentials are exchanged over https only",
			registryHost, endpoint.Scheme)
	}
	if !allowed.permits(endpoint.Host, registryHost) {
		// The whole vulnerability, in one condition.
		return nil, fmt.Errorf(
			"%s issued a token realm on %q, which is neither its own host nor a realm host declared for it; "+
				"refusing to send its credentials elsewhere. Declare it in GITLESS_REGISTRY_TOKEN_REALMS "+
				"as %s=%s if that is genuinely where this registry issues tokens",
			registryHost, endpoint.Host, registryHost, endpoint.Hostname())
	}
	return endpoint, nil
}

// permits answers whether realmHost may be paid on behalf of registryHost.
//
// Every comparison is a whole-string match, case-insensitively, including the
// port. Not a suffix and not a substring: "registry.example" and
// "evil.registry.example" share a suffix, and "registry.example" and
// "registry.example.attacker.test" share a prefix, which is how a host rule
// written with HasSuffix or Contains becomes no rule at all.
func (allowed Allowlist) permits(realmHost, registryHost string) bool {
	if strings.EqualFold(strings.TrimSpace(realmHost), strings.TrimSpace(registryHost)) {
		return true
	}
	for _, declared := range allowed[normalize(registryHost)] {
		if strings.EqualFold(normalize(realmHost), declared) {
			return true
		}
	}
	return false
}

func normalize(host string) string {
	return strings.ToLower(strings.TrimSpace(host))
}

// WithScope returns endpoint with scope and service added, leaving any query
// the realm itself carried in place.
//
// Built through the query rather than concatenated onto the realm string.
// `realm + "?scope=..."` produces a URL naming neither when the realm already
// carries a query, which is a legal thing for a realm to do.
func WithScope(endpoint *url.URL, scope, service string) string {
	query := endpoint.Query()
	query.Set("scope", scope)
	if service != "" {
		query.Set("service", service)
	}
	withScope := *endpoint
	withScope.RawQuery = query.Encode()
	return withScope.String()
}
