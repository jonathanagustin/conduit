// Package process runs a local preview command with a cleaned environment.
package process

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

// Runtime supervises one child process per workspace.
type Runtime struct {
	mu       sync.Mutex
	command  []string
	dir      func(domain.Workspace) string
	runs     map[string]*child
	egress   *domain.EgressConfig
	resolver func(host string, port int) string
}

type child struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	status string
	logs   []string
	bytes  int
	closed bool
	done   chan struct{}
	fail   string
}

// New returns a runtime that executes command. dir, when set, is the working
// directory. The child inherits PATH only. This is a host process, not a container.
func New(command []string, dir func(domain.Workspace) string) *Runtime {
	if len(command) == 0 {
		command = []string{"sleep", "3600"}
	}
	return &Runtime{command: command, dir: dir, runs: map[string]*child{}}
}

// NewWithEgress validates the policy for the controlled DialContext helper.
// Start refuses arbitrary child execution because this adapter cannot enforce it.
func NewWithEgress(command []string, dir func(domain.Workspace) string, egress domain.EgressConfig) (*Runtime, error) {
	validated, err := domain.NormalizeAndValidateEgress(egress)
	if err != nil {
		return nil, err
	}
	rt := New(command, dir)
	rt.egress = &validated
	return rt, nil
}

// SetEndpointResolver binds logical allowed hosts (such as git-host and local-registry)
// to concrete dial addresses for deterministic socket testing.
func (runtime *Runtime) SetEndpointResolver(resolver func(host string, port int) string) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	runtime.resolver = resolver
}

func (runtime *Runtime) Start(_ context.Context, workspace domain.Workspace) (domain.Runtime, error) {
	if runtime.egress != nil {
		reason := "process runtime cannot enforce workspace isolation"
		return domain.Runtime{WorkspaceID: workspace.ID, Status: domain.StatusFailed, Failure: reason, Origin: domain.OriginLocalRuntime}, fmt.Errorf("%w: %s", domain.ErrDisabled, reason)
	}
	if domain.IsNetworkPolicyEditCommand(runtime.command) {
		return domain.Runtime{WorkspaceID: workspace.ID, Status: domain.StatusFailed, Failure: "workspace cannot modify its own network policy", Origin: domain.OriginLocalRuntime}, domain.ErrForbidden
	}
	runtime.mu.Lock()
	if current, ok := runtime.runs[workspace.ID]; ok {
		current.mu.Lock()
		status := current.status
		result := view(workspace, current)
		current.mu.Unlock()
		if status == domain.StatusRunning || status == domain.StatusStarting {
			runtime.mu.Unlock()
			return result, nil
		}
	}
	cmd := exec.Command(runtime.command[0], runtime.command[1:]...)
	cmd.Env = CommandEnv()
	if runtime.dir != nil {
		cmd.Dir = runtime.dir(workspace)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		runtime.mu.Unlock()
		return domain.Runtime{}, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		runtime.mu.Unlock()
		return domain.Runtime{WorkspaceID: workspace.ID, Status: domain.StatusFailed, Failure: err.Error(), Origin: domain.OriginLocalRuntime}, domain.ErrInvalid
	}
	entry := &child{cmd: cmd, status: domain.StatusStarting, done: make(chan struct{})}
	runtime.runs[workspace.ID] = entry
	runtime.mu.Unlock()
	go collect(entry, stdout)
	go func() {
		waitErr := cmd.Wait()
		entry.mu.Lock()
		if !entry.closed {
			entry.status = domain.StatusFailed
			if waitErr != nil {
				entry.fail = waitErr.Error()
			} else {
				entry.fail = "process exited"
			}
		}
		entry.mu.Unlock()
		close(entry.done)
	}()
	timer := time.NewTimer(150 * time.Millisecond)
	select {
	case <-entry.done:
	case <-timer.C:
	}
	timer.Stop()
	entry.mu.Lock()
	if entry.status == domain.StatusStarting {
		entry.status = domain.StatusRunning
	}
	result := view(workspace, entry)
	failed := entry.status == domain.StatusFailed
	entry.mu.Unlock()
	if failed {
		return result, domain.ErrInvalid
	}
	return result, nil
}

