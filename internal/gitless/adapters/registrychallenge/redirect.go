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
// The rule STRIPS the credential and follows the redirect. It first refused
// instead, which was wrong in a way no test caught and no reasoning about the
// token exchange could have caught: the policy sits on the client, so it also
// governed credentialed BLOB fetches, and Docker Hub and ghcr.io both answer
// those with a cross-host redirect to a CDN. Refusing broke every read against
// a CDN-backed registry. CheckRedirect below carries the measurement.
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

// CheckRedirect is an http.Client redirect policy that will not let a
// credentialed request carry its credential off the host it was addressed to,
// or down from https to http. It strips the credential and follows; it does not
// refuse.
//
// It is shaped to be assigned directly: client.CheckRedirect =
// registrychallenge.CheckRedirect. via[0] is the request the caller actually
// made, which is the only host and scheme this package ever authorised.
//
// Stripping rather than refusing, because refusing broke real pulls. This policy
// is set on the CLIENT, so it governs every request the resolver and publisher
// make -- not just the token exchange it was reasoned about. Blob fetches are
// credentialed too, and mainstream registries answer them with a cross-host
// redirect to a CDN carrying a pre-signed URL. Measured, not assumed:
//
//	GET https://registry-1.docker.io/v2/library/alpine/blobs/sha256:79ff19e9...
//	  Authorization: Bearer ...
//	-> 307 location: https://production.cloudfront.docker.com/registry-v2/...
//	     ...?Expires=...&Signature=...&Key-Pair-Id=...
//
// ghcr.io does the same, to pkg-containers.githubusercontent.com. A policy that
// returned an error here failed ocipublish.CurrentDigest -- the read the Flux
// converger performs every reconcile -- against any CDN-backed registry, which
// is the ordinary deployment rather than an exotic one.
//
// Stripping keeps the whole security property and costs nothing. The credential
// still reaches only the exact origin it was addressed to; the request still
// completes, because a pre-signed CDN URL carries its own authorisation in the
// query string and never wanted the header. This is also what the standard
// library already does across domains -- shouldCopyHeaderOnRedirect drops
// Authorization -- so the cross-host row is unchanged from net/http, and the
// two rows net/http gets wrong (a SUBDOMAIN, and a same-host scheme downgrade)
// are brought into line with it rather than turned into failures.
//
// The alternative considered and rejected: refuse the scheme-downgrade row,
// since no registry needs it. That would leave two rules where one will do, and
// buy nothing -- a stripped credential is already safe from the passive observer
// that row exposes.
//
// A request carrying no Authorization header is left alone. That is deliberate
// rather than lax: it has no credential to lose, and constraining it would
// break anonymous pulls to buy nothing.
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
	if carriesCredentialSafely(request, original) {
		return nil
	}
	// net/http fills the redirect's headers by copying the original's, and only
	// then calls this policy, so deleting here is what actually goes on the
	// wire. Delete rather than blank: an empty Authorization header is still a
	// header, and it tells the next host that one was being carried.
	request.Header.Del("Authorization")
	return nil
}

// carriesCredentialSafely reports whether the redirect stays at the exact origin
// the credential was addressed to.
//
// Whole-string and case-insensitive on the host, including the port, for the
// reason permits spells out: "registry.example" and "evil.registry.example"
// share a suffix, and a suffix comparison here is what would hand the credential
// to the subdomain. The standard library's own rule IS the permissive one -- it
// admits subdomains on purpose, for browsers.
//
// The scheme is checked separately because the standard library does not check
// it at all: same host over http is, to net/http, the same host.
func carriesCredentialSafely(request *http.Request, original *http.Request) bool {
	if !strings.EqualFold(strings.TrimSpace(request.URL.Host), strings.TrimSpace(original.URL.Host)) {
		return false
	}
	return !strings.EqualFold(original.URL.Scheme, "https") ||
		strings.EqualFold(request.URL.Scheme, "https")
}
