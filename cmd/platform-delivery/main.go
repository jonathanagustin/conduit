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
	controlplaneadapter "github.com/jonathanagustin/conduit/internal/delivery/reconciliation/adapters/controlplane"
	notificationsadapter "github.com/jonathanagustin/conduit/internal/delivery/reconciliation/adapters/notifications"
	reconciliationpostgres "github.com/jonathanagustin/conduit/internal/delivery/reconciliation/adapters/postgres"
	reconciliationapplication "github.com/jonathanagustin/conduit/internal/delivery/reconciliation/application"
	"github.com/jonathanagustin/conduit/internal/foundryinterop"
	"github.com/jonathanagustin/conduit/internal/hub/composition"
	identity "github.com/jonathanagustin/conduit/internal/identity/domain"
	notificationspostgres "github.com/jonathanagustin/conduit/internal/notifications/adapters/postgres"
	notificationsapplication "github.com/jonathanagustin/conduit/internal/notifications/application"
	notificationports "github.com/jonathanagustin/conduit/internal/notifications/ports"
	httpmetrics "github.com/jonathanagustin/conduit/internal/observability/adapters/httpmetrics"
	metricsapplication "github.com/jonathanagustin/conduit/internal/observability/application"
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
	repository, closeReconciliation, reconciliationSupervisor, readStats, notificationStore, notificationDeliveries, err :=
		configureReconciliation(ctx, os.Getenv, repository)
	if err != nil {
		slog.Error("initialize durable reconciliation", "error", err)
		os.Exit(1)
	}
	defer closeReconciliation()
	if reconciliationSupervisor != nil {
		if err := reconciliationSupervisor.Prime(ctx); err != nil {
			slog.Error("prime durable reconciliation", "error", err)
			os.Exit(1)
		}
		go reconciliationSupervisor.Run(ctx)
	}
	// Nil unless an operator configured a notification transport. There is no
	// prime step: an undelivered inbox entry is not a readiness condition, and
	// blocking a Hub's startup on a third-party endpoint would let a broken
	// webhook stop deliveries of software.
	if notificationDeliveries != nil {
		slog.Info("notification delivery enabled")
		go notificationDeliveries.Run(ctx)
	}
	// Nil unless there is a notification store to record into, which is the
	// same condition the notification routes mount on. A Hub without one still
	// accepts recalls; it just has nobody to tell.
	recallOpenings, err := composition.NewRecallOpenings(notificationStore, slog.Default())
	if err != nil {
		slog.Error("configure recall announcements", "error", err)
		os.Exit(1)
	}
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
			RecallOpenings:                       recallOpenings,
			EntityConfigurationAdapters: map[string]controlplane.EntityConfigurationAdapter{
				foundryinterop.AdapterID: foundryinterop.AdaptEntityConfiguration,
			},
			ExportSchedulerContext: ctx,
			ExportSchedulerErrorHandler: func(err error) {
				slog.Error("run scheduled Export Pipeline", "error", err)
			},
		}, composition.Options{NotificationStore: notificationStore}),
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

	// Gitless GitOps is off unless configured on. A misconfiguration is a
	// startup failure rather than a first-tick surprise, because a Hub that
	// started cleanly and silently never deployed an environment is worse than
	// one that refused to start.
	gitlessResolutionLog, closeGitlessResolutionLog, err := openGitlessResolutionLog(ctx, os.Getenv)
	if err != nil {
		slog.Error("open Gitless resolution log", "error", err)
		os.Exit(1)
	}
	defer closeGitlessResolutionLog()
	gitlessScheduler, err := configureGitlessWithResolutionLog(os.Getenv, gitlessResolutionLog)
	if err != nil {
		slog.Error("configure Gitless GitOps", "error", err)
		os.Exit(1)
	}
	if gitlessScheduler != nil {
		bindings := gitlessScheduler.Bindings()
		environments := make([]string, 0, len(bindings))
		for _, binding := range bindings {
			environments = append(environments, binding.Environment+"="+binding.Selector.String())
		}
		slog.Info("Gitless GitOps enabled", "bindings", strings.Join(environments, " "))
		go gitlessScheduler.Run(ctx)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("shutdown failed", "error", err)
		}
	}()

	// Metrics are a separate listener and are off unless an address is set.
	// Bolting an unauthenticated scrape route onto the Hub's port -- which
	// carries operator and Agent traffic behind OIDC, mTLS or SPIFFE -- would
	// turn an authenticated surface into a partly public one. An empty
	// METRICS_ADDRESS adds no reachable surface at all, which is what an
	// existing deployment gets until somebody decides otherwise.
	if stopMetrics := startMetricsListener(ctx, os.Getenv("METRICS_ADDRESS"), readStats); stopMetrics != nil {
		defer stopMetrics()
	}

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

