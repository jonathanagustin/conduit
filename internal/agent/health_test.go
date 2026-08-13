package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type healthCommandRunner struct {
	output []byte
	args   []string
}

func (runner *healthCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	runner.args = append([]string{name}, args...)
	return runner.output, nil
}

func TestKubectlHealthDiscoveryParsesApolloAnnotationIntoHTTPSProbe(t *testing.T) {
	pods := map[string]any{"items": []any{map[string]any{
		"metadata": map[string]any{"name": "api-abc", "uid": "pod-uid", "annotations": map[string]string{
			healthProbeAnnotation: `{"containerProbes":{"api":{"path":"/telemetry","port":"8446"}}}`,
		}},
		"spec": map[string]any{"containers": []any{map[string]any{"name": "api"}}},
		"status": map[string]any{"podIP": "10.0.0.8", "containerStatuses": []any{map[string]any{
			"name": "api", "containerID": "containerd://abc", "ready": true, "started": true,
			"state": map[string]any{"running": map[string]any{}},
		}}},
	}}}
	encoded, _ := json.Marshal(pods)
	runner := &healthCommandRunner{output: encoded}
	probes, err := (KubectlHealthPodDiscoverer{Runner: runner}).Discover(context.Background(), controlplane.HealthTarget{
		InstallationID: "api", Entity: "api", Product: "app.example:api", Version: "1.2.3", Namespace: "example",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != 1 || probes[0].URL != "https://10.0.0.8:8446/telemetry/status/health" ||
		probes[0].NodeID != "pod-uid" || probes[0].ContainerID != "containerd://abc" ||
		!probes[0].Liveness || !probes[0].Readiness {
		t.Fatalf("discovered probes = %#v", probes)
	}
	if !strings.Contains(strings.Join(runner.args, " "), "--selector app.kubernetes.io/instance=api") {
		t.Fatalf("kubectl arguments = %#v", runner.args)
	}
}

func TestHTTPSHealthEndpointProberPreservesChecksAndBoundsPayloads(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/status/health" || request.Header.Get("Accept") != "application/json" {
			t.Errorf("request = %#v", request)
		}
		_, _ = w.Write([]byte(`{"checks":{"DISK_CAPACITY":{"type":"DISK_CAPACITY","state":"WARNING","message":"low","params":{"remaining":25}}}}`))
	}))
	defer server.Close()
	checks := (HTTPSHealthEndpointProber{Client: server.Client()}).Probe(context.Background(), HealthProbeTarget{URL: server.URL + "/status/health"})
	if len(checks) != 1 || checks[0].ID != "DISK_CAPACITY" || checks[0].Result != controlplane.HealthWarning || checks[0].Params["remaining"] != float64(25) {
		t.Fatalf("health checks = %#v", checks)
	}

	oversized := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 10_001)))
	}))
	defer oversized.Close()
	checks = (HTTPSHealthEndpointProber{Client: oversized.Client()}).Probe(context.Background(), HealthProbeTarget{URL: oversized.URL})
	if len(checks) != 1 || checks[0].ID != "HEALTH_CHECK_MAX_LEN" || checks[0].Result != controlplane.HealthWarning {
		t.Fatalf("oversized health response = %#v", checks)
	}

	failing := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "sensitive", http.StatusInternalServerError)
	}))
	defer failing.Close()
	checks = (HTTPSHealthEndpointProber{Client: failing.Client()}).Probe(context.Background(), HealthProbeTarget{URL: failing.URL})
	if len(checks) != 1 || checks[0].Result != controlplane.HealthError || strings.Contains(checks[0].Message, "sensitive") {
		t.Fatalf("failed health response = %#v", checks)
	}
}

type healthControl struct {
	targets     []controlplane.HealthTarget
	reported    []controlplane.HealthObservation
	environment string
	agentID     string
}

func (control *healthControl) HealthTargets(context.Context, string, string) ([]controlplane.HealthTarget, error) {
	return control.targets, nil
}

