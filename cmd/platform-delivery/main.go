package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jonathanagustin/conduit/internal/buildinfo"
	"github.com/jonathanagustin/conduit/internal/consoleui"
	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/foundryinterop"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	address := os.Getenv("LISTEN_ADDRESS")
	if address == "" {
		address = ":8081"
	}
	consoleBootstrap, err := consoleui.LoadBootstrapFile(os.Getenv("CONSOLE_BOOTSTRAP_FILE"))
	if err != nil {
		slog.Error("load console bootstrap", "error", err)
		os.Exit(1)
	}
	repository, closeRepository, err := openRepository(ctx, os.Getenv)
	if err != nil {
		slog.Error("initialize durable repository", "error", err)
		os.Exit(1)
	}
	defer closeRepository()
	bundlePrivateKey, err := loadBundleSigningKey(os.Getenv("BUNDLE_SIGNING_KEY_FILE"))
	if err != nil {
		slog.Error("load Bundle signing key", "error", err)
		os.Exit(1)
	}
	trustedBundleKeys, err := loadTrustedBundleKeys(os.Getenv("BUNDLE_TRUSTED_KEYS_FILE"))
	if err != nil {
		slog.Error("load trusted Bundle keys", "error", err)
		os.Exit(1)
	}
	if len(bundlePrivateKey) == 0 {
		slog.Warn("BUNDLE_SIGNING_KEY_FILE is unset; Bundle signatures will use an ephemeral development key")
	}
	artifactScanner, err := loadArtifactScanner(os.Getenv("EXTERNAL_ARTIFACT_SCANNER"), os.Getenv("EXTERNAL_ARTIFACT_SCANNER_TIMEOUT"))
	if err != nil {
		slog.Error("configure external artifact scanner", "error", err)
		os.Exit(1)
	}
	allowInsecureAgentIdentity := os.Getenv("ALLOW_INSECURE_AGENT_IDENTITY_HEADERS") == "true"
	allowInsecureOperatorIdentity := os.Getenv("ALLOW_INSECURE_OPERATOR_IDENTITY_HEADERS") == "true"
	operatorIdentityResolver, err := loadOIDCOperatorIdentityResolver(ctx, os.Getenv)
	if err != nil {
		slog.Error("configure operator OIDC identity", "error", err)
		os.Exit(1)
	}
	if operatorIdentityResolver != nil && allowInsecureOperatorIdentity {
		slog.Error("OIDC operator identity cannot be combined with insecure identity headers")
		os.Exit(1)
	}
	bootstrapAdministrators := splitCommaSeparated(os.Getenv("PLATFORM_BOOTSTRAP_ADMINISTRATORS"))
	spiffeEndpoint := strings.TrimSpace(os.Getenv("SPIFFE_ENDPOINT_SOCKET"))
	agentClientCAFile := os.Getenv("AGENT_CLIENT_CA_FILE")
	tlsCertificateFile := os.Getenv("TLS_CERT_FILE")
	tlsKeyFile := os.Getenv("TLS_KEY_FILE")
	if spiffeEndpoint != "" && (allowInsecureAgentIdentity || allowInsecureOperatorIdentity) {
		slog.Error("SPIFFE identity cannot be combined with insecure identity headers")
		os.Exit(1)
	}
	if spiffeEndpoint != "" && (agentClientCAFile != "" || tlsCertificateFile != "" || tlsKeyFile != "") {
		slog.Error("SPIFFE identity cannot be combined with static TLS identity files")
		os.Exit(1)
	}
	if spiffeEndpoint != "" && len(bundlePrivateKey) == 0 {
		slog.Error("SPIFFE production identity requires BUNDLE_SIGNING_KEY_FILE")
		os.Exit(1)
	}
	if operatorIdentityResolver != nil && spiffeEndpoint == "" && (tlsCertificateFile == "" || tlsKeyFile == "") {
		slog.Error("OIDC operator identity requires TLS_CERT_FILE and TLS_KEY_FILE or SPIFFE server identity")
		os.Exit(1)
	}
	if !allowInsecureAgentIdentity {
		if spiffeEndpoint == "" && (agentClientCAFile == "" || tlsCertificateFile == "" || tlsKeyFile == "") {
			slog.Error("secure Agent identity requires AGENT_CLIENT_CA_FILE, TLS_CERT_FILE, and TLS_KEY_FILE")
			os.Exit(1)
		}
	}
	server := &http.Server{
		Addr: address,
		Handler: composition.NewHubHandler(repository, controlplane.HandlerOptions{
			ConsoleHandler:                       consoleui.Handler(consoleBootstrap),
			AllowInsecureAgentIdentityHeaders:    allowInsecureAgentIdentity,
			AllowInsecureOperatorIdentityHeaders: allowInsecureOperatorIdentity,
			OperatorIdentityResolver:             operatorIdentityResolver,
			BootstrapAdministrators:              bootstrapAdministrators,
			BundlePrivateKey:                     bundlePrivateKey,
			TrustedBundleKeys:                    trustedBundleKeys,
			ArtifactScanner:                      artifactScanner,
			EntityConfigurationAdapters: map[string]controlplane.EntityConfigurationAdapter{
				foundryinterop.AdapterID: foundryinterop.AdaptEntityConfiguration,
			},
			ExportSchedulerContext: ctx,
			ExportSchedulerErrorHandler: func(err error) {
				slog.Error("run scheduled Export Pipeline", "error", err)
			},
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	if agentClientCAFile != "" {
		server.TLSConfig, err = controlplane.AgentServerTLSConfig(agentClientCAFile)
		if err != nil {
			slog.Error("configure Agent client certificate verification", "error", err)
			os.Exit(1)
		}
	}
	var spiffeSource *workloadapi.X509Source
	if spiffeEndpoint != "" {
		spiffeSource, err = workloadapi.NewX509Source(ctx, workloadapi.WithClientOptions(workloadapi.WithAddr(spiffeEndpoint)))
		if err != nil {
			slog.Error("connect to SPIFFE Workload API", "error", err)
			os.Exit(1)
		}
		defer spiffeSource.Close()
		server.TLSConfig, err = controlplane.SPIFFEServerTLSConfig(spiffeSource)
		if err != nil {
			slog.Error("configure SPIFFE transport", "error", err)
			os.Exit(1)
		}
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown failed", "error", err)
		}
	}()

	slog.Info("Platform delivery control plane listening", "address", address, "secureAgentIdentity", !allowInsecureAgentIdentity,
		"version", buildinfo.Version, "commit", buildinfo.Commit)
	if spiffeSource != nil {
		err = server.ListenAndServeTLS("", "")
	} else if tlsCertificateFile != "" || tlsKeyFile != "" {
		err = server.ListenAndServeTLS(tlsCertificateFile, tlsKeyFile)
	} else {
		err = server.ListenAndServe()
	}
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func openRepository(
	ctx context.Context, getenv func(string) string,
) (controlplane.Repository, func(), error) {
	if getenv == nil {
		return nil, func() {}, errors.New("environment reader is required")
	}
	kind := strings.TrimSpace(getenv("REPOSITORY_KIND"))
	if kind == "" {
		kind = "file"
	}
	switch kind {
	case "file":
		dataDirectory := strings.TrimSpace(getenv("DATA_DIRECTORY"))
		if dataDirectory == "" {
			dataDirectory = ".platform-delivery"
		}
		repository, err := controlplane.NewFileRepository(dataDirectory)
		return repository, func() {}, err
	case "postgres":
		dataSourceName, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
		if err != nil {
			return nil, func() {}, err
		}
		repository, err := controlplane.NewPostgresRepository(ctx, dataSourceName)
		if err != nil {
			return nil, func() {}, err
		}
		return repository, func() { _ = repository.Close() }, nil
	default:
		return nil, func() {}, fmt.Errorf("REPOSITORY_KIND must be file or postgres, got %q", kind)
	}
}

func readSingleLineSecretFile(path, variable string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s is required", variable)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", variable, err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, 64*1024+1))
	if err != nil {
		return "", fmt.Errorf("read %s: %w", variable, err)
	}
	if len(contents) > 64*1024 {
		return "", fmt.Errorf("%s exceeds 64 KiB", variable)
	}
	value := strings.TrimSpace(string(contents))
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("%s must contain exactly one non-empty line", variable)
	}
	return value, nil
}

