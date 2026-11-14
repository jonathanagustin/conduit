package domain

import (
	"crypto/x509"
	"errors"
	"strings"
)

// DebugBrokerIdentity identifies a separate outbound debug broker. Parsing an
// identity supplies no namespace grant, image admission or execution authority.
// CONDUIT-INVENTED: identity.DebugBrokerIdentity
// Displaced-by: a reviewed replacement debug-broker credential identity and enrollment contract.
type DebugBrokerIdentity struct {
	Environment string
	BrokerID    string
}

// DebugBrokerIdentityFromCertificate parses a certificate whose trust chain
// must already have been verified by the transport. The credential has one URI
// SAN with a distinct role; delivery-Agent/operator aliases and ambiguous SANs
// are rejected. A certificate's common name and request headers grant nothing.
func DebugBrokerIdentityFromCertificate(certificate *x509.Certificate) (DebugBrokerIdentity, error) {
	invalid := errors.New("verified debug-broker certificate with one dedicated URI SAN is required")
	if certificate == nil || len(certificate.URIs) != 1 {
		return DebugBrokerIdentity{}, invalid
	}
	uri := certificate.URIs[0]
	if uri == nil || uri.Scheme != "spiffe" || uri.Host != agentIdentityTrustDomain || uri.User != nil || uri.RawQuery != "" || uri.ForceQuery || uri.Fragment != "" || uri.Opaque != "" {
		return DebugBrokerIdentity{}, invalid
	}
	parts := strings.Split(uri.EscapedPath(), "/")
	if len(parts) != 5 || parts[0] != "" || parts[1] != "environment" || parts[3] != "debug-broker" {
		return DebugBrokerIdentity{}, invalid
	}
	environment, broker := parts[2], parts[4]
	if len(environment) > 63 || len(broker) > 63 || !DNSLabelPattern.MatchString(environment) || !DNSLabelPattern.MatchString(broker) {
		return DebugBrokerIdentity{}, invalid
	}
	return DebugBrokerIdentity{Environment: environment, BrokerID: broker}, nil
}