func (runtime *Runtime) Stop(ctx context.Context, workspaceID string) (domain.Runtime, error) {
	if err := ctx.Err(); err != nil {
		return domain.Runtime{}, err
	}
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return domain.Runtime{WorkspaceID: workspaceID, Status: domain.StatusStopped, Origin: domain.OriginLocalRuntime}, nil
	}
	entry.mu.Lock()
	cmd := entry.cmd
	running := entry.status == domain.StatusRunning || entry.status == domain.StatusStarting
	entry.mu.Unlock()
	if running {
		if cmd == nil || cmd.Process == nil {
			return domain.Runtime{}, domain.ErrConflict
		}
		if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			return domain.Runtime{}, err
		}
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-entry.done:
		case <-ctx.Done():
			return domain.Runtime{}, ctx.Err()
		case <-timer.C:
			return domain.Runtime{}, context.DeadlineExceeded
		}
	}
	entry.mu.Lock()
	entry.status = domain.StatusStopped
	entry.closed = true
	entry.mu.Unlock()
	return domain.Runtime{ID: "rt_" + workspaceID, WorkspaceID: workspaceID, Status: domain.StatusStopped, Origin: domain.OriginLocalRuntime}, nil
}

func (runtime *Runtime) Status(_ context.Context, workspaceID string) (domain.Runtime, error) {
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return domain.Runtime{}, domain.ErrNotFound
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	return domain.Runtime{ID: "rt_" + workspaceID, WorkspaceID: workspaceID, Status: entry.status, Failure: entry.fail, Origin: domain.OriginLocalRuntime}, nil
}

func (runtime *Runtime) Logs(_ context.Context, workspaceID, cursor string, limit int) (domain.LogPage, error) {
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return domain.LogPage{}, domain.ErrNotFound
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	start := 0
	if cursor != "" {
		for index, line := range entry.logs {
			if line == cursor {
				start = index + 1
			}
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	lines := append([]string{}, entry.logs[start:]...)
	next := ""
	if len(lines) > limit {
		lines = lines[:limit]
		next = lines[len(lines)-1]
	}
	return domain.LogPage{Lines: lines, NextCursor: next, Origin: domain.OriginLocalRuntime, Status: entry.status, ValueTier: domain.ValueTier}, nil
}

func (runtime *Runtime) Clear(_ context.Context, workspaceID string) error {
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return nil
	}
	entry.mu.Lock()
	entry.logs = nil
	entry.bytes = 0
	entry.mu.Unlock()
	return nil
}

func collect(entry *child, reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 256*1024)
	for scanner.Scan() {
		text, _ := domain.RedactLine(scanner.Text())
		entryAppend(entry, text)
	}
}

func entryAppend(entry *child, line string) {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.closed || entry.bytes >= domain.MaxLogBytes {
		if entry.bytes >= domain.MaxLogBytes && (len(entry.logs) == 0 || entry.logs[len(entry.logs)-1] != "[truncated]") {
			entry.logs = append(entry.logs, "[truncated]")
		}
		return
	}
	entry.logs = append(entry.logs, line)
	entry.bytes += len(line)
}

func view(workspace domain.Workspace, entry *child) domain.Runtime {
	return domain.Runtime{
		ID: "rt_" + workspace.ID, WorkspaceID: workspace.ID, Owner: workspace.Owner,
		Status: entry.status, Failure: entry.fail, Origin: domain.OriginLocalRuntime,
	}
}

// Crash is a test helper that waits until a short-lived child has exited.
func (runtime *Runtime) Crash(workspaceID string) error {
	runtime.mu.Lock()
	entry, ok := runtime.runs[workspaceID]
	runtime.mu.Unlock()
	if !ok {
		return domain.ErrNotFound
	}
	select {
	case <-entry.done:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("child did not exit")
	}
}

