// Package container runs a workspace command in a container with a dedicated
// internal network. Only the configured git host and registry are forwarded.
// The container has no host socket and no cluster credentials.
//
// CONDUIT-INVENTED: devworkspace.IsolatedContainer
// Displaced-by: an admitted per-workspace runtime that enforces default-deny egress and credential absence
package container

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

const boundaryTag = "conduit-workspace-boundary:local"

// Runtime executes workspace commands inside an isolated container.
type Runtime struct {
	Bin           string
	Egress        domain.EgressConfig
	BoundaryImage string
	ModuleRoot    string
	mu            sync.Mutex
	runs          map[string]*session
}

type session struct {
	mu      sync.Mutex
	status  string
	failure string
	logs    []string
	box     *sandbox
}

// Unavailable refuses to start a host process when container isolation was
// requested but is not configured.
type Unavailable struct {
	Reason error
}

func (runtime Unavailable) Start(context.Context, domain.Workspace) (domain.Runtime, error) {
	return domain.Runtime{}, fmt.Errorf("%w: %v", domain.ErrDisabled, runtime.Reason)
}

func (runtime Unavailable) Stop(context.Context, string) (domain.Runtime, error) {
	return domain.Runtime{}, fmt.Errorf("%w: %v", domain.ErrDisabled, runtime.Reason)
}

func (runtime Unavailable) Status(context.Context, string) (domain.Runtime, error) {
	return domain.Runtime{}, fmt.Errorf("%w: %v", domain.ErrDisabled, runtime.Reason)
}

func (runtime Unavailable) Logs(context.Context, string, string, int) (domain.LogPage, error) {
	return domain.LogPage{}, fmt.Errorf("%w: %v", domain.ErrDisabled, runtime.Reason)
}

func (runtime Unavailable) Clear(context.Context, string) error {
	return fmt.Errorf("%w: %v", domain.ErrDisabled, runtime.Reason)
}

// NewFromEnv requires an operator-supplied git host. An empty host is refused
// rather than widened to the public network.
func NewFromEnv(getenv func(string) string) (*Runtime, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	gitHost := strings.TrimSpace(getenv("DEVWORKSPACE_GIT_HOST"))
	if gitHost == "" {
		return nil, fmt.Errorf("%w: workspace.egress.gitHost is required and must not be empty", domain.ErrInvalidEgress)
	}
	validated, err := domain.NormalizeAndValidateEgress(domain.EgressConfig{
		GitHost:      gitHost,
		GitPort:      atoiPort(getenv("DEVWORKSPACE_GIT_PORT")),
		Registry:     getenv("DEVWORKSPACE_REGISTRY"),
		RegistryPort: atoiPort(getenv("DEVWORKSPACE_REGISTRY_PORT")),
	})
	if err != nil {
		return nil, err
	}
	bin := strings.TrimSpace(getenv("DEVWORKSPACE_CONTAINER_BIN"))
	if bin == "" {
		bin = "docker"
	}
	return &Runtime{
		Bin:           bin,
		Egress:        validated,
		BoundaryImage: strings.TrimSpace(getenv("DEVWORKSPACE_BOUNDARY_IMAGE")),
		ModuleRoot:    strings.TrimSpace(getenv("CONDUIT_MODULE_ROOT")),
		runs:          map[string]*session{},
	}, nil
}

// ProveFromEnv runs the live boundary check for the operator egress configuration.
func ProveFromEnv(ctx context.Context, getenv func(string) string) (Report, error) {
	runtime, err := NewFromEnv(getenv)
	if err != nil {
		return Report{}, err
	}
	return runtime.Prove(ctx)
}

// Report is what a workspace container observed, plus the host-side control
// that the denied policy endpoint was actually listening.
type Report struct {
	UID                int             `json:"uid"`
	CapEff             string          `json:"capEff"`
	CapBnd             string          `json:"capBnd"`
	CapBounded         bool            `json:"capBounded"`
	Dials              []DialResult    `json:"dials"`
	Credentials        []CredentialHit `json:"credentials"`
	ForbiddenEnv       []string        `json:"forbiddenEnv"`
	PolicyWriteError   string          `json:"policyWriteError"`
	NetworkMode        string          `json:"networkMode"`
	Networks           []string        `json:"networks"`
	NetworkInternal    bool            `json:"networkInternal"`
	User               string          `json:"user"`
	Binds              []string        `json:"binds"`
	ReadOnly           bool            `json:"readOnly"`
	Privileged         bool            `json:"privileged"`
	CapDrop            []string        `json:"capDrop"`
	HostCanaryReadable bool            `json:"hostCanaryReadable"`
	UpstreamPolicyOK   bool            `json:"upstreamPolicyOK"`
	CanaryPath         string          `json:"canaryPath"`
}