func loadOIDCOperatorIdentityResolver(
	ctx context.Context, getenv func(string) string,
) (controlplane.OperatorIdentityResolver, error) {
	if getenv == nil {
		return nil, errors.New("environment reader is required")
	}
	issuer := strings.TrimSpace(getenv("OPERATOR_OIDC_ISSUER"))
	profile := strings.TrimSpace(getenv("OPERATOR_OIDC_PROFILE"))
	configuredValues := []string{profile, getenv("OPERATOR_OIDC_JWKS_URL"), getenv("OPERATOR_OIDC_CLIENT_ID"),
		getenv("OPERATOR_OIDC_USERNAME_CLAIM"), getenv("OPERATOR_OIDC_GROUPS_CLAIM"),
		getenv("OPERATOR_OIDC_SERVICE_ACCOUNT_CLAIM"), getenv("OPERATOR_OIDC_AUDIENCE_CLAIM"),
		getenv("OPERATOR_OIDC_TOKEN_PURPOSE_CLAIM"), getenv("OPERATOR_OIDC_EXPECTED_TOKEN_PURPOSE"),
		getenv("OPERATOR_OIDC_SCOPE_CLAIM"), getenv("OPERATOR_OIDC_REQUIRED_SCOPES"),
		getenv("OPERATOR_OIDC_SIGNING_ALGORITHMS"), getenv("OPERATOR_OIDC_CA_FILE")}
	if issuer == "" {
		for _, value := range configuredValues {
			if strings.TrimSpace(value) != "" {
				return nil, errors.New("OPERATOR_OIDC_ISSUER is required when any OIDC setting is configured")
			}
		}
		return nil, nil
	}
	configuration := controlplane.OIDCOperatorIdentityConfig{
		Issuer:               issuer,
		JWKSURL:              getenv("OPERATOR_OIDC_JWKS_URL"),
		ClientID:             getenv("OPERATOR_OIDC_CLIENT_ID"),
		AudienceClaim:        getenv("OPERATOR_OIDC_AUDIENCE_CLAIM"),
		UsernameClaim:        getenv("OPERATOR_OIDC_USERNAME_CLAIM"),
		GroupsClaim:          getenv("OPERATOR_OIDC_GROUPS_CLAIM"),
		ServiceAccountClaim:  getenv("OPERATOR_OIDC_SERVICE_ACCOUNT_CLAIM"),
		TokenPurposeClaim:    getenv("OPERATOR_OIDC_TOKEN_PURPOSE_CLAIM"),
		ExpectedTokenPurpose: getenv("OPERATOR_OIDC_EXPECTED_TOKEN_PURPOSE"),
		ScopeClaim:           getenv("OPERATOR_OIDC_SCOPE_CLAIM"),
		RequiredScopes:       splitCommaSeparated(getenv("OPERATOR_OIDC_REQUIRED_SCOPES")),
		AllowedSigningAlgs:   splitCommaSeparated(getenv("OPERATOR_OIDC_SIGNING_ALGORITHMS")),
	}
	switch profile {
	case "", "generic":
	case "multipass":
		if configuration.UsernameClaim == "" {
			configuration.UsernameClaim = "sub"
		}
		if len(configuration.RequiredScopes) == 0 {
			configuration.RequiredScopes = []string{"apollo-api:use-api"}
		}
	case "cognito-access":
		if configuration.AudienceClaim == "" {
			configuration.AudienceClaim = "client_id"
		}
		if configuration.UsernameClaim == "" {
			configuration.UsernameClaim = "username"
		}
		if configuration.GroupsClaim == "" {
			configuration.GroupsClaim = "cognito:groups"
		}
		if configuration.TokenPurposeClaim == "" && configuration.ExpectedTokenPurpose == "" {
			configuration.TokenPurposeClaim = "token_use"
			configuration.ExpectedTokenPurpose = "access"
		}
	default:
		return nil, fmt.Errorf("OPERATOR_OIDC_PROFILE must be generic, multipass, or cognito-access, got %q", profile)
	}
	httpClient, err := oidcHTTPClient(getenv("OPERATOR_OIDC_CA_FILE"))
	if err != nil {
		return nil, err
	}
	configuration.HTTPClient = httpClient
	return controlplane.NewOIDCOperatorIdentityResolver(ctx, configuration)
}

