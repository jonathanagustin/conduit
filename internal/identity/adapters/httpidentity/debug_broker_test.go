package httpidentity_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/identity/adapters/httpidentity"
)

type brokerTestCA struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
}

func newBrokerTestCA(t *testing.T) brokerTestCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "debug test CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return brokerTestCA{certificate, key}
}
func (ca brokerTestCA) client(t *testing.T, uri string, expiry time.Time, usage x509.ExtKeyUsage) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "not authority"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: expiry, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}, URIs: []*url.URL{identity}}
	raw, err := x509.CreateCertificate(rand.Reader, template, ca.certificate, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{raw}, PrivateKey: key, Leaf: leaf}
}
func TestDebugBrokerIdentityRequiresRealVerifiedClientTLS(t *testing.T) {
	ca := newBrokerTestCA(t)
	stranger := newBrokerTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca.certificate)
	var entered atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered.Add(1)
		identity, err := httpidentity.DebugBrokerIdentityFromRequest(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if identity.Environment != "dev" || identity.BrokerID != "debug-one" {
			t.Errorf("wrong identity: %#v", identity)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}
	server.StartTLS()
	defer server.Close()
	validURI := "spiffe://platform.delivery/environment/dev/debug-broker/debug-one"
	valid := ca.client(t, validURI, time.Now().Add(time.Hour), x509.ExtKeyUsageClientAuth)
	cases := []struct {
		name          string
		certificate   *tls.Certificate
		authorization string
		want          int
		handshake     bool
	}{
		{name: "valid broker", certificate: &valid, want: http.StatusNoContent},
		{name: "no client certificate", handshake: true},
		{name: "untrusted issuer", certificate: certificatePointer(stranger.client(t, validURI, time.Now().Add(time.Hour), x509.ExtKeyUsageClientAuth)), handshake: true},
		{name: "expired", certificate: certificatePointer(ca.client(t, validURI, time.Now().Add(-time.Minute), x509.ExtKeyUsageClientAuth)), handshake: true},
		{name: "server only certificate", certificate: certificatePointer(ca.client(t, validURI, time.Now().Add(time.Hour), x509.ExtKeyUsageServerAuth)), handshake: true},
		{name: "delivery agent", certificate: certificatePointer(ca.client(t, "spiffe://platform.delivery/environment/dev/agent/delivery", time.Now().Add(time.Hour), x509.ExtKeyUsageClientAuth)), want: http.StatusUnauthorized},
		{name: "operator", certificate: certificatePointer(ca.client(t, "spiffe://platform.delivery/operator/alice", time.Now().Add(time.Hour), x509.ExtKeyUsageClientAuth)), want: http.StatusUnauthorized},
		{name: "two credentials", certificate: &valid, authorization: "Bearer ignored", want: http.StatusUnauthorized},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			transport := server.Client().Transport.(*http.Transport).Clone()
			// Force presentation even for an untrusted issuer, exercising server verification.
			transport.TLSClientConfig.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				if test.certificate == nil {
					return &tls.Certificate{}, nil
				}
				return test.certificate, nil
			}
			client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
			defer transport.CloseIdleConnections()
			request, err := http.NewRequest(http.MethodGet, server.URL, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("X-Platform-Environment-ID", "other")
			request.Header.Set("X-Platform-Agent-ID", "forged")
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			before := entered.Load()
			response, err := client.Do(request)
			if test.handshake {
				if err == nil {
					response.Body.Close()
					t.Fatal("TLS accepted invalid client")
				}
				if entered.Load() != before {
					t.Fatal("invalid TLS reached handler")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != test.want {
				t.Fatalf("status %d; want %d", response.StatusCode, test.want)
			}
		})
	}
}
func certificatePointer(certificate tls.Certificate) *tls.Certificate { return &certificate }

func TestDebugBrokerRejectsUnverifiedOrInconsistentRequestState(t *testing.T) {
	ca := newBrokerTestCA(t)
	valid := ca.client(t, "spiffe://platform.delivery/environment/dev/debug-broker/debug-one", time.Now().Add(time.Hour), x509.ExtKeyUsageClientAuth).Leaf
	other := ca.client(t, "spiffe://platform.delivery/environment/dev/debug-broker/other", time.Now().Add(time.Hour), x509.ExtKeyUsageClientAuth).Leaf
	for _, state := range []*tls.ConnectionState{
		nil, {},
		{PeerCertificates: []*x509.Certificate{valid}},
		{VerifiedChains: [][]*x509.Certificate{{valid, ca.certificate}}},
		{PeerCertificates: []*x509.Certificate{nil}, VerifiedChains: [][]*x509.Certificate{{valid, ca.certificate}}},
		{PeerCertificates: []*x509.Certificate{valid}, VerifiedChains: [][]*x509.Certificate{{}}},
		{PeerCertificates: []*x509.Certificate{valid}, VerifiedChains: [][]*x509.Certificate{{nil}}},
		{PeerCertificates: []*x509.Certificate{valid}, VerifiedChains: [][]*x509.Certificate{{other, ca.certificate}}},
		{PeerCertificates: []*x509.Certificate{valid}, VerifiedChains: [][]*x509.Certificate{{valid, ca.certificate}, {other, ca.certificate}}},
	} {
		request := httptest.NewRequest(http.MethodGet, "https://broker.test", nil)
		request.TLS = state
		request.Header.Set("X-Platform-Environment-ID", "dev")
		request.Header.Set("X-Platform-Agent-ID", "debug-one")
		if _, err := httpidentity.DebugBrokerIdentityFromRequest(request); err == nil {
			t.Fatal("accepted unverified/inconsistent transport state")
		}
	}
	if _, err := httpidentity.DebugBrokerIdentityFromRequest(nil); err == nil {
		t.Fatal("accepted nil request")
	}
}
