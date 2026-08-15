// Package httpidentity reads a credential off an HTTP request and hands it to
// Identity.
//
// This is the inbound adapter and the only place that knows a credential can
// arrive as an Authorization header, a verified TLS chain, or — behind an
// explicit development opt-in — a plain header. What any of them then has to
// prove is the domain's, so nothing here decides who gets in.
package httpidentity

import (
	"net/http"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
	"github.com/jonathanagustin/conduit/internal/identity/ports"
)

// AgentIdentityFromRequest reads the Agent's stable identity from its verified
// client certificate. The certificate authority, not a request header, binds an
// Agent to exactly one Environment.
//
// The header path exists for local development and is reachable only when the
// caller has explicitly opted in; it is never a fallback for a certificate that
// failed to verify, because that would make a rejected certificate as good as
// none.
func AgentIdentityFromRequest(request *http.Request, allowInsecureHeaders bool) (domain.AgentIdentity, error) {
	if request.TLS != nil && len(request.TLS.VerifiedChains) > 0 && len(request.TLS.PeerCertificates) > 0 {
		return domain.AgentIdentityFromCertificate(request.TLS.PeerCertificates[0])
	}
	if allowInsecureHeaders {
		environment := strings.TrimSpace(request.Header.Get("X-Platform-Environment-ID"))
		agentID := strings.TrimSpace(request.Header.Get("X-Platform-Agent-ID"))
		if !domain.DNSLabelPattern.MatchString(environment) || !domain.DNSLabelPattern.MatchString(agentID) {
			return domain.AgentIdentity{}, errInvalidDevelopmentHeaders
		}
		return domain.AgentIdentity{Environment: environment, AgentID: agentID}, nil
	}
	return domain.AgentIdentity{}, errClientCertificateRequired
}

// OperatorResolver turns a request into operator claims: it finds the
// credential, has the verifier establish that a token is authentic, and lets
// the policy decide whether that token is an operator here.
//
// Now is injected so the token-freshness rule can be tested at a fixed moment.
func OperatorResolver(
	policy domain.ClaimPolicy, verifier ports.TokenVerifier, now func() time.Time,
) func(*http.Request) (domain.Claims, error) {
	if now == nil {
		now = time.Now
	}
	return func(request *http.Request) (domain.Claims, error) {
		if request == nil || verifier == nil {
			return domain.Claims{}, domain.ErrUnauthorized
		}
		// A verified client certificate identifies an operator only when no
		// token was offered. A request carrying both is not two credentials to
		// choose the more convenient of.
		if len(request.Header.Values("Authorization")) == 0 {
			if request.TLS != nil && len(request.TLS.VerifiedChains) > 0 && len(request.TLS.VerifiedChains[0]) > 0 {
				return domain.OperatorIdentityFromCertificate(request.TLS.VerifiedChains[0][0])
			}
			return domain.Claims{}, domain.ErrUnauthorized
		}
		token, err := exactBearerToken(request.Header)
		// The length is checked before verification so that an oversized token
		// is refused without being parsed.
		if err != nil || len(token) > policy.MaximumTokenBytes {
			return domain.Claims{}, domain.ErrUnauthorized
		}
		verified, raw, err := verifier.Verify(request.Context(), token)
		if err != nil {
			return domain.Claims{}, domain.ErrUnauthorized
		}
		return policy.OperatorClaims(verified, raw, now())
	}
}

// exactBearerToken requires exactly one Authorization header carrying exactly
// one bearer token. More than one header, or a value that could be read as two
// credentials, is refused rather than resolved by picking one.
func exactBearerToken(header http.Header) (string, error) {
	values := header.Values("Authorization")
	if len(values) != 1 {
		return "", domain.ErrUnauthorized
	}
	parts := strings.Fields(values[0])
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.ContainsAny(parts[1], ",\r\n") {
		return "", domain.ErrUnauthorized
	}
	return parts[1], nil
}
