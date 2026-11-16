package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jonathanagustin/conduit/internal/apollo/workloadmetadata"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// healthProbeAnnotation is defined once in the compatibility leaf alongside the rest
// of the documented Apollo workload metadata, so the key this reads and the keys
// the platform emits cannot drift apart.
const healthProbeAnnotation = workloadmetadata.ApolloHealthProbeAnnotation

type HealthControlPlane interface {
	HealthTargets(context.Context, string, string) ([]controlplane.HealthTarget, error)
	ReportHealth(context.Context, string, string, []controlplane.HealthObservation) error
}

type HealthPodDiscoverer interface {
	Discover(context.Context, controlplane.HealthTarget) ([]HealthProbeTarget, error)
}

type HealthEndpointProber interface {
	Probe(context.Context, HealthProbeTarget) []controlplane.HealthCheck
}

type HealthProbeTarget struct {
	InstallationID string
	ProductVersion string
	NodeID         string
	ContainerID    string
	URL            string
	Liveness       bool
	Readiness      bool
}

type HealthMonitor struct {
	ControlPlane HealthControlPlane
	Discoverer   HealthPodDiscoverer
	Prober       HealthEndpointProber
	Environment  string
	AgentID      string
	Interval     time.Duration
	Now          func() time.Time
	OnError      func(error)
}

func (monitor HealthMonitor) Run(ctx context.Context) error {
	if monitor.ControlPlane == nil || monitor.Discoverer == nil || monitor.Prober == nil ||
		strings.TrimSpace(monitor.Environment) == "" || strings.TrimSpace(monitor.AgentID) == "" {
		return errors.New("health monitor control plane, discovery, prober, Environment, and Agent ID are required")
	}
	interval := monitor.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	if interval < 5*time.Second {
		return errors.New("health polling interval must be at least 5 seconds")
	}
	run := func() {
		if err := monitor.RunOnce(ctx); err != nil && !errors.Is(err, context.Canceled) && monitor.OnError != nil {
			monitor.OnError(err)
		}
	}
	run()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			run()
		}
	}
}

func (monitor HealthMonitor) RunOnce(ctx context.Context) error {
	targets, err := monitor.ControlPlane.HealthTargets(ctx, monitor.Environment, monitor.AgentID)
	if err != nil {
		return fmt.Errorf("list health targets: %w", err)
	}
	if len(targets) == 0 {
		return nil
	}
	now := time.Now().UTC()
	if monitor.Now != nil {
		now = monitor.Now().UTC()
	}
	observations := make([]controlplane.HealthObservation, 0, len(targets))
	for _, target := range targets {
		probes, err := monitor.Discoverer.Discover(ctx, target)
		if err != nil {
			return fmt.Errorf("discover health probes for %s: %w", target.InstallationID, err)
		}
		nodes := map[string]*controlplane.EntityNodeHealth{}
		for _, probe := range probes {
			node := nodes[probe.NodeID]
			if node == nil {
				node = &controlplane.EntityNodeHealth{NodeID: probe.NodeID, ProductVersion: probe.ProductVersion}
				nodes[probe.NodeID] = node
			}
			node.Containers = append(node.Containers, controlplane.EntityContainerHealth{
				ContainerID: probe.ContainerID, Liveness: probe.Liveness, Readiness: probe.Readiness,
				Checks: monitor.Prober.Probe(ctx, probe),
			})
		}
		details := controlplane.EntityHealthDetails{Quorum: target.Quorum, Nodes: make([]controlplane.EntityNodeHealth, 0, len(nodes))}
		for _, node := range nodes {
			sort.Slice(node.Containers, func(i, j int) bool { return node.Containers[i].ContainerID < node.Containers[j].ContainerID })
			details.Nodes = append(details.Nodes, *node)
		}
		sort.Slice(details.Nodes, func(i, j int) bool { return details.Nodes[i].NodeID < details.Nodes[j].NodeID })
		observations = append(observations, controlplane.HealthObservation{
			InstallationID: target.InstallationID, ObservedAt: now, Details: details,
		})
	}
	if err := monitor.ControlPlane.ReportHealth(ctx, monitor.Environment, monitor.AgentID, observations); err != nil {
		return fmt.Errorf("report health: %w", err)
	}
	return nil
}

