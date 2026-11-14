package composition

import (
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/application"
)

func TestSelectedRunIdentityUsesTheRunNotAHardcodedInstallation(t *testing.T) {
	installation, product, err := selectedRunIdentity(application.PendingChange{
		InstallationID: "excalidraw-2",
		TargetID:       "excalidraw-2:memory",
		Environment:    "dev",
	})
	if err != nil || installation != "excalidraw-2" || product != "app.example:excalidraw-2" {
		t.Fatalf("got %s %s %v", installation, product, err)
	}
}

func TestSelectedRunIdentityFallsBackToTheTargetRunID(t *testing.T) {
	installation, product, err := selectedRunIdentity(application.PendingChange{TargetID: "hello-page:memory"})
	if err != nil || installation != "hello-page" || product != "app.example:hello-page" {
		t.Fatalf("got %s %s %v", installation, product, err)
	}
}

func TestSelectedRunIdentityKeepsAnExplicitProduct(t *testing.T) {
	installation, product, err := selectedRunIdentity(application.PendingChange{
		InstallationID: "whiteboard", ProductID: "app.example:excalidraw",
	})
	if err != nil || installation != "whiteboard" || product != "app.example:excalidraw" {
		t.Fatalf("got %s %s %v", installation, product, err)
	}
}

func TestSelectedRunIdentityRefusesAMissingRun(t *testing.T) {
	if _, _, err := selectedRunIdentity(application.PendingChange{}); err == nil {
		t.Fatal("accepted a proposal with no run or installation")
	}
}
