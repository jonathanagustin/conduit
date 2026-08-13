package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/buildinfo"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

func main() {
	// Helm re-invokes this binary as its post-renderer, so the subcommand is
	// handled before any Agent configuration is read: a post-render call has no
	// control plane, no identity, and no environment of its own.
	if len(os.Args) > 1 && os.Args[1] == agent.PostRenderCommand {
		if len(os.Args) != 3 {
			slog.Error("post-render requires exactly one metadata file argument")
			os.Exit(2)
		}
		if err := agent.RunPostRenderer(os.Args[2], os.Stdin, os.Stdout); err != nil {
			slog.Error("post-render rendered manifests", "error", err)
			os.Exit(1)
		}
		return
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	slog.Info("starting platform Spoke agent", "version", buildinfo.Version, "commit", buildinfo.Commit)
	controlPlaneURL := requiredEnvironment("CONTROL_PLANE_URL")
	environment := requiredEnvironment("ENVIRONMENT_ID")
	agentID := requiredEnvironment("AGENT_ID")
	leaseDuration := durationEnvironment("LEASE_DURATION", 2*time.Minute)
	pollInterval := durationEnvironment("POLL_INTERVAL", 15*time.Second)
	helmTimeout := durationEnvironment("HELM_TIMEOUT", 5*time.Minute)
	healthPollInterval := durationEnvironment("HEALTH_POLL_INTERVAL", 5*time.Second)
	if healthPollInterval < 5*time.Second {
		slog.Error("HEALTH_POLL_INTERVAL must be at least 5s")
		os.Exit(1)
	}
	helmPlainHTTP := os.Getenv("HELM_PLAIN_HTTP") == "true"
	var materialPreparer *agent.ExternalMaterialPreparer
	if command := os.Getenv("EXTERNAL_MATERIAL_PREPARER"); command != "" {
		prepared, prepareErr := agent.NewExternalMaterialPreparer(command)
		if prepareErr != nil {
			slog.Error("configure external material preparer", "error", prepareErr)
			os.Exit(1)
		}
		materialPreparer = prepared
	}

	spiffeEndpoint := strings.TrimSpace(os.Getenv("SPIFFE_ENDPOINT_SOCKET"))
	legacyCA := os.Getenv("CONTROL_PLANE_CA_FILE")
	legacyCertificate := os.Getenv("CONTROL_PLANE_CLIENT_CERT_FILE")
	legacyKey := os.Getenv("CONTROL_PLANE_CLIENT_KEY_FILE")
	var httpClient *http.Client
	var err error
	var spiffeSource *workloadapi.X509Source
	if spiffeEndpoint != "" {
		if legacyCA != "" || legacyCertificate != "" || legacyKey != "" {
			slog.Error("SPIFFE identity cannot be combined with static control plane TLS files")
			os.Exit(1)
		}
		if !strings.HasPrefix(controlPlaneURL, "https://") {
			slog.Error("SPIFFE identity requires an https CONTROL_PLANE_URL")
			os.Exit(1)
		}
		spiffeSource, err = workloadapi.NewX509Source(ctx, workloadapi.WithClientOptions(workloadapi.WithAddr(spiffeEndpoint)))
		if err == nil {
			httpClient, err = agent.NewSPIFFEControlPlaneHTTPClient(spiffeSource, requiredEnvironment("CONTROL_PLANE_SPIFFE_ID"))
		}
	} else {
		httpClient, err = agent.NewControlPlaneHTTPClient(legacyCA, legacyCertificate, legacyKey)
	}
	if err != nil {
		slog.Error("configure control plane transport", "error", err)
		os.Exit(1)
	}
	if spiffeSource != nil {
		defer spiffeSource.Close()
	}
	client, err := agent.NewHTTPControlPlane(controlPlaneURL, httpClient)
	if err != nil {
		slog.Error("configure control plane client", "error", err)
		os.Exit(1)
	}
	healthClient, err := agent.NewHealthProbeHTTPClient(os.Getenv("HEALTH_PROBE_CA_FILE"))
	if err != nil {
		slog.Error("configure health probe transport", "error", err)
		os.Exit(1)
	}
	// The Helm 4 post-renderer plugin is installed lazily by the executor, once
	// it has detected the Helm major version. Installing it here would expose it
	// to a Helm 3, which rejects the plugin type and corrupts every command's
	// JSON output with the load failure.
	executor := agent.HelmExecutor{
		Timeout: helmTimeout, PlainHTTP: helmPlainHTTP,
		Environment: environment, Version: buildinfo.Version,
		PluginRoot: filepath.Join(os.TempDir(), "platform-helm-plugins"),
	}
	worker := agent.Worker{
		ControlPlane: client,
		Executor:     executor,
		Environment:  environment, AgentID: agentID, LeaseDuration: leaseDuration,
	}
	attachMaterialPreparer(&executor, &worker, materialPreparer)

	monitor := agent.HealthMonitor{

		ControlPlane: client, Discoverer: agent.KubectlHealthPodDiscoverer{},
		Prober:      agent.HTTPSHealthEndpointProber{Client: healthClient},
		Environment: environment, AgentID: agentID, Interval: healthPollInterval,
		OnError: func(err error) { slog.Error("health monitoring failed", "error", err) },
	}
	go func() {
		if err := monitor.Run(ctx); err != nil {
			slog.Error("health monitor stopped", "error", err)
			stop()
		}
	}()
	for {
		executed, err := worker.RunOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("plan execution failed", "error", err)
		} else if executed {
			slog.Info("plan execution completed")
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func attachMaterialPreparer(
	executor *agent.HelmExecutor,
	worker *agent.Worker,
	materialPreparer *agent.ExternalMaterialPreparer,
) {
	// Assign interfaces only when the concrete pointer exists. A typed nil
	// pointer stored in an interface is non-nil and would otherwise bypass the
	// executor's missing-provider checks.
	if materialPreparer != nil {
		executor.Registry = materialPreparer
		executor.Images = materialPreparer
		worker.Executor = executor
		worker.Materials = materialPreparer
	}
}

func requiredEnvironment(name string) string {
	value := os.Getenv(name)
	if value == "" {
		slog.Error("required environment variable is missing", "name", name)
		os.Exit(1)
	}
	return value
}

func durationEnvironment(name string, fallback time.Duration) time.Duration {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		slog.Error("invalid duration environment variable", "name", name, "value", strconv.Quote(value))
		os.Exit(1)
	}
	return duration
}
