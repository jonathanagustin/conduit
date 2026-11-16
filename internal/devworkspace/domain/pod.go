package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"strings"
)

// PodRequest is the least-privilege shape a provisioner may apply.
type PodRequest struct {
	Name            string
	Namespace       string
	Image           string
	Owner           string
	Branch          string
	RunAsUser       int64
	CPULimit        string
	MemoryLimit     string
	Egress          *EgressConfig
	ExtraMountPaths []string
	ExtraEnv        map[string]string
}

// PodDocuments are the JSON/YAML objects a provisioner applies together.
type PodDocuments struct {
	Pod           []byte
	NetworkPolicy []byte
	PackageCR     []byte
}

// RenderPod requests non-root container settings and a deny-by-default
// network policy rendered from workspace.egress, and a matching UDS Package CR.
// Rendering alone does not establish a working rootless builder or DNS policy.
// The documents never grant cluster-admin, never mount a host Docker socket,
// and never mount or inject cluster credentials.
func RenderPod(request PodRequest) (PodDocuments, error) {
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.Namespace) == "" {
		return PodDocuments{}, ErrInvalid
	}
	if request.RunAsUser < 1 {
		request.RunAsUser = 65534
	}
	if request.CPULimit == "" {
		request.CPULimit = "200m"
	}
	if request.MemoryLimit == "" {
		request.MemoryLimit = "512Mi"
	}
	if request.Image == "" {
		request.Image = "workspace-editor:local"
	}
	for _, mountPath := range request.ExtraMountPaths {
		if isForbiddenMountPath(mountPath) {
			return PodDocuments{}, fmt.Errorf("%w: forbidden workspace mount %q", ErrInvalid, mountPath)
		}
	}
	for k, v := range request.ExtraEnv {
		if isForbiddenContainerEnv(k, v) {
			return PodDocuments{}, fmt.Errorf("%w: forbidden workspace env %q", ErrInvalid, k)
		}
	}

	if request.Egress == nil {
		return PodDocuments{}, fmt.Errorf("%w: operator workspace.egress is required", ErrInvalidEgress)
	}
	egressCfg := *request.Egress
	validatedEgress, err := NormalizeAndValidateEgress(egressCfg)
	if err != nil {
		return PodDocuments{}, err
	}

	labels := map[string]string{
		"app":                   "dev-workspace",
		"conduit.dev/workspace": request.Name,
	}
	if ownerLabel := sanitizeK8sLabelValue(request.Owner); ownerLabel != "" {
		labels["conduit.dev/owner"] = ownerLabel
	}
	annotations := map[string]string{
		"conduit.dev/isolation": IsolationNotice,
		"conduit.dev/builder":   "rootless",
	}
	if branch := strings.TrimSpace(request.Branch); branch != "" {
		annotations["conduit.dev/branch"] = branch
	}

	envList := []any{
		map[string]string{"name": "BUILDKIT_ROOTLESS", "value": "1"},
		map[string]string{"name": "BUILDAH_ISOLATION", "value": "chroot"},
		map[string]string{"name": "STORAGE_DRIVER", "value": "vfs"},
		map[string]string{"name": "CONDUIT_ISOLATION_MODE", "value": "container"},
		map[string]string{"name": "HOME", "value": "/work"},
	}
	if branch := strings.TrimSpace(request.Branch); branch != "" {
		envList = append(envList, map[string]string{"name": "WORKSPACE_BRANCH", "value": branch})
	}

	pod := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":        request.Name,
			"namespace":   request.Namespace,
			"labels":      labels,
			"annotations": annotations,
		},
		"spec": map[string]any{
			"automountServiceAccountToken": false,
			"enableServiceLinks":           false,
			"securityContext": map[string]any{
				"runAsNonRoot":   true,
				"runAsUser":      request.RunAsUser,
				"runAsGroup":     request.RunAsUser,
				"fsGroup":        request.RunAsUser,
				"seccompProfile": map[string]string{"type": "RuntimeDefault"},
			},
			"containers": []any{map[string]any{
				"name":  "workspace",
				"image": request.Image,
				"env":   envList,
				"securityContext": map[string]any{
					"runAsNonRoot":             true,
					"runAsUser":                request.RunAsUser,
					"runAsGroup":               request.RunAsUser,
					"readOnlyRootFilesystem":   true,
					"allowPrivilegeEscalation": false,
					"procMount":                "Default",
					"capabilities":             map[string]any{"drop": []string{"ALL"}},
				},
				"resources": map[string]any{
					"limits": map[string]string{
						"cpu":    request.CPULimit,
						"memory": request.MemoryLimit,
					},
					"requests": map[string]string{
						"cpu":    "50m",
						"memory": "128Mi",
					},
				},
				"volumeMounts": []any{
					map[string]any{
						"name":      "work",
						"mountPath": "/work",
					},
					map[string]any{
						"name":      "tmp",
						"mountPath": "/tmp",
					},
				},
			}},
			"volumes": []any{
				map[string]any{
					"name":     "work",
					"emptyDir": map[string]any{},
				},
				map[string]any{
					"name":     "tmp",
					"emptyDir": map[string]any{},
				},
			},
		},
	}

	egressRules := buildNetworkPolicyEgressRules(validatedEgress)
	allowSummary := make([]string, 0, len(validatedEgress.Allow))
	for _, entry := range validatedEgress.Allow {
		allowSummary = append(allowSummary, fmt.Sprintf("%s=%s:%d/%s", entry.Name, entry.Host, entry.Port, entry.Protocol))
	}
	policy := map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]any{
			"name":      request.Name + "-egress",
			"namespace": request.Namespace,
			"annotations": map[string]string{
				"conduit.dev/isolation":    IsolationNotice,
				"conduit.dev/egress-allow": strings.Join(allowSummary, ","),
			},
		},
		"spec": map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]string{
					"app":                   "dev-workspace",
					"conduit.dev/workspace": request.Name,
				},
			},
			"policyTypes": []string{"Egress"},
			"egress":      egressRules,
		},
	}
	podBytes, err := json.Marshal(pod)
	if err != nil {
		return PodDocuments{}, err
	}
	policyBytes, err := json.Marshal(policy)
	if err != nil {
		return PodDocuments{}, err
	}
	pkgCR, err := RenderWorkspacePackageCR(request.Namespace, request.Name, validatedEgress)
	if err != nil {
		return PodDocuments{}, err
	}
	if err := ValidateWorkspaceContainerSpec(podBytes, policyBytes); err != nil {
		return PodDocuments{}, err
	}
	return PodDocuments{Pod: podBytes, NetworkPolicy: policyBytes, PackageCR: pkgCR}, nil
}

