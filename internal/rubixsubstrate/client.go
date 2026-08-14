package rubixsubstrate

// A typed client for the recovered Rubix networking and credential services.
//
// Every method resolves its verb and path through RouteFor against the gated
// route table, marshals the recovered request bean, and decodes the recovered
// response bean. The method/path/request/response of each call are exactly the
// ones read from the *Blocking service interfaces (javap); nothing about the
// contract is invented here. What this file adds is only the mechanical Conjure
// transport any generated Dialogue client performs: build the path, send JSON,
// read JSON. client_test.go exercises all fourteen against a stub and checks
// each hits the recovered verb and path.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Client calls the Rubix networking and credential services over HTTP.
type Client struct {
	baseURL    string
	httpClient *http.Client
	// bearer supplies the Authorization token for each call. It is a function so
	// a caller can rotate a short-lived token without rebuilding the client.
	bearer func(context.Context) (string, error)
}

// NewClient builds a client against baseURL (scheme://host, no trailing slash
// required). bearer may be nil for an unauthenticated transport; when set, its
// value is sent as "Authorization: Bearer <token>".
func NewClient(baseURL string, httpClient *http.Client, bearer func(context.Context) (string, error)) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), httpClient: httpClient, bearer: bearer}
}

// call resolves the route for service+endpoint, expands path parameters, sends
// body (nil for none) and decodes into out (nil to discard the body).
func (c *Client) call(ctx context.Context, service Service, endpoint string, pathArgs map[string]string, body, out any) error {
	route, ok := RouteFor(service, endpoint)
	if !ok {
		return fmt.Errorf("rubixsubstrate: no recovered route for %s.%s", service, endpoint)
	}
	path, err := expandPath(route.Path, pathArgs)
	if err != nil {
		return fmt.Errorf("rubixsubstrate: %s.%s: %w", service, endpoint, err)
	}

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("rubixsubstrate: encode %s.%s request: %w", service, endpoint, err)
		}
		reader = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, route.Method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.bearer != nil {
		token, err := c.bearer(ctx)
		if err != nil {
			return fmt.Errorf("rubixsubstrate: obtain bearer for %s.%s: %w", service, endpoint, err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("rubixsubstrate: %s.%s returned %d: %s", service, endpoint, response.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("rubixsubstrate: decode %s.%s response: %w", service, endpoint, err)
	}
	return nil
}

// expandPath substitutes {name} segments with escaped argument values and fails
// if the template and arguments disagree, so a missing or extra parameter is a
// hard error rather than a malformed request.
func expandPath(template string, args map[string]string) (string, error) {
	used := 0
	var out strings.Builder
	for _, segment := range strings.Split(template, "/") {
		if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			name := segment[1 : len(segment)-1]
			value, ok := args[name]
			if !ok {
				return "", fmt.Errorf("missing path parameter %q", name)
			}
			if value == "" {
				return "", fmt.Errorf("empty path parameter %q", name)
			}
			out.WriteString("/")
			out.WriteString(url.PathEscape(value))
			used++
			continue
		}
		if segment == "" {
			continue
		}
		out.WriteString("/")
		out.WriteString(segment)
	}
	if used != len(args) {
		return "", fmt.Errorf("path %q takes %d parameters, got %d", template, used, len(args))
	}
	return out.String(), nil
}

// --- EgressAllowlistConfigurationService ---

func (c *Client) SetEgressConfig(ctx context.Context, req ApplyNetworkEgressConfigurationRequest) (ApplyNetworkEgressConfigurationResponse, error) {
	var out ApplyNetworkEgressConfigurationResponse
	err := c.call(ctx, EgressAllowlistConfigurationService, "setEgressConfig", nil, req, &out)
	return out, err
}

func (c *Client) GetEgressConfig(ctx context.Context) (EgressConfigurations, error) {
	var out EgressConfigurations
	err := c.call(ctx, EgressAllowlistConfigurationService, "getEgressConfig", nil, nil, &out)
	return out, err
}

func (c *Client) CheckEgressEnabled(ctx context.Context) (bool, error) {
	var out bool
	err := c.call(ctx, EgressAllowlistConfigurationService, "checkEgressEnabled", nil, nil, &out)
	return out, err
}

// --- IngressAllowlistConfigurationService ---

func (c *Client) SetDomainIngress(ctx context.Context, req SetDomainIngressRequest) (SetDomainIngressResponse, error) {
	var out SetDomainIngressResponse
	err := c.call(ctx, IngressAllowlistConfigurationService, "setDomainIngress", nil, req, &out)
	return out, err
}

func (c *Client) ListDomainIngresses(ctx context.Context) (IngressConfigurations, error) {
	var out IngressConfigurations
	err := c.call(ctx, IngressAllowlistConfigurationService, "listDomainIngresses", nil, nil, &out)
	return out, err
}

func (c *Client) CheckIngressEnabled(ctx context.Context) (bool, error) {
	var out bool
	err := c.call(ctx, IngressAllowlistConfigurationService, "checkIngressEnabled", nil, nil, &out)
	return out, err
}

func (c *Client) GetFallbackConfig(ctx context.Context) (GetFallbackConfigResponse, error) {
	var out GetFallbackConfigResponse
	err := c.call(ctx, IngressAllowlistConfigurationService, "getFallbackConfig", nil, nil, &out)
	return out, err
}

func (c *Client) RevertIngressToFallbackConfig(ctx context.Context, domain Domain) (ApplyNetworkIngressRollbackResponse, error) {
	var out ApplyNetworkIngressRollbackResponse
	err := c.call(ctx, IngressAllowlistConfigurationService, "revertIngressToFallbackConfig", map[string]string{"domain": string(domain)}, nil, &out)
	return out, err
}

// --- MetadataRetrievalService ---

func (c *Client) GetEgressCidrs(ctx context.Context) (GetEgressCidrsResponse, error) {
	var out GetEgressCidrsResponse
	err := c.call(ctx, MetadataRetrievalService, "getEgressCidrs", nil, nil, &out)
	return out, err
}

// --- UserCredentialManagerProvisioningService ---

func (c *Client) RetrieveCredential(ctx context.Context, username Username, req RetrieveCredentialRequest) (RetrieveCredentialPayload, error) {
	var out RetrieveCredentialPayload
	err := c.call(ctx, UserCredentialManagerProvisioning, "retrieveCredential", map[string]string{"username": string(username)}, req, &out)
	return out, err
}

func (c *Client) GetClusterConfigurations(ctx context.Context, username Username, req RetrieveClusterConfigurationsRequest) (ClusterConfigurations, error) {
	var out ClusterConfigurations
	err := c.call(ctx, UserCredentialManagerProvisioning, "getClusterConfigurations", map[string]string{"username": string(username)}, req, &out)
	return out, err
}

func (c *Client) RetrieveCredentialForInfraUser(ctx context.Context, username Username, req RetrieveCredentialForInfraUserRequest) (RetrieveCredentialForInfraUserPayload, error) {
	var out RetrieveCredentialForInfraUserPayload
	err := c.call(ctx, UserCredentialManagerProvisioning, "RetrieveCredentialForInfraUser", map[string]string{"username": string(username)}, req, &out)
	return out, err
}

func (c *Client) RetrieveServiceAccountCredentialFromToken(ctx context.Context) (RetrieveServiceAccountCredentialFromTokenPayload, error) {
	var out RetrieveServiceAccountCredentialFromTokenPayload
	err := c.call(ctx, UserCredentialManagerProvisioning, "retrieveServiceAccountCredentialFromToken", nil, nil, &out)
	return out, err
}

// --- UserCredentialManagerValidationService ---

func (c *Client) ValidateConfig(ctx context.Context, username Username, req ValidateConfigRequest) (ValidationPayload, error) {
	var out ValidationPayload
	err := c.call(ctx, UserCredentialManagerValidation, "validateConfig", map[string]string{"username": string(username)}, req, &out)
	return out, err
}
