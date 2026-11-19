package productconfiguration

import (
	"fmt"
	"strings"
)

// Existing status resource paths are shared by the rendering helpers.
const (
	LivenessSubpath  = "/status/liveness"
	ReadinessSubpath = "/status/readiness"
	HealthSubpath    = "/status/health"
)

// ManagedFileMountPath retains the existing compatibility rendering behavior.
func ManagedFileMountPath(productName string) string {
	return "/opt/palantir/services/" + productName + "/var/conf"
}

// StatusProbePaths retains the existing compatibility rendering behavior.
func StatusProbePaths(endpoint EndpointDefinition) (liveness, readiness string) {
	base := strings.TrimSuffix(endpoint.EffectivePath(), "/")
	return base + LivenessSubpath, base + ReadinessSubpath
}

// HealthProbePath retains the existing compatibility rendering behavior.
func HealthProbePath(endpoint EndpointDefinition) string {
	return strings.TrimSuffix(endpoint.EffectivePath(), "/") + HealthSubpath
}

// AugmentProducedDiscovery retains the existing compatibility rendering behavior.
func AugmentProducedDiscovery(entry DiscoveryProduces, host string) (map[string]any, bool) {
	payload := map[string]any{}
	for key, node := range entry.Payload {
		var value any
		if err := node.Decode(&value); err == nil {
			payload[key] = value
		}
	}
	if entry.Role != "" {
		payload["role"] = entry.Role
	}
	if entry.Port < 1 || entry.Port > 65535 || strings.TrimSpace(host) == "" {
		return payload, false
	}
	scheme := entry.Scheme
	if scheme == "" {
		scheme = "https"
	}
	requestPath := entry.Path
	if requestPath == "" {
		requestPath = "/"
	}
	uri := fmt.Sprintf("%s://%s:%d%s", scheme, host, entry.Port, requestPath)
	payload["port"] = entry.Port
	payload["scheme"] = scheme
	payload["path"] = requestPath
	payload["uri"] = uri
	payload["uris"] = []any{uri}
	return payload, true
}
