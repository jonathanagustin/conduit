package rubixsubstrate

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// recordingRubix is a stub backend: it records the path parameter and decoded
// request bean each endpoint received and returns a distinctive canned response,
// so a round trip proves both directions of the recovered contract. Setting
// failWith makes every method return that error, to exercise error mapping.
type recordingRubix struct {
	lastUsername Username
	lastDomain   Domain
	lastRequest  any
	failWith     error
}

func portPtr(p Port) *Port { return &p }

func (b *recordingRubix) SetEgressConfig(_ context.Context, req ApplyNetworkEgressConfigurationRequest) (ApplyNetworkEgressConfigurationResponse, error) {
	b.lastRequest = req
	return ApplyNetworkEgressConfigurationResponse{}, b.failWith
}

func (b *recordingRubix) GetEgressConfig(context.Context) (EgressConfigurations, error) {
	return EgressConfigurations{{
		Type:                   "tcpEgressConfiguration",
		TCPEgressConfiguration: &TCPEgressConfiguration{Name: "n", Cidr: "10.0.0.0/8", PortSpecifier: PortSpecifier{Type: "port", Port: portPtr(443)}},
	}}, b.failWith
}

func (b *recordingRubix) CheckEgressEnabled(context.Context) (bool, error) {
	return true, b.failWith
}

func (b *recordingRubix) SetDomainIngress(_ context.Context, req SetDomainIngressRequest) (SetDomainIngressResponse, error) {
	b.lastRequest = req
	return SetDomainIngressResponse{}, b.failWith
}

func (b *recordingRubix) ListDomainIngresses(context.Context) (IngressConfigurations, error) {
	return IngressConfigurations{"d.example": {CountryCodes: []CountryIsoCode{"US"}, Cidrs: []Cidr{"10.0.0.0/8"}}}, b.failWith
}

func (b *recordingRubix) CheckIngressEnabled(context.Context) (bool, error) {
	return false, b.failWith
}

func (b *recordingRubix) GetFallbackConfig(context.Context) (GetFallbackConfigResponse, error) {
	return GetFallbackConfigResponse{GlobalIngressConfiguration: IngressConfiguration{CountryCodes: []CountryIsoCode{}, Cidrs: []Cidr{}}}, b.failWith
}

func (b *recordingRubix) RevertIngressToFallbackConfig(_ context.Context, domain Domain) (ApplyNetworkIngressRollbackResponse, error) {
	b.lastDomain = domain
	return ApplyNetworkIngressRollbackResponse{}, b.failWith
}

func (b *recordingRubix) GetEgressCidrs(context.Context) (GetEgressCidrsResponse, error) {
	return GetEgressCidrsResponse{"10.0.0.0/8"}, b.failWith
}

func (b *recordingRubix) RetrieveCredential(_ context.Context, username Username, req RetrieveCredentialRequest) (RetrieveCredentialPayload, error) {
	b.lastUsername, b.lastRequest = username, req
	return RetrieveCredentialPayload{
		Credential:               "cred",
		MappedNamespaces:         map[NamespaceType]ConcreteNamespace{"compute": "ns-1"},
		MultipleMappedNamespaces: map[NamespaceType][]ConcreteNamespace{"compute": {"ns-1", "ns-2"}},
	}, b.failWith
}

func (b *recordingRubix) GetClusterConfigurations(_ context.Context, username Username, req RetrieveClusterConfigurationsRequest) (ClusterConfigurations, error) {
	b.lastUsername, b.lastRequest = username, req
	return ClusterConfigurations{Clusters: []Cluster{{ApolloEnvironmentID: "env-1", ClusterMarkings: []ClusterMarking{ClusterMarkingInternalRoutingAvailable}}}}, b.failWith
}

func (b *recordingRubix) RetrieveCredentialForInfraUser(_ context.Context, username Username, req RetrieveCredentialForInfraUserRequest) (RetrieveCredentialForInfraUserPayload, error) {
	b.lastUsername, b.lastRequest = username, req
	return RetrieveCredentialForInfraUserPayload{Credential: "infra-cred"}, b.failWith
}

func (b *recordingRubix) RetrieveServiceAccountCredentialFromToken(context.Context) (RetrieveServiceAccountCredentialFromTokenPayload, error) {
	return RetrieveServiceAccountCredentialFromTokenPayload{Credential: "token-cred"}, b.failWith
}

func (b *recordingRubix) ValidateConfig(_ context.Context, username Username, req ValidateConfigRequest) (ValidationPayload, error) {
	b.lastUsername, b.lastRequest = username, req
	return ValidationPayload{SyntaxValid: true, SemanticsValid: false}, b.failWith
}

// serverCase drives one endpoint through the real Client against NewHandler and
// asserts the backend saw the expected path parameter and request bean and the
// client received the canned response bean -- both directions of the contract.
type serverCase struct {
	name     string
	service  Service
	endpoint string
	run      func(t *testing.T, c *Client, b *recordingRubix)
}

