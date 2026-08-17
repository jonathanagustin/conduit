package simulator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/agent"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

func step(product string) controlplane.PlanStep {
	return controlplane.PlanStep{
		Position: 1, Action: "reconcile", InstallationID: "example-api", Entity: "example-api",
		Product: product, Version: "0.1.0", Namespace: "example",
		ReleaseDigest: "sha256:release", ConfigDigest: "sha256:config",
	}
}

// The rule this pins is the Hub's, not the simulator's: a Plan that succeeded
// may not report a degraded Entity. The Hub refuses the finish with "invalid
// plan transition: successful plan reported ... health=WARNING" and leaves the
// Plan RUNNING forever. An earlier version of Observe returned WARNING for the
// degraded marker and did exactly that.
//
// Only the live degraded scenario caught it, and that is a script somebody has
// to remember to run. This is the cheap guard that stops the branch coming
// back.
func TestObserveAlwaysReportsHealthyWhateverTheProductIsCalled(t *testing.T) {
	t.Parallel()
	for _, product := range []string{
		"app.example:example-api",
		"app.example:example" + DegradedMarker,
		"app.example:example" + FailureMarker,
	} {
		reported, err := Executor{}.Observe(context.Background(), step(product))
		if err != nil {
			t.Fatalf("observe %s: %v", product, err)
		}
		if reported.Health != controlplane.HealthHealthy {
			t.Errorf("observe %s reported health %q; a successful Plan may only report healthy",
				product, reported.Health)
		}
	}
}

// Degradation has to arrive somewhere, and the health-observation path is the
// only place the Hub accepts it. Asserted alongside the rule above because the
// two together are the design: the delivery report says whether the apply
// worked, the probe says how the Entity is faring afterwards. Testing either
// alone would leave the other free to absorb the behaviour.
func TestTheProbeIsWhereDegradationIsReported(t *testing.T) {
	t.Parallel()
	healthy := EndpointProber{}.Probe(context.Background(), agent.HealthProbeTarget{InstallationID: "example-api"})
	if len(healthy) != 1 || healthy[0].Result != controlplane.HealthHealthy {
		t.Fatalf("a healthy Entity probed as %#v", healthy)
	}
	degraded := EndpointProber{}.Probe(context.Background(),
		agent.HealthProbeTarget{InstallationID: "example" + DegradedMarker})
	if len(degraded) != 1 || degraded[0].Result != controlplane.HealthWarning {
		t.Fatalf("a degraded Entity probed as %#v", degraded)
	}
}

// The fleet projection compares the reported version and digests against the
// desired ones, so an observation that invented its own would render every
// simulated installation drifted and out of date -- and the simulation would be
// testing a permanent disagreement rather than a working system.
func TestObserveEchoesTheStepItWasGiven(t *testing.T) {
	t.Parallel()
	given := step("app.example:example-api")
	reported, err := Executor{}.Observe(context.Background(), given)
	if err != nil {
		t.Fatal(err)
	}
	if reported.Version != given.Version || reported.ReleaseDigest != given.ReleaseDigest ||
		reported.ConfigDigest != given.ConfigDigest || reported.Namespace != given.Namespace ||
		reported.InstallationID != given.InstallationID || reported.Product != given.Product {
		t.Errorf("observe invented coordinates: %#v from %#v", reported, given)
	}
	if reported.RolloutStatus != controlplane.RolloutUpToDate ||
		reported.ConfigStatus != controlplane.ConfigApplied ||
		reported.Lifecycle != controlplane.LifecycleInstalled {
		t.Errorf("observe reported a state the fleet would show as drifted: %#v", reported)
	}
}

// The failure is keyed off the Product coordinate, which the seed controls, and
// it carries diagnostics -- a bare error would render an empty diagnostics
// section in the console and the failure scenario would assert nothing about
// the path it exists to exercise.
func TestApplyFailsOnlyForTheMarkedProductAndCarriesDiagnostics(t *testing.T) {
	t.Parallel()
	if _, err := (Executor{}).Apply(context.Background(), controlplane.Plan{}, step("app.example:example-api")); err != nil {
		t.Fatalf("an unmarked Product failed to apply: %v", err)
	}

	_, err := (Executor{}).Apply(context.Background(), controlplane.Plan{},
		step("app.example:example"+FailureMarker))
	if err == nil {
		t.Fatal("the marked Product applied successfully")
	}
	var diagnostic agent.DiagnosticError
	if !errors.As(err, &diagnostic) {
		t.Fatalf("the failure was not a DiagnosticError: %T %v", err, err)
	}
	if len(diagnostic.Resources) == 0 || diagnostic.Output == "" {
		t.Errorf("the failure carried no diagnostics: %#v", diagnostic)
	}
}

// A cancelled context has to win over the delay. Without this the Agent would
// keep applying through a shutdown, and the harness kills it between scenarios.
func TestApplyStopsWhenTheContextIsCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Executor{ApplyDelay: time.Minute}).Apply(ctx, controlplane.Plan{},
		step("app.example:example-api")); !errors.Is(err, context.Canceled) {
		t.Fatalf("apply ignored a cancelled context: %v", err)
	}
}

// Identifiers derived from the target rather than generated: two polls of one
// installation must describe the same pod, or the Hub sees a cluster replacing
// its pods continuously.
func TestDiscoveryIsStableAcrossPolls(t *testing.T) {
	t.Parallel()
	target := controlplane.HealthTarget{
		InstallationID: "example-api", Entity: "example-api", Version: "0.1.0", Namespace: "example",
	}
	first, err := PodDiscoverer{}.Discover(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	second, err := PodDiscoverer{}.Discover(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("expected one probe target per poll, got %d and %d", len(first), len(second))
	}
	if first[0] != second[0] {
		t.Errorf("two polls described different pods:\n%#v\n%#v", first[0], second[0])
	}
}
