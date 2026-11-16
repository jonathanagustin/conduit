package packagingtools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jonathanagustin/conduit/internal/packagingworkflow/domain"
	"github.com/jonathanagustin/conduit/internal/packagingworkflow/fixtures"
)

func TestZarfLintAndCreateOnARenderedPackage(t *testing.T) {
	bin := os.Getenv("ZARF_BIN")
	if bin == "" {
		if path, err := LookZarf(); err == nil {
			bin = path
		} else {
			t.Skip("zarf is not installed")
		}
	}
	source := []byte("name: static-web\nmemory: 128Mi\n")
	files, err := fixtures.Render(source, domain.DigestBytes(source), fixtures.TemplateVersion, fixtures.ToolVersions)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zarf.yaml"), files.Zarf, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "uds-package.yaml"), files.CR, 0o644); err != nil {
		t.Fatal(err)
	}
	runner := ZarfRunner{Binary: bin}
	if err := runner.Lint(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	archive, err := runner.Create(context.Background(), dir, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(archive)
	if err != nil || info.Size() == 0 {
		t.Fatalf("archive = %s %v", archive, err)
	}
}

func TestFilteredZarfEnvDropsClusterCredentials(t *testing.T) {
	t.Setenv("KUBECONFIG", "/secret/kubeconfig")
	t.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")
	t.Setenv("KUBE_TOKEN", "nope")
	for _, entry := range filteredZarfEnv() {
		if entry == "KUBECONFIG=/secret/kubeconfig" || entry == "KUBERNETES_SERVICE_HOST=10.0.0.1" || entry == "KUBE_TOKEN=nope" {
			t.Fatalf("leaked cluster credential %q", entry)
		}
	}
}

func TestLookZarfDoesNotSelectAnUnrequestedDeveloperInstall(t *testing.T) {
	home := t.TempDir()
	installed := filepath.Join(home, ".local", "share", "mise", "installs", "zarf", "1.0.0", "zarf")
	if err := os.MkdirAll(filepath.Dir(installed), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())
	t.Setenv("ZARF_BIN", "")
	if binary, err := LookZarf(); err == nil || binary != "" {
		t.Fatalf("implicitly selected developer install %q: %v", binary, err)
	}
}

func TestLookZarfPreservesExplicitSelectionAndFailsClosedOnBadOverride(t *testing.T) {
	dir := t.TempDir()
	selected := filepath.Join(dir, "zarf")
	if err := os.WriteFile(selected, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("ZARF_BIN", "")
	if binary, err := LookZarf(); err != nil || binary != selected {
		t.Fatalf("PATH selection=%q err=%v", binary, err)
	}
	override := filepath.Join(t.TempDir(), "qualified-zarf")
	if err := os.WriteFile(override, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZARF_BIN", override)
	if binary, err := LookZarf(); err != nil || binary != override {
		t.Fatalf("override=%q err=%v", binary, err)
	}
	t.Setenv("ZARF_BIN", filepath.Join(dir, "missing"))
	if binary, err := LookZarf(); err == nil || binary != "" {
		t.Fatalf("invalid override fell through to PATH: %q err=%v", binary, err)
	}
}