func (control *healthControl) ReportHealth(_ context.Context, environment, agentID string, observations []controlplane.HealthObservation) error {
	control.environment, control.agentID = environment, agentID
	control.reported = observations
	return nil
}

type healthDiscovery struct {
	probes []HealthProbeTarget
	// err fails a polling round, which is the only way to reach the error
	// handling in Run.
	err error
}

func (discovery healthDiscovery) Discover(context.Context, controlplane.HealthTarget) ([]HealthProbeTarget, error) {
	return discovery.probes, discovery.err
}

type healthProber struct{ checks []controlplane.HealthCheck }

func (prober healthProber) Probe(context.Context, HealthProbeTarget) []controlplane.HealthCheck {
	return prober.checks
}

func TestHealthMonitorReportsEachTargetAndEnforcesFiveSecondCadence(t *testing.T) {
	control := &healthControl{targets: []controlplane.HealthTarget{{
		InstallationID: "api", Entity: "api", Product: "app.example:api", Version: "1.2.3", Namespace: "example", Quorum: "ALL",
	}}}
	monitor := HealthMonitor{
		ControlPlane: control,
		Discoverer: healthDiscovery{probes: []HealthProbeTarget{{
			InstallationID: "api", ProductVersion: "1.2.3", NodeID: "pod", ContainerID: "container",
			Liveness: true, Readiness: true,
		}}},
		Prober: healthProber{checks: []controlplane.HealthCheck{{
			ID: "DATABASE", Result: controlplane.HealthHealthy, Params: map[string]any{},
		}}},
		Environment: "dev-use2", AgentID: "agent-dev", Now: func() time.Time { return time.Unix(200, 0) },
	}
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if control.environment != "dev-use2" || control.agentID != "agent-dev" || len(control.reported) != 1 ||
		len(control.reported[0].Details.Nodes) != 1 || len(control.reported[0].Details.Nodes[0].Containers[0].Checks) != 1 {
		t.Fatalf("reported health = %#v", control.reported)
	}
	// Cancelled up front rather than a background context: this asserts that
	// Run refuses the interval before it starts ticking, and if it ever stops
	// refusing, the test must end rather than poll forever. A mutation making
	// the interval valid otherwise hangs here instead of failing.
	monitor.Interval = time.Second
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := monitor.Run(cancelled); err == nil || !strings.Contains(err.Error(), "at least 5 seconds") {
		t.Fatalf("short cadence error = %v", err)
	}
}

func TestHealthContextPathRejectsTraversal(t *testing.T) {
	for _, path := range []string{"", "relative", "/../admin", "/health?token=x", `\windows`} {
		t.Run(fmt.Sprintf("%q", path), func(t *testing.T) {
			if _, err := validHealthContextPath(path); err == nil {
				t.Fatalf("unsafe path %q accepted", path)
			}
		})
	}
}

// healthCheckIdentifier and containsHealthState guard what this Agent will
// forward to the Hub as a health check. Both are the last thing between a
// Product's own annotation and the control plane's recorded state.
func TestHealthCheckIdentifierAndStateAcceptOnlyTheDocumentedForms(t *testing.T) {
	t.Parallel()

	t.Run("identifiers are upper snake case", func(t *testing.T) {
		t.Parallel()
		for value, want := range map[string]bool{
			"HEALTHY":              true,
			"HEALTH_CHECK_MAX_LEN": true,
			"A":                    true,
			"A_":                   true,
			// Z is the inclusive top of the range, both as the first character
			// and inside the value. Without a case containing one, > 'Z' and
			// >= 'Z' are indistinguishable.
			"Z":       true,
			"ZULU":    true,
			"HEALTHZ": true,
			// Empty has no first character to test, and indexing it would
			// panic if the emptiness check were dropped.
			"":         false,
			"_LEADING": false,
			"lower":    false,
			"Mixed":    false,
			// The boundaries either side of A-Z: '@' is 'A'-1 and '[' is 'Z'+1.
			"@":              false,
			"[":              false,
			"HAS DIGIT1":     false,
			"HAS-HYPHEN":     false,
			"TRAILING lower": false,
		} {
			if got := healthCheckIdentifier(value); got != want {
				t.Errorf("healthCheckIdentifier(%q) = %v, want %v", value, got, want)
			}
		}
	})

	t.Run("only the four documented states are states", func(t *testing.T) {
		t.Parallel()
		for _, state := range []string{
			controlplane.HealthHealthy, controlplane.HealthRepairing,
			controlplane.HealthWarning, controlplane.HealthError,
		} {
			if !containsHealthState(state) {
				t.Errorf("%q is a documented state but was rejected", state)
			}
		}
		for _, value := range []string{"", "healthy", "HEALTHY_", "UNKNOWN", "OK"} {
			if containsHealthState(value) {
				t.Errorf("%q was accepted as a health state", value)
			}
		}
	})
}

