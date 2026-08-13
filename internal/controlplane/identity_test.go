package controlplane

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestAgentRoutesRequireVerifiedEnvironmentIdentity(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())

	request := httptest.NewRequest(
		http.MethodPost, "/v1/environments/prod/leases", bytes.NewBufferString(`{"durationSeconds":60}`),
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("request without certificate returned %d, want %d", response.Code, http.StatusUnauthorized)
	}

	certificate := testAgentCertificate(t, "dev", "spoke-dev")
	request = httptest.NewRequest(
		http.MethodPost, "/v1/environments/prod/leases", bytes.NewBufferString(`{"durationSeconds":60}`),
	)
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-Environment certificate returned %d, want %d", response.Code, http.StatusForbidden)
	}

	certificate = testAgentCertificate(t, "prod", "spoke-prod")
	request = httptest.NewRequest(
		http.MethodPost, "/v1/environments/prod/leases", bytes.NewBufferString(`{"durationSeconds":60}`),
	)
	request.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{certificate},
		VerifiedChains:   [][]*x509.Certificate{{certificate}},
	}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("authorized certificate returned %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
}

// The existing tests send no credentials at all, which leaves the interesting
// case untested: a request that does present the development headers must still
// be refused when they are not enabled. That is the whole of what the flag
// controls, and NewHandler's zero-value options are what production gets.
func TestDevelopmentIdentityHeadersAreRefusedWhenNotEnabled(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())

	agent := httptest.NewRequest(
		http.MethodPost, "/v1/environments/prod/leases", bytes.NewBufferString(`{"durationSeconds":60}`),
	)
	agent.Header.Set("X-Platform-Environment-ID", "prod")
	agent.Header.Set("X-Platform-Agent-ID", "spoke-prod")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, agent)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("header-only Agent identity returned %d, want %d: %s",
			response.Code, http.StatusUnauthorized, response.Body.String())
	}

	operator := httptest.NewRequest(http.MethodGet, "/v1/environments/dev-use2/suppressions", nil)
	operator.Header.Set("X-Platform-Subject", "operator")
	operator.Header.Set("X-Platform-Groups", "administrators")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, operator)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("header-only operator identity returned %d, want %d: %s",
			response.Code, http.StatusUnauthorized, response.Body.String())
	}
}

func TestSuppressionAdministrationRequiresVerifiedOperatorIdentity(t *testing.T) {
	t.Parallel()
	handler := NewHandler(NewMemoryRepository())
	requests := []*http.Request{
		httptest.NewRequest(http.MethodGet, "/v1/environments/dev-use2/suppressions", nil),
		httptest.NewRequest(http.MethodPost, "/v1/environments/dev-use2/suppressions", bytes.NewBufferString(`{"reason":"incident"}`)),
		httptest.NewRequest(http.MethodDelete, "/v1/suppressions/sup_unknown", nil),
	}
	for _, request := range requests {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s returned %d, want %d", request.Method, request.URL.Path, response.Code, http.StatusUnauthorized)
		}
	}
}

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