func serverCases() []serverCase {
	return []serverCase{
		{"setEgressConfig", EgressAllowlistConfigurationService, "setEgressConfig", func(t *testing.T, c *Client, b *recordingRubix) {
			req := ApplyNetworkEgressConfigurationRequest{EgressConfigurations: EgressConfigurations{{Type: "tcpEgressConfiguration", TCPEgressConfiguration: &TCPEgressConfiguration{Name: "n", Cidr: "10.0.0.0/8", PortSpecifier: PortSpecifier{Type: "port", Port: portPtr(80)}}}}}
			if _, err := c.SetEgressConfig(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(b.lastRequest, req) {
				t.Errorf("backend request: got %+v want %+v", b.lastRequest, req)
			}
		}},
		{"getEgressConfig", EgressAllowlistConfigurationService, "getEgressConfig", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.GetEgressConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].Type != "tcpEgressConfiguration" || got[0].TCPEgressConfiguration == nil || *got[0].TCPEgressConfiguration.PortSpecifier.Port != 443 {
				t.Errorf("response bean not round-tripped: %+v", got)
			}
		}},
		{"checkEgressEnabled", EgressAllowlistConfigurationService, "checkEgressEnabled", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.CheckEgressEnabled(context.Background())
			if err != nil || got != true {
				t.Errorf("got %v err %v want true", got, err)
			}
		}},
		{"setDomainIngress", IngressAllowlistConfigurationService, "setDomainIngress", func(t *testing.T, c *Client, b *recordingRubix) {
			req := SetDomainIngressRequest{Domain: "d.example", IngressConfiguration: IngressConfiguration{CountryCodes: []CountryIsoCode{"US"}, Cidrs: []Cidr{"10.0.0.0/8"}}}
			if _, err := c.SetDomainIngress(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(b.lastRequest, req) {
				t.Errorf("backend request: got %+v want %+v", b.lastRequest, req)
			}
		}},
		{"listDomainIngresses", IngressAllowlistConfigurationService, "listDomainIngresses", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.ListDomainIngresses(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if cfg, ok := got["d.example"]; !ok || len(cfg.CountryCodes) != 1 || cfg.CountryCodes[0] != "US" {
				t.Errorf("map alias not round-tripped: %+v", got)
			}
		}},
		{"checkIngressEnabled", IngressAllowlistConfigurationService, "checkIngressEnabled", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.CheckIngressEnabled(context.Background())
			if err != nil || got != false {
				t.Errorf("got %v err %v want false", got, err)
			}
		}},
		{"getFallbackConfig", IngressAllowlistConfigurationService, "getFallbackConfig", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.GetFallbackConfig(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if got.GlobalIngressConfiguration.CountryCodes == nil || got.GlobalIngressConfiguration.Cidrs == nil {
				t.Errorf("object not round-tripped: %+v", got)
			}
		}},
		{"revertIngressToFallbackConfig", IngressAllowlistConfigurationService, "revertIngressToFallbackConfig", func(t *testing.T, c *Client, b *recordingRubix) {
			if _, err := c.RevertIngressToFallbackConfig(context.Background(), "d.example"); err != nil {
				t.Fatal(err)
			}
			if b.lastDomain != "d.example" {
				t.Errorf("path parameter domain: got %q want d.example", b.lastDomain)
			}
		}},
		{"getEgressCidrs", MetadataRetrievalService, "getEgressCidrs", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.GetEgressCidrs(context.Background())
			if err != nil || len(got) != 1 || got[0] != "10.0.0.0/8" {
				t.Errorf("set alias not round-tripped: %+v err %v", got, err)
			}
		}},
		{"retrieveCredential", UserCredentialManagerProvisioning, "retrieveCredential", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.RetrieveCredential(context.Background(), "alice", RetrieveCredentialRequest{Password: "p", EnvironmentID: "e"})
			if err != nil {
				t.Fatal(err)
			}
			if b.lastUsername != "alice" {
				t.Errorf("path parameter username: got %q want alice", b.lastUsername)
			}
			if got.Credential != "cred" || len(got.MultipleMappedNamespaces["compute"]) != 2 {
				t.Errorf("payload not round-tripped: %+v", got)
			}
		}},
		{"getClusterConfigurations", UserCredentialManagerProvisioning, "getClusterConfigurations", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.GetClusterConfigurations(context.Background(), "alice", RetrieveClusterConfigurationsRequest{Password: "p"})
			if err != nil {
				t.Fatal(err)
			}
			if b.lastUsername != "alice" {
				t.Errorf("path parameter username: got %q want alice", b.lastUsername)
			}
			if len(got.Clusters) != 1 || got.Clusters[0].ClusterMarkings[0] != ClusterMarkingInternalRoutingAvailable {
				t.Errorf("enum not round-tripped: %+v", got)
			}
		}},
		{"RetrieveCredentialForInfraUser", UserCredentialManagerProvisioning, "RetrieveCredentialForInfraUser", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.RetrieveCredentialForInfraUser(context.Background(), "alice", RetrieveCredentialForInfraUserRequest{Password: "p"})
			if err != nil || got.Credential != "infra-cred" || b.lastUsername != "alice" {
				t.Errorf("got %+v err %v user %q", got, err, b.lastUsername)
			}
		}},
		{"retrieveServiceAccountCredentialFromToken", UserCredentialManagerProvisioning, "retrieveServiceAccountCredentialFromToken", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.RetrieveServiceAccountCredentialFromToken(context.Background())
			if err != nil || got.Credential != "token-cred" {
				t.Errorf("got %+v err %v", got, err)
			}
		}},
		{"validateConfig", UserCredentialManagerValidation, "validateConfig", func(t *testing.T, c *Client, b *recordingRubix) {
			got, err := c.ValidateConfig(context.Background(), "alice", ValidateConfigRequest{Password: "p"})
			if err != nil {
				t.Fatal(err)
			}
			if b.lastUsername != "alice" {
				t.Errorf("path parameter username: got %q want alice", b.lastUsername)
			}
			if !got.SyntaxValid || got.SemanticsValid {
				t.Errorf("payload not round-tripped: %+v", got)
			}
		}},
	}
}

