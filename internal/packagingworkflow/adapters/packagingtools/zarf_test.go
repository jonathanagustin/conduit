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
		} else if _, err := os.Stat("/home/box/.local/share/mise/installs/zarf/1.0.0/zarf"); err == nil {
			bin = "/home/box/.local/share/mise/installs/zarf/1.0.0/zarf"
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