func configureReconciliation(
	ctx context.Context, getenv func(string) string, repository controlplane.Repository,
) (
	controlplane.Repository, func(), *reconciliationapplication.Supervisor,
	metricsapplication.ReadStats, notificationports.Store,
	*notificationsapplication.Dispatcher, error,
) {
	if getenv == nil || repository == nil {
		return nil, func() {}, nil, nil, nil, nil, errors.New("environment reader and repository are required")
	}
	kind := strings.TrimSpace(getenv("REPOSITORY_KIND"))
	if kind == "" || kind == "file" {
		// Explicit single-replica development mode keeps the compatibility
		// repository and does not claim durable multi-replica reconciliation.
		// It reports no queue statistics either, because the queues it would
		// describe belong to the PostgreSQL repository; exporting zeroes here
		// would tell a scraper the fleet is idle rather than that this mode has
		// nothing to say.
		return repository, func() {}, nil, nil, nil, nil, nil
	}
	if kind != "postgres" {
		return nil, func() {}, nil, nil, nil, nil, fmt.Errorf("REPOSITORY_KIND must be file or postgres, got %q", kind)
	}
	dataSourceName, err := readSingleLineSecretFile(getenv("POSTGRES_DSN_FILE"), "POSTGRES_DSN_FILE")
	if err != nil {
		return nil, func() {}, nil, nil, nil, nil, err
	}
	store, err := reconciliationpostgres.New(ctx, dataSourceName)
	if err != nil {
		return nil, func() {}, nil, nil, nil, nil, err
	}
	closeReconciliationStore := func() { _ = store.Close() }
	closeStore := closeReconciliationStore
	bridge, err := controlplaneadapter.NewRepository(repository, store)
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	planner, err := controlplaneadapter.NewPlanner(bridge, store)
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	// The notification store shares the reconciliation DSN. One database, two
	// components in one ledger, which is the arrangement
	// docs/operations/schema-migrations.md already describes: a single place to
	// look during an incident is worth more than a separation nobody queries.
	notificationStore, err := notificationspostgres.New(ctx, dataSourceName)
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	// From here on both stores are open, so every failure path has to close
	// both. The shadowed name is deliberate: the returned closer is this one,
	// and a later edit that forgets a store leaks a connection pool for the
	// lifetime of the process.
	closeStore = func() {
		_ = notificationStore.Close()
		closeReconciliationStore()
	}
	emitter, err := notificationsapplication.NewEmitter(notificationStore)
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	escalations, err := notificationsadapter.NewEscalations(emitter)
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	worker, err := reconciliationapplication.NewWorker(store, planner, reconciliationapplication.SystemClock{},
		escalations, reconciliationapplication.Config{
			LeaseDuration: 30 * time.Second, MaximumObservationAge: 15 * time.Minute,
			MaximumAttempts: 10, BaseRetryDelay: 2 * time.Second, RepairBatchSize: 200,
		})
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	supervisor, err := reconciliationapplication.NewSupervisor(worker, bridge,
		reconciliationapplication.SupervisorConfig{
			PollInterval: time.Second, SyncInterval: 30 * time.Second,
			OnError: func(err error) { slog.Error("durable reconciliation cycle", "error", err) },
		})
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	// The stats reader is the store itself, narrowed to the one call metrics
	// needs. Narrowed rather than passed whole so the metrics path cannot reach
	// anything that writes.
	readStats := func(ctx context.Context) (metricsapplication.ReconciliationStats, error) {
		stats, err := store.Stats(ctx, time.Now())
		if err != nil {
			return metricsapplication.ReconciliationStats{}, err
		}
		return metricsapplication.ReconciliationStats{
			WorkPending: stats.WorkPending, WorkLeased: stats.WorkLeased,
			WorkExhausted: stats.WorkExhausted, OldestPendingAge: stats.OldestPendingAge,
			OutboxPending: stats.OutboxPending, OutboxLeased: stats.OutboxLeased,
			OutboxExhausted: stats.OutboxExhausted, OldestOutboxAge: stats.OldestOutboxAge,
		}, nil
	}
	// Notification delivery, off unless an operator configured a transport.
	// NewNotificationTransport answers nil for an unconfigured deployment and
	// NewNotificationDispatcher answers nil for a nil transport, so an upgrade
	// that changes nothing in the environment starts no delivery loop and opens
	// no connection. A misconfiguration is a startup failure instead, on the
	// same reasoning the Gitless scheduler uses: a Hub that came up cleanly and
	// silently never told anybody anything is worse than one that refused.
	transport, err := composition.NewNotificationTransport(getenv, slog.Default())
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	deliveryConfig := composition.DefaultDispatcherConfig()
	deliveryConfig.OnError = func(err error) { slog.Error("notification delivery cycle", "error", err) }
	deliveries, err := composition.NewNotificationDispatcher(
		notificationStore, transport, notificationsapplication.SystemClock{}, deliveryConfig)
	if err != nil {
		closeStore()
		return nil, func() {}, nil, nil, nil, nil, err
	}
	return bridge, closeStore, supervisor, readStats, notificationStore, deliveries, nil
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
		ClaimPolicy: identity.ClaimPolicy{
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
		},
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

// startMetricsListener serves the metrics endpoint on its own address, and
// returns nil when there is nothing to serve or nowhere to serve it.
//
// Two ways it declines, and they are different. No address means the operator
// did not ask for metrics. No stats reader means the file repository is in use,
// whose queues these metrics describe and which does not have them -- exporting
// zeroes there would tell a scraper the fleet is idle rather than that this mode
// has nothing to say. Both are logged, because a silently absent metrics
// endpoint is exactly the kind of thing somebody debugs for an hour.
func startMetricsListener(ctx context.Context, address string, readStats metricsapplication.ReadStats) func() {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil
	}
	if readStats == nil {
		slog.Warn("METRICS_ADDRESS is set but the repository reports no queue statistics; " +
			"metrics are available only with REPOSITORY_KIND=postgres")
		return nil
	}
	server := &http.Server{
		Addr:              address,
		Handler:           httpmetrics.Handler(metricsapplication.Gather(buildinfo.Version, readStats), httpmetrics.Options{}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		slog.Info("metrics listening", "address", address)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			// A failed metrics listener must not take the Hub down. Delivery
			// works without observability; it is worse to refuse to serve a
			// fleet because a diagnostic port was already in use.
			slog.Error("metrics listener failed", "error", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	return func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}
}