type DialResult struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Expect string `json:"expect"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type CredentialHit struct {
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

// Prove starts listeners for the allowlist, runs a probe inside the workspace
// container, and checks that a policy endpoint on the other network answers
// from that network and not from the workspace.
func (runtime *Runtime) Prove(ctx context.Context) (Report, error) {
	validated, err := domain.NormalizeAndValidateEgress(runtime.Egress)
	if err != nil {
		return Report{}, err
	}
	if validated.GitPort == validated.RegistryPort {
		return Report{}, fmt.Errorf("%w: git and registry ports must differ so the forwarder can tell them apart", domain.ErrInvalidEgress)
	}
	image, err := runtime.image(ctx)
	if err != nil {
		return Report{}, err
	}
	box, err := newSandbox(runtime.Bin)
	if err != nil {
		return Report{}, err
	}
	defer box.cleanup()
	if err := box.createNetworks(ctx); err != nil {
		return Report{}, err
	}
	if err := box.startListener(ctx, image, "git", validated.GitPort, "git-up"); err != nil {
		return Report{}, err
	}
	if err := box.startListener(ctx, image, "reg", validated.RegistryPort, "reg-up"); err != nil {
		return Report{}, err
	}
	if err := box.startListener(ctx, image, "policy", 9443, "policy"); err != nil {
		return Report{}, err
	}
	denyPort := 9
	if denyPort == validated.GitPort || denyPort == validated.RegistryPort {
		denyPort = 7
	}
	rules := []forwardRule{
		{Listen: validated.GitPort, Upstream: netJoin("git-up", validated.GitPort)},
		{Listen: validated.RegistryPort, Upstream: netJoin("reg-up", validated.RegistryPort)},
		{Listen: denyPort, Upstream: ""},
	}
	if err := box.startForwarder(ctx, image, rules); err != nil {
		return Report{}, err
	}
	if err := box.attachInternal(ctx); err != nil {
		return Report{}, err
	}
	forwarderIP, err := box.ip(ctx, box.forwarder, box.internalNet)
	if err != nil {
		return Report{}, err
	}
	policyIP, err := box.ip(ctx, box.name("policy"), box.upstreamNet)
	if err != nil {
		return Report{}, err
	}
	canaryDir, err := os.MkdirTemp("", "conduit-canary-")
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(canaryDir)
	canaryPath := filepath.Join(canaryDir, "kubeconfig")
	canaryBody := []byte("apiVersion: v1\nkind: Config\nclusters: []\n")
	if err := os.WriteFile(canaryPath, canaryBody, 0o600); err != nil {
		return Report{}, err
	}
	hostBytes, hostErr := os.ReadFile(canaryPath)
	spec := probeSpec{
		Dials: []dialSpec{
			{Name: "git-host", Host: validated.GitHost, Port: validated.GitPort, Expect: "allow"},
			{Name: "local-registry", Host: validated.Registry, Port: validated.RegistryPort, Expect: "allow"},
			{Name: "wrong-port", Host: validated.GitHost, Port: denyPort, Expect: "deny"},
			{Name: "other-host", Host: "denied.example.internal", Port: 443, Expect: "deny"},
			{Name: "other-address", Host: "1.2.3.4", Port: 80, Expect: "deny"},
			{Name: "policy", Host: policyIP, Port: 9443, Expect: "deny"},
		},
		CredentialPaths: []string{
			canaryPath,
			"/var/run/secrets/kubernetes.io/serviceaccount/token",
			"/root/.kube/config",
			"/.kube/config",
			"/var/run/docker.sock",
			"/run/docker.sock",
			"/etc/kubernetes/kubeconfig",
		},
		ForbiddenEnv: []string{
			"KUBECONFIG", "KUBERNETES_SERVICE_HOST", "KUBERNETES_SERVICE_PORT", "KUBERNETES_PORT",
			"BEARER_TOKEN", "SERVICEACCOUNT_TOKEN", "DOCKER_HOST",
		},
	}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return Report{}, err
	}
	workspaceName := box.name("ws")
	args, err := domain.ContainerRunArgs(domain.LaunchRequest{
		Image:   image,
		Name:    workspaceName,
		Network: box.internalNet,
		Command: []string{"probe"},
		Env:     map[string]string{"CONDUIT_PROBE": base64.StdEncoding.EncodeToString(encoded)},
		ExtraHosts: []string{
			validated.GitHost + ":" + forwarderIP,
			validated.Registry + ":" + forwarderIP,
		},
	})
	if err != nil {
		return Report{}, err
	}
	stdout, _, runErr := box.cli.run(ctx, args...)
	observed, inspectErr := box.inspect(ctx, workspaceName)
	if runErr != nil {
		return Report{}, runErr
	}
	if inspectErr != nil {
		return Report{}, inspectErr
	}
	var inside probeResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &inside); err != nil {
		return Report{}, fmt.Errorf("workspace probe output: %w: %s", err, strings.TrimSpace(stdout))
	}
	controlOK, controlErr := box.control(ctx, image, policyIP)
	if controlErr != nil {
		return Report{}, controlErr
	}
	return Report{
		UID:                inside.UID,
		CapEff:             inside.CapEff,
		CapBnd:             inside.CapBnd,
		CapBounded:         inside.CapBounded,
		Dials:              inside.Dials,
		Credentials:        inside.Credentials,
		ForbiddenEnv:       inside.ForbiddenEnv,
		PolicyWriteError:   inside.PolicyWriteError,
		NetworkMode:        observed.NetworkMode,
		Networks:           observed.Networks,
		NetworkInternal:    observed.NetworkInternal,
		User:               observed.User,
		Binds:              observed.Binds,
		ReadOnly:           observed.ReadOnly,
		Privileged:         observed.Privileged,
		CapDrop:            observed.CapDrop,
		HostCanaryReadable: hostErr == nil && string(hostBytes) == string(canaryBody),
		UpstreamPolicyOK:   controlOK,
		CanaryPath:         canaryPath,
	}, nil
}

func (runtime *Runtime) image(ctx context.Context) (string, error) {
	if strings.TrimSpace(runtime.BoundaryImage) != "" {
		return runtime.BoundaryImage, nil
	}
	root := runtime.ModuleRoot
	if root == "" {
		found, err := findModuleRoot()
		if err != nil {
			return "", err
		}
		root = found
	}
	return ensureBoundaryImage(ctx, runtime.Bin, root)
}

func atoiPort(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		return -1
	}
	return port
}

type probeSpec struct {
	Dials           []dialSpec `json:"dials"`
	CredentialPaths []string   `json:"credentialPaths"`
	ForbiddenEnv    []string   `json:"forbiddenEnv"`
}

type dialSpec struct {
	Name   string `json:"name"`
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Expect string `json:"expect"`
}

type probeResult struct {
	UID              int             `json:"uid"`
	CapEff           string          `json:"capEff"`
	CapBnd           string          `json:"capBnd"`
	CapBounded       bool            `json:"capBounded"`
	Dials            []DialResult    `json:"dials"`
	Credentials      []CredentialHit `json:"credentials"`
	ForbiddenEnv     []string        `json:"forbiddenEnv"`
	PolicyWriteError string          `json:"policyWriteError"`
}

type forwardRule struct {
	Listen   int    `json:"listen"`
	Upstream string `json:"upstream"`
}

type observedContainer struct {
	NetworkMode     string
	Networks        []string
	NetworkInternal bool
	User            string
	Binds           []string
	ReadOnly        bool
	Privileged      bool
	CapDrop         []string
}

type sandbox struct {
	cli         dockerCLI
	id          string
	internalNet string
	upstreamNet string
	forwarder   string
	names       []string
	networks    []string
}

func newSandbox(bin string) (*sandbox, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(buf)
	return &sandbox{
		cli:         dockerCLI{bin: bin},
		id:          id,
		internalNet: "cws-in-" + id,
		upstreamNet: "cws-up-" + id,
	}, nil
}

func (box *sandbox) name(role string) string {
	return "cws-" + role + "-" + box.id
}

func (box *sandbox) createNetworks(ctx context.Context) error {
	if _, _, err := box.cli.run(ctx, "network", "create", "--internal", box.internalNet); err != nil {
		return fmt.Errorf("create internal network: %w", err)
	}
	box.networks = append(box.networks, box.internalNet)
	if _, _, err := box.cli.run(ctx, "network", "create", box.upstreamNet); err != nil {
		return fmt.Errorf("create upstream network: %w", err)
	}
	box.networks = append(box.networks, box.upstreamNet)
	return nil
}

func (box *sandbox) startListener(ctx context.Context, image, role string, port int, alias string) error {
	name := box.name(role)
	_, _, err := box.cli.run(ctx,
		"run", "--detach", "--name", name,
		"--network", box.upstreamNet, "--network-alias", alias,
		image, "listen", strconv.Itoa(port),
	)
	if err != nil {
		return err
	}
	box.names = append(box.names, name)
	return box.waitRunning(ctx, name)
}

func (box *sandbox) startForwarder(ctx context.Context, image string, rules []forwardRule) error {
	encoded, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	name := box.name("fwd")
	_, _, err = box.cli.run(ctx,
		"run", "--detach", "--name", name,
		"--network", box.upstreamNet,
		"--env", "CONDUIT_FORWARD="+string(encoded),
		image, "forward",
	)
	if err != nil {
		return err
	}
	box.forwarder = name
	box.names = append(box.names, name)
	return box.waitRunning(ctx, name)
}

func (box *sandbox) attachInternal(ctx context.Context) error {
	_, _, err := box.cli.run(ctx, "network", "connect", box.internalNet, box.forwarder)
	return err
}

func (box *sandbox) ip(ctx context.Context, containerName, network string) (string, error) {
	out, _, err := box.cli.run(ctx, "inspect", "-f", fmt.Sprintf(`{{(index .NetworkSettings.Networks %q).IPAddress}}`, network), containerName)
	if err != nil {
		return "", err
	}
	ip := strings.TrimSpace(out)
	if ip == "" {
		return "", fmt.Errorf("container %s has no address on %s", containerName, network)
	}
	return ip, nil
}

func (box *sandbox) waitRunning(ctx context.Context, name string) error {
	deadline := time.Now().Add(5 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		out, _, err := box.cli.run(ctx, "inspect", "-f", "{{.State.Running}} {{.State.Status}} {{.State.Error}}", name)
		last = strings.TrimSpace(out)
		if err == nil && strings.HasPrefix(last, "true ") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	logs, _, _ := box.cli.run(ctx, "logs", name)
	return fmt.Errorf("container %s did not stay running (%s): %s", name, last, strings.TrimSpace(logs))
}

func (box *sandbox) inspect(ctx context.Context, name string) (observedContainer, error) {
	out, _, err := box.cli.run(ctx, "inspect", name)
	if err != nil {
		return observedContainer{}, err
	}
	var rows []struct {
		HostConfig struct {
			NetworkMode    string   `json:"NetworkMode"`
			Binds          []string `json:"Binds"`
			CapDrop        []string `json:"CapDrop"`
			ReadonlyRootfs bool     `json:"ReadonlyRootfs"`
			Privileged     bool     `json:"Privileged"`
		} `json:"HostConfig"`
		Config struct {
			User string `json:"User"`
		} `json:"Config"`
		NetworkSettings struct {
			Networks map[string]json.RawMessage `json:"Networks"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		return observedContainer{}, err
	}
	if len(rows) != 1 {
		return observedContainer{}, fmt.Errorf("inspect %s returned %d rows", name, len(rows))
	}
	row := rows[0]
	// Podman reports the bridge driver as NetworkMode even for a named
	// network. Membership and the network's inspected Internal property are
	// the evidence of isolation; the driver's label alone cannot establish it.
	networks := make([]string, 0, len(row.NetworkSettings.Networks))
	for network := range row.NetworkSettings.Networks {
		networks = append(networks, network)
	}
	sort.Strings(networks)
	internal := false
	if len(networks) == 1 && networks[0] == box.internalNet && row.HostConfig.NetworkMode != "host" {
		networkJSON, _, err := box.cli.run(ctx, "network", "inspect", box.internalNet)
		if err != nil {
			return observedContainer{}, err
		}
		var networkRows []struct {
			Internal bool `json:"Internal"`
		}
		if err := json.Unmarshal([]byte(networkJSON), &networkRows); err != nil {
			return observedContainer{}, err
		}
		if len(networkRows) != 1 {
			return observedContainer{}, fmt.Errorf("inspect network %s returned %d rows", box.internalNet, len(networkRows))
		}
		internal = networkRows[0].Internal
	}
	return observedContainer{
		NetworkMode:     string(row.HostConfig.NetworkMode),
		Networks:        networks,
		NetworkInternal: internal,
		User:            row.Config.User,
		Binds:           row.HostConfig.Binds,
		ReadOnly:        row.HostConfig.ReadonlyRootfs,
		Privileged:      row.HostConfig.Privileged,
		CapDrop:         row.HostConfig.CapDrop,
	}, nil
}

