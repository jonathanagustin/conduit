package main

import (
	"context"
	"strings"
	"testing"
)

func TestContainerRuntimeRequiresGitHostBeforeCompute(t *testing.T) {
	t.Setenv("DEVWORKSPACE_RUNTIME", "container")
	t.Setenv("DEVWORKSPACE_GIT_HOST", "")
	err := requireWorkspaceBoundary(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gitHost") {
		t.Fatalf("unconfigured container runtime: %v", err)
	}
}

func TestHostPreviewComputeDoesNotRequireContainerBoundary(t *testing.T) {
	t.Setenv("DEVWORKSPACE_RUNTIME", "")
	if err := requireWorkspaceBoundary(context.Background()); err != nil {
		t.Fatal(err)
	}
}