// CommandEnv filters inherited variables. It does not restrict filesystem access
// or establish rootless builder or container execution.
func CommandEnv() []string { return []string{"PATH=" + os.Getenv("PATH")} }

// InheritedSecret reports whether a secret or cluster credential variable would be passed to a child.
func InheritedSecret(env []string) bool {
	for _, item := range env {
		name, _, _ := strings.Cut(item, "=")
		lower := strings.ToLower(name)
		if strings.Contains(lower, "token") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
			return true
		}
	}
	return HasClusterCredentials(env)
}

// HasClusterCredentials reports whether environment variables expose Kubernetes
// cluster credentials, kubeconfigs, or a host Docker daemon socket.
func HasClusterCredentials(env []string) bool {
	for _, item := range env {
		name, val, _ := strings.Cut(item, "=")
		upper := strings.ToUpper(strings.TrimSpace(name))
		switch upper {
		case "KUBECONFIG", "KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_PORT", "KUBERNETES_PORT", "BEARER_TOKEN", "SERVICEACCOUNT_TOKEN":
			return true
		case "DOCKER_HOST":
			if strings.TrimSpace(val) != "" {
				return true
			}
		}
		if strings.HasPrefix(upper, "KUBERNETES_PORT_") || strings.HasPrefix(upper, "KUBERNETES_SERVICE_") {
			return true
		}
	}
	return false
}

// VerifyNoClusterCredentialsInWorkspace checks the supplied environment and a
// small set of relative paths. It does not confine absolute host file access.
func VerifyNoClusterCredentialsInWorkspace(env []string, workDir string) error {
	if HasClusterCredentials(env) {
		return fmt.Errorf("%w: cluster credential environment variable present in workspace", domain.ErrForbidden)
	}
	if strings.TrimSpace(workDir) == "" {
		return nil
	}
	for _, rel := range []string{
		".kube/config",
		"kubeconfig",
		"var/run/secrets/kubernetes.io/serviceaccount/token",
		"var/run/docker.sock",
		"run/docker.sock",
	} {
		if _, err := os.Stat(filepath.Join(workDir, filepath.FromSlash(rel))); err == nil {
			return fmt.Errorf("%w: forbidden credential or socket %s present in workspace", domain.ErrForbidden, rel)
		}
	}
	return nil
}

// DialContext applies the allowlist to this application-controlled connection.
// It does not intercept sockets opened directly by a child process.
func (runtime *Runtime) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	runtime.mu.Lock()
	egress := runtime.egress
	resolver := runtime.resolver
	runtime.mu.Unlock()
	if egress == nil {
		return nil, fmt.Errorf("%w: workspace egress allowlist is not configured", domain.ErrEgressDenied)
	}
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrEgressDenied, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid port %q", domain.ErrEgressDenied, portStr)
	}
	proto := "TCP"
	if strings.HasPrefix(strings.ToLower(network), "udp") {
		proto = "UDP"
	}
	if err := domain.AllowsEndpoint(*egress, host, port, proto); err != nil {
		return nil, err
	}
	dialAddr := address
	if resolver != nil {
		if mapped := strings.TrimSpace(resolver(host, port)); mapped != "" {
			dialAddr = mapped
		}
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, dialAddr)
}

// ExecInWorkspace refuses execution because this adapter has no filesystem or
// network namespace. A filtered environment and a client-side dial helper do not
// isolate arbitrary child code.
func (runtime *Runtime) ExecInWorkspace(ctx context.Context, _ domain.Workspace, argv []string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if domain.IsNetworkPolicyEditCommand(argv) {
		return "", domain.ErrForbidden
	}
	return "", fmt.Errorf("%w: process runtime cannot enforce workspace isolation", domain.ErrDisabled)
}
