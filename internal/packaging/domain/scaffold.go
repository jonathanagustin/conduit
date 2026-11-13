package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
)

const ScaffoldProfile = "internal-stateless/v1"
const ZarfVersion = "0.65.1"
const UDSCommit = "9c808fd0af6bd0dca138da05f4a0d4785f109c32"

type ScaffoldInput struct {
	Profile        string `json:"profile"`
	Name           string `json:"name"`
	Version        string `json:"version"`
	SourceRevision string `json:"sourceRevision"`
	SourceSHA256   string `json:"sourceSHA256"`
	Image          string `json:"image"`
	Port           int    `json:"port"`
	UID            int    `json:"uid"`
	CPUMilli       int    `json:"cpuMilli"`
	MemoryMiB      int    `json:"memoryMiB"`
	ZarfVersion    string `json:"zarfVersion"`
	UDSCommit      string `json:"udsCommit"`
}
type Artifact struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}
type Scaffold struct {
	ValueTier string     `json:"valueTier"`
	Status    string     `json:"status"`
	Digest    string     `json:"sha256"`
	Files     []Artifact `json:"files"`
	Warnings  []string   `json:"warnings"`
}

var appName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,38}[a-z0-9]$`)
var releaseVersion = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)
var digestHex = regexp.MustCompile(`^[0-9a-f]{64}$`)
var pinnedImage = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)+@sha256:[0-9a-f]{64}$`)

// GenerateScaffold emits only a tightly constrained operator-selected profile.
// No inferred value grants deployment authority. A valid schema is not a tested app.
// CONDUIT-INVENTED: packaging.GenerateScaffold
// Displaced-by: a reviewed production packaging profile contract.
func GenerateScaffold(in ScaffoldInput) (Scaffold, error) {
	out := Scaffold{ValueTier: "CONDUIT_NATIVE", Status: "scaffold-needs-validation", Files: []Artifact{}, Warnings: []string{
		"Operator-selected inputs are declarations, not verified runtime requirements.",
		"Internal-only, one stateless replica: no ingress, identity provider, persistent storage, secrets or network egress is generated.",
		"The fixed nonroot security context and resource budget require workload testing. No readiness probe or offline compatibility is asserted. Network is deny-by-default; service reachability is not configured.",
		"Helm rendering, target CRD validation, image provenance, mesh behavior and disconnected user/upgrade journeys must pass before deployment approval.",
	}}
	if in.Profile != ScaffoldProfile || in.ZarfVersion != ZarfVersion || in.UDSCommit != UDSCommit {
		return out, fmt.Errorf("unsupported profile or schema pin")
	}
	if in.Name == "kube-system" || in.Name == "kube-public" || in.Name == "kube-node-lease" || in.Name == "pepr-system" || in.Name == "istio-system" || in.Name == "zarf" || in.Name == "uds-system" {
		return out, fmt.Errorf("reserved namespace")
	}
	if !appName.MatchString(in.Name) || !releaseVersion.MatchString(in.Version) {
		return out, fmt.Errorf("bounded DNS name and release version required")
	}
	if !commitPattern.MatchString(in.SourceRevision) || !digestHex.MatchString(in.SourceSHA256) {
		return out, fmt.Errorf("source commit and SHA-256 required")
	}
	if !pinnedImage.MatchString(in.Image) || len(in.Image) > 256 {
		return out, fmt.Errorf("literal digest-pinned image required")
	}
	if in.Port < 1024 || in.Port > 65535 || in.UID < 1 || in.UID > 65535 || in.CPUMilli < 1 || in.CPUMilli > 64000 || in.MemoryMiB < 16 || in.MemoryMiB > 65536 {
		return out, fmt.Errorf("explicit bounded unprivileged port, UID and resources required")
	}
	type M = map[string]any
	add := func(path string, value any) error {
		b, e := json.MarshalIndent(value, "", "  ")
		if e != nil {
			return e
		}
		out.Files = append(out.Files, Artifact{path, string(b) + "\n"})
		return nil
	}
	labels := M{"app.kubernetes.io/name": in.Name}
	metadata := M{"name": in.Name, "namespace": in.Name, "labels": labels}
	budget := M{"cpu": strconv.Itoa(in.CPUMilli) + "m", "memory": strconv.Itoa(in.MemoryMiB) + "Mi"}
	container := M{"name": in.Name, "image": in.Image, "ports": []any{M{"name": "http", "containerPort": in.Port}}, "securityContext": M{"allowPrivilegeEscalation": false, "capabilities": M{"drop": []string{"ALL"}}}, "resources": M{"requests": budget, "limits": budget}}
	deployment := M{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": metadata, "spec": M{"replicas": 1, "selector": M{"matchLabels": labels}, "template": M{"metadata": M{"labels": labels}, "spec": M{"automountServiceAccountToken": false, "securityContext": M{"runAsNonRoot": true, "runAsUser": in.UID, "seccompProfile": M{"type": "RuntimeDefault"}}, "containers": []any{container}}}}}
	service := M{"apiVersion": "v1", "kind": "Service", "metadata": metadata, "spec": M{"type": "ClusterIP", "selector": labels, "ports": []any{M{"name": "http", "port": in.Port, "targetPort": "http", "protocol": "TCP"}}}}
	pkg := M{"apiVersion": "uds.dev/v1alpha1", "kind": "Package", "metadata": M{"name": in.Name, "namespace": in.Name}, "spec": M{"network": M{"serviceMesh": M{"mode": "ambient"}, "allow": []any{}}}}
	chart := func(name string) M {
		return M{"apiVersion": "v2", "name": name, "version": in.Version, "type": "application"}
	}
	artifacts := map[string]any{
		"charts/application/Chart.yaml":                chart(in.Name),
		"charts/application/values.yaml":               M{},
		"charts/application/templates/deployment.yaml": deployment,
		"charts/application/templates/service.yaml":    service,
		"charts/uds-config/Chart.yaml":                 chart("uds-" + in.Name + "-config"),
		"charts/uds-config/values.yaml":                M{},
		"charts/uds-config/templates/package.yaml":     pkg,
		"scaffold-input.json":                          in,
		"zarf.yaml":                                    M{"kind": "ZarfPackageConfig", "metadata": M{"name": in.Name, "version": in.Version}, "components": []any{M{"name": in.Name, "required": true, "images": []string{in.Image}, "charts": []any{M{"name": "uds-" + in.Name + "-config", "namespace": in.Name, "version": in.Version, "localPath": "charts/uds-config"}, M{"name": in.Name, "namespace": in.Name, "version": in.Version, "localPath": "charts/application"}}}}},
	}
	names := make([]string, 0, len(artifacts))
	for name := range artifacts {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := add(name, artifacts[name]); err != nil {
			return Scaffold{}, err
		}
	}
	// Canonical JSON array provides unambiguous framing for path/content digests.
	b, err := json.Marshal(out.Files)
	if err != nil {
		return Scaffold{}, err
	}
	sum := sha256.Sum256(b)
	out.Digest = hex.EncodeToString(sum[:])
	return out, nil
}
