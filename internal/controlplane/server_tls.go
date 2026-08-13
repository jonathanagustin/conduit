package controlplane

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"

	"github.com/spiffe/go-spiffe/v2/bundle/x509bundle"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
)

const HubSPIFFEID = "spiffe://platform.delivery/hub"

type SPIFFEX509Source interface {
	x509svid.Source
	x509bundle.Source
}

type exactX509SVIDSource struct {
	source   x509svid.Source
	expected spiffeid.ID
}

func (source exactX509SVIDSource) GetX509SVID() (*x509svid.SVID, error) {
	svid, err := source.source.GetX509SVID()
	if err != nil {
		return nil, err
	}
	if svid == nil || svid.ID != source.expected {
		actual := "<nil>"
		if svid != nil {
			actual = svid.ID.String()
		}
		return nil, fmt.Errorf("Hub X509-SVID is %s; expected %s", actual, source.expected)
	}
	return svid, nil
}

// AgentServerTLSConfig trusts client certificates issued by the configured
// Environment identity CA. Client certificates are optional at the TLS layer
// so operator and health routes can use separate authentication; Agent routes
// enforce a verified certificate in the HTTP handler.
func AgentServerTLSConfig(clientCAFile string) (*tls.Config, error) {
	contents, err := os.ReadFile(strings.TrimSpace(clientCAFile))
	if err != nil {
		return nil, fmt.Errorf("read Agent client CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if !clientCAs.AppendCertsFromPEM(contents) {
		return nil, fmt.Errorf("Agent client CA file contains no certificates")
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		ClientAuth: tls.VerifyClientCertIfGiven,
		ClientCAs:  clientCAs,
	}, nil
}

// SPIFFEServerTLSConfig serves the Hub's automatically rotated X509-SVID and
// refreshes the client trust bundle for every TLS handshake. Client
// certificates remain optional at the TLS layer so health and public routes
// stay reachable; Agent and operator handlers still require an authenticated
// certificate with the appropriate platform.delivery SPIFFE path.
func SPIFFEServerTLSConfig(source SPIFFEX509Source) (*tls.Config, error) {
	if source == nil {
		return nil, fmt.Errorf("SPIFFE X.509 source is required")
	}
	hubID, err := spiffeid.FromString(HubSPIFFEID)
	if err != nil {
		return nil, fmt.Errorf("parse Hub SPIFFE ID: %w", err)
	}
	exactSource := exactX509SVIDSource{source: source, expected: hubID}
	_, err = exactSource.GetX509SVID()
	if err != nil {
		return nil, fmt.Errorf("load Hub X509-SVID: %w", err)
	}
	config := tlsconfig.TLSServerConfig(exactSource)
	config.ClientAuth = tls.VerifyClientCertIfGiven
	config.GetConfigForClient = func(*tls.ClientHelloInfo) (*tls.Config, error) {
		bundle, bundleErr := source.GetX509BundleForTrustDomain(hubID.TrustDomain())
		if bundleErr != nil {
			return nil, fmt.Errorf("load SPIFFE client trust bundle: %w", bundleErr)
		}
		clientCAs := x509.NewCertPool()
		for _, authority := range bundle.X509Authorities() {
			clientCAs.AddCert(authority)
		}
		if len(bundle.X509Authorities()) == 0 {
			return nil, fmt.Errorf("SPIFFE client trust bundle contains no authorities")
		}
		current := config.Clone()
		current.GetConfigForClient = nil
		current.ClientCAs = clientCAs
		return current, nil
	}
	return config, nil
}