func TestServerRoundTripsRecoveredContract(t *testing.T) {
	cases := serverCases()
	covered := map[string]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backend := &recordingRubix{}
			server := httptest.NewServer(NewHandler(backend))
			defer server.Close()
			client := NewClient(server.URL, server.Client(), nil)
			tc.run(t, client, backend)
			if _, ok := RouteFor(tc.service, tc.endpoint); !ok {
				t.Fatalf("%s.%s not in route table", tc.service, tc.endpoint)
			}
			covered[opKey(tc.service, tc.endpoint)] = true
		})
	}
	for _, route := range Routes {
		if !covered[opKey(route.Service, route.Endpoint)] {
			t.Errorf("route %s.%s has no server round-trip test", route.Service, route.Endpoint)
		}
	}
	if len(covered) != len(Routes) {
		t.Fatalf("server covers %d routes, table has %d", len(covered), len(Routes))
	}
}

// TestHandlerServesExactlyTheRecoveredTable proves the served surface is the
// gated table: every route has a handler (no route is unserved) and there is no
// handler that is not a recovered route (no invented operation).
func TestHandlerServesExactlyTheRecoveredTable(t *testing.T) {
	handlers := handlersFor(&recordingRubix{})
	if len(handlers) != len(Routes) {
		t.Fatalf("handlers cover %d operations, table has %d", len(handlers), len(Routes))
	}
	for _, route := range Routes {
		if _, ok := handlers[opKey(route.Service, route.Endpoint)]; !ok {
			t.Errorf("no handler for recovered route %s.%s", route.Service, route.Endpoint)
		}
	}
	known := map[string]bool{}
	for _, route := range Routes {
		known[opKey(route.Service, route.Endpoint)] = true
	}
	for key := range handlers {
		if !known[key] {
			t.Errorf("handler %s serves an operation absent from the recovered table", key)
		}
	}
	// NewHandler must wire the whole table without panicking.
	_ = NewHandler(&recordingRubix{})
}

// TestServerRejectsUnknownRoute confirms the mux serves only recovered paths: an
// unrecovered path is a 404, mirroring the client's inability to address one.
func TestServerRejectsUnknownRoute(t *testing.T) {
	server := httptest.NewServer(NewHandler(&recordingRubix{}))
	defer server.Close()
	resp, err := server.Client().Get(server.URL + "/not-a-recovered-path")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown route: got %d want 404", resp.StatusCode)
	}
}

// TestServerRejectsMalformedBody confirms a body endpoint answers 400 on invalid
// JSON rather than invoking the backend.
func TestServerRejectsMalformedBody(t *testing.T) {
	server := httptest.NewServer(NewHandler(&recordingRubix{}))
	defer server.Close()
	resp, err := server.Client().Post(server.URL+"/validate/alice", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body: got %d want 400", resp.StatusCode)
	}
}

// TestServerMapsBackendErrorToNon2xx confirms a backend failure becomes a non-2xx
// the Client surfaces as an error -- the inverse of TestClientSurfacesNon2xx.
func TestServerMapsBackendErrorToNon2xx(t *testing.T) {
	backend := &recordingRubix{failWith: context.DeadlineExceeded}
	server := httptest.NewServer(NewHandler(backend))
	defer server.Close()
	client := NewClient(server.URL, server.Client(), nil)
	if _, err := client.GetEgressConfig(context.Background()); err == nil {
		t.Fatal("expected the client to surface the backend error")
	}
}
