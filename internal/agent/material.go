package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jonathanagustin/conduit/internal/controlplane"
)

// ExternalMaterialPreparer delegates provider-specific registry and secret
// resolution to one Environment-owned executable. Only opaque references and
// metadata are sent on stdin. Output is discarded so provider material cannot
// enter Agent logs or control-plane reports.
type ExternalMaterialPreparer struct {
	Command string
	run     func(context.Context, string, []byte) error
}

func NewExternalMaterialPreparer(command string) (*ExternalMaterialPreparer, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, errors.New("external material preparer command is required")
	}
	if !filepath.IsAbs(command) || filepath.Clean(command) != command || strings.ContainsRune(command, '\x00') {
		return nil, errors.New("external material preparer command must be a clean absolute path")
	}
	return &ExternalMaterialPreparer{Command: command, run: runMaterialCommand}, nil
}

func (preparer *ExternalMaterialPreparer) Prepare(
	ctx context.Context, access controlplane.RegistryAccess, namespace string,
) error {
	return preparer.invoke(ctx, "RegistryAccess", namespace, access)
}

func (preparer *ExternalMaterialPreparer) PrepareImages(
	ctx context.Context, images []controlplane.Artifact, access []controlplane.RegistryAccess, namespace string,
) error {
	request := struct {
		Images         []controlplane.Artifact       `json:"images"`
		RegistryAccess []controlplane.RegistryAccess `json:"registryAccess,omitempty"`
	}{Images: images, RegistryAccess: access}
	return preparer.invoke(ctx, "ImagePrePull", namespace, request)
}

func (preparer *ExternalMaterialPreparer) ApplySecretMutation(
	ctx context.Context, mutation controlplane.SecretMutation,
) error {
	return preparer.invoke(ctx, "SecretMutation", mutation.Namespace, mutation)
}

func (preparer *ExternalMaterialPreparer) RollbackSecretMutation(
	ctx context.Context, mutation controlplane.SecretMutation,
) error {
	return preparer.invoke(ctx, "SecretMutationRollback", mutation.Namespace, mutation)
}

func (preparer *ExternalMaterialPreparer) invoke(ctx context.Context, kind, namespace string, access any) error {
	if preparer == nil {
		return errors.New("external material preparer is not configured")
	}
	request := struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Namespace  string `json:"namespace"`
		Access     any    `json:"access"`
	}{APIVersion: "platform.delivery/v1alpha1", Kind: kind, Namespace: namespace, Access: access}
	payload, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("encode external material request: %w", err)
	}
	run := preparer.run
	if run == nil {
		run = runMaterialCommand
	}
	if err := run(ctx, preparer.Command, payload); err != nil {
		return fmt.Errorf("external material preparer failed: %w", err)
	}
	return nil
}

func runMaterialCommand(ctx context.Context, command string, payload []byte) error {
	process := exec.CommandContext(ctx, command)
	process.Stdin = bytes.NewReader(payload)
	process.Stdout = io.Discard
	process.Stderr = io.Discard
	return process.Run()
}