// The discovery path reads an annotation a Product controls, so its bounds are
// the boundary between a Product's configuration and this Agent's behaviour.
// Every one of these is a refusal that must happen at exactly the right point.
func TestKubectlHealthDiscoveryBoundsWhatAnAnnotationCanAskFor(t *testing.T) {
	t.Parallel()

	discover := func(t *testing.T, annotation string) ([]HealthProbeTarget, error) {
		t.Helper()
		pods := map[string]any{"items": []any{map[string]any{
			"metadata": map[string]any{"name": "api-abc", "uid": "pod-uid", "annotations": map[string]string{
				healthProbeAnnotation: annotation,
			}},
			"spec": map[string]any{"containers": []any{map[string]any{"name": "api"}}},
			"status": map[string]any{"podIP": "10.0.0.8", "containerStatuses": []any{map[string]any{
				"name": "api", "containerID": "containerd://abc", "ready": true, "started": true,
				"state": map[string]any{"running": map[string]any{}},
			}}},
		}}}
		encoded, _ := json.Marshal(pods)
		return (KubectlHealthPodDiscoverer{Runner: &healthCommandRunner{output: encoded}}).Discover(
			context.Background(), controlplane.HealthTarget{
				InstallationID: "api", Entity: "api", Product: "app.example:api", Version: "1.2.3", Namespace: "example",
			})
	}

	probeFor := func(port string) string {
		return `{"containerProbes":{"api":{"path":"/telemetry","port":"` + port + `"}}}`
	}

	t.Run("the port must be a usable TCP port", func(t *testing.T) {
		t.Parallel()
		for port, valid := range map[string]bool{
			"1": true, "8446": true, "65535": true,
			// Zero is not a port you can connect to, and 65536 does not exist.
			"0": false, "65536": false, "-1": false,
			"": false, "http": false, "84 46": false,
		} {
			_, err := discover(t, probeFor(port))
			if (err == nil) != valid {
				t.Errorf("port %q accepted=%v, want %v (%v)", port, err == nil, valid, err)
			}
		}
	})

	t.Run("the annotation is bounded", func(t *testing.T) {
		t.Parallel()
		// A Pod annotation can hold far more than this. Reading an unbounded
		// one into a probe specification is work a Product could ask for
		// without limit.
		// Padded with whitespace inside the object: JSON allows it, and
		// TrimSpace only removes it from the ends, so the length the bound
		// sees is the length constructed here.
		padded := func(size int) string {
			body := `{"containerProbes":{"api":{"path":"/telemetry","port":"8446"}}}`
			return `{` + strings.Repeat(" ", size-len(body)) + body[1:]
		}
		if _, err := discover(t, padded(4096)); err != nil {
			t.Errorf("an annotation of exactly 4096 bytes was refused: %v", err)
		}
		_, err := discover(t, padded(4097))
		if err == nil {
			t.Error("an annotation past 4096 bytes was accepted")
		} else if !strings.Contains(err.Error(), "4096") {
			t.Errorf("error %q does not name the bound", err)
		}
	})

	t.Run("a probe naming an absent container is refused", func(t *testing.T) {
		t.Parallel()
		// Otherwise the Agent reports health for a container that is not
		// there, which reads as a healthy Pod with a missing workload.
		_, err := discover(t, `{"containerProbes":{"absent":{"path":"/telemetry","port":"8446"}}}`)
		if err == nil {
			t.Error("a probe for an absent container was accepted")
		}
	})
}