func (box *sandbox) control(ctx context.Context, image, policyIP string) (bool, error) {
	spec := probeSpec{Dials: []dialSpec{{Name: "policy-control", Host: policyIP, Port: 9443, Expect: "allow"}}}
	encoded, err := json.Marshal(spec)
	if err != nil {
		return false, err
	}
	args, err := domain.ContainerRunArgs(domain.LaunchRequest{
		Image:   image,
		Name:    box.name("ctl"),
		Network: box.upstreamNet,
		Command: []string{"probe"},
		Env:     map[string]string{"CONDUIT_PROBE": base64.StdEncoding.EncodeToString(encoded)},
		Remove:  true,
	})
	if err != nil {
		return false, err
	}
	stdout, _, err := box.cli.run(ctx, args...)
	if err != nil {
		return false, fmt.Errorf("upstream policy control: %w", err)
	}
	var inside probeResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &inside); err != nil {
		return false, err
	}
	if len(inside.Dials) != 1 || !inside.Dials[0].OK {
		return false, fmt.Errorf("policy control dial: %+v", inside.Dials)
	}
	return true, nil
}

func (box *sandbox) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, name := range box.names {
		_, _, _ = box.cli.run(ctx, "rm", "--force", name)
	}
	for _, name := range []string{box.name("ws"), box.name("ctl")} {
		_, _, _ = box.cli.run(ctx, "rm", "--force", name)
	}
	for _, network := range box.networks {
		_, _, _ = box.cli.run(ctx, "network", "rm", network)
	}
}

