// Package simulator is a fake cluster behind the Agent's ports.
//
// The Agent reaches the world through four interfaces: agent.Executor applies
// and observes Helm releases, agent.SecretMutationExecutor applies secret
// changes, and agent.HealthPodDiscoverer with agent.HealthEndpointProber find
// and probe pods. Everything else the Agent does -- leasing a Plan, starting
// it, heartbeating, finishing it with a report, posting health observations --
// goes to the Hub over HTTP and is not faked here. So a Worker built with these
// adapters runs the real delivery protocol against a real Hub and only pretends
// about the cluster.
//
// That is the point. Reported state reaches the console by exactly two routes,
// both of which require an Agent holding a lease token minted for an approved
// Plan, so nothing short of an Agent can move the fleet screens off UNKNOWN and
// stale. A script POSTing reported state cannot: the endpoints are
// lease-gated, and health observations are refused unless a Plan has already
// succeeded.
//
// This package is imported only by cmd/platform-simulated-agent, which is never
// added to a Containerfile. It is deliberately NOT reachable from
// cmd/platform-spoke-agent: an agent's charter is to report what is actually
// running, and a shipped binary that can be switched into fabricating healthy
// reports would let a control plane lie about the state of the world, which is
// the one thing it exists not to do. The repository already refuses to ship a
// third component -- scripts/assemble-oci-manifest.sh asserts the release
// manifest carries exactly hub and spoke-agent -- so that exclusion is enforced
// rather than merely intended.
package simulator

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// FailureMarker and DegradedMarker are how a simulation asks for something
// other than success. They are matched against the Product coordinate, which
// the seed controls, so the scenario is stated in the desired state and nowhere
// else.
//
// Keyed off Plan content rather than off configuration or chance on purpose.
// Random failure would make the console suite flaky, and the suites here run
// with retries disabled precisely so that a race is visible rather than
// retried away. A runtime fault-injection channel would put the scenario
// somewhere the test that depends on it cannot see.
const (
	FailureMarker  = "-doomed"
	DegradedMarker = "-degraded"
)

// Executor stands in for agent.HelmExecutor.
type Executor struct {
	// ApplyDelay makes an execution observable rather than instantaneous, so a
	// Plan spends a moment RUNNING and the Hub's heartbeat path is exercised.
	// It is the only knob, and it does not change any outcome.
	ApplyDelay time.Duration
}

var _ agent.Executor = Executor{}

func (executor Executor) Apply(
	ctx context.Context, _ controlplane.Plan, step controlplane.PlanStep,
) (agent.Receipt, error) {
	if executor.ApplyDelay > 0 {
		select {
		case <-ctx.Done():
			return agent.Receipt{}, ctx.Err()
		case <-time.After(executor.ApplyDelay):
		}
	}
	if strings.Contains(step.Product, FailureMarker) {
		// DiagnosticError rather than a bare error: it is what carries the
		// output and resource diagnostics the console's failed-Plan view
		// renders, so a simulated failure exercises that view rather than an
		// empty one.
		return agent.Receipt{}, agent.DiagnosticError{
			Err:    errors.New("simulated apply failure"),
			Output: "Error: INSTALLATION FAILED: simulated failure for " + step.InstallationID,
			Resources: []controlplane.ResourceDiagnostic{{
				Kind: "Deployment", Name: step.InstallationID, Namespace: step.Namespace,
				Message: "simulated: no healthy replicas",
			}},
		}
	}
	return agent.Receipt{Step: step, PreviousRevision: 0}, nil
}

// Observe echoes the step's own coordinates back. That is not laziness: the
// fleet projection compares the reported version and digests against the
// desired ones, so an observation that invented its own would render every
// simulated installation as drifted and out of date, and the simulation would
// be testing a permanent disagreement rather than a working system.
func (executor Executor) Observe(
	_ context.Context, step controlplane.PlanStep,
) (controlplane.ReportedEntity, error) {
	health := controlplane.HealthHealthy
	if strings.Contains(step.Product, DegradedMarker) {
		health = controlplane.HealthWarning
	}
	return controlplane.ReportedEntity{
		InstallationID: step.InstallationID, Product: step.Product, Version: step.Version,
		ReleaseDigest: step.ReleaseDigest, Namespace: step.Namespace, ConfigDigest: step.ConfigDigest,
		Health: health, RolloutStatus: controlplane.RolloutUpToDate,
		ConfigStatus: controlplane.ConfigApplied, Lifecycle: controlplane.LifecycleInstalled,
	}, nil
}

// Rollback succeeds. A simulated rollback that failed would be a second
// scenario keyed off nothing the seed states, and the states it reaches --
// which installations could not be unwound -- are already covered by the
// Worker's own unit tests.
func (executor Executor) Rollback(_ context.Context, _ agent.Receipt) error { return nil }

// SecretMutations accepts every mutation.
//
// Present because Worker.RunOnce fails a Plan outright when it carries secret
// mutations and no material preparer is configured. No simulation seeds one
// today, so leaving this out would cost nothing now and turn the simulator into
// a silent plan-failure generator the first time one did.
type SecretMutations struct{}

var _ agent.SecretMutationExecutor = SecretMutations{}

func (SecretMutations) ApplySecretMutation(_ context.Context, _ controlplane.SecretMutation) error {
	return nil
}

func (SecretMutations) RollbackSecretMutation(_ context.Context, _ controlplane.SecretMutation) error {
	return nil
}

// PodDiscoverer stands in for agent.KubectlHealthPodDiscoverer.
//
// One pod per target, with identifiers derived from the target rather than
// generated, so two polls of the same installation report the same node and
// container. A discoverer that minted fresh identifiers each round would look
// to the Hub like a cluster replacing its pods continuously.
type PodDiscoverer struct{}

var _ agent.HealthPodDiscoverer = PodDiscoverer{}

func (PodDiscoverer) Discover(
	_ context.Context, target controlplane.HealthTarget,
) ([]agent.HealthProbeTarget, error) {
	return []agent.HealthProbeTarget{{
		InstallationID: target.InstallationID,
		ProductVersion: target.Version,
		NodeID:         "simulated-node-" + target.InstallationID,
		ContainerID:    "simulated-container-" + target.InstallationID,
		URL:            "https://simulated/" + target.Namespace + "/" + target.Entity + "/health",
		Liveness:       true,
		Readiness:      true,
	}}, nil
}

// EndpointProber stands in for agent.HTTPSHealthEndpointProber and never opens
// a socket. The URL PodDiscoverer fabricates is not resolvable, which is the
// point: a simulation that reached the network would be answering a question
// about this machine.
type EndpointProber struct{}

var _ agent.HealthEndpointProber = EndpointProber{}

func (EndpointProber) Probe(_ context.Context, target agent.HealthProbeTarget) []controlplane.HealthCheck {
	result := controlplane.HealthHealthy
	message := "Simulated health probe reports the Entity is serving."
	if strings.Contains(target.InstallationID, DegradedMarker) {
		result = controlplane.HealthWarning
		message = "Simulated health probe reports the Entity is degraded."
	}
	return []controlplane.HealthCheck{{
		ID: "SIMULATED_PROBE", Message: message, Result: result,
		Params: map[string]any{"installationId": target.InstallationID},
	}}
}
