// Command platform-simulated-agent runs the real Agent against a fake cluster.
//
// It is the second half of scripts/simulate-console.sh. That script gives the
// console a real Hub with real state, but nothing reports, so every
// installation sits UNKNOWN and stale and no Plan ever moves past
// AWAITING_APPROVAL. This joins that Hub as an Agent and runs the genuine
// delivery protocol -- lease, start, heartbeat, finish with a report, then poll
// health -- with internal/agent/adapters/simulator standing in for Helm and
// kubectl. The console then shows a fleet that moves.
//
// This is NOT a mode of cmd/platform-spoke-agent, and that is the whole design.
// An Agent's job is to report what is actually running; a shipped binary that
// could be switched into fabricating healthy reports would let the control
// plane lie about the state of the world. Keeping it a separate command means
// the capability is absent from the released image rather than merely disabled
// in it -- scripts/assemble-oci-manifest.sh asserts the release manifest
// carries exactly hub and spoke-agent, so a third shipped component fails
// assembly.
//
// It also refuses an https control plane. Combined with the Hub-side
// requirement that identity headers only work when the Hub was started with
// ALLOW_INSECURE_AGENT_IDENTITY_HEADERS, that means this command can only ever
// join a Hub already configured the way no deployment should be.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/agent/adapters/simulator"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	controlPlaneURL := required("CONTROL_PLANE_URL")
	environment := required("ENVIRONMENT")
	agentID := os.Getenv("AGENT_ID")
	if agentID == "" {
		agentID = "simulated-agent-" + environment
	}
	if strings.HasPrefix(controlPlaneURL, "https://") {
		slog.Error("the simulated Agent refuses an https control plane; it exists to join a Hub running with insecure identity headers, and joining a real one would report a cluster that does not exist")
		os.Exit(1)
	}

	client, err := agent.NewHTTPControlPlane(controlPlaneURL, &http.Client{
		// A zero Transport would consult the environment's proxy settings, and
		// the Hub this joins is on the loopback interface.
		Timeout: 30 * time.Second, Transport: &http.Transport{},
	})
	if err != nil {
		slog.Error("configure control plane client", "error", err)
		os.Exit(1)
	}

	executor := simulator.Executor{ApplyDelay: duration("APPLY_DELAY", time.Second)}
	worker := agent.Worker{
		ControlPlane: client, Executor: executor, Materials: simulator.SecretMutations{},
		Environment: environment, AgentID: agentID,
		LeaseDuration: duration("LEASE_DURATION", time.Minute),
	}
	monitor := agent.HealthMonitor{
		ControlPlane: client, Discoverer: simulator.PodDiscoverer{}, Prober: simulator.EndpointProber{},
		Environment: environment, AgentID: agentID,
		Interval: duration("HEALTH_POLL_INTERVAL", 5*time.Second),
		OnError:  func(err error) { slog.Error("simulated health monitoring failed", "error", err) },
	}
	go func() {
		if err := monitor.Run(ctx); err != nil {
			slog.Error("simulated health monitor stopped", "error", err)
			stop()
		}
	}()

	slog.Info("simulated Agent joined", "controlPlane", controlPlaneURL, "environment", environment, "agent", agentID)
	pollInterval := duration("POLL_INTERVAL", 2*time.Second)
	for {
		executed, err := worker.RunOnce(ctx)
		if err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("simulated plan execution failed", "error", err)
		} else if executed {
			slog.Info("simulated plan execution completed")
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

func required(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		slog.Error("required environment variable is unset", "name", name)
		os.Exit(1)
	}
	return value
}

func duration(name string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		slog.Error("invalid duration", "name", name, "value", raw)
		os.Exit(1)
	}
	return parsed
}