func oidcHTTPClient(caFile string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if strings.TrimSpace(caFile) != "" {
		contents, err := os.ReadFile(strings.TrimSpace(caFile))
		if err != nil {
			return nil, fmt.Errorf("read OPERATOR_OIDC_CA_FILE: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(contents) {
			return nil, errors.New("OPERATOR_OIDC_CA_FILE contains no certificates")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}, nil
}

func loadArtifactScanner(command, timeoutValue string) (controlplane.ArtifactScanner, error) {
	if strings.TrimSpace(command) == "" {
		if strings.TrimSpace(timeoutValue) != "" {
			return nil, errors.New("EXTERNAL_ARTIFACT_SCANNER_TIMEOUT requires EXTERNAL_ARTIFACT_SCANNER")
		}
		return nil, nil
	}
	var timeout time.Duration
	var err error
	if strings.TrimSpace(timeoutValue) != "" {
		timeout, err = time.ParseDuration(timeoutValue)
		if err != nil {
			return nil, fmt.Errorf("parse EXTERNAL_ARTIFACT_SCANNER_TIMEOUT: %w", err)
		}
	}
	return controlplane.NewExternalArtifactScanner(command, timeout)
}

func loadBundleSigningKey(path string) (ed25519.PrivateKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, rest := pem.Decode(contents)
	if block == nil || len(strings.TrimSpace(string(rest))) != 0 {
		return nil, errors.New("expected one PEM-encoded PKCS#8 private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("Bundle signing key must be Ed25519")
	}
	return append(ed25519.PrivateKey(nil), key...), nil
}

func splitCommaSeparated(value string) []string {
	seen := map[string]struct{}{}
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, exists := seen[item]; !exists {
			seen[item] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}

func loadTrustedBundleKeys(path string) (map[string]ed25519.PublicKey, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	encoded := make(map[string]string)
	if err := json.Unmarshal(contents, &encoded); err != nil {
		return nil, fmt.Errorf("decode trusted-key map: %w", err)
	}
	keys := make(map[string]ed25519.PublicKey, len(encoded))
	for id, value := range encoded {
		decoded, decodeErr := base64.RawStdEncoding.DecodeString(value)
		if decodeErr != nil {
			decoded, decodeErr = base64.StdEncoding.DecodeString(value)
		}
		if decodeErr != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("trusted Bundle key %q must be a base64 Ed25519 public key", id)
		}
		keys[id] = ed25519.PublicKey(decoded)
	}
	return keys, nil
}