type KubectlHealthPodDiscoverer struct {
	Runner CommandRunner
}

func (discoverer KubectlHealthPodDiscoverer) Discover(
	ctx context.Context, target controlplane.HealthTarget,
) ([]HealthProbeTarget, error) {
	runner := discoverer.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	output, err := runner.Run(ctx, "kubectl", "get", "pods", "--namespace", target.Namespace,
		"--selector", "app.kubernetes.io/instance="+target.Entity, "--output", "json")
	if err != nil {
		return nil, err
	}
	var list struct {
		Items []struct {
			Metadata struct {
				Name        string            `json:"name"`
				UID         string            `json:"uid"`
				Annotations map[string]string `json:"annotations"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Name string `json:"name"`
				} `json:"containers"`
			} `json:"spec"`
			Status struct {
				PodIP             string `json:"podIP"`
				ContainerStatuses []struct {
					Name        string `json:"name"`
					ContainerID string `json:"containerID"`
					Ready       bool   `json:"ready"`
					Started     *bool  `json:"started"`
					State       struct {
						Running *struct{} `json:"running,omitempty"`
					} `json:"state"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(output, &list); err != nil {
		return nil, fmt.Errorf("decode Pod discovery: %w", err)
	}
	probes := []HealthProbeTarget{}
	for _, pod := range list.Items {
		annotation := strings.TrimSpace(pod.Metadata.Annotations[healthProbeAnnotation])
		if annotation == "" {
			continue
		}
		if len(annotation) > 4096 {
			return nil, fmt.Errorf("Pod %s health annotation exceeds 4096 bytes", pod.Metadata.Name)
		}
		var specification struct {
			ContainerProbes map[string]struct {
				Path string `json:"path"`
				Port string `json:"port"`
			} `json:"containerProbes"`
		}
		decoder := json.NewDecoder(strings.NewReader(annotation))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&specification); err != nil || len(specification.ContainerProbes) == 0 {
			return nil, fmt.Errorf("Pod %s has an invalid %s annotation", pod.Metadata.Name, healthProbeAnnotation)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("Pod %s has trailing data in %s", pod.Metadata.Name, healthProbeAnnotation)
		}
		if net.ParseIP(pod.Status.PodIP) == nil {
			return nil, fmt.Errorf("Pod %s health annotation is active but podIP is unavailable", pod.Metadata.Name)
		}
		statuses := map[string]struct {
			id, name       string
			ready, started bool
		}{}
		for _, status := range pod.Status.ContainerStatuses {
			started := status.State.Running != nil
			if status.Started != nil {
				started = *status.Started
			}
			id := status.ContainerID
			if id == "" {
				id = status.Name
			}
			statuses[status.Name] = struct {
				id, name       string
				ready, started bool
			}{id: id, name: status.Name, ready: status.Ready, started: started}
		}
		nodeID := pod.Metadata.UID
		if nodeID == "" {
			nodeID = pod.Metadata.Name
		}
		for container, probe := range specification.ContainerProbes {
			status, exists := statuses[container]
			if !exists {
				return nil, fmt.Errorf("Pod %s health annotation references absent container %s", pod.Metadata.Name, container)
			}
			port, err := strconv.Atoi(probe.Port)
			if err != nil || port < 1 || port > 65535 {
				return nil, fmt.Errorf("Pod %s container %s health port is invalid", pod.Metadata.Name, container)
			}
			contextPath, err := validHealthContextPath(probe.Path)
			if err != nil {
				return nil, fmt.Errorf("Pod %s container %s: %w", pod.Metadata.Name, container, err)
			}
			probes = append(probes, HealthProbeTarget{
				InstallationID: target.InstallationID, ProductVersion: target.Version, NodeID: nodeID,
				ContainerID: status.id, URL: "https://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(port)) + contextPath,
				Liveness: status.started, Readiness: status.ready,
			})
		}
	}
	sort.Slice(probes, func(i, j int) bool {
		if probes[i].NodeID == probes[j].NodeID {
			return probes[i].ContainerID < probes[j].ContainerID
		}
		return probes[i].NodeID < probes[j].NodeID
	})
	return probes, nil
}

func validHealthContextPath(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, "/") || strings.ContainsAny(value, "?#\\") {
		return "", errors.New("health context path must be an absolute URL path without query or fragment")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.EscapedPath() != value || strings.Contains(value, "//") || strings.Contains(value, "/../") || strings.HasSuffix(value, "/..") {
		return "", errors.New("health context path is not canonical")
	}
	if value == "/" {
		return "/status/health", nil
	}
	return strings.TrimSuffix(value, "/") + "/status/health", nil
}

type HTTPSHealthEndpointProber struct {
	Client *http.Client
}

func (prober HTTPSHealthEndpointProber) Probe(ctx context.Context, target HealthProbeTarget) []controlplane.HealthCheck {
	client := prober.Client
	if client == nil {
		client = &http.Client{Timeout: 4 * time.Second}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	if err != nil {
		return probeFailure("HEALTH_PROBE_CONFIGURATION", "Health endpoint configuration is invalid")
	}
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return probeFailure("HEALTH_PROBE_ERROR", "Health endpoint could not be queried")
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(io.LimitReader(response.Body, 10_001))
	if err != nil {
		return probeFailure("HEALTH_PROBE_ERROR", "Health endpoint response could not be read")
	}
	if len(contents) > 10_000 {
		return []controlplane.HealthCheck{{ID: "HEALTH_CHECK_MAX_LEN", Result: controlplane.HealthWarning,
			Message: "Health response exceeded 10,000 bytes and details were removed", Params: map[string]any{}}}
	}
	if response.StatusCode != http.StatusOK {
		return probeFailure("HEALTH_PROBE_HTTP_ERROR", "Health endpoint did not return HTTP 200")
	}
	var payload struct {
		Checks map[string]struct {
			Type    string         `json:"type"`
			State   string         `json:"state"`
			Message string         `json:"message,omitempty"`
			Params  map[string]any `json:"params,omitempty"`
		} `json:"checks"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || payload.Checks == nil {
		return probeFailure("HEALTH_PROBE_INVALID_RESPONSE", "Health endpoint returned an invalid response")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return probeFailure("HEALTH_PROBE_INVALID_RESPONSE", "Health endpoint returned an invalid response")
	}
	checks := make([]controlplane.HealthCheck, 0, len(payload.Checks))
	for id, check := range payload.Checks {
		if id != check.Type || !healthCheckIdentifier(id) ||
			!containsHealthState(check.State) {
			return probeFailure("HEALTH_PROBE_INVALID_RESPONSE", "Health endpoint returned an invalid response")
		}
		if check.Params == nil {
			check.Params = map[string]any{}
		}
		checks = append(checks, controlplane.HealthCheck{ID: id, Message: check.Message, Params: check.Params, Result: check.State})
	}
	sort.Slice(checks, func(i, j int) bool { return checks[i].ID < checks[j].ID })
	return checks
}

func probeFailure(id, message string) []controlplane.HealthCheck {
	return []controlplane.HealthCheck{{ID: id, Result: controlplane.HealthError, Message: message, Params: map[string]any{}}}
}

func healthCheckIdentifier(value string) bool {
	if value == "" || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for _, character := range value {
		if (character < 'A' || character > 'Z') && character != '_' {
			return false
		}
	}
	return true
}

func containsHealthState(value string) bool {
	for _, state := range []string{controlplane.HealthHealthy, controlplane.HealthRepairing, controlplane.HealthWarning, controlplane.HealthError} {
		if value == state {
			return true
		}
	}
	return false
}
