package registrychallenge

import (
	"fmt"
	"net/http"
	"strings"
)

// This file is the second half of TokenEndpoint's rule, and it exists because
// the first half alone is not the promise the package makes.
//
// TokenEndpoint decides which host a realm may NAME. Nothing in it decides
// where that host may then REDIRECT, and Go's http.Client follows a 302 by
// default. Measured against the shipped resolver and publisher, a realm that is
// permitted -- the registry's own host -- could answer the token request with a
// redirect and move the credential:
//
//	-> https://attacker.example/steal      cross-host      credential stripped
//	-> https://evil.registry.example/steal subdomain       credential FORWARDED
//	-> http://registry.example/steal       same host, http credential FORWARDED
//
// Only the first row was safe, and it was safe because of the standard library
// rather than because of anything here: net/http's shouldCopyHeaderOnRedirect
// keeps Authorization for a redirect to the same domain or a SUBDOMAIN of it,
// and compares hostnames while ignoring the scheme entirely. That is precisely
// why rows two and three leaked. Both falsify something stated out loud:
// challenge_test.go names https://evil.registry.example/token as a host that
// must never receive the credential, and TokenEndpoint's own comment says https
// is required with nothing relaxing it.
//
// The materiality is honest and small: in every row the redirecting host
// already holds the credential legitimately, so this reaches a new principal
// only where that host has an attacker-controlled subdomain -- wildcard DNS, a
// subdomain takeover, a multi-tenant *.registry.example -- and row three's real
// exposure is to a passive network observer. It is fixed because the promise
// was absolute, and because row one's protection is inherited: a change in the
// standard library would remove it silently, so the test pins that row here.
//
// The rule lives beside TokenEndpoint rather than in each adapter for the same
// reason permits does: a rule about where a credential may go, implemented
// twice, drifts.

// redirectLimit is the standard library's own cap, restated because assigning
// CheckRedirect replaces the default policy that enforced it. A same-host
// https redirect loop is legitimate under this rule and would otherwise spin.
const redirectLimit = 10

// WithRedirectPolicy returns a copy of client whose redirect policy is
// CheckRedirect. A nil client yields one with no timeout, which is the
// http.Client zero value the caller would otherwise have got.
//
// A copy, not a mutation, because the client belongs to the caller: the
// composition root hands the same *http.Client shape to several adapters, and
// an adapter that reaches into it changes something it does not own. The copy
// shares the Transport, which is the part that holds connections and is meant
// to be shared.
func WithRedirectPolicy(client *http.Client) *http.Client {
	guarded := &http.Client{}
	if client != nil {
		copied := *client
		guarded = &copied
	}
	guarded.CheckRedirect = CheckRedirect
	return guarded
}

// CheckRedirect is an http.Client redirect policy that refuses to let a
// credentialed request follow a redirect off the host it was addressed to, or
// down from https to http.
//
// It is shaped to be assigned directly: client.CheckRedirect =
// registrychallenge.CheckRedirect. via[0] is the request the caller actually
// made, which is the only host and scheme this package ever authorised.
//
// A request carrying no Authorization header is left alone. That is deliberate
// rather than lax: it has no credential to lose, and registries legitimately
// redirect uncredentialed traffic to a CDN on another host, so constraining it
// would break pulls to buy nothing. What must not happen is a credential
// arriving somewhere TokenEndpoint never approved.
func CheckRedirect(request *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	if len(via) >= redirectLimit {
		return fmt.Errorf("stopped after %d redirects", redirectLimit)
	}
	original := via[0]
	if original.Header.Get("Authorization") == "" {
		return nil
	}
	// Whole-string and case-insensitive, including the port, for the reason
	// permits spells out: "registry.example" and "evil.registry.example" share
	// a suffix, and it is a suffix comparison here that would forward the
	// credential to the subdomain. The standard library's own rule IS the
	// permissive one -- it admits subdomains on purpose, for browsers.
	if !strings.EqualFold(strings.TrimSpace(request.URL.Host), strings.TrimSpace(original.URL.Host)) {
		return fmt.Errorf(
			"%s redirected an authenticated request to %s; a credential is sent to the host it was "+
				"addressed to and nowhere a redirect names",
			original.URL.Host, request.URL.Redacted())
	}
	// Scheme is checked separately because the standard library does not check
	// it at all: same host over http is, to net/http, the same host.
	if strings.EqualFold(original.URL.Scheme, "https") && !strings.EqualFold(request.URL.Scheme, "https") {
		return fmt.Errorf(
			"%s redirected an authenticated request to %s; a credential is not followed from https down to %s",
			original.URL.Host, request.URL.Redacted(), request.URL.Scheme)
	}
	return nil
}
