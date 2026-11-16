package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/container"
)

// requireWorkspaceBoundary runs the live container probe when the operator
// selected the container runtime. Packaging does not continue on the host if
// that probe cannot show the allowlist, the denials, and the missing credentials.
func requireWorkspaceBoundary(ctx context.Context) error {
	if os.Getenv("DEVWORKSPACE_RUNTIME") != "container" {
		return nil
	}
	if _, err := container.ProveFromEnv(ctx, os.Getenv); err != nil {
		return fmt.Errorf("workspace container boundary: %w", err)
	}
	return nil
}