type dockerCLI struct {
	bin string
}

func (cli dockerCLI) run(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, cli.bin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		err = fmt.Errorf("%s %s: %w: %s", cli.bin, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), stderr.String(), err
}

func netJoin(host string, port int) string {
	return host + ":" + strconv.Itoa(port)
}

var (
	imageOnce sync.Once
	imageRef  string
	imageErr  error
)

func ensureBoundaryImage(ctx context.Context, bin, moduleRoot string) (string, error) {
	imageOnce.Do(func() {
		imageRef, imageErr = buildBoundaryImage(ctx, bin, moduleRoot)
	})
	return imageRef, imageErr
}

func buildBoundaryImage(ctx context.Context, bin, moduleRoot string) (string, error) {
	dir, err := os.MkdirTemp("", "conduit-boundary-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	binary := filepath.Join(dir, "boundary")
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-s -w", "-o", binary, "./cmd/workspace-boundary")
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("build workspace boundary: %w: %s", err, strings.TrimSpace(string(output)))
	}
	dockerfile := "FROM scratch\nCOPY boundary /boundary\nENTRYPOINT [\"/boundary\"]\n"
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		return "", err
	}
	cli := dockerCLI{bin: bin}
	if _, _, err := cli.run(ctx, "build", "--tag", boundaryTag, dir); err != nil {
		return "", err
	}
	return boundaryTag, nil
}

func findModuleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found for workspace boundary image")
		}
		dir = parent
	}
}
