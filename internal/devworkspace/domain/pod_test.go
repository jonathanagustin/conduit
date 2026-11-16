package domain

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRenderPodIsLeastPrivilege(t *testing.T) {
	documents, err := RenderPod(PodRequest{
		Name:      "ws-1",
		Namespace: "workspaces",
		Owner:     "ada",
		Branch:    "workspace/ada/ws-1",
		Egress: &EgressConfig{
			GitHost: "git.example.internal",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ToLower(string(documents.Pod) + string(documents.NetworkPolicy) + string(documents.PackageCR))
	for _, want := range []string{
		`"runasnonroot":true`,
		`"readonlyrootfilesystem":true`,
		`"automountserviceaccounttoken":false`,
		`"enableservicelinks":false`,
		`"cpu":"200m"`,
		`"memory":"512mi"`,
		`"drop":["all"]`,
		`"buildkit_rootless"`,
		`"buildah_isolation"`,
		`"chroot"`,
		`"storage_driver"`,
		`"vfs"`,
		strings.ToLower(IsolationNotice),
		`"git.example.internal"`,
		`"registry.uds.svc.cluster.local"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("pod documents missing %s\n%s", want, text)
		}
	}
	for _, forbidden := range []string{
		"cluster-admin",
		"docker.sock",
		"hostpath",
		"kubeconfig",
		"kubernetes_service_host",
		"/var/run/secrets/kubernetes.io/serviceaccount",
	} {
		if bytes.Contains([]byte(text), []byte(forbidden)) {
			t.Fatalf("pod documents contain forbidden %s\n%s", forbidden, text)
		}
	}
}

func TestValidateWorkspaceContainerSpecRejectsDockerSocketAndClusterCredentials(t *testing.T) {
	base := PodRequest{
		Name:      "ws-1",
		Namespace: "workspaces",
		Egress:    &EgressConfig{GitHost: "git.example.internal"},
	}
	cases := []struct {
		name string
		req  PodRequest
	}{
		{
			name: "docker socket mount",
			req: PodRequest{
				Name:             base.Name,
				Namespace:        base.Namespace,
				Egress:           base.Egress,
				ExtraMountPaths:  []string{"/var/run/docker.sock"},
			},
		},
		{
			name: "serviceaccount token mount",
			req: PodRequest{
				Name:             base.Name,
				Namespace:        base.Namespace,
				Egress:           base.Egress,
				ExtraMountPaths:  []string{"/var/run/secrets/kubernetes.io/serviceaccount"},
			},
		},
		{
			name: "DOCKER_HOST env",
			req: PodRequest{
				Name:      base.Name,
				Namespace: base.Namespace,
				Egress:    base.Egress,
				ExtraEnv:  map[string]string{"DOCKER_HOST": "unix:///var/run/docker.sock"},
			},
		},
		{
			name: "KUBECONFIG env",
			req: PodRequest{
				Name:      base.Name,
				Namespace: base.Namespace,
				Egress:    base.Egress,
				ExtraEnv:  map[string]string{"KUBECONFIG": "/etc/kubernetes/admin.conf"},
			},
		},
		{
			name: "KUBERNETES_SERVICE_HOST env",
			req: PodRequest{
				Name:      base.Name,
				Namespace: base.Namespace,
				Egress:    base.Egress,
				ExtraEnv:  map[string]string{"KUBERNETES_SERVICE_HOST": "10.96.0.1"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := RenderPod(tc.req); err == nil {
				t.Fatalf("expected RenderPod to reject %s", tc.name)
			}
		})
	}
}

func TestEgressConfigDefaultTwoEntriesAndValidation(t *testing.T) {
	cfg, err := ParseEgressValuesYAML([]byte(`
workspace:
  egress:
    gitHost: "git.example.internal"
`))
	if err != nil {
		t.Fatalf("ParseEgressValuesYAML failed: %v", err)
	}
	if cfg.Registry != DefaultWorkspaceRegistryHost || cfg.RegistryPort != DefaultWorkspaceRegistryPort {
		t.Fatalf("registry default = %q:%d", cfg.Registry, cfg.RegistryPort)
	}
	if len(cfg.Allow) != 2 {
		t.Fatalf("expected 2 default allow entries, got %d: %#v", len(cfg.Allow), cfg.Allow)
	}
	if cfg.Allow[0].Name != "git-host" || cfg.Allow[0].Host != "git.example.internal" || cfg.Allow[0].Port != 443 || cfg.Allow[0].Protocol != "TCP" {
		t.Fatalf("unexpected git-host entry: %#v", cfg.Allow[0])
	}
	if cfg.Allow[1].Name != "local-registry" || cfg.Allow[1].Host != DefaultWorkspaceRegistryHost || cfg.Allow[1].Port != 5000 || cfg.Allow[1].Protocol != "TCP" {
		t.Fatalf("unexpected local-registry entry: %#v", cfg.Allow[1])
	}
	if err := AllowsEndpoint(cfg, "git.example.internal", 443, "TCP"); err != nil {
		t.Fatalf("expected git-host:443 TCP to be allowed: %v", err)
	}
	if err := AllowsEndpoint(cfg, DefaultWorkspaceRegistryHost, 5000, "TCP"); err != nil {
		t.Fatalf("expected local-registry:5000 TCP to be allowed: %v", err)
	}
	if err := AllowsEndpoint(cfg, "example.com", 443, "TCP"); !errors.Is(err, ErrEgressDenied) {
		t.Fatalf("expected arbitrary host to be denied with ErrEgressDenied, got %v", err)
	}

	invalidYAML := []struct {
		name string
		raw  string
	}{
		{
			name: "missing gitHost",
			raw: `
workspace:
  egress:
    registry: "registry.uds.svc.cluster.local:5000"
`,
		},
		{
			name: "empty gitHost",
			raw: `
workspace:
  egress:
    gitHost: "   "
`,
		},
		{
			name: "wildcard gitHost",
			raw: `
workspace:
  egress:
    gitHost: "*.example.internal"
`,
		},
		{
			name: "wildcard allow entry",
			raw: `
workspace:
  egress:
    gitHost: "git.example.internal"
    allow:
      - name: "any"
        host: "*"
        port: 443
        protocol: "TCP"
`,
		},
		{
			name: "subdomain wildcard allow entry",
			raw: `
workspace:
  egress:
    gitHost: "git.example.internal"
    allow:
      - name: "sub-wildcard"
        host: "*.npmjs.org"
        port: 443
        protocol: "TCP"
`,
		},
		{
			name: "0.0.0.0/0 allow entry",
			raw: `
workspace:
  egress:
    gitHost: "git.example.internal"
    allow:
      - name: "all-ipv4"
        host: "0.0.0.0/0"
        port: 443
        protocol: "TCP"
`,
		},
		{
			name: "::/0 allow entry",
			raw: `
workspace:
  egress:
    gitHost: "git.example.internal"
    allow:
      - name: "all-ipv6"
        host: "::/0"
        port: 443
        protocol: "TCP"
`,
		},
		{
			name: "empty allow entry",
			raw: `
workspace:
  egress:
    gitHost: "git.example.internal"
    allow:
      - name: ""
        host: ""
        port: 0
        protocol: ""
`,
		},
	}
	for _, tc := range invalidYAML {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseEgressValuesYAML([]byte(tc.raw)); !errors.Is(err, ErrInvalidEgress) {
				t.Fatalf("expected ErrInvalidEgress for %s, got %v", tc.name, err)
			}
		})
	}
}

func TestProtectedEgressPathsAndCommands(t *testing.T) {
	for _, p := range []string{
		"workspace-networkpolicy.yaml",
		"deploy/networkpolicy.yaml",
		"config/workspace-egress.yaml",
	} {
		if !IsProtectedEgressPath(p) {
			t.Fatalf("expected %s to be protected", p)
		}
	}
	if IsProtectedEgressPath("src/main.go") {
		t.Fatal("src/main.go should not be a protected egress path")
	}
	if !IsNetworkPolicyEditCommand([]string{"kubectl", "apply", "-f", "networkpolicy.yaml"}) {
		t.Fatal("expected kubectl apply networkpolicy to be detected")
	}
	if IsNetworkPolicyEditCommand([]string{"go", "test", "./..."}) {
		t.Fatal("go test should not be flagged as a network policy edit")
	}
}

func TestConfineRejectsEscape(t *testing.T) {
	for _, path := range []string{"../etc/passwd", "/etc/passwd", `..\secret`, "foo/../../etc/passwd"} {
		if _, err := Confine(path); err == nil {
			t.Fatalf("accepted %s", path)
		}
	}
	got, err := Confine("src/main.txt")
	if err != nil || got != "src/main.txt" {
		t.Fatalf("confine = %s %v", got, err)
	}
}

