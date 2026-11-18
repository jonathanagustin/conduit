package conduitcli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/operator"
	spacedomain "github.com/jonathanagustin/conduit/internal/spaces/domain"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

const defaultTimeout = 30 * time.Second

// ConnectionConfig is the typed, validated boundary between CLI flags and
// transport construction. It contains paths to trust material, never material
// contents, and deliberately has no permissive map-based configuration.
type ConnectionConfig struct {
	PlanReadTransport        string
	EnvironmentReadTransport string
	PackagingReadTransport   string
	SpaceID                  string
	Server                   string
	Timeout                  time.Duration
	CAFile                   string
	ClientCertFile           string
	ClientKeyFile            string
	SPIFFEEndpoint           string
	HubSPIFFEID              string
	BearerTokenFile          string
	OIDCIssuer               string
	OIDCClientID             string
	OIDCClientSecretFile     string
	OIDCTokenURL             string
	OIDCScope                string
	OIDCCAFile               string
	DevelopmentSubject       string
	DevelopmentGroups        []string
}

type clientConnection struct {
	client  *http.Client
	cleanup func()
}

func (configuration ConnectionConfig) validate() (*url.URL, error) {
	if configuration.EnvironmentReadTransport != "" && configuration.EnvironmentReadTransport != "rest" && configuration.EnvironmentReadTransport != "connect" {
		return nil, errors.New("--read-transport must be rest or connect")
	}
	if configuration.PlanReadTransport != "" && configuration.PlanReadTransport != "rest" && configuration.PlanReadTransport != "connect" {
		return nil, errors.New("--read-transport must be rest or connect")
	}
	if configuration.PackagingReadTransport != "" && configuration.PackagingReadTransport != "rest" && configuration.PackagingReadTransport != "connect" {
		return nil, errors.New("--read-transport must be rest or connect")
	}
	if configuration.SpaceID != "" && !spacedomain.ValidID(configuration.SpaceID) {
		return nil, errors.New("--space-id must name a native Space identity")
	}
	server, err := url.Parse(strings.TrimSpace(configuration.Server))
	if err != nil || server.Scheme == "" || server.Host == "" {
		return nil, errors.New("--server must be an absolute http(s) URL")
	}
	if server.Scheme != "http" && server.Scheme != "https" {
		return nil, errors.New("--server must use http or https")
	}
	// Request paths are absolute, so a base path is dropped rather than
	// prefixed. Reject it here instead of sending every call to the wrong URL.
	if strings.Trim(server.Path, "/") != "" || server.RawQuery != "" || server.Fragment != "" {
		return nil, errors.New("--server must name a scheme and host without a path, query, or fragment")
	}
	if configuration.Timeout <= 0 {
		return nil, errors.New("--timeout must be greater than zero")
	}
	clientCertificateConfigured := strings.TrimSpace(configuration.ClientCertFile) != ""
	clientKeyConfigured := strings.TrimSpace(configuration.ClientKeyFile) != ""
	if clientCertificateConfigured != clientKeyConfigured {
		return nil, errors.New("--client-cert-file and --client-key-file must be supplied together")
	}
	if clientCertificateConfigured && strings.TrimSpace(configuration.CAFile) == "" {
		return nil, errors.New("static mTLS requires --ca-file")
	}
	secureModes := 0
	if clientCertificateConfigured {
		secureModes++
	}
	if strings.TrimSpace(configuration.SPIFFEEndpoint) != "" || strings.TrimSpace(configuration.HubSPIFFEID) != "" {
		if strings.TrimSpace(configuration.SPIFFEEndpoint) == "" || strings.TrimSpace(configuration.HubSPIFFEID) == "" {
			return nil, errors.New("--spiffe-endpoint and --hub-spiffe-id must be supplied together")
		}
		secureModes++
	}
	if strings.TrimSpace(configuration.BearerTokenFile) != "" {
		secureModes++
	}
	if err := configuration.validateOIDC(); err != nil {
		return nil, err
	}
	if configuration.oidcComplete() {
		secureModes++
	}
	development := strings.TrimSpace(configuration.DevelopmentSubject) != ""
	if !development && len(normalized(configuration.DevelopmentGroups)) != 0 {
		return nil, errors.New("--development-groups requires --development-subject")
	}
	if development && secureModes != 0 {
		return nil, errors.New("--development-subject cannot be combined with a production identity")
	}
	if secureModes > 1 {
		return nil, errors.New("static mTLS, SPIFFE, bearer-token, and OIDC identity are mutually exclusive")
	}
	if development {
		if server.Scheme != "http" || !isLoopbackHost(server.Hostname()) {
			return nil, errors.New("--development-subject is allowed only with a loopback http server")
		}
	} else {
		if server.Scheme != "https" {
			return nil, errors.New("authenticated CLI connections must use https")
		}
		if secureModes == 0 {
			return nil, errors.New("configure static mTLS, SPIFFE, bearer-token, or OIDC identity")
		}
	}
	return server, nil
}

