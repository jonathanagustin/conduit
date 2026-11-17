package container

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectRequiresExactlyTheDedicatedInternalNetwork(t *testing.T) {
	for _, test := range []struct {
		name, mode string
		networks   []string
		internal   bool
		want       bool
	}{
		{name: "Docker named network", mode: "cws-in-fixture", networks: []string{"cws-in-fixture"}, internal: true, want: true},
		{name: "Podman bridge driver", mode: "bridge", networks: []string{"cws-in-fixture"}, internal: true, want: true},
		{name: "ordinary bridge", mode: "bridge", networks: []string{"podman"}, internal: true},
		{name: "additional upstream network", mode: "bridge", networks: []string{"cws-in-fixture", "upstream"}, internal: true},
		{name: "named network with egress", mode: "bridge", networks: []string{"cws-in-fixture"}},
		{name: "missing membership", mode: "cws-in-fixture", internal: true},
		{name: "host mode contradiction", mode: "host", networks: []string{"cws-in-fixture"}, internal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			members := map[string]any{}
			for _, network := range test.networks {
				members[network] = map[string]any{"IPAddress": "10.0.0.2"}
			}
			raw, err := json.Marshal([]any{map[string]any{
				"HostConfig":      map[string]any{"NetworkMode": test.mode},
				"NetworkSettings": map[string]any{"Networks": members},
			}})
			if err != nil {
				t.Fatal(err)
			}
			containerPath := filepath.Join(dir, "container.json")
			networkPath := filepath.Join(dir, "network.json")
			if err := os.WriteFile(containerPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			// Podman spells this property "internal", Docker "Internal".
			raw, err = json.Marshal([]any{map[string]any{"internal": test.internal}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(networkPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("CONDUIT_INSPECT_CONTAINER", containerPath)
			t.Setenv("CONDUIT_INSPECT_NETWORK", networkPath)
			bin := filepath.Join(dir, "engine")
			if err := os.WriteFile(bin, []byte("#!/bin/sh\nif [ \"$1\" = inspect ]; then cat \"$CONDUIT_INSPECT_CONTAINER\"; else cat \"$CONDUIT_INSPECT_NETWORK\"; fi\n"), 0o700); err != nil {
				t.Fatal(err)
			}
			box := &sandbox{cli: dockerCLI{bin: bin}, internalNet: "cws-in-fixture"}
			observed, err := box.inspect(context.Background(), "workspace")
			if err != nil {
				t.Fatal(err)
			}
			if observed.NetworkInternal != test.want || observed.NetworkMode != test.mode {
				t.Fatalf("observed mode=%q networks=%v internal=%v; want mode=%q internal=%v", observed.NetworkMode, observed.Networks, observed.NetworkInternal, test.mode, test.want)
			}
			if len(test.networks) != 0 && !reflect.DeepEqual(observed.Networks, test.networks) {
				t.Fatalf("observed memberships=%v, want %v", observed.Networks, test.networks)
			}
		})
	}
}