// The five-second floor on the polling cadence is inclusive, and an unset
// interval takes it. Polling faster than that multiplies kubectl and probe
// traffic across every Pod in the Environment, which is why there is a floor
// at all rather than a warning.
func TestHealthMonitorPollingIntervalFloor(t *testing.T) {
	t.Parallel()
	build := func(interval time.Duration) HealthMonitor {
		return HealthMonitor{
			ControlPlane: &healthControl{},
			Discoverer:   healthDiscovery{},
			Prober:       healthProber{},
			Environment:  "dev-use2", AgentID: "agent-dev", Interval: interval,
		}
	}
	for name, test := range map[string]struct {
		interval  time.Duration
		wantError bool
	}{
		// Zero means unset and takes the floor rather than being rejected.
		"unset":             {interval: 0},
		"exactly the floor": {interval: 5 * time.Second},
		"above the floor":   {interval: time.Minute},
		"just below":        {interval: 4999 * time.Millisecond, wantError: true},
		"negative":          {interval: -time.Second, wantError: true},
	} {
		monitor := build(test.interval)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Run returns as soon as it has validated and seen the context end.
		err := monitor.Run(ctx)
		named := err != nil && strings.Contains(err.Error(), "at least 5 seconds")
		if named != test.wantError {
			t.Errorf("%s (%v): rejected=%v, want %v (%v)", name, test.interval, named, test.wantError, err)
		}
	}
}

// The clock is injectable so observation timestamps are deterministic. Without
// the substitution every observation carries the wall clock, and a test cannot
// distinguish a correct timestamp from any other.
func TestHealthMonitorUsesTheInjectedClock(t *testing.T) {
	t.Parallel()
	frozen := time.Unix(1_700_000_000, 0).UTC()
	control := &healthControl{targets: []controlplane.HealthTarget{{
		InstallationID: "api", Entity: "api", Product: "app.example:api", Version: "1.2.3",
		Namespace: "example", Quorum: "ALL",
	}}}
	monitor := HealthMonitor{
		ControlPlane: control,
		Discoverer: healthDiscovery{probes: []HealthProbeTarget{{
			InstallationID: "api", ProductVersion: "1.2.3", NodeID: "pod", ContainerID: "container",
			Liveness: true, Readiness: true,
		}}},
		Prober: healthProber{checks: []controlplane.HealthCheck{{
			ID: "DATABASE", Result: controlplane.HealthHealthy, Params: map[string]any{},
		}}},
		Environment: "dev-use2", AgentID: "agent-dev", Now: func() time.Time { return frozen },
	}
	if err := monitor.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(control.reported) != 1 {
		t.Fatalf("reported = %#v", control.reported)
	}
	if !control.reported[0].ObservedAt.Equal(frozen) {
		t.Errorf("observed at %v, want the injected %v", control.reported[0].ObservedAt, frozen)
	}
}

// The 10,000-byte cap on a health response is inclusive: a document of
// exactly that size is still parsed. Testing only one byte past it cannot
// distinguish > from >=, and under >= a Product whose health document happens
// to land on the limit loses its checks and is reported as a warning it never
// emitted.
func TestHTTPSHealthEndpointProberAcceptsAResponseOfExactlyTheBound(t *testing.T) {
	t.Parallel()
	// Padding inside a message field so the document stays valid JSON at any
	// length. The envelope is measured, then the padding fills the remainder.
	envelope := `{"checks":{"DISK_CAPACITY":{"type":"DISK_CAPACITY","state":"HEALTHY","message":"","params":{}}}}`
	padding := strings.Repeat("y", 10_000-len(envelope))
	document := strings.Replace(envelope, `"message":""`, `"message":"`+padding+`"`, 1)
	if len(document) != 10_000 {
		t.Fatalf("constructed document is %d bytes, want exactly 10000", len(document))
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(document))
	}))
	defer server.Close()
	checks := (HTTPSHealthEndpointProber{Client: server.Client()}).Probe(
		context.Background(), HealthProbeTarget{URL: server.URL})
	if len(checks) != 1 || checks[0].ID != "DISK_CAPACITY" {
		t.Fatalf("a response of exactly the bound was not parsed: %#v", checks)
	}
	if checks[0].Result != controlplane.HealthHealthy {
		t.Errorf("result = %v, want the document's own state", checks[0].Result)
	}
}

