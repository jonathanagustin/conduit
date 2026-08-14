package rubixsubstrate

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capture records what the stub server saw for the single call under test.
type capture struct {
	method string
	path   string
	auth   string
	body   string
}

// clientCase drives one endpoint: invoke calls the client method against the
// stub, which returns canned. want* are the recovered verb and path the call
// must produce.
type clientCase struct {
	name     string
	wantVerb string
	wantPath string
	service  Service
	endpoint string
	canned   string
	invoke   func(ctx context.Context, c *Client) error
}

func clientCases() []clientCase {
	return []clientCase{
		{"setEgressConfig", "PUT", "/egress-allowlist", EgressAllowlistConfigurationService, "setEgressConfig", `{}`,
			func(ctx context.Context, c *Client) error {
				_, err := c.SetEgressConfig(ctx, ApplyNetworkEgressConfigurationRequest{EgressConfigurations: EgressConfigurations{}})
				return err
			}},
		{"getEgressConfig", "GET", "/egress-allowlist", EgressAllowlistConfigurationService, "getEgressConfig", `[]`,
			func(ctx context.Context, c *Client) error { _, err := c.GetEgressConfig(ctx); return err }},
		{"checkEgressEnabled", "GET", "/egress-allowlist/enabled", EgressAllowlistConfigurationService, "checkEgressEnabled", `true`,
			func(ctx context.Context, c *Client) error { _, err := c.CheckEgressEnabled(ctx); return err }},
		{"setDomainIngress", "PUT", "/ingress-allowlist", IngressAllowlistConfigurationService, "setDomainIngress", `{}`,
			func(ctx context.Context, c *Client) error {
				_, err := c.SetDomainIngress(ctx, SetDomainIngressRequest{Domain: "d.example"})
				return err
			}},
		{"listDomainIngresses", "GET", "/ingress-allowlist", IngressAllowlistConfigurationService, "listDomainIngresses", `{}`,
			func(ctx context.Context, c *Client) error { _, err := c.ListDomainIngresses(ctx); return err }},
		{"checkIngressEnabled", "GET", "/ingress-allowlist/enabled", IngressAllowlistConfigurationService, "checkIngressEnabled", `false`,
			func(ctx context.Context, c *Client) error { _, err := c.CheckIngressEnabled(ctx); return err }},
		{"getFallbackConfig", "GET", "/ingress-allowlist/fallback", IngressAllowlistConfigurationService, "getFallbackConfig", `{"globalIngressConfiguration":{"countryCodes":[],"cidrs":[]}}`,
			func(ctx context.Context, c *Client) error { _, err := c.GetFallbackConfig(ctx); return err }},
		{"revertIngressToFallbackConfig", "POST", "/ingress-allowlist/rollback/d.example", IngressAllowlistConfigurationService, "revertIngressToFallbackConfig", `{}`,
			func(ctx context.Context, c *Client) error {
				_, err := c.RevertIngressToFallbackConfig(ctx, "d.example")
				return err
			}},
		{"getEgressCidrs", "GET", "/metadata/egress", MetadataRetrievalService, "getEgressCidrs", `["10.0.0.0/8"]`,
			func(ctx context.Context, c *Client) error { _, err := c.GetEgressCidrs(ctx); return err }},
		{"retrieveCredential", "POST", "/provision/alice", UserCredentialManagerProvisioning, "retrieveCredential", `{"credential":"c","mappedNamespaces":{},"multipleMappedNamespaces":{}}`,
			func(ctx context.Context, c *Client) error {
				_, err := c.RetrieveCredential(ctx, "alice", RetrieveCredentialRequest{Password: "p"})
				return err
			}},
		{"getClusterConfigurations", "POST", "/provision/alice/clusters", UserCredentialManagerProvisioning, "getClusterConfigurations", `{"clusters":[]}`,
			func(ctx context.Context, c *Client) error {
				_, err := c.GetClusterConfigurations(ctx, "alice", RetrieveClusterConfigurationsRequest{Password: "p"})
				return err
			}},
		{"RetrieveCredentialForInfraUser", "POST", "/provision/alice/infra-user", UserCredentialManagerProvisioning, "RetrieveCredentialForInfraUser", `{"credential":"c"}`,
			func(ctx context.Context, c *Client) error {
				_, err := c.RetrieveCredentialForInfraUser(ctx, "alice", RetrieveCredentialForInfraUserRequest{Password: "p"})
				return err
			}},
		{"retrieveServiceAccountCredentialFromToken", "GET", "/provision/token", UserCredentialManagerProvisioning, "retrieveServiceAccountCredentialFromToken", `{"credential":"c"}`,
			func(ctx context.Context, c *Client) error {
				_, err := c.RetrieveServiceAccountCredentialFromToken(ctx)
				return err
			}},
		{"validateConfig", "POST", "/validate/alice", UserCredentialManagerValidation, "validateConfig", `{"syntaxValid":true,"semanticsValid":true}`,
			func(ctx context.Context, c *Client) error {
				_, err := c.ValidateConfig(ctx, "alice", ValidateConfigRequest{Password: "p"})
				return err
			}},
	}
}

