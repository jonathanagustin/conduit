// Package cluster applies a least-privilege pod when kubectl can reach a cluster.
package cluster

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/devworkspace/ports"
)

// Provisioner applies rendered pod documents. Available reports whether a
// cluster answered; otherwise Create returns domain.ErrNoCluster.
type Provisioner struct {
	kubectl string
	image   string
	egress  *domain.EgressConfig
}

// New returns a provisioner that looks for kubectl on PATH.
func New(image string) *Provisioner {
	return &Provisioner{kubectl: "kubectl", image: image}
}

// NewWithEgress returns a provisioner bound to a validated workspace.egress configuration.
func NewWithEgress(image string, egress domain.EgressConfig) (*Provisioner, error) {
	validated, err := domain.NormalizeAndValidateEgress(egress)
	if err != nil {
		return nil, err
	}
	return &Provisioner{kubectl: "kubectl", image: image, egress: &validated}, nil
}

// Available reports whether a cluster answered a readiness probe.
func (provisioner *Provisioner) Available(ctx context.Context) bool {
	probe, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(probe, provisioner.kubectl, "get", "--raw=/readyz")
	return cmd.Run() == nil
}

// Create applies the pod and its network policy.
func (provisioner *Provisioner) Create(ctx context.Context, workspace domain.Workspace) error {
	if !provisioner.Available(ctx) {
		return domain.ErrNoCluster
	}
	documents, err := domain.RenderPod(domain.PodRequest{
		Name:      podName(workspace.ID),
		Namespace: "workspaces",
		Image:     provisioner.image,
		Owner:     workspace.Owner,
		Branch:    workspace.Branch,
		Egress:    provisioner.egress,
	})
	if err != nil {
		return err
	}
	if err := provisioner.apply(ctx, documents.Pod); err != nil {
		return err
	}
	return provisioner.apply(ctx, documents.NetworkPolicy)
}

// Delete removes the pod. An unavailable cluster cannot establish absence.
func (provisioner *Provisioner) Delete(ctx context.Context, workspaceID string) error {
	if !provisioner.Available(ctx) {
		return domain.ErrNoCluster
	}
	cmd := exec.CommandContext(ctx, provisioner.kubectl, "delete", "pod", podName(workspaceID), "--namespace", "workspaces", "--ignore-not-found")
	return cmd.Run()
}

func (provisioner *Provisioner) apply(ctx context.Context, document []byte) error {
	cmd := exec.CommandContext(ctx, provisioner.kubectl, "apply", "-f", "-")
	cmd.Stdin = bytes.NewReader(document)
	return cmd.Run()
}

func podName(id string) string {
	name := "ws-" + id
	if len(name) > 63 {
		return name[:63]
	}
	return name
}

// Fallback uses primary and, when it reports no cluster, secondary.
type Fallback struct {
	Primary   ports.Provisioner
	Secondary ports.Provisioner
}

func (fallback Fallback) Create(ctx context.Context, workspace domain.Workspace) error {
	err := fallback.Primary.Create(ctx, workspace)
	if errors.Is(err, domain.ErrNoCluster) && fallback.Secondary != nil {
		return fallback.Secondary.Create(ctx, workspace)
	}
	return err
}

func (fallback Fallback) Delete(ctx context.Context, workspaceID string) error {
	// A recorded no-cluster creation does not prove a previously real pod was
	// removed. Only the primary adapter can confirm deletion or absence.
	return fallback.Primary.Delete(ctx, workspaceID)
}