func (configuration ConnectionConfig) connect(ctx context.Context) (clientConnection, error) {
	if configuration.Timeout == 0 {
		configuration.Timeout = defaultTimeout
	}
	if _, err := configuration.validate(); err != nil {
		return clientConnection{}, err
	}
	if strings.TrimSpace(configuration.SPIFFEEndpoint) != "" {
		source, err := workloadapi.NewX509Source(
			ctx,
			workloadapi.WithClientOptions(workloadapi.WithAddr(strings.TrimSpace(configuration.SPIFFEEndpoint))),
		)
		if err != nil {
			return clientConnection{}, fmt.Errorf("connect to SPIFFE Workload API: %w", err)
		}
		client, err := operator.NewSPIFFEHTTPClient(source, configuration.HubSPIFFEID)
		if err != nil {
			source.Close()
			return clientConnection{}, fmt.Errorf("configure SPIFFE transport: %w", err)
		}
		client.Timeout = configuration.Timeout
		client.CheckRedirect = rejectRedirect
		return clientConnection{client: client, cleanup: func() { _ = source.Close() }}, nil
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(configuration.CAFile) != "" {
		caPEM, err := os.ReadFile(configuration.CAFile)
		if err != nil {
			return clientConnection{}, fmt.Errorf("read Hub CA file: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return clientConnection{}, errors.New("Hub CA file contains no PEM certificates")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	if strings.TrimSpace(configuration.ClientCertFile) != "" {
		certificate, err := tls.LoadX509KeyPair(configuration.ClientCertFile, configuration.ClientKeyFile)
		if err != nil {
			return clientConnection{}, fmt.Errorf("load operator client certificate: %w", err)
		}
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		transport.TLSClientConfig.Certificates = []tls.Certificate{certificate}
	}
	var roundTripper http.RoundTripper = transport
	if strings.TrimSpace(configuration.BearerTokenFile) != "" {
		token, err := readBearerTokenFile(configuration.BearerTokenFile)
		if err != nil {
			return clientConnection{}, err
		}
		roundTripper = bearerTokenTransport{base: transport, token: token}
	}
	if configuration.oidcComplete() {
		token, err := fetchOIDCAccessToken(ctx, configuration)
		if err != nil {
			return clientConnection{}, err
		}
		roundTripper = bearerTokenTransport{base: transport, token: token}
	}
	if strings.TrimSpace(configuration.DevelopmentSubject) != "" {
		roundTripper = developmentIdentityTransport{
			base:    transport,
			subject: strings.TrimSpace(configuration.DevelopmentSubject),
			groups:  normalized(configuration.DevelopmentGroups),
		}
	}
	return clientConnection{
		client:  &http.Client{Transport: roundTripper, Timeout: configuration.Timeout, CheckRedirect: rejectRedirect},
		cleanup: func() { transport.CloseIdleConnections() },
	}, nil
}

const maximumBearerTokenBytes = 64 * 1024

func readBearerTokenFile(path string) (string, error) {
	contents, err := readPrivateContents(path, privateFileLabels{
		inspect: "inspect bearer token file",
		open:    "open bearer token file",
		read:    "read bearer token file",
		regular: "bearer token file must be a regular file",
		perms:   "bearer token file permissions must not grant group or other access",
		empty:   "bearer token file must contain exactly one non-empty token",
		tooLong: "bearer token exceeds %d bytes",
	})
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(contents, "{") {
		grant, err := decodeRefreshGrant(contents)
		if err != nil {
			return "", err
		}
		return grant.Token, nil
	}
	if contents == "" || strings.ContainsAny(contents, " \t\r\n,") {
		return "", errors.New("bearer token file must contain exactly one non-empty token")
	}
	return contents, nil
}

func readOIDCClientSecret(path string) (string, error) {
	return readPrivateFile(path, privateFileLabels{
		inspect: "inspect OIDC client secret file",
		open:    "open OIDC client secret file",
		read:    "read OIDC client secret file",
		regular: "OIDC client secret file must be a regular file",
		perms:   "OIDC client secret file permissions must not grant group or other access",
		empty:   "OIDC client secret file must contain exactly one non-empty secret",
		tooLong: "OIDC client secret exceeds %d bytes",
	})
}

type privateFileLabels struct {
	inspect string
	open    string
	read    string
	regular string
	perms   string
	empty   string
	tooLong string
}

func readPrivateFile(path string, labels privateFileLabels) (string, error) {
	token, err := readPrivateContents(path, labels)
	if err != nil {
		return "", err
	}
	if token == "" || strings.ContainsAny(token, " \t\r\n,") {
		return "", errors.New(labels.empty)
	}
	return token, nil
}

func readPrivateContents(path string, labels privateFileLabels) (string, error) {
	path = strings.TrimSpace(path)
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", labels.inspect, err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New(labels.regular)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", errors.New(labels.perms)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", labels.open, err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maximumBearerTokenBytes+1))
	if err != nil {
		return "", fmt.Errorf("%s: %w", labels.read, err)
	}
	if len(contents) > maximumBearerTokenBytes {
		return "", fmt.Errorf(labels.tooLong, maximumBearerTokenBytes)
	}
	return strings.TrimSpace(string(contents)), nil
}

func (configuration ConnectionConfig) oidcRequested() bool {
	return strings.TrimSpace(configuration.OIDCIssuer) != "" ||
		strings.TrimSpace(configuration.OIDCClientID) != "" ||
		strings.TrimSpace(configuration.OIDCClientSecretFile) != "" ||
		strings.TrimSpace(configuration.OIDCTokenURL) != "" ||
		strings.TrimSpace(configuration.OIDCScope) != "" ||
		strings.TrimSpace(configuration.OIDCCAFile) != ""
}

func (configuration ConnectionConfig) oidcComplete() bool {
	return strings.TrimSpace(configuration.OIDCIssuer) != "" &&
		strings.TrimSpace(configuration.OIDCClientID) != "" &&
		strings.TrimSpace(configuration.OIDCClientSecretFile) != ""
}

func (configuration ConnectionConfig) validateOIDC() error {
	if !configuration.oidcRequested() {
		return nil
	}
	if !configuration.oidcComplete() {
		return errors.New("--oidc-issuer, --oidc-client-id, and --oidc-client-secret-file must be supplied together")
	}
	if err := requireHTTPSURL(configuration.OIDCIssuer, "--oidc-issuer"); err != nil {
		return err
	}
	if strings.TrimSpace(configuration.OIDCTokenURL) != "" {
		if err := requireHTTPSURL(configuration.OIDCTokenURL, "--oidc-token-url"); err != nil {
			return err
		}
	}
	return nil
}

func requireHTTPSURL(raw, name string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be an absolute https URL without userinfo, query, or fragment", name)
	}
	return nil
}

type bearerTokenTransport struct {
	base  http.RoundTripper
	token string
}

func (transport bearerTokenTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+transport.token)
	return transport.base.RoundTrip(clone)
}

func rejectRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

type developmentIdentityTransport struct {
	base    http.RoundTripper
	subject string
	groups  []string
}

func (transport developmentIdentityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header = request.Header.Clone()
	clone.Header.Set("X-Platform-Subject", transport.subject)
	if len(transport.groups) != 0 {
		clone.Header.Set("X-Platform-Groups", strings.Join(transport.groups, ","))
	}
	return transport.base.RoundTrip(clone)
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalized(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
