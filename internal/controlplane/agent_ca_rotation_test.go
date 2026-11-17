package controlplane

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The rehearsal behind docs/operations/key-rotation.md, "Agent CA".
//
// Each step is a real TLS handshake against a Hub handler configured the way
// cmd/platform-delivery configures it: AgentServerTLSConfig over the file
// AGENT_CLIENT_CA_FILE names, secure Agent identity, no insecure headers. A
// step starts a new server because the Hub reads that file once, at start --
// which is itself part of the procedure: changing the bundle takes a restart.

type rotationAuthority struct {
	certificate *x509.Certificate
	key         ed25519.PrivateKey
	pem         []byte
}

func newRotationAuthority(t *testing.T, name string) rotationAuthority {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
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
	return rotationAuthority{
		certificate: certificate, key: privateKey,
		pem: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

// issueAgent signs a client certificate carrying the Agent identity URI SAN,
// which is what a cert-manager Certificate with `uris:` produces.
func (authority rotationAuthority) issueAgent(t *testing.T, environment, agent string) tls.Certificate {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := url.Parse("spiffe://platform.delivery/environment/" + environment + "/agent/" + agent)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: agent},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour),
		URIs: []*url.URL{identity}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, authority.certificate, publicKey, authority.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: privateKey}
}

// hubTrusting starts a Hub whose AGENT_CLIENT_CA_FILE holds exactly these
// roots, and returns a function that polls for a lease with a client
// certificate. The answer is the HTTP status, or 0 when the Hub refused the
// connection's certificate outright.
func hubTrusting(t *testing.T, roots ...rotationAuthority) func(tls.Certificate) int {
	t.Helper()
	var bundle []byte
	for _, root := range roots {
		bundle = append(bundle, root.pem...)
	}
	path := filepath.Join(t.TempDir(), "agent-ca.pem")
	if err := os.WriteFile(path, bundle, 0o600); err != nil {
		t.Fatal(err)
	}
	config, err := AgentServerTLSConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(NewHandler(NewMemoryRepository(), HandlerOptions{}))
	server.TLS = config
	server.StartTLS()
	t.Cleanup(server.Close)
	return func(certificate tls.Certificate) int {
		t.Helper()
		client := server.Client()
		transport := client.Transport.(*http.Transport).Clone()
		// Offered unconditionally. Go's client otherwise withholds a certificate
		// whose issuer the server did not ask for, and the request would arrive
		// with none and be answered 401 by the handler -- a refusal, but the
		// client's rather than the Hub's. This makes the Hub do the refusing.
		transport.TLSClientConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &certificate, nil
		}
		transport.DisableKeepAlives = true
		response, err := (&http.Client{Transport: transport, Timeout: 10 * time.Second}).Post(
			server.URL+"/v1/environments/prod/leases", "application/json", strings.NewReader(`{}`))
		if err != nil {
			return 0
		}
		defer response.Body.Close()
		return response.StatusCode
	}
}

func TestAgentCARotationWithOverlapAdmitsBothRootsAndRetiringTheOldRootRefusesItsCertificates(t *testing.T) {
	oldRoot, newRoot := newRotationAuthority(t, "agent CA 2025"), newRotationAuthority(t, "agent CA 2026")
	oldAgent, newAgent := oldRoot.issueAgent(t, "prod", "primary"), newRoot.issueAgent(t, "prod", "primary")
	// An idle Environment answers a lease poll 204: the Agent was admitted.
	const admitted = http.StatusNoContent

	// Before rotation: only the old root is trusted.
	poll := hubTrusting(t, oldRoot)
	if got := poll(oldAgent); got != admitted {
		t.Fatalf("before rotation, the old root's Agent: %d, want %d", got, admitted)
	}
	if got := poll(newAgent); got != 0 {
		t.Fatalf("before rotation, a certificate from a root the Hub does not trust was answered %d", got)
	}

	// Overlap: the bundle carries both roots, so Agents can be moved to the new
	// CA one at a time while the rest keep working.
	poll = hubTrusting(t, oldRoot, newRoot)
	if got := poll(oldAgent); got != admitted {
		t.Fatalf("during overlap, the old root's Agent: %d, want %d", got, admitted)
	}
	if got := poll(newAgent); got != admitted {
		t.Fatalf("during overlap, the new root's Agent: %d, want %d", got, admitted)
	}

	// Retirement: the old root is removed. Its certificates stop working at the
	// handshake, whatever their own expiry says.
	poll = hubTrusting(t, newRoot)
	if got := poll(newAgent); got != admitted {
		t.Fatalf("after retirement, the new root's Agent: %d, want %d", got, admitted)
	}
	if got := poll(oldAgent); got != 0 {
		t.Fatalf("after retirement, the old root's Agent was answered %d; removing a root must refuse its certificates", got)
	}
}

func TestAnAgentCAFileThatHoldsNoCertificateIsRefusedAtStart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-ca.pem")
	if err := os.WriteFile(path, []byte("not a certificate\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AgentServerTLSConfig(path); err == nil {
		t.Fatal("a CA file with no certificate was accepted")
	}
	if _, err := AgentServerTLSConfig(filepath.Join(t.TempDir(), "missing.pem")); err == nil {
		t.Fatal("a missing CA file was accepted")
	}
}
