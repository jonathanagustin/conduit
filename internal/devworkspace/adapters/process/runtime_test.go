package process

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func TestFailedStartDoubleStartAndStopAfterCrash(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "starts")
	runtime := New([]string{"sh", "-c", "echo $$ >> " + marker + "; sleep 30"}, nil)
	workspace := domain.Workspace{ID: "wsp_one", Owner: "ada"}
	failing := New([]string{"false"}, nil)
	if _, err := failing.Start(context.Background(), workspace); err == nil {
		t.Fatal("failed start was accepted")
	}
	first, err := runtime.Start(context.Background(), workspace)
	if err != nil || first.Status != domain.StatusRunning {
		t.Fatalf("start = %#v %v", first, err)
	}
	second, err := runtime.Start(context.Background(), workspace)
	if err != nil || second.ID != first.ID {
		t.Fatalf("second start = %#v %v", second, err)
	}
	body, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(body), "\n"); lines != 1 {
		t.Fatalf("start count = %d\n%s", lines, body)
	}
	if _, err := runtime.Stop(context.Background(), workspace.ID); err != nil {
		t.Fatal(err)
	}
	crashing := New([]string{"sh", "-c", "echo password=hunter2; sleep 0.2; exit 1"}, nil)
	crashSpace := domain.Workspace{ID: "wsp_crash", Owner: "ada"}
	started, err := crashing.Start(context.Background(), crashSpace)
	if err != nil || started.Status != domain.StatusRunning {
		t.Fatalf("crash start = %#v %v", started, err)
	}
	if err := crashing.Crash(crashSpace.ID); err != nil {
		t.Fatal(err)
	}
	status, err := crashing.Status(context.Background(), crashSpace.ID)
	if err != nil || status.Status != domain.StatusFailed {
		t.Fatalf("after crash = %#v %v", status, err)
	}
	stopped, err := crashing.Stop(context.Background(), crashSpace.ID)
	if err != nil || stopped.Status != domain.StatusStopped {
		t.Fatalf("stop after crash = %#v %v", stopped, err)
	}
	deadline := time.Now().Add(time.Second)
	var page domain.LogPage
	for time.Now().Before(deadline) {
		page, err = crashing.Logs(context.Background(), crashSpace.ID, "", 20)
		if err == nil && len(page.Lines) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil || strings.Contains(strings.Join(page.Lines, "\n"), "hunter2") {
		t.Fatalf("logs = %#v %v", page, err)
	}
	if InheritedSecret(append(CommandEnv(), "TOKEN=nope")) && !InheritedSecret([]string{"TOKEN=nope"}) {
		t.Fatal("secret check inverted")
	}
	if InheritedSecret(CommandEnv()) {
		t.Fatal("child environment inherited a credential")
	}
}

func startLocalTCPServer(t *testing.T, banner string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen tcp: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = c.Write([]byte(banner))
			}(conn)
		}
	}()
	return listener.Addr().String()
}

