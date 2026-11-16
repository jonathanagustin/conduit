package gitstore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
)

func TestBootstrapSourceFromLocalDirectoryCreatesGitHubRepoAndPushesBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// 1. Prepare a plain local source directory on the filesystem (no .git yet).
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "package.json"), []byte(`{"name":"excalidraw","scripts":{"start":"vite"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("# Excalidraw\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, ".env.secret"), []byte("DO_NOT_COMMIT=1"), 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Stand up a mock GitHub REST API server recording repo creation and Git Data API pushes.
	var (
		mu          sync.Mutex
		createdRepo string
		pushedRefs  = map[string]string{}
		blobs       = map[string]string{}
	)
	ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/info/refs"):
			w.WriteHeader(http.StatusServiceUnavailable)
		case r.Method == http.MethodGet && r.URL.Path == "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "operator"})
		case r.Method == http.MethodPost && r.URL.Path == "/user/repos":
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			createdRepo = "operator/" + body.Name
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name":      body.Name,
				"full_name": createdRepo,
				"html_url":  "http://" + r.Host + "/" + createdRepo,
				"clone_url": "http://" + r.Host + "/" + createdRepo + ".git",
			})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/blobs"):
			var body struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			sha := fmt.Sprintf("%040x", len(blobs)+1)
			blobs[sha] = body.Content
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": sha})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/trees"):
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": strings.Repeat("b", 40)})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/commits"):
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": strings.Repeat("c", 40)})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/git/ref/heads/"):
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "Not Found"})
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/git/refs/heads/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/git/refs"):
			var body struct {
				Ref string `json:"ref"`
				SHA string `json:"sha"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			pushedRefs[body.Ref] = body.SHA
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"ref": body.Ref, "object": map[string]string{"sha": body.SHA, "type": "commit"}})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ghServer.Close()

	destDir := filepath.Join(t.TempDir(), "repo")
	res, err := gitstore.BootstrapSource(ctx, gitstore.BootstrapOptions{
		WorkspaceID:      "wsp_excalidraw",
		SourceRepo:       srcDir,
		Ref:              "main",
		Branch:           "workspace/operator/wsp_excalidraw",
		DestDir:          destDir,
		CreateGitHubRepo: true,
		GitHubRepo:       "excalidraw-packaged",
		GitHubToken:      "test-pat-token",
		GitHubAPIBaseURL: ghServer.URL,
	})
	if err != nil {
		t.Fatalf("BootstrapSource: %v", err)
	}
	if !res.CreatedGitHub || res.RepoURL != ghServer.URL+"/operator/excalidraw-packaged" {
		t.Fatalf("expected created GitHub repo URL, got %+v", res)
	}
	if res.BaseCommit == "" {
		t.Fatalf("expected non-empty BaseCommit")
	}
	// Secret file in source directory must not be copied into /repo.
	if _, err := os.Stat(filepath.Join(destDir, ".env.secret")); !os.IsNotExist(err) {
		t.Fatalf("expected .env.secret to be excluded from bootstrapped repo")
	}

	mu.Lock()
	defer mu.Unlock()
	if createdRepo != "operator/excalidraw-packaged" {
		t.Fatalf("createdRepo = %q, want operator/excalidraw-packaged", createdRepo)
	}
	if pushedRefs["refs/heads/main"] == "" || pushedRefs["refs/heads/workspace/operator/wsp_excalidraw"] == "" {
		t.Fatalf("expected both main and workspace branch refs pushed to GitHub, got %+v", pushedRefs)
	}
}