func TestClientHitsRecoveredRoutes(t *testing.T) {
	cases := clientCases()
	covered := map[string]bool{}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen capture
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				seen = capture{method: r.Method, path: r.URL.Path, auth: r.Header.Get("Authorization"), body: string(body)}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tc.canned)
			}))
			defer server.Close()

			client := NewClient(server.URL, server.Client(), func(context.Context) (string, error) { return "test-token", nil })
			if err := tc.invoke(context.Background(), client); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if seen.method != tc.wantVerb {
				t.Errorf("%s verb: got %s want %s", tc.name, seen.method, tc.wantVerb)
			}
			if seen.path != tc.wantPath {
				t.Errorf("%s path: got %s want %s", tc.name, seen.path, tc.wantPath)
			}
			if seen.auth != "Bearer test-token" {
				t.Errorf("%s authorization not forwarded: got %q", tc.name, seen.auth)
			}
			// The route table is the source of truth: what the client sent must be
			// the verb and path RouteFor holds for this endpoint.
			route, ok := RouteFor(tc.service, tc.endpoint)
			if !ok {
				t.Fatalf("%s not in route table", tc.endpoint)
			}
			if seen.method != route.Method {
				t.Errorf("%s drifted from table verb %s", tc.name, route.Method)
			}
			covered[string(tc.service)+"."+tc.endpoint] = true
		})
	}

	// Every recovered route must be exercised, so a new endpoint without a client
	// method (or a client method without a test) fails here.
	for _, route := range Routes {
		if !covered[string(route.Service)+"."+route.Endpoint] {
			t.Errorf("route %s.%s has no client-method test", route.Service, route.Endpoint)
		}
	}
	if len(covered) != len(Routes) {
		t.Fatalf("client covers %d routes, table has %d", len(covered), len(Routes))
	}
}

func TestExpandPath(t *testing.T) {
	if got, err := expandPath("/provision/{username}/clusters", map[string]string{"username": "a b"}); err != nil || got != "/provision/a%20b/clusters" {
		t.Fatalf("escape: got %q err %v", got, err)
	}
	if _, err := expandPath("/provision/{username}", nil); err == nil {
		t.Error("missing parameter should fail")
	}
	if _, err := expandPath("/provision/{username}", map[string]string{"username": ""}); err == nil {
		t.Error("empty parameter should fail")
	}
	if _, err := expandPath("/metadata/egress", map[string]string{"extra": "x"}); err == nil {
		t.Error("extra parameter should fail")
	}
	if got, err := expandPath("/metadata/egress", nil); err != nil || got != "/metadata/egress" {
		t.Fatalf("no-param: got %q err %v", got, err)
	}
}

func TestClientSurfacesNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()
	client := NewClient(server.URL, server.Client(), nil)
	if _, err := client.GetEgressConfig(context.Background()); err == nil {
		t.Fatal("expected error on 403")
	}
}