func buildNetworkPolicyEgressRules(cfg EgressConfig) []any {
	rules := []any{
		map[string]any{
			"to": []any{map[string]any{
				"namespaceSelector": map[string]any{
					"matchLabels": map[string]string{"kubernetes.io/metadata.name": "kube-system"},
				},
				"podSelector": map[string]any{
					"matchLabels": map[string]string{"k8s-app": "kube-dns"},
				},
			}},
			"ports": []any{
				map[string]any{"protocol": "UDP", "port": 53},
				map[string]any{"protocol": "TCP", "port": 53},
			},
		},
	}
	for _, entry := range cfg.Allow {
		rule := map[string]any{
			"ports": []any{
				map[string]any{"protocol": entry.Protocol, "port": entry.Port},
			},
		}
		if strings.Contains(entry.Host, "/") {
			rule["to"] = []any{map[string]any{
				"ipBlock": map[string]any{"cidr": entry.Host},
			}}
		} else if ip := net.ParseIP(entry.Host); ip != nil {
			mask := "/32"
			if ip.To4() == nil {
				mask = "/128"
			}
			rule["to"] = []any{map[string]any{
				"ipBlock": map[string]any{"cidr": entry.Host + mask},
			}}
		} else {
			rule["to"] = []any{map[string]any{
				"podSelector": map[string]any{
					"matchLabels": map[string]string{"conduit.dev/egress-target": sanitizeK8sLabelValue(entry.Name)},
				},
			}}
		}
		rules = append(rules, rule)
	}
	return rules
}

// ValidateWorkspaceContainerSpec checks a rendered Pod and NetworkPolicy JSON
// document pair for rootless builder execution, absence of host Docker sockets,
// absence of cluster credentials, and default-deny egress without wildcards.
func ValidateWorkspaceContainerSpec(podBytes, policyBytes []byte) error {
	combined := bytes.ToLower(append(append([]byte{}, podBytes...), policyBytes...))
	for _, forbidden := range []string{
		"cluster-admin",
		"docker.sock",
		"containerd.sock",
		"hostpath",
		"/var/run/secrets/kubernetes.io/serviceaccount",
		`"automountserviceaccounttoken":true`,
		`"enableservicelinks":true`,
		`"privileged":true`,
		`"hostnetwork":true`,
		`"hostpid":true`,
		`"hostipc":true`,
		"0.0.0.0/0",
		"::/0",
	} {
		if bytes.Contains(combined, []byte(forbidden)) {
			return fmt.Errorf("%w: workspace container spec contains forbidden token %s", ErrInvalid, forbidden)
		}
	}
	lowerPod := bytes.ToLower(podBytes)
	for _, required := range []string{
		`"automountserviceaccounttoken":false`,
		`"enableservicelinks":false`,
		`"runasnonroot":true`,
		`"readonlyrootfilesystem":true`,
		`"allowprivilegeescalation":false`,
		`"buildkit_rootless"`,
	} {
		if !bytes.Contains(lowerPod, []byte(required)) {
			return fmt.Errorf("%w: workspace pod missing required isolation setting %s", ErrInvalid, required)
		}
	}
	return nil
}

func isForbiddenMountPath(mountPath string) bool {
	lower := strings.ToLower(strings.TrimSpace(mountPath))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "docker.sock") ||
		strings.Contains(lower, "containerd.sock") ||
		strings.Contains(lower, "kubernetes.io/serviceaccount") ||
		strings.Contains(lower, ".kube")
}

func isForbiddenContainerEnv(key, value string) bool {
	upperKey := strings.ToUpper(strings.TrimSpace(key))
	lowerVal := strings.ToLower(strings.TrimSpace(value))
	switch upperKey {
	case "KUBECONFIG", "KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_PORT", "KUBERNETES_PORT", "BEARER_TOKEN", "SERVICEACCOUNT_TOKEN":
		return true
	case "DOCKER_HOST":
		return strings.Contains(lowerVal, "docker.sock") || strings.Contains(lowerVal, "unix://") || strings.Contains(lowerVal, "tcp://")
	}
	return strings.Contains(lowerVal, "docker.sock") || strings.Contains(lowerVal, "kubernetes.io/serviceaccount")
}

func sanitizeK8sLabelValue(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if len(out) > 63 {
		out = strings.Trim(out[:63], "-_.")
	}
	return out
}
