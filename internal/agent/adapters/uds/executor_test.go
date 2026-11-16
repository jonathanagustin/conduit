package uds_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/agent/adapters/uds"
	"github.com/jonathanagustin/conduit/internal/controlplane"
)

type fakeRunner struct {
	calls     [][]string
	packageCR string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	if name == "kubectl" && len(args) >= 2 && args[0] == "get" && args[1] == "package.uds.dev" {
		return []byte(f.packageCR), nil
	}
	return []byte("ok"), nil
}

func TestUDSExecutorAppliesAndObservesReadyPackage(t *testing.T) {
	t.Parallel()
	digest := "sha256:" + strings.Repeat("c", 64)
	runner := &fakeRunner{
		packageCR: `apiVersion: uds.dev/v1alpha1
kind: Package
metadata:
  name: excalidraw
  namespace: excalidraw
  generation: 2
  annotations:
    conduit.delivery/source-digest: ` + digest + `
status:
  phase: Ready
  observedGeneration: 2
  endpoints:
    - excalidraw.uds.dev
`,
	}
	exec := uds.Executor{
		Runner:              runner,
		PackageManifestPath: "/tmp/uds-package.yaml",
	}
	step := controlplane.PlanStep{
		InstallationID: "excalidraw",
		Product:        "ghcr.io/example/excalidraw",
		Version:        "0.1.0",
		ReleaseDigest:  digest,
		Namespace:      "excalidraw",
		ConfigDigest:   "sha256:" + strings.Repeat("d", 64),
	}
	receipt, err := exec.Apply(context.Background(), controlplane.Plan{}, step)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if receipt.Step.InstallationID != "excalidraw" {
		t.Fatalf("receipt = %#v", receipt)
	}
	if len(runner.calls) != 2 || runner.calls[0][0] != "zarf" || runner.calls[1][0] != "kubectl" {
		t.Fatalf("unexpected command calls: %#v", runner.calls)
	}

	entity, err := exec.Observe(context.Background(), step)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if entity.Health != controlplane.HealthHealthy || entity.RolloutStatus != controlplane.RolloutUpToDate || entity.ReleaseDigest != digest {
		t.Fatalf("unexpected reported entity: %#v", entity)
	}

	if err := exec.Rollback(context.Background(), receipt); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
}
