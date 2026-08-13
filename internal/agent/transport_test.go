package agent

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

type fakeSPIFFESource struct{}

func (fakeSPIFFESource) GetX509SVID() (*x509svid.SVID, error) {
	return &x509svid.SVID{ID: spiffeid.RequireFromString("spiffe://platform.delivery/environment/dev/agent/spoke-dev")}, nil
}

func (fakeSPIFFESource) GetX509BundleForTrustDomain(spiffeid.TrustDomain) (*x509bundle.Bundle, error) {
	return nil, nil
}

func TestSPIFFEControlPlaneTransportRequiresExactHubIdentity(t *testing.T) {
	if _, err := NewSPIFFEControlPlaneHTTPClient(nil, "spiffe://platform.delivery/hub"); err == nil {
		t.Fatal("nil SPIFFE source accepted")
	}
	if _, err := NewSPIFFEControlPlaneHTTPClient(fakeSPIFFESource{}, "spiffe://platform.delivery/other"); err == nil {
		t.Fatal("non-Hub SPIFFE identity accepted")
	}
	client, err := NewSPIFFEControlPlaneHTTPClient(fakeSPIFFESource{}, "spiffe://platform.delivery/hub")
	if err != nil {
		t.Fatal(err)
	}
	if client.Transport == nil {
		t.Fatal("SPIFFE transport was not configured")
	}
	// Without a timeout, a Hub that accepts the connection and never answers
	// holds this Agent's request open for as long as the process lives.
	if client.Timeout != 30*time.Second {
		t.Errorf("SPIFFE control plane client timeout = %v, want 30s", client.Timeout)
	}
	// The proxy is taken from the environment rather than left at the clone's
	// default, so an Environment behind an egress proxy can reach the Hub.
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport = %T, want *http.Transport", client.Transport)
	}
	if transport.Proxy == nil {
		t.Error("transport has no proxy resolver; an egress-proxied Environment cannot reach the Hub")
	}
	// Asserting the config is merely non-nil proves nothing: Clone() always
	// produces one. What makes this SPIFFE mTLS rather than ordinary TLS is
	// that the client presents an SVID and verifies the peer itself, which is
	// why InsecureSkipVerify is set alongside a custom verifier.
	tlsConfig := transport.TLSClientConfig
	if tlsConfig == nil {
		t.Fatal("transport carries no TLS configuration")
	}
	if tlsConfig.GetClientCertificate == nil {
		t.Error("no client certificate source: the Agent would connect without presenting its SVID")
	}
	if tlsConfig.VerifyPeerCertificate == nil {
		t.Error("no peer verifier: the Hub's SPIFFE identity would not be checked")
	}
	if !tlsConfig.InsecureSkipVerify {
		t.Error("chain verification was left on, which rejects SVIDs the custom verifier is there to accept")
	}
}

// writeCertificate produces a self-signed certificate and key on disk, which
// is what these constructors load. Generating one is the only way to exercise
// the loading paths at all -- a fixture PEM would go stale.
func writeCertificate(t *testing.T) (caPath, certPath, keyPath string) {
	t.Helper()
	directory := t.TempDir()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "platform-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encodedKey, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	caPath = filepath.Join(directory, "ca.pem")
	certPath = filepath.Join(directory, "client.crt")
	keyPath = filepath.Join(directory, "client.key")
	certificatePEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	for _, path := range []string{caPath, certPath} {
		if err := os.WriteFile(path, certificatePEM, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: encodedKey}), 0o600); err != nil {
		t.Fatal(err)
	}
	return caPath, certPath, keyPath
}

