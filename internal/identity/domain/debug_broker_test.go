package domain

import (
	"crypto/x509"
	"net/url"
	"strings"
	"testing"
)

func brokerCertificate(t *testing.T, values ...string) *x509.Certificate {
	t.Helper()
	certificate := &x509.Certificate{}
	for _, value := range values {
		uri, err := url.Parse(value)
		if err != nil {
			t.Fatal(err)
		}
		certificate.URIs = append(certificate.URIs, uri)
	}
	return certificate
}
func TestDedicatedBrokerCredentialCannotBecomeOperatorOrDeliveryAgent(t *testing.T) {
	certificate := brokerCertificate(t, "spiffe://platform.delivery/environment/dev/debug-broker/debug-one")
	identity, err := DebugBrokerIdentityFromCertificate(certificate)
	if err != nil || identity.Environment != "dev" || identity.BrokerID != "debug-one" {
		t.Fatal(identity, err)
	}
	if _, err := OperatorIdentityFromCertificate(certificate); err == nil {
		t.Fatal("broker became operator")
	}
	if _, err := AgentIdentityFromCertificate(certificate); err == nil {
		t.Fatal("broker became delivery Agent")
	}
	for _, uri := range []string{"spiffe://platform.delivery/operator/alice", "spiffe://platform.delivery/environment/dev/agent/delivery"} {
		if _, err := DebugBrokerIdentityFromCertificate(brokerCertificate(t, uri)); err == nil {
			t.Fatal("other role became broker", uri)
		}
	}
}
func TestBrokerIdentityRejectsAmbiguousOrNoncanonicalBindings(t *testing.T) {
	valid := "spiffe://platform.delivery/environment/dev/debug-broker/debug-one"
	for _, value := range []string{
		"spiffe://other.example/environment/dev/debug-broker/debug-one",
		valid + "/", valid + "?grant=all", valid + "?", valid + "#fragment",
		"spiffe://operator@platform.delivery/environment/dev/debug-broker/debug-one",
		"spiffe://platform.delivery/environment/%64ev/debug-broker/debug-one",
		"spiffe://platform.delivery/environment/DEV/debug-broker/debug-one",
		"spiffe://platform.delivery/environment/dev/debug-broker/" + strings.Repeat("a", 64),
		"spiffe://platform.delivery/environment//debug-broker/debug-one",
	} {
		if _, err := DebugBrokerIdentityFromCertificate(brokerCertificate(t, value)); err == nil {
			t.Fatal("accepted ambiguous broker identity", value)
		}
	}
	for _, certificate := range []*x509.Certificate{nil, {}, {URIs: []*url.URL{nil}}, brokerCertificate(t, valid, valid), brokerCertificate(t, valid, "spiffe://platform.delivery/operator/alice")} {
		if _, err := DebugBrokerIdentityFromCertificate(certificate); err == nil {
			t.Fatal("accepted ambiguous SAN set")
		}
	}
}
