package domain

import (
	"errors"
	"strings"
	"testing"
)

func TestContainerRunArgsAreRootlessAndCredentialFree(t *testing.T) {
	args, err := ContainerRunArgs(LaunchRequest{
		Image:   "workspace-boundary:local",
		Name:    "ws-probe",
		Network: "cws-in-abc",
		Command: []string{"probe"},
		ExtraHosts: []string{
			"git.example.internal:172.28.0.4",
			"registry.uds.svc.cluster.local:172.28.0.4",
		},
		Env: map[string]string{"CONDUIT_PROBE": `{"dials":[]}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--network cws-in-abc",
		"--user 65534:65534",
		"--read-only",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--add-host git.example.internal:172.28.0.4",
		"--add-host registry.uds.svc.cluster.local:172.28.0.4",
		"workspace-boundary:local probe",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %s", want, joined)
		}
	}
	for _, banned := range []string{"docker.sock", "kubeconfig", "KUBECONFIG", "--privileged", "--network host", "--network=host", "/.kube"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("args contain %q: %s", banned, joined)
		}
	}
}

func TestContainerRunArgsRejectOpenNetworksSocketsAndCredentials(t *testing.T) {
	base := LaunchRequest{Image: "workspace-boundary:local", Name: "ws", Command: []string{"probe"}}
	for _, network := range []string{"", "host", "bridge", "default", "none"} {
		request := base
		request.Network = network
		if _, err := ContainerRunArgs(request); err == nil {
			t.Fatalf("network %q was accepted", network)
		}
	}
	for _, mount := range []string{"/var/run/docker.sock:/var/run/docker.sock", "/home/user/.kube:/root/.kube", "kubeconfig:/kubeconfig"} {
		request := base
		request.Network = "cws-in-abc"
		request.Mounts = []string{mount}
		if _, err := ContainerRunArgs(request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("mount %q: %v", mount, err)
		}
	}
	request := base
	request.Network = "cws-in-abc"
	request.Env = map[string]string{"KUBECONFIG": "/secret/kubeconfig"}
	if _, err := ContainerRunArgs(request); !errors.Is(err, ErrInvalid) {
		t.Fatalf("credential env: %v", err)
	}
	request.Env = nil
	request.Command = []string{"kubectl", "apply", "-f", "networkpolicy.yaml"}
	if _, err := ContainerRunArgs(request); !errors.Is(err, ErrForbidden) {
		t.Fatalf("policy edit command: %v", err)
	}
	request.Command = []string{"probe"}
	request.ExtraHosts = []string{"*:0.0.0.0"}
	if _, err := ContainerRunArgs(request); err == nil {
		t.Fatal("wildcard extra host was accepted")
	}
}
