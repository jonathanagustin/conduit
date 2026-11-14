package domain

import (
	"bytes"
	"encoding/json"
	"strings"
)

// PodRequest is the least-privilege shape a provisioner may apply.
type PodRequest struct {
	Name        string
	Namespace   string
	Image       string
	RunAsUser   int64
	CPULimit    string
	MemoryLimit string
}

// PodDocuments are the JSON objects a provisioner applies together.
type PodDocuments struct {
	Pod           []byte
	NetworkPolicy []byte
}

// RenderPod builds a non-root pod and a deny-by-default network policy.
// The documents never grant cluster-admin and never mount a service account token.
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
	pod := map[string]any{
		"apiVersion": "v1",
		"kind":       "Pod",
		"metadata": map[string]any{
			"name":      request.Name,
			"namespace": request.Namespace,
			"labels":    map[string]string{"app": "dev-workspace"},
		},
		"spec": map[string]any{
			"automountServiceAccountToken": false,
			"securityContext": map[string]any{
				"runAsNonRoot":   true,
				"runAsUser":      request.RunAsUser,
				"seccompProfile": map[string]string{"type": "RuntimeDefault"},
			},
			"containers": []any{map[string]any{
				"name":  "workspace",
				"image": request.Image,
				"securityContext": map[string]any{
					"runAsNonRoot":             true,
					"runAsUser":                request.RunAsUser,
					"readOnlyRootFilesystem":   true,
					"allowPrivilegeEscalation": false,
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
				"volumeMounts": []any{map[string]any{
					"name":      "work",
					"mountPath": "/work",
				}},
			}},
			"volumes": []any{map[string]any{
				"name":     "work",
				"emptyDir": map[string]any{},
			}},
		},
	}
	policy := map[string]any{
		"apiVersion": "networking.k8s.io/v1",
		"kind":       "NetworkPolicy",
		"metadata": map[string]any{
			"name":      request.Name + "-egress",
			"namespace": request.Namespace,
		},
		"spec": map[string]any{
			"podSelector": map[string]any{
				"matchLabels": map[string]string{"app": "dev-workspace"},
			},
			"policyTypes": []string{"Egress"},
			"egress": []any{map[string]any{
				"ports": []any{map[string]any{"protocol": "TCP", "port": 8081}},
			}},
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
	if bytes.Contains(bytes.ToLower(podBytes), []byte("cluster-admin")) || bytes.Contains(bytes.ToLower(policyBytes), []byte("cluster-admin")) {
		return PodDocuments{}, ErrInvalid
	}
	return PodDocuments{Pod: podBytes, NetworkPolicy: policyBytes}, nil
}