func TestWorkspaceDefaultEgressAllowlistAndPolicyDenial(t *testing.T) {
	gitTarget := startLocalTCPServer(t, "GIT_OK")
	registryTarget := startLocalTCPServer(t, "REGISTRY_OK")
	forbiddenTarget := startLocalTCPServer(t, "FORBIDDEN_OK")

	egress, err := domain.ParseEgressValuesYAML([]byte(`
workspace:
  egress:
    gitHost: "git.example.internal"
`))
	if err != nil {
		t.Fatalf("ParseEgressValuesYAML: %v", err)
	}

	runtime, err := NewWithEgress([]string{"sh", "-c", "sleep 30"}, nil, egress)
	if err != nil {
		t.Fatalf("NewWithEgress: %v", err)
	}
	runtime.SetEndpointResolver(func(host string, port int) string {
		switch {
		case host == "git.example.internal" && port == 443:
			return gitTarget
		case host == domain.DefaultWorkspaceRegistryHost && port == domain.DefaultWorkspaceRegistryPort:
			return registryTarget
		default:
			return forbiddenTarget
		}
	})

	ws := domain.Workspace{ID: "wsp_isolated", Owner: "ada", Branch: "workspace/ada/wsp_isolated"}
	state, err := runtime.Start(context.Background(), ws)
	if err != nil || state.Status != domain.StatusRunning {
		t.Fatalf("Start workspace = %#v %v", state, err)
	}
	t.Cleanup(func() { _, _ = runtime.Stop(context.Background(), ws.ID) })

	// (1) Connection to configured git host and to the local registry succeeds.
	gitConn, err := runtime.DialContext(context.Background(), "tcp", "git.example.internal:443")
	if err != nil {
		t.Fatalf("expected connection to configured git host to succeed: %v", err)
	}
	buf := make([]byte, 16)
	n, _ := gitConn.Read(buf)
	_ = gitConn.Close()
	if string(buf[:n]) != "GIT_OK" {
		t.Fatalf("unexpected git response: %q", string(buf[:n]))
	}

	regAddr := net.JoinHostPort(domain.DefaultWorkspaceRegistryHost, strconv.Itoa(domain.DefaultWorkspaceRegistryPort))
	regConn, err := runtime.DialContext(context.Background(), "tcp", regAddr)
	if err != nil {
		t.Fatalf("expected connection to local registry to succeed: %v", err)
	}
	n, _ = regConn.Read(buf)
	_ = regConn.Close()
	if string(buf[:n]) != "REGISTRY_OK" {
		t.Fatalf("unexpected registry response: %q", string(buf[:n]))
	}

	// (2) Connection to any other host or address is refused.
	for _, blocked := range []string{
		"registry.npmjs.org:443",
		"1.1.1.1:443",
		"git.example.internal:22",
		"registry.uds.svc.cluster.local:443",
		forbiddenTarget,
	} {
		conn, dialErr := runtime.DialContext(context.Background(), "tcp", blocked)
		if conn != nil {
			_ = conn.Close()
		}
		if !errors.Is(dialErr, domain.ErrEgressDenied) {
			t.Fatalf("expected connection to %s to be refused with ErrEgressDenied, got %v", blocked, dialErr)
		}
	}

	// (3) A config that adds a wildcard or 0.0.0.0/0 entry fails validation.
	for _, badYAML := range []string{
		"workspace:\n  egress:\n    gitHost: \"git.example.internal\"\n    allow:\n      - name: wildcard\n        host: \"*.example.com\"\n        port: 443\n        protocol: TCP\n",
		"workspace:\n  egress:\n    gitHost: \"git.example.internal\"\n    allow:\n      - name: open-v4\n        host: \"0.0.0.0/0\"\n        port: 443\n        protocol: TCP\n",
	} {
		badCfg, parseErr := domain.ParseEgressValuesYAML([]byte(badYAML))
		if !errors.Is(parseErr, domain.ErrInvalidEgress) {
			t.Fatalf("expected ParseEgressValuesYAML to fail with ErrInvalidEgress, got %#v %v", badCfg, parseErr)
		}
		if _, rtErr := NewWithEgress([]string{"true"}, nil, domain.EgressConfig{
			GitHost: "git.example.internal",
			Allow: []domain.EgressAllowEntry{
				{Name: "open", Host: "0.0.0.0/0", Port: 443, Protocol: "TCP"},
			},
		}); !errors.Is(rtErr, domain.ErrInvalidEgress) {
			t.Fatalf("expected NewWithEgress to reject 0.0.0.0/0, got %v", rtErr)
		}
	}

	// (4) From inside the workspace, an attempt to edit the network policy is denied.
	for _, cmd := range [][]string{
		{"kubectl", "apply", "-f", "networkpolicy.yaml"},
		{"kubectl", "patch", "networkpolicy", "wsp-isolated-netpol"},
		{"sh", "-c", "cat <<EOF > networkpolicy.yaml\nspec: {}\nEOF"},
		{"iptables", "-F"},
	} {
		if _, execErr := runtime.ExecInWorkspace(context.Background(), ws, cmd); !errors.Is(execErr, domain.ErrForbidden) {
			t.Fatalf("expected ExecInWorkspace(%v) to be denied with ErrForbidden, got %v", cmd, execErr)
		}
	}
}

func TestWorkspaceContainerHasNoClusterCredentials(t *testing.T) {
	t.Setenv("KUBECONFIG", "/tmp/should-not-leak-kubeconfig")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.96.0.1")
	t.Setenv("KUBERNETES_SERVICE_PORT", "443")
	t.Setenv("DOCKER_HOST", "unix:///var/run/docker.sock")

	env := CommandEnv()
	if HasClusterCredentials(env) {
		t.Fatalf("CommandEnv leaked cluster or docker credentials: %v", env)
	}
	if !HasClusterCredentials(append([]string{}, "KUBECONFIG=/etc/kubeconfig")) {
		t.Fatal("HasClusterCredentials failed to detect KUBECONFIG")
	}
	if !HasClusterCredentials(append([]string{}, "KUBERNETES_SERVICE_HOST=10.96.0.1")) {
		t.Fatal("HasClusterCredentials failed to detect KUBERNETES_SERVICE_HOST")
	}

	workDir := t.TempDir()
	egress := domain.EgressConfig{GitHost: "git.example.internal"}
	runtime, err := NewWithEgress([]string{"sh", "-c", "sleep 30"}, func(domain.Workspace) string { return workDir }, egress)
	if err != nil {
		t.Fatalf("NewWithEgress: %v", err)
	}
	ws := domain.Workspace{ID: "wsp_nocreds", Owner: "ada"}
	if _, err := runtime.Start(context.Background(), ws); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _, _ = runtime.Stop(context.Background(), ws.ID) })

	if err := VerifyNoClusterCredentialsInWorkspace(env, workDir); err != nil {
		t.Fatalf("VerifyNoClusterCredentialsInWorkspace failed: %v", err)
	}
}
