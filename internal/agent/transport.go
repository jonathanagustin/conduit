package agent

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

type SPIFFEX509Source interface {
	x509svid.Source
	x509bundle.Source
}

// NewControlPlaneHTTPClient creates the outbound-only Agent transport. It uses
// the standard HTTPS_PROXY/NO_PROXY environment contract and optionally loads
// a private Hub CA and per-Environment mTLS certificate.
func NewControlPlaneHTTPClient(caFile, certificateFile, keyFile string) (*http.Client, error) {
	caFile = strings.TrimSpace(caFile)
	certificateFile = strings.TrimSpace(certificateFile)
	keyFile = strings.TrimSpace(keyFile)
	if (certificateFile == "") != (keyFile == "") {
		return nil, fmt.Errorf("control plane client certificate and key must be configured together")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		contents, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read control plane CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(contents) {
			return nil, fmt.Errorf("control plane CA file contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	if certificateFile != "" {
		certificate, err := tls.LoadX509KeyPair(certificateFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("load control plane client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

// NewSPIFFEControlPlaneHTTPClient authenticates both peers with automatically
// rotated X509-SVIDs from the SPIFFE Workload API. The Hub identity is exact,
// not merely any member of the trust domain.
func NewSPIFFEControlPlaneHTTPClient(source SPIFFEX509Source, expectedHubID string) (*http.Client, error) {
	if source == nil {
		return nil, fmt.Errorf("SPIFFE X.509 source is required")
	}
	hubID, err := spiffeid.FromString(strings.TrimSpace(expectedHubID))
	if err != nil {
		return nil, fmt.Errorf("parse control plane SPIFFE ID: %w", err)
	}
	if hubID.String() != "spiffe://platform.delivery/hub" {
		return nil, fmt.Errorf("control plane SPIFFE ID must be spiffe://platform.delivery/hub")
	}
	tlsConfig := tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(hubID))
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: 30 * time.Second}, nil
}

// NewHealthProbeHTTPClient creates a TLS-verifying client for annotated Pod
// endpoints. A private CA may be mounted by the Environment; verification is
// never disabled.
func NewHealthProbeHTTPClient(caFile string) (*http.Client, error) {
	caFile = strings.TrimSpace(caFile)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		contents, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read health probe CA: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(contents) {
			return nil, fmt.Errorf("health probe CA file contains no certificates")
		}
		tlsConfig.RootCAs = roots
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Transport: transport, Timeout: 4 * time.Second}, nil
}
