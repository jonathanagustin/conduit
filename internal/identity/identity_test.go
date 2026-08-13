package identity

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"net/url"
	"testing"
)

func TestAgentIdentityFromCertificate(t *testing.T) {
	t.Parallel()
	identityURI, err := url.Parse("spiffe://platform.delivery/environment/dev-use2/agent/spoke-dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := AgentIdentityFromCertificate(&x509.Certificate{URIs: []*url.URL{identityURI}})
	if err != nil {
		t.Fatal(err)
	}
	if agent.Environment != "dev-use2" || agent.AgentID != "spoke-dev-use2" {
		t.Fatalf("unexpected identity: %#v", agent)
	}
}

func TestOperatorIdentityRequiresDistinctSPIFFEIdentity(t *testing.T) {
	t.Parallel()
	identityURI, err := url.Parse("spiffe://platform.delivery/operator/reviewer%40example.test")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := OperatorIdentityFromCertificate(&x509.Certificate{
		URIs: []*url.URL{identityURI}, Subject: pkix.Name{OrganizationalUnit: []string{"release-reviewers", "security"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if claims.Username != "reviewer@example.test" || len(claims.Groups) != 2 {
		t.Fatalf("claims=%#v", claims)
	}
	if _, err := OperatorIdentityFromCertificate(testAgentCertificate(t, "dev", "spoke-dev")); err == nil {
		t.Fatal("Agent identity was accepted as an operator")
	}
}

func TestAgentIdentityRejectsWrongTrustDomain(t *testing.T) {
	t.Parallel()
	identityURI, err := url.Parse("spiffe://example.invalid/environment/dev-use2/agent/spoke-dev-use2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AgentIdentityFromCertificate(&x509.Certificate{URIs: []*url.URL{identityURI}}); err == nil {
		t.Fatal("expected an untrusted URI SAN to be rejected")
	}
}

// A minimal agent certificate. Duplicated from the control plane's own test
// rather than shared: a test helper crossing a package boundary is a dependency
// the boundary exists to prevent, and it is ten lines.
func testAgentCertificate(t *testing.T, environment, agentID string) *x509.Certificate {
	t.Helper()
	identityURI, err := url.Parse(
		"spiffe://platform.delivery/environment/" + environment + "/agent/" + agentID,
	)
	if err != nil {
		t.Fatal(err)
	}
	return &x509.Certificate{URIs: []*url.URL{identityURI}}
}