// A container's own Started flag wins over inferring readiness from its state.
// Kubernetes sets Started false while a startup probe is still running even
// though the container is Running, so inferring from state alone reports a
// Product as started before it can serve anything.
func TestKubectlHealthDiscoveryTrustsTheStartedFlagOverRunningState(t *testing.T) {
	t.Parallel()
	discover := func(t *testing.T, containerStatus map[string]any) []HealthProbeTarget {
		t.Helper()
		pods := map[string]any{"items": []any{map[string]any{
			"metadata": map[string]any{"name": "api-abc", "uid": "pod-uid", "annotations": map[string]string{
				healthProbeAnnotation: `{"containerProbes":{"api":{"path":"/telemetry","port":"8446"}}}`,
			}},
			"spec":   map[string]any{"containers": []any{map[string]any{"name": "api"}}},
			"status": map[string]any{"podIP": "10.0.0.8", "containerStatuses": []any{containerStatus}},
		}}}
		encoded, _ := json.Marshal(pods)
		probes, err := (KubectlHealthPodDiscoverer{Runner: &healthCommandRunner{output: encoded}}).Discover(
			context.Background(), controlplane.HealthTarget{
				InstallationID: "api", Entity: "api", Product: "app.example:api", Version: "1.2.3", Namespace: "example",
			})
		if err != nil {
			t.Fatal(err)
		}
		return probes
	}

	t.Run("running with started false is not started", func(t *testing.T) {
		t.Parallel()
		probes := discover(t, map[string]any{
			"name": "api", "containerID": "containerd://abc", "ready": false, "started": false,
			"state": map[string]any{"running": map[string]any{}},
		})
		if len(probes) != 1 || probes[0].Liveness {
			t.Errorf("a container still running its startup probe was reported started: %#v", probes)
		}
	})

	t.Run("running with no started flag is started", func(t *testing.T) {
		t.Parallel()
		// Older kubelets omit the field entirely, and then the running state
		// is the only signal there is.
		probes := discover(t, map[string]any{
			"name": "api", "containerID": "containerd://abc", "ready": true,
			"state": map[string]any{"running": map[string]any{}},
		})
		if len(probes) != 1 || !probes[0].Liveness {
			t.Errorf("a running container with no started flag was reported unstarted: %#v", probes)
		}
	})

	t.Run("not running and no flag is not started", func(t *testing.T) {
		t.Parallel()
		probes := discover(t, map[string]any{
			"name": "api", "containerID": "containerd://abc", "ready": false,
			"state": map[string]any{"waiting": map[string]any{}},
		})
		if len(probes) != 1 || probes[0].Liveness {
			t.Errorf("a waiting container was reported started: %#v", probes)
		}
	})
}

