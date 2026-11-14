package packagingtools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectRepoDetectsAStaticJavascriptSite(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package.json", `{
  "name": "static-web",
  "scripts": {"build": "vite build", "preview": "vite preview --port 8080"},
  "devDependencies": {"vite": "5.0.0"}
}`)
	writeFile(t, root, "index.html", "<!doctype html><title>site</title>")
	writeFile(t, root, "src/main.js", "console.log('ok')")
	writeFile(t, root, "node_modules/vite/index.js", "module.exports = {}")
	got, err := New(WithRepoRoot(root)).InspectRepo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Language != "javascript" || got.PackageManager != "npm" || got.Kind != "static" {
		t.Fatalf("inspect = %#v", got)
	}
	if got.StartCommand != "npm run preview" {
		t.Fatalf("start = %q", got.StartCommand)
	}
	if got.HasDockerfile {
		t.Fatal("fixture should not ship a Dockerfile")
	}
	joined := strings.Join(got.Tree, ",")
	if !strings.Contains(joined, "package.json") || !strings.Contains(joined, "src/main.js") {
		t.Fatalf("tree = %#v", got.Tree)
	}
	if !strings.Contains(joined, "node_modules/") {
		t.Fatalf("expected skipped node_modules dir, tree = %#v", got.Tree)
	}
	for _, entry := range got.Tree {
		if strings.HasPrefix(entry, "node_modules/") && entry != "node_modules/" {
			t.Fatalf("walked into node_modules: %s", entry)
		}
	}
}

func TestReadFileStaysInsideTheBoundRepo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "README.md", "# site\n")
	service := New(WithRepoRoot(root))
	got, err := service.ReadFile(context.Background(), "README.md")
	if err != nil || got.Content != "# site\n" || got.Truncated || len(got.SHA256) != 64 {
		t.Fatalf("read = %#v %v", got, err)
	}
	if _, err := service.ReadFile(context.Background(), "../secret"); err == nil {
		t.Fatal("expected path escape to fail")
	}
	if _, err := service.ReadFile(context.Background(), "/etc/passwd"); err == nil {
		t.Fatal("expected absolute path to fail")
	}
	if _, err := New().ReadFile(context.Background(), "README.md"); err == nil {
		t.Fatal("expected unbound root to fail")
	}
}

func TestReadFileCapsSizeAndRejectsBinary(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("a", maxRepoFileBytes+32)
	writeFile(t, root, "big.txt", big)
	writeFile(t, root, "blob.bin", "a\x00b")
	service := New(WithRepoRoot(root))
	got, err := service.ReadFile(context.Background(), "big.txt")
	if err != nil || !got.Truncated || len(got.Content) != maxRepoFileBytes {
		t.Fatalf("truncated = %#v %v", got, err)
	}
	if _, err := service.ReadFile(context.Background(), "blob.bin"); err == nil {
		t.Fatal("expected binary file to fail")
	}
}

func writeFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
