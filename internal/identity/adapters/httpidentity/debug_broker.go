package httpidentity

import (
	"net/http"

	"github.com/jonathanagustin/conduit/internal/identity/domain"
)

// DebugBrokerIdentityFromRequest reads only the leaf authenticated by the TLS
// server. The server must use a configured client CA and standard client-certificate
// verification. There is no development-header or bearer-token fallback, and
// identity alone supplies no enrollment or execution authority.
func DebugBrokerIdentityFromRequest(request *http.Request) (domain.DebugBrokerIdentity, error) {
	if request == nil || request.TLS == nil || len(request.Header.Values("Authorization")) != 0 || len(request.TLS.PeerCertificates) == 0 || len(request.TLS.VerifiedChains) == 0 {
		return domain.DebugBrokerIdentity{}, errClientCertificateRequired
	}
	leaf := request.TLS.PeerCertificates[0]
	if leaf == nil {
		return domain.DebugBrokerIdentity{}, errClientCertificateRequired
	}
	for _, chain := range request.TLS.VerifiedChains {
		if len(chain) == 0 || chain[0] == nil || !leaf.Equal(chain[0]) {
			return domain.DebugBrokerIdentity{}, errClientCertificateRequired
		}
	}
	return domain.DebugBrokerIdentityFromCertificate(leaf)
}
