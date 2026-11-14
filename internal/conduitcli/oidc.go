package conduitcli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	maximumOIDCResponseBytes = 64 * 1024
	// maximumOIDCTokenBytes matches the Hub's default token bound. A larger
	// token would be refused there after the secret had already been spent.
	maximumOIDCTokenBytes = 16 * 1024
)

func fetchOIDCAccessToken(ctx context.Context, configuration ConnectionConfig) (string, error) {
	secret, err := readOIDCClientSecret(configuration.OIDCClientSecretFile)
	if err != nil {
		return "", err
	}
	client, err := oidcHTTPClient(configuration)
	if err != nil {
		return "", err
	}
	defer client.CloseIdleConnections()
	tokenURL := strings.TrimSpace(configuration.OIDCTokenURL)
	if tokenURL == "" {
		tokenURL, err = discoverOIDCTokenEndpoint(ctx, client, configuration.OIDCIssuer)
		if err != nil {
			return "", err
		}
	}
	if err := requireHTTPSURL(tokenURL, "OIDC token endpoint"); err != nil {
		return "", err
	}
	return requestOIDCAccessToken(ctx, client, tokenURL, strings.TrimSpace(configuration.OIDCClientID), secret, strings.TrimSpace(configuration.OIDCScope))
}

func oidcHTTPClient(configuration ConnectionConfig) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(configuration.OIDCCAFile) != "" {
		pemBytes, err := os.ReadFile(configuration.OIDCCAFile)
		if err != nil {
			return nil, fmt.Errorf("read OIDC CA file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pemBytes) {
			return nil, errors.New("OIDC CA file contains no PEM certificates")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	return &http.Client{Transport: transport, Timeout: configuration.Timeout, CheckRedirect: rejectRedirect}, nil
}

func discoverOIDCTokenEndpoint(ctx context.Context, client *http.Client, issuer string) (string, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	documentURL := issuer + "/.well-known/openid-configuration"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, documentURL, nil)
	if err != nil {
		return "", fmt.Errorf("create OIDC discovery request: %w", err)
	}
	contents, status, err := doOIDC(client, request)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("OIDC discovery returned %d", status)
	}
	var document struct {
		Issuer        string `json:"issuer"`
		TokenEndpoint string `json:"token_endpoint"`
	}
	if err := json.Unmarshal(contents, &document); err != nil {
		return "", fmt.Errorf("decode OIDC discovery document: %w", err)
	}
	if strings.TrimRight(document.Issuer, "/") != issuer {
		return "", errors.New("OIDC discovery issuer does not match --oidc-issuer")
	}
	if strings.TrimSpace(document.TokenEndpoint) == "" {
		return "", errors.New("OIDC discovery document has no token endpoint")
	}
	return document.TokenEndpoint, nil
}

func requestOIDCAccessToken(ctx context.Context, client *http.Client, tokenURL, clientID, secret, scope string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	if scope != "" {
		form.Set("scope", scope)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("create OIDC token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	contents, status, err := doOIDC(client, request)
	if err != nil {
		return "", err
	}
	var response struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Error       string `json:"error"`
	}
	if json.Unmarshal(contents, &response) != nil || status != http.StatusOK {
		if response.Error != "" && len(response.Error) <= 128 && !strings.ContainsAny(response.Error, " \r\n") {
			return "", fmt.Errorf("OIDC token endpoint returned %d: %s", status, response.Error)
		}
		return "", fmt.Errorf("OIDC token endpoint returned %d", status)
	}
	tokenType := strings.ToLower(strings.TrimSpace(response.TokenType))
	if tokenType != "" && tokenType != "bearer" {
		return "", fmt.Errorf("OIDC token type %q is not bearer", response.TokenType)
	}
	token := strings.TrimSpace(response.AccessToken)
	if token == "" || strings.ContainsAny(token, " \t\r\n") || len(token) > maximumOIDCTokenBytes {
		return "", errors.New("OIDC token endpoint returned no usable access token")
	}
	return token, nil
}

func doOIDC(client *http.Client, request *http.Request) ([]byte, int, error) {
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, fmt.Errorf("OIDC request: %w", err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, maximumOIDCResponseBytes+1))
	if err != nil {
		return nil, response.StatusCode, fmt.Errorf("read OIDC response: %w", err)
	}
	if len(contents) > maximumOIDCResponseBytes {
		return nil, response.StatusCode, fmt.Errorf("OIDC response exceeds %d bytes", maximumOIDCResponseBytes)
	}
	return contents, response.StatusCode, nil
}