// Run polls forever, so a failing round has nowhere to return an error to and
// hands it to OnError instead. Two things about that were unpinned: a
// cancelled context is not an error worth reporting -- it is the caller
// shutting the monitor down, and reporting it makes every clean shutdown look
// like a failure -- and OnError may be unset, in which case a failing round
// must be swallowed rather than panic the Agent.
func TestHealthMonitorReportsRoundFailuresButNotShutdown(t *testing.T) {
	t.Parallel()

	build := func(discoverErr error, onError func(error)) HealthMonitor {
		return HealthMonitor{
			ControlPlane: &healthControl{targets: []controlplane.HealthTarget{{
				InstallationID: "api", Entity: "api", Product: "app.example:api",
				Version: "1.2.3", Namespace: "example", Quorum: "ALL",
			}}},
			Discoverer:  healthDiscovery{err: discoverErr},
			Prober:      healthProber{},
			Environment: "dev-use2", AgentID: "agent-dev",
			Interval: 5 * time.Second, OnError: onError,
		}
	}

	t.Run("a failing round is reported", func(t *testing.T) {
		t.Parallel()
		reported := make(chan error, 4)
		monitor := build(errors.New("kubectl unavailable"), func(err error) { reported <- err })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := monitor.Run(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-reported:
			if !strings.Contains(err.Error(), "kubectl unavailable") {
				t.Errorf("reported %v", err)
			}
		default:
			t.Error("a failing round was not reported to OnError")
		}
	})

	t.Run("a shutdown is not reported as a failure", func(t *testing.T) {
		t.Parallel()
		reported := make(chan error, 4)
		monitor := build(context.Canceled, func(err error) { reported <- err })
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := monitor.Run(ctx); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-reported:
			t.Errorf("a cancellation was reported as a failure: %v", err)
		default:
		}
	})

	t.Run("a failing round with no handler does not panic", func(t *testing.T) {
		t.Parallel()
		monitor := build(errors.New("kubectl unavailable"), nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := monitor.Run(ctx); err != nil {
			t.Fatal(err)
		}
	})
}