// NewControlPlaneHTTPClient is the non-SPIFFE Agent transport, and none of it
// was reached. Every branch here is a way of ending up with a client that
// trusts the wrong thing or presents nothing.
func TestNewControlPlaneHTTPClientLoadsMaterialOrRefusesIt(t *testing.T) {
	t.Parallel()
	caPath, certPath, keyPath := writeCertificate(t)

	t.Run("a certificate and key must be configured together", func(t *testing.T) {
		t.Parallel()
		// Half a pair silently produces a client that presents nothing, and
		// the Hub rejects it with an error that looks like a server problem.
		if _, err := NewControlPlaneHTTPClient("", certPath, ""); err == nil {
			t.Error("a certificate with no key was accepted")
		}
		if _, err := NewControlPlaneHTTPClient("", "", keyPath); err == nil {
			t.Error("a key with no certificate was accepted")
		}
		// Neither is valid: mTLS is optional.
		if _, err := NewControlPlaneHTTPClient("", "", ""); err != nil {
			t.Errorf("a client with no mTLS material was refused: %v", err)
		}
	})

	t.Run("whitespace-only paths count as unset", func(t *testing.T) {
		t.Parallel()
		if _, err := NewControlPlaneHTTPClient("  ", "  ", "  "); err != nil {
			t.Errorf("whitespace paths were treated as configured: %v", err)
		}
	})

	t.Run("a CA is loaded and a bad one is refused", func(t *testing.T) {
		t.Parallel()
		client, err := NewControlPlaneHTTPClient(caPath, "", "")
		if err != nil {
			t.Fatal(err)
		}
		if client.Transport.(*http.Transport).TLSClientConfig.RootCAs == nil {
			t.Error("the configured CA was not installed as a root")
		}
		// A file that is not PEM at all leaves an empty pool, which would
		// otherwise trust nothing and fail every probe at connect time.
		notPEM := filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(notPEM, []byte("not a certificate"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := NewControlPlaneHTTPClient(notPEM, "", ""); err == nil {
			t.Error("a CA file with no certificates was accepted")
		}
		if _, err := NewControlPlaneHTTPClient(filepath.Join(t.TempDir(), "absent.pem"), "", ""); err == nil {
			t.Error("a missing CA file was accepted")
		}
	})

	t.Run("a client certificate is loaded and a broken pair is refused", func(t *testing.T) {
		t.Parallel()
		client, err := NewControlPlaneHTTPClient(caPath, certPath, keyPath)
		if err != nil {
			t.Fatal(err)
		}
		tlsConfig := client.Transport.(*http.Transport).TLSClientConfig
		if len(tlsConfig.Certificates) != 1 {
			t.Error("the configured client certificate was not presented")
		}
		// TLS 1.2 is the floor; anything lower is not negotiated at all.
		if tlsConfig.MinVersion != tls.VersionTLS12 {
			t.Errorf("minimum TLS version = %x, want TLS 1.2", tlsConfig.MinVersion)
		}
		if _, err := NewControlPlaneHTTPClient("", certPath, caPath); err == nil {
			t.Error("a certificate and a non-matching key were accepted")
		}
	})

	t.Run("the client is bounded and proxy-aware", func(t *testing.T) {
		t.Parallel()
		client, err := NewControlPlaneHTTPClient("", "", "")
		if err != nil {
			t.Fatal(err)
		}
		if client.Timeout != 30*time.Second {
			t.Errorf("timeout = %v, want 30s", client.Timeout)
		}
		if client.Transport.(*http.Transport).Proxy == nil {
			t.Error("no proxy resolver: an egress-proxied Environment cannot reach the Hub")
		}
	})
}

// The health probe client talks to Pods rather than the Hub, and its timeout
// is deliberately much shorter: a probe that hangs delays every other target
// in the same polling round.
func TestNewHealthProbeHTTPClientVerifiesAndIsBoundedTightly(t *testing.T) {
	t.Parallel()
	caPath, _, _ := writeCertificate(t)

	client, err := NewHealthProbeHTTPClient(caPath)
	if err != nil {
		t.Fatal(err)
	}
	if client.Timeout != 4*time.Second {
		t.Errorf("timeout = %v, want 4s", client.Timeout)
	}
	tlsConfig := client.Transport.(*http.Transport).TLSClientConfig
	if tlsConfig.RootCAs == nil {
		t.Error("the Environment's CA was not installed")
	}
	// Verification is never disabled, whatever the Environment mounts.
	if tlsConfig.InsecureSkipVerify {
		t.Error("health probe verification was disabled")
	}
	if tlsConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("minimum TLS version = %x, want TLS 1.2", tlsConfig.MinVersion)
	}

	if _, err := NewHealthProbeHTTPClient(""); err != nil {
		t.Errorf("a probe client with no private CA was refused: %v", err)
	}
	notPEM := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(notPEM, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewHealthProbeHTTPClient(notPEM); err == nil {
		t.Error("a CA file with no certificates was accepted")
	}
}
