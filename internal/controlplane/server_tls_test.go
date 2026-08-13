package controlplane

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"github.com/jonathanagustin/conduit/internal/identity"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

type fakeServerSPIFFESource struct {
	id          spiffeid.ID
	svid        *x509svid.SVID
	authorities []*x509.Certificate
}

func (source fakeServerSPIFFESource) GetX509SVID() (*x509svid.SVID, error) {
	if source.svid != nil {
		return source.svid, nil
	}
	return &x509svid.SVID{ID: source.id}, nil
}

func TestSPIFFETransportAuthenticatesAndRotatesWorkloadIdentity(t *testing.T) {
	authority, authorityKey := testSPIFFEAuthority(t)
	hubID := spiffeid.RequireFromString(HubSPIFFEID)
	agentID := spiffeid.RequireFromString("spiffe://platform.delivery/environment/dev/agent/spoke-dev")
	hubSource := fakeServerSPIFFESource{svid: testSPIFFESVID(t, hubID, authority, authorityKey), authorities: []*x509.Certificate{authority}}
	agentSource := fakeServerSPIFFESource{svid: testSPIFFESVID(t, agentID, authority, authorityKey), authorities: []*x509.Certificate{authority}}
	serverTLS, err := SPIFFEServerTLSConfig(hubSource)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		identity, identityErr := identity.AgentIdentityFromRequest(request, false)
		if identityErr != nil || identity.Environment != "dev" || identity.AgentID != "spoke-dev" {
			http.Error(w, "untrusted", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})}
	serverError := make(chan error, 1)
	go func() { serverError <- server.Serve(tls.NewListener(listener, serverTLS)) }()
	t.Cleanup(func() {
		_ = server.Close()
		<-serverError
	})
	clientTLS := tlsconfig.MTLSClientConfig(agentSource, agentSource, tlsconfig.AuthorizeID(hubID))
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS}, Timeout: 2 * time.Second}
	response, err := client.Get("https://" + listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("SPIFFE-authenticated request returned %d", response.StatusCode)
	}
}

func (source fakeServerSPIFFESource) GetX509BundleForTrustDomain(domain spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	return x509bundle.FromX509Authorities(domain, source.authorities), nil
}

func TestSPIFFEServerTLSConfigRequiresHubIdentityAndDynamicBundle(t *testing.T) {
	if _, err := SPIFFEServerTLSConfig(nil); err == nil {
		t.Fatal("nil SPIFFE source accepted")
	}
	if _, err := SPIFFEServerTLSConfig(fakeServerSPIFFESource{
		id: spiffeid.RequireFromString("spiffe://platform.delivery/other"),
	}); err == nil {
		t.Fatal("non-Hub SVID accepted")
	}
	authority, _ := testSPIFFEAuthority(t)
	source := &fakeServerSPIFFESource{
		id: spiffeid.RequireFromString(HubSPIFFEID), authorities: []*x509.Certificate{authority},
	}
	config, err := SPIFFEServerTLSConfig(source)
	if err != nil {
		t.Fatal(err)
	}
	if config.GetCertificate == nil || config.GetConfigForClient == nil || config.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Fatalf("unexpected SPIFFE TLS config: %#v", config)
	}
	current, err := config.GetConfigForClient(nil)
	if err != nil {
		t.Fatal(err)
	}
	// CertPool.Equal rather than counting Subjects, which is deprecated and only
	// ever asserted that the pool held one of something. This asserts it holds
	// exactly the authority the bundle published.
	expectedClientCAs := x509.NewCertPool()
	expectedClientCAs.AddCert(authority)
	if current.ClientCAs == nil || !current.ClientCAs.Equal(expectedClientCAs) || current.GetConfigForClient != nil {
		t.Fatalf("dynamic client trust bundle was not installed: %#v", current)
	}
	source.id = spiffeid.RequireFromString("spiffe://platform.delivery/other")
	if _, err := config.GetCertificate(nil); err == nil {
		t.Fatal("rotated non-Hub SVID was accepted")
	}
}

func testSPIFFEAuthority(t *testing.T) (*x509.Certificate, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "platform.delivery test CA"},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, publicKey, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, privateKey
}

func testSPIFFESVID(
	t *testing.T, id spiffeid.ID, authority *x509.Certificate, authorityKey ed25519.PrivateKey,
) *x509svid.SVID {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: id.String()},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		URIs: []*url.URL{id.URL()}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, authority, publicKey, authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return &x509svid.SVID{ID: id, Certificates: []*x509.Certificate{certificate, authority}, PrivateKey: privateKey}
}