// Everything the Agent reports is sorted before it is sent: containers within
// a node, nodes within an Entity, probes within a discovery, and checks within
// a probe. The Hub records these observations verbatim, so unsorted output
// makes every poll differ from the last for reasons that have nothing to do
// with health, and any diff of two observations becomes unreadable.
func TestHealthReportingIsDeterministicallyOrdered(t *testing.T) {
	t.Parallel()

	t.Run("nodes and containers are sorted", func(t *testing.T) {
		t.Parallel()
		control := &healthControl{targets: []controlplane.HealthTarget{{
			InstallationID: "api", Entity: "api", Product: "app.example:api",
			Version: "1.2.3", Namespace: "example", Quorum: "ALL",
		}}}
		// Deliberately reversed on both axes.
		monitor := HealthMonitor{
			ControlPlane: control,
			Discoverer: healthDiscovery{probes: []HealthProbeTarget{
				{InstallationID: "api", ProductVersion: "1.2.3", NodeID: "node-b", ContainerID: "container-z"},
				{InstallationID: "api", ProductVersion: "1.2.3", NodeID: "node-b", ContainerID: "container-a"},
				{InstallationID: "api", ProductVersion: "1.2.3", NodeID: "node-a", ContainerID: "container-m"},
			}},
			Prober:      healthProber{},
			Environment: "dev-use2", AgentID: "agent-dev",
		}
		// Repeated because nodes are collected in a map: without the sort the
		// order is whatever Go's map iteration produces, which is right about
		// half the time with two nodes. A single run cannot tell a sorted
		// result from a lucky one.
		const want = "node-a/container-m,node-b/container-a,node-b/container-z"
		for attempt := range 25 {
			control.reported = nil
			if err := monitor.RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			var order []string
			for _, node := range control.reported[0].Details.Nodes {
				for _, container := range node.Containers {
					order = append(order, node.NodeID+"/"+container.ContainerID)
				}
			}
			if strings.Join(order, ",") != want {
				t.Fatalf("run %d: order = %v, want %v", attempt, order, want)
			}
		}
	})

	t.Run("discovered probes are sorted by node then container", func(t *testing.T) {
		t.Parallel()
		// Two containers on one node is what makes the tie-break observable:
		// without it the node comparison alone would order these correctly.
		pods := map[string]any{"items": []any{
			map[string]any{
				"metadata": map[string]any{"name": "api-b", "uid": "node-b", "annotations": map[string]string{
					healthProbeAnnotation: `{"containerProbes":{"zulu":{"path":"/t","port":"8446"},"alpha":{"path":"/t","port":"8446"}}}`,
				}},
				"spec": map[string]any{"containers": []any{
					map[string]any{"name": "zulu"}, map[string]any{"name": "alpha"},
				}},
				"status": map[string]any{"podIP": "10.0.0.2", "containerStatuses": []any{
					map[string]any{"name": "zulu", "containerID": "c-zulu", "ready": true, "started": true,
						"state": map[string]any{"running": map[string]any{}}},
					map[string]any{"name": "alpha", "containerID": "c-alpha", "ready": true, "started": true,
						"state": map[string]any{"running": map[string]any{}}},
				}},
			},
			map[string]any{
				"metadata": map[string]any{"name": "api-a", "uid": "node-a", "annotations": map[string]string{
					healthProbeAnnotation: `{"containerProbes":{"mike":{"path":"/t","port":"8446"}}}`,
				}},
				"spec": map[string]any{"containers": []any{map[string]any{"name": "mike"}}},
				"status": map[string]any{"podIP": "10.0.0.1", "containerStatuses": []any{
					map[string]any{"name": "mike", "containerID": "c-mike", "ready": true, "started": true,
						"state": map[string]any{"running": map[string]any{}}},
				}},
			},
		}}
		encoded, _ := json.Marshal(pods)
		probes, err := (KubectlHealthPodDiscoverer{Runner: &healthCommandRunner{output: encoded}}).Discover(
			context.Background(), controlplane.HealthTarget{
				InstallationID: "api", Entity: "api", Product: "app.example:api", Version: "1.2.3", Namespace: "example",
			})
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		for _, probe := range probes {
			order = append(order, probe.NodeID+"/"+probe.ContainerID)
		}
		want := "node-a/c-mike,node-b/c-alpha,node-b/c-zulu"
		if strings.Join(order, ",") != want {
			t.Errorf("order = %v, want %v", order, want)
		}
	})

	t.Run("checks within a probe are sorted", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"checks":{
				"ZULU":{"type":"ZULU","state":"HEALTHY","params":{}},
				"ALPHA":{"type":"ALPHA","state":"HEALTHY","params":{}},
				"MIKE":{"type":"MIKE","state":"HEALTHY","params":{}}}}`))
		}))
		defer server.Close()
		checks := (HTTPSHealthEndpointProber{Client: server.Client()}).Probe(
			context.Background(), HealthProbeTarget{URL: server.URL})
		var order []string
		for _, check := range checks {
			order = append(order, check.ID)
		}
		// JSON object order is not preserved by Go's decoder, so without the
		// sort this varies between runs of the same unchanged Product.
		if strings.Join(order, ",") != "ALPHA,MIKE,ZULU" {
			t.Errorf("order = %v, want ALPHA,MIKE,ZULU", order)
		}
	})
}

// A prober with no client of its own still gets one, bounded at four seconds
// like the configured path. Without the substitution the zero-value
// http.Client is used, which has no timeout at all, and one unresponsive Pod
// stalls the entire polling round indefinitely.
func TestHTTPSHealthEndpointProberSuppliesABoundedDefaultClient(t *testing.T) {
	t.Parallel()
	// A listener that accepts and then says nothing. A refused connection
	// would return immediately whether or not there is a timeout, so it cannot
	// distinguish the two; only a peer that hangs can.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			// Held open deliberately; closed when the listener closes.
			defer connection.Close()
		}
	}()

	started := time.Now()
	checks := (HTTPSHealthEndpointProber{}).Probe(
		context.Background(), HealthProbeTarget{URL: "https://" + listener.Addr().String() + "/status/health"})
	elapsed := time.Since(started)

	if len(checks) != 1 || checks[0].Result != controlplane.HealthError {
		t.Fatalf("checks = %#v, want a single error", checks)
	}
	// Four seconds plus slack, and comfortably under the ten-second TLS
	// handshake timeout http.DefaultTransport applies on its own. That
	// distinction is the whole assertion: a client with no Timeout still
	// eventually gives up after ten seconds, so a loose threshold cannot tell
	// the two apart -- and every target in the round waits the difference.
	if elapsed > 8*time.Second {
		t.Errorf("the probe took %v; the default client carries no timeout of its own", elapsed)
	}
}
