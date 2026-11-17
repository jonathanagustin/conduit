package packagingtools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
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

func TestReadFileCanonicalizesAliasedRepositoryRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "owned")
	const raw = "# guide\npassword=hunter2hunter2\n"
	writeFile(t, root, "docs/guide.md", raw)
	writeFile(t, root, ".env", "API_TOKEN=fixture\n")
	writeFile(t, parent, "outside.txt", "outside repository\n")
	alias := filepath.Join(parent, "checkout-alias")
	for name, target := range map[string]string{
		alias:                               root,
		filepath.Join(root, "README.md"):    filepath.Join("docs", "guide.md"),
		filepath.Join(root, "outside-link"): filepath.Join(parent, "outside.txt"),
		filepath.Join(root, "secret-link"):  ".env",
	} {
		if err := os.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte(raw))
	for name, checkout := range map[string]string{"direct": root, "alias": alias} {
		t.Run(name, func(t *testing.T) {
			service := New(WithRepoRoot(checkout))
			if !service.RepositoryBound() {
				t.Fatal("existing checkout must remain bound")
			}
			for _, requested := range []string{"docs/guide.md", "README.md"} {
				got, err := service.ReadFile(t.Context(), requested)
				if err != nil {
					t.Fatal(err)
				}
				if got.Path != "docs/guide.md" || got.Content != "# guide\npassword=[REDACTED]\n" || got.SHA256 != hex.EncodeToString(sum[:]) || got.Truncated {
					t.Fatalf("%s: canonical path/raw digest/redaction mismatch: %#v", requested, got)
				}
			}
			for _, requested := range []string{"../outside.txt", "outside-link"} {
				if _, err := service.ReadFile(t.Context(), requested); err == nil {
					t.Fatalf("escape accepted: %s", requested)
				}
			}
			if _, err := service.ReadFile(t.Context(), "secret-link"); !errors.Is(err, ErrSecretPath) {
				t.Fatalf("secret symlink accepted: %v", err)
			}
		})
	}
}
