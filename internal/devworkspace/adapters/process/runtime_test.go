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
	runtime := New([]string{"sh", "-c", `echo $$ >> "$1"; exec sleep 30`, "preview-test", marker}, nil)
	workspace := domain.Workspace{ID: "wsp_one", Owner: "ada"}
	stopFixture(t, runtime, workspace.ID)
	// A failed OS spawn is an unconditional refusal, unlike an asynchronously
	// exiting child whose Wait observation can follow Start's grace window.
	failing := New([]string{filepath.Join(dir, "missing-preview-command")}, nil)
	if failed, err := failing.Start(context.Background(), workspace); !errors.Is(err, domain.ErrInvalid) || failed.Status != domain.StatusFailed {
		t.Fatalf("failed spawn = %#v %v", failed, err)
	}
	first, err := runtime.Start(context.Background(), workspace)
	if err != nil || first.Status != domain.StatusRunning {
		t.Fatalf("start = %#v %v", first, err)
	}
	second, err := runtime.Start(context.Background(), workspace)
	if err != nil || second.ID != first.ID {
		t.Fatalf("second start = %#v %v", second, err)
	}
	// The marker is the fixture's readiness signal; Start acknowledges a process,
	// not arbitrary application readiness within a fixed scheduler interval.
	var body []byte
	deadline := time.Now().Add(5 * time.Second)
	for {
		body, err = os.ReadFile(marker)
		if err == nil && len(body) > 0 {
			break
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture did not write readiness marker: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lines := strings.Count(string(body), "\n"); lines != 1 {
		t.Fatalf("start count = %d\n%s", lines, body)
	}
	if _, err := runtime.Stop(context.Background(), workspace.ID); err != nil {
		t.Fatal(err)
	}
	crashSignal := filepath.Join(dir, "exit-preview")
	crashing := New([]string{"sh", "-c", `echo password=hunter2; while [ ! -f "$1" ]; do sleep 0.01; done; exit 1`, "crash-test", crashSignal}, nil)
	crashSpace := domain.Workspace{ID: "wsp_crash", Owner: "ada"}
	stopFixture(t, crashing, crashSpace.ID)
	started, err := crashing.Start(context.Background(), crashSpace)
	if err != nil || started.Status != domain.StatusRunning {
		t.Fatalf("crash start = %#v %v", started, err)
	}
	// Observe the fixture output before signalling its exit; do not race a
	// natural 200ms exit against Start's 150ms observation window.
	var page domain.LogPage
	deadline = time.Now().Add(5 * time.Second)
	for {
		page, err = crashing.Logs(context.Background(), crashSpace.ID, "", 20)
		if err == nil && len(page.Lines) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("fixture emitted no startup log: %#v %v", page, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := os.WriteFile(crashSignal, []byte("exit"), 0600); err != nil {
		t.Fatal(err)
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
	deadline = time.Now().Add(time.Second)
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

func stopFixture(t *testing.T, runtime *Runtime, id string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := runtime.Stop(ctx, id); err != nil {
			t.Errorf("fixture cleanup: %v", err)
		}
	})
}

func TestImmediateExitEventuallyReportsFailed(t *testing.T) {
	runtime := New([]string{"false"}, nil)
	workspace := domain.Workspace{ID: "wsp_exit", Owner: "ada"}
	stopFixture(t, runtime, workspace.ID)
	started, err := runtime.Start(t.Context(), workspace)
	if err != nil && (!errors.Is(err, domain.ErrInvalid) || started.Status != domain.StatusFailed) {
		t.Fatalf("start = %#v %v", started, err)
	}
	if err == nil && started.Status != domain.StatusRunning && started.Status != domain.StatusStarting {
		t.Fatalf("unexpected provisional status: %#v", started)
	}
	runtime.mu.Lock()
	entry := runtime.runs[workspace.ID]
	runtime.mu.Unlock()
	if entry == nil {
		t.Fatal("fixture did not launch a tracked child")
	}
	// Wait for this owned child's exit observation rather than assuming its OS
	// execution and waiter goroutine both finish before the startup grace timer.
	select {
	case <-entry.done:
	case <-time.After(5 * time.Second):
		t.Fatal("child exit was not observed")
	}
	status, err := runtime.Status(t.Context(), workspace.ID)
	if err != nil || status.Status != domain.StatusFailed || status.Failure == "" {
		t.Fatalf("exited child = %#v %v", status, err)
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

func TestControlledDialAllowlistAndCommandRejection(t *testing.T) {
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

	// Controlled helper connections honor the configured endpoints; no child runs.
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

	// The helper refuses other endpoints; this is not a kernel egress test.
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

func TestFilteredEnvironmentDoesNotInheritClusterCredentials(t *testing.T) {
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
	if _, err := runtime.Start(context.Background(), ws); !errors.Is(err, domain.ErrDisabled) {
		t.Fatalf("expected unavailable isolation, got %v", err)
	}

	if err := VerifyNoClusterCredentialsInWorkspace(env, workDir); err != nil {
		t.Fatalf("VerifyNoClusterCredentialsInWorkspace failed: %v", err)
	}
}

// The control uses a real child socket/file read, rather than the cooperative
// DialContext helper. A cwd and filtered environment do not confine a process.
func TestIsolationRequestsRefuseHostProcessExecution(t *testing.T) {
	for _, mode := range []string{"network", "host-file"} {
		t.Run(mode, func(t *testing.T) {
			workDir := t.TempDir()
			destination := startLocalTCPServer(t, "reachable")
			if mode == "host-file" {
				destination = filepath.Join(t.TempDir(), "outside-workspace")
				if err := os.WriteFile(destination, []byte("test sentinel"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			makeCommand := func(marker string) []string {
				return []string{os.Args[0], "-test.run=^TestIsolationProbeHelper$", "--", "conduit-isolation-probe", mode, destination, marker}
			}
			// Establish that this exact helper can reach the socket or file as an
			// ordinary local preview, so the isolation check cannot pass vacuously.
			controlMarker := filepath.Join(t.TempDir(), "control")
			control := New(makeCommand(controlMarker), func(domain.Workspace) string { return workDir })
			controlWS := domain.Workspace{ID: "control"}
			t.Cleanup(func() { _, _ = control.Stop(context.Background(), controlWS.ID) })
			_, _ = control.Start(t.Context(), controlWS)
			if err := control.Crash(controlWS.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(controlMarker); err != nil {
				t.Fatalf("uncontained probe did not establish reachability: %v", err)
			}

			marker := filepath.Join(t.TempDir(), "must-not-exist")
			isolated, err := NewWithEgress(makeCommand(marker), func(domain.Workspace) string { return workDir }, domain.EgressConfig{GitHost: "git.example.internal"})
			if err != nil {
				t.Fatal(err)
			}
			workspace := domain.Workspace{ID: "must-not-start"}
			t.Cleanup(func() { _, _ = isolated.Stop(context.Background(), workspace.ID) })
			result, err := isolated.Start(t.Context(), workspace)
			if !errors.Is(err, domain.ErrDisabled) || result.Status != domain.StatusFailed {
				t.Fatalf("unconfined Start accepted isolation request: %#v, %v", result, err)
			}
			if _, err := isolated.Status(t.Context(), workspace.ID); !errors.Is(err, domain.ErrNotFound) {
				t.Fatalf("refused request created a child: %v", err)
			}
			for _, runtime := range []*Runtime{isolated, New(nil, nil)} {
				if _, err := runtime.ExecInWorkspace(t.Context(), workspace, makeCommand(marker)); !errors.Is(err, domain.ErrDisabled) {
					t.Fatalf("unconfined Exec accepted: %v", err)
				}
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("refused execution touched host state: %v", err)
			}
		})
	}
}

func TestIsolationProbeHelper(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "conduit-isolation-probe" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	args := os.Args[index+1:]
	if len(args) != 3 {
		t.Fatal("invalid probe arguments")
	}
	switch args[0] {
	case "network":
		conn, err := net.DialTimeout("tcp", args[1], time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	case "host-file":
		if _, err := os.ReadFile(args[1]); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("invalid probe mode")
	}
	if err := os.WriteFile(args[2], []byte("probe succeeded"), 0o600); err != nil {
		t.Fatal(err)
	}
}
