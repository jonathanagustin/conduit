// Package domain translates a credential into the principal a request acts as.
//
// It answers one question — who is this — and deliberately not the next one,
// which is what they may do. Authentication and authorization drifting into one
// package is how a certificate's mere validity comes to imply a grant.
//
// Everything here is decidable from a certificate, a policy and a token's
// claims. How a token is verified against an issuer's keys, and how a
// credential arrives over HTTP, are adapters' work: this package must not
// import net/http, and the boundary gate enforces that.
//
// The claims it mints travel with it: a package that produces an identity but
// cannot name its own type is not a context.
package domain

import (
	"crypto/x509"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const agentIdentityTrustDomain = "platform.delivery"

type AgentIdentity struct {
	Environment string
	AgentID     string
}

func OperatorIdentityFromCertificate(certificate *x509.Certificate) (Claims, error) {
	if certificate == nil {
		return Claims{}, fmt.Errorf("client certificate is required")
	}
	for _, identityURI := range certificate.URIs {
		if identityURI.Scheme != "spiffe" || identityURI.Host != agentIdentityTrustDomain {
			continue
		}
		parts := strings.Split(strings.Trim(identityURI.EscapedPath(), "/"), "/")
		if len(parts) != 2 || parts[0] != "operator" {
			continue
		}
		subject, err := url.PathUnescape(parts[1])
		if err != nil || strings.TrimSpace(subject) == "" || strings.ContainsAny(subject, "\x00/\\") {
			return Claims{}, fmt.Errorf("operator certificate URI SAN contains an invalid subject")
		}
		return Claims{Username: subject, Groups: normalizedStrings(certificate.Subject.OrganizationalUnit)}, nil
	}
	return Claims{}, fmt.Errorf(
		"operator certificate requires URI SAN spiffe://%s/operator/<subject>", agentIdentityTrustDomain,
	)
}

// AgentIdentityFromCertificate reads the stable identity from a verified
// client certificate URI SAN. The certificate authority, not a request
// header, binds an Agent to exactly one Environment.
func AgentIdentityFromCertificate(certificate *x509.Certificate) (AgentIdentity, error) {
	if certificate == nil {
		return AgentIdentity{}, fmt.Errorf("client certificate is required")
	}
	for _, identityURI := range certificate.URIs {
		if identityURI.Scheme != "spiffe" || identityURI.Host != agentIdentityTrustDomain {
			continue
		}
		parts := strings.Split(strings.Trim(identityURI.EscapedPath(), "/"), "/")
		if len(parts) != 4 || parts[0] != "environment" || parts[2] != "agent" {
			continue
		}
		environment := strings.TrimSpace(parts[1])
		agentID := strings.TrimSpace(parts[3])
		if !DNSLabelPattern.MatchString(environment) || !DNSLabelPattern.MatchString(agentID) {
			return AgentIdentity{}, fmt.Errorf("agent certificate URI SAN contains an invalid Environment or Agent ID")
		}
		return AgentIdentity{Environment: environment, AgentID: agentID}, nil
	}
	return AgentIdentity{}, fmt.Errorf(
		"client certificate requires URI SAN spiffe://%s/environment/<environment>/agent/<agent>",
		agentIdentityTrustDomain,
	)
}

// ErrUnauthorized is what a credential that does not establish an identity
// produces. It lives here because "we do not know who this is" is Identity's
// answer; "you may not" is Governance's and is a different error.
var ErrUnauthorized = errors.New("unauthorized")

type Claims struct {
	Username       string   `json:"username"`
	Groups         []string `json:"groups,omitempty"`
	ServiceAccount bool     `json:"serviceAccount,omitempty"`
	OnBehalfOf     string   `json:"onBehalfOf,omitempty"`
}

// DNSLabelPattern is a DNS label, restated rather than imported. The pattern is two lines and
// importing the control plane for it would undo the point of this package;
// internal/agent already keeps its own for the same reason.
var DNSLabelPattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9]*[a-z0-9])?$`)

// normalizedStrings trims, de-duplicates and orders a claim's values so two
// certificates carrying the same groups in a different order mint the same
// identity.
func normalizedStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = struct{}{}
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
