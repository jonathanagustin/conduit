package packagingtools

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
)

var digestImage = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)

const (
	sevError   = "error"
	sevWarning = "warning"
)

func (service *Service) ValidateManifests(_ context.Context, manifests string) (domain.ManifestReport, error) {
	if strings.TrimSpace(manifests) == "" {
		return report(domain.ManifestFinding{Path: "/", Rule: "schema", Severity: sevError, Message: "manifests are empty"}), nil
	}
	decoder := yaml.NewDecoder(strings.NewReader(manifests))
	var findings []domain.ManifestFinding
	docs := 0
	for {
		var raw any
		err := decoder.Decode(&raw)
		if err == io.EOF {
			break
		}
		if err != nil {
			return report(domain.ManifestFinding{Path: "/", Rule: "schema", Severity: sevError, Message: "manifests are not valid YAML"}), nil
		}
		if raw == nil {
			continue
		}
		docs++
		doc, ok := asMap(raw)
		if !ok {
			findings = append(findings, domain.ManifestFinding{Path: fmt.Sprintf("document[%d]", docs), Rule: "schema", Severity: sevError, Message: "document is not an object"})
			continue
		}
		findings = append(findings, checkDocument(fmt.Sprintf("document[%d]", docs), doc)...)
	}
	if docs == 0 {
		return report(domain.ManifestFinding{Path: "/", Rule: "schema", Severity: sevError, Message: "manifests are empty"}), nil
	}
	return report(findings...), nil
}

func report(findings ...domain.ManifestFinding) domain.ManifestReport {
	ok := true
	for _, finding := range findings {
		if finding.Severity == sevError {
			ok = false
		}
	}
	return domain.ManifestReport{OK: ok, Findings: findings}
}

func checkDocument(path string, doc map[string]any) []domain.ManifestFinding {
	kind := stringValue(doc["kind"])
	pod, podPath, ok := podSpec(doc, path, kind)
	if !ok {
		return nil
	}
	var findings []domain.ManifestFinding
	podSC, _ := asMap(pod["securityContext"])
	containers, _ := asSlice(pod["containers"])
	if len(containers) == 0 {
		return []domain.ManifestFinding{{Path: podPath + ".containers", Rule: "schema", Severity: sevError, Message: "workload has no containers"}}
	}
	for i, item := range containers {
		container, ok := asMap(item)
		if !ok {
			findings = append(findings, domain.ManifestFinding{Path: fmt.Sprintf("%s.containers[%d]", podPath, i), Rule: "schema", Severity: sevError, Message: "container is not an object"})
			continue
		}
		findings = append(findings, checkContainer(fmt.Sprintf("%s.containers[%d]", podPath, i), container, podSC)...)
	}
	return findings
}

func podSpec(doc map[string]any, path, kind string) (map[string]any, string, bool) {
	spec, ok := asMap(doc["spec"])
	if !ok {
		return nil, "", false
	}
	switch kind {
	case "Pod":
		return spec, path + ".spec", true
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet", "Job":
		template, _ := asMap(spec["template"])
		pod, ok := asMap(template["spec"])
		return pod, path + ".spec.template.spec", ok
	case "CronJob":
		job, _ := asMap(spec["jobTemplate"])
		jobSpec, _ := asMap(job["spec"])
		template, _ := asMap(jobSpec["template"])
		pod, ok := asMap(template["spec"])
		return pod, path + ".spec.jobTemplate.spec.template.spec", ok
	default:
		return nil, "", false
	}
}

func checkContainer(path string, container, podSC map[string]any) []domain.ManifestFinding {
	var findings []domain.ManifestFinding
	image := stringValue(container["image"])
	if image == "" || !digestImage.MatchString(image) {
		findings = append(findings, domain.ManifestFinding{Path: path + ".image", Rule: "digest-pin", Severity: sevError, Message: "image must be pinned by a sha256 digest"})
	}
	if _, ok := asMap(container["readinessProbe"]); !ok {
		findings = append(findings, domain.ManifestFinding{Path: path + ".readinessProbe", Rule: "probes", Severity: sevError, Message: "readiness probe is required"})
	}
	if _, ok := asMap(container["livenessProbe"]); !ok {
		findings = append(findings, domain.ManifestFinding{Path: path + ".livenessProbe", Rule: "probes", Severity: sevError, Message: "liveness probe is required"})
	}
	resources, _ := asMap(container["resources"])
	requests, _ := asMap(resources["requests"])
	limits, _ := asMap(resources["limits"])
	if stringValue(requests["memory"]) == "" || stringValue(requests["cpu"]) == "" {
		findings = append(findings, domain.ManifestFinding{Path: path + ".resources.requests", Rule: "resources", Severity: sevError, Message: "cpu and memory requests are required"})
	}
	if stringValue(limits["memory"]) == "" || stringValue(limits["cpu"]) == "" {
		findings = append(findings, domain.ManifestFinding{Path: path + ".resources.limits", Rule: "resources", Severity: sevError, Message: "cpu and memory limits are required"})
	} else if mem := stringValue(limits["memory"]); mem != "" {
		if _, reason, blocked := domain.AssessMemory(mem); blocked {
			findings = append(findings, domain.ManifestFinding{Path: path + ".resources.limits.memory", Rule: "resources", Severity: sevError, Message: reason})
		}
	}
	sc, _ := asMap(container["securityContext"])
	if !boolOr(sc, "runAsNonRoot", boolOr(podSC, "runAsNonRoot", false)) {
		findings = append(findings, domain.ManifestFinding{Path: path + ".securityContext.runAsNonRoot", Rule: "non-root", Severity: sevError, Message: "runAsNonRoot must be true"})
	}
	if sc == nil || !hasFalse(sc, "allowPrivilegeEscalation") {
		findings = append(findings, domain.ManifestFinding{Path: path + ".securityContext.allowPrivilegeEscalation", Rule: "security-context", Severity: sevError, Message: "allowPrivilegeEscalation must be false"})
	}
	if !dropsAll(sc) {
		findings = append(findings, domain.ManifestFinding{Path: path + ".securityContext.capabilities.drop", Rule: "security-context", Severity: sevError, Message: "capabilities must drop ALL"})
	}
	if sc == nil || !hasTrue(sc, "readOnlyRootFilesystem") {
		findings = append(findings, domain.ManifestFinding{Path: path + ".securityContext.readOnlyRootFilesystem", Rule: "security-context", Severity: sevWarning, Message: "readOnlyRootFilesystem should be true where the application can run that way"})
	}
	return findings
}

func dropsAll(sc map[string]any) bool {
	caps, _ := asMap(sc["capabilities"])
	drop, _ := asSlice(caps["drop"])
	for _, item := range drop {
		if strings.EqualFold(stringValue(item), "ALL") {
			return true
		}
	}
	return false
}

func asMap(value any) (map[string]any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		return typed, true
	case map[any]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[fmt.Sprint(key)] = item
		}
		return out, true
	default:
		return nil, false
	}
}

func asSlice(value any) ([]any, bool) {
	switch typed := value.(type) {
	case []any:
		return typed, true
	default:
		return nil, false
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		if value == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func boolOr(values map[string]any, key string, fallback bool) bool {
	if values == nil {
		return fallback
	}
	switch typed := values[key].(type) {
	case bool:
		return typed
	default:
		return fallback
	}
}

func hasTrue(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && value
}

func hasFalse(values map[string]any, key string) bool {
	value, ok := values[key].(bool)
	return ok && !value
}
