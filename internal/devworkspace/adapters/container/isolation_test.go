package container

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func TestNewFromEnvRequiresGitHost(t *testing.T) {
	_, err := NewFromEnv(func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "gitHost") {
		t.Fatalf("missing git host: %v", err)
	}
	_, err = NewFromEnv(func(key string) string {
		if key == "DEVWORKSPACE_GIT_HOST" {
			return "*"
		}
		return ""
	})
	if err == nil {
		t.Fatal("wildcard git host was accepted")
	}
}

func TestIsolatedWorkspaceDeniesEgressAndHidesCredentials(t *testing.T) {
	if _, err := domain.ParseEgressValuesYAML([]byte("gitHost: '*'\n")); err == nil {
		t.Fatal("wildcard git host passed validation")
	}
	if _, err := domain.ParseEgressValuesYAML([]byte("gitHost: git.example.internal\nallow:\n  - {name: all, host: 0.0.0.0/0, port: 443, protocol: TCP}\n")); err == nil {
		t.Fatal("allow-all entry passed validation")
	}
	t.Setenv("KUBECONFIG", "/var/lib/conduit-not-mounted/kubeconfig")
	t.Setenv("SERVICEACCOUNT_TOKEN", "not-a-cluster-token")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	runtime, err := NewFromEnv(func(key string) string {
		switch key {
		case "DEVWORKSPACE_GIT_HOST":
			return "git.example.internal"
		case "DEVWORKSPACE_CONTAINER_BIN":
			return "docker"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runtime.Prove(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !report.HostCanaryReadable {
		t.Fatal("host could not read the canary kubeconfig, so the container check is inconclusive")
	}
	if !report.UpstreamPolicyOK {
		t.Fatal("policy endpoint did not answer on the upstream network, so a workspace refusal is inconclusive")
	}
	if report.UID != 65534 || !report.CapBounded {
		t.Fatalf("workspace is not an unprivileged user: uid=%d cap=%s bounded=%v", report.UID, report.CapEff, report.CapBounded)
	}
	if report.User != "65534:65534" || !report.ReadOnly || report.Privileged {
		t.Fatalf("observed launch user=%s readOnly=%v privileged=%v", report.User, report.ReadOnly, report.Privileged)
	}
	if report.NetworkMode == "" || report.NetworkMode == "host" || report.NetworkMode == "bridge" || report.NetworkMode == "default" {
		t.Fatalf("workspace network %q is not a dedicated internal network", report.NetworkMode)
	}
	if len(report.Binds) != 0 {
		t.Fatalf("workspace has host mounts: %v", report.Binds)
	}
	dropped := false
	for _, cap := range report.CapDrop {
		if cap == "ALL" {
			dropped = true
		}
	}
	if !dropped {
		t.Fatalf("capabilities were not dropped: %v", report.CapDrop)
	}
	if report.PolicyWriteError == "" {
		t.Fatal("workspace wrote a network policy file")
	}
	if len(report.ForbiddenEnv) != 0 {
		t.Fatalf("cluster credential environment leaked into the container: %v", report.ForbiddenEnv)
	}
	for _, cred := range report.Credentials {
		if cred.Present {
			t.Fatalf("credential path %s is visible inside the workspace", cred.Path)
		}
	}
	seen := map[string]DialResult{}
	for _, dial := range report.Dials {
		seen[dial.Name] = dial
	}
	for _, name := range []string{"git-host", "local-registry"} {
		dial, ok := seen[name]
		if !ok || !dial.OK {
			t.Fatalf("allowed endpoint %s did not succeed: %+v", name, dial)
		}
	}
	for _, name := range []string{"wrong-port", "other-host", "other-address", "policy"} {
		dial, ok := seen[name]
		if !ok || dial.OK {
			t.Fatalf("denied endpoint %s was reachable: %+v", name, dial)
		}
	}
}

func TestRuntimeStartIsIsolatedAndStopRemovesIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	runtime, err := NewFromEnv(func(key string) string {
		if key == "DEVWORKSPACE_GIT_HOST" {
			return "git.example.internal"
		}
		if key == "DEVWORKSPACE_CONTAINER_BIN" {
			return "docker"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := runtime.Start(ctx, domain.Workspace{ID: "ws-live"})
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != domain.StatusRunning {
		t.Fatalf("status %s", started.Status)
	}
	t.Cleanup(func() {
		_, _ = runtime.Stop(context.Background(), "ws-live")
	})
	runtime.mu.Lock()
	entry := runtime.runs["ws-live"]
	runtime.mu.Unlock()
	if entry == nil || entry.box == nil {
		t.Fatal("running workspace has no container session")
	}
	box := entry.box
	workspaceName := box.name("ws")
	observed, err := box.inspect(ctx, workspaceName)
	if err != nil {
		t.Fatal(err)
	}
	if observed.User != "65534:65534" || observed.Privileged || !observed.ReadOnly || len(observed.Binds) != 0 {
		t.Fatalf("running container is not isolated: %+v", observed)
	}
	if observed.NetworkMode == "host" || !strings.HasPrefix(observed.NetworkMode, "cws-in-") {
		t.Fatalf("running container network %q", observed.NetworkMode)
	}
	stopped, err := runtime.Stop(ctx, "ws-live")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != domain.StatusStopped {
		t.Fatalf("stop status %s", stopped.Status)
	}
	if _, _, err := box.cli.run(ctx, "inspect", workspaceName); err == nil {
		t.Fatal("workspace container still exists after stop")
	}
}

func TestUnavailableDoesNotStart(t *testing.T) {
	runtime := Unavailable{Reason: errors.New("workspace.egress.gitHost is required")}
	_, err := runtime.Start(context.Background(), domain.Workspace{ID: "ws"})
	if !errors.Is(err, domain.ErrDisabled) {
		t.Fatalf("start: %v", err)
	}
}
