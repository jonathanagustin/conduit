package domain

import (
	"fmt"
	"sort"
	"strings"
)

// LaunchRequest is the container execution contract for one workspace command.
// It carries no host socket, kubeconfig, or cluster credential.
type LaunchRequest struct {
	Image      string
	Name       string
	Network    string
	Command    []string
	Env        map[string]string
	ExtraHosts []string
	Mounts     []string
	Detach     bool
	Remove     bool
}

// ContainerRunArgs returns the container-runtime arguments for an isolated
// workspace command. The network must be a dedicated internal network created
// for this workspace. Host, bridge, and none are rejected: none cannot reach
// the allowlist, and the others do not default-deny egress.
func ContainerRunArgs(request LaunchRequest) ([]string, error) {
	if strings.TrimSpace(request.Image) == "" {
		return nil, fmt.Errorf("%w: workspace image is required", ErrInvalid)
	}
	if strings.TrimSpace(request.Name) == "" {
		return nil, fmt.Errorf("%w: workspace container name is required", ErrInvalid)
	}
	network := strings.TrimSpace(request.Network)
	switch strings.ToLower(network) {
	case "", "host", "bridge", "default", "none":
		return nil, fmt.Errorf("%w: workspace network must be a dedicated internal network", ErrInvalid)
	}
	if len(request.Command) == 0 || strings.TrimSpace(request.Command[0]) == "" {
		return nil, fmt.Errorf("%w: workspace command is required", ErrInvalid)
	}
	if IsNetworkPolicyEditCommand(request.Command) {
		return nil, fmt.Errorf("%w: workspace cannot modify its own network policy", ErrForbidden)
	}
	for _, mount := range request.Mounts {
		if forbiddenLaunchMount(mount) {
			return nil, fmt.Errorf("%w: forbidden workspace mount %q", ErrInvalid, mount)
		}
	}
	for _, host := range request.ExtraHosts {
		if forbiddenLaunchHost(host) {
			return nil, fmt.Errorf("%w: forbidden workspace extra host %q", ErrInvalid, host)
		}
	}
	env := map[string]string{
		"HOME":                   "/tmp",
		"BUILDKIT_ROOTLESS":      "1",
		"BUILDAH_ISOLATION":      "chroot",
		"STORAGE_DRIVER":         "vfs",
		"CONDUIT_ISOLATION_MODE": "container",
	}
	for key, value := range request.Env {
		if isForbiddenContainerEnv(key, value) {
			return nil, fmt.Errorf("%w: forbidden workspace env %q", ErrInvalid, key)
		}
		env[key] = value
	}
	args := []string{"run"}
	if request.Detach {
		args = append(args, "--detach")
	} else if request.Remove {
		args = append(args, "--rm")
	}
	args = append(args,
		"--name", request.Name,
		"--network", network,
		"--user", "65534:65534",
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--tmpfs", "/tmp:rw,nosuid,nodev,size=16m",
	)
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "--env", key+"="+env[key])
	}
	for _, host := range request.ExtraHosts {
		args = append(args, "--add-host", host)
	}
	args = append(args, request.Image)
	args = append(args, request.Command...)
	return args, nil
}

func forbiddenLaunchMount(mount string) bool {
	lower := strings.ToLower(strings.TrimSpace(mount))
	if lower == "" {
		return true
	}
	return isForbiddenMountPath(lower) ||
		strings.Contains(lower, "kubeconfig") ||
		strings.Contains(lower, "/.kube") ||
		strings.Contains(lower, ":/var/run/docker.sock") ||
		strings.Contains(lower, ":/run/docker.sock")
}

func forbiddenLaunchHost(host string) bool {
	lower := strings.ToLower(strings.TrimSpace(host))
	return lower == "" || strings.Contains(lower, "*") || strings.Contains(lower, "0.0.0.0") || strings.Contains(lower, "::/0")
}
