package gitstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

func ingestionOptions(source, dest, remote string) BootstrapOptions {
	return BootstrapOptions{WorkspaceID: "wsp_safe_ingest", SourceRepo: source, Ref: "main", DestDir: dest, LocalRemoteDir: remote, LocalOnly: true, Branch: "workspace/operator/safe"}
}
func requireFile(t *testing.T, path, content string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != content {
		t.Fatalf("file changed: %s content=%q err=%v", path, raw, err)
	}
}
func requireNoStaging(t *testing.T, dest string) {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".bootstrap-*"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging remains: %v %v", entries, err)
	}
}

func TestBootstrapIngestionPreservesPopulatedDestinationBeforeFetching(t *testing.T) {
	dest := bootstrapTestRepo(t)
	head, _ := gitOut(t.Context(), dest, "rev-parse", "HEAD")
	marker := filepath.Join(dest, "uncommitted.txt")
	if err := os.WriteFile(marker, []byte("operator work"), 0600); err != nil {
		t.Fatal(err)
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++; w.WriteHeader(503) }))
	defer server.Close()
	remote := filepath.Join(t.TempDir(), "remote.git")
	_, err := BootstrapSource(t.Context(), ingestionOptions(server.URL+"/source.git", dest, remote))
	if !errors.Is(err, domain.ErrConflict) || requests != 0 {
		t.Fatalf("failure=%v fetched=%d", err, requests)
	}
	requireFile(t, marker, "operator work")
	actual, _ := gitOut(t.Context(), dest, "rev-parse", "HEAD")
	if actual != head {
		t.Fatal("existing Git head changed")
	}
	if _, err := os.Stat(remote); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote created: %v", err)
	}
	requireNoStaging(t, dest)
}

func TestBootstrapIngestionRejectsOverlappingAndAliasPaths(t *testing.T) {
	for _, relation := range []string{"same", "inside source", "contains source", "destination symlink", "parent symlink", "source symlink"} {
		t.Run(relation, func(t *testing.T) {
			parent := t.TempDir()
			source := filepath.Join(parent, "source")
			if err := os.Mkdir(source, 0700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(source, "valuable")
			if err := os.WriteFile(marker, []byte("keep source"), 0600); err != nil {
				t.Fatal(err)
			}
			dest := source
			switch relation {
			case "inside source":
				dest = filepath.Join(source, "checkout")
			case "contains source":
				dest = parent
			case "destination symlink":
				dest = filepath.Join(parent, "alias")
				if err := os.Symlink(source, dest); err != nil {
					t.Fatal(err)
				}
			case "parent symlink":
				alias := filepath.Join(parent, "alias")
				if err := os.Symlink(source, alias); err != nil {
					t.Fatal(err)
				}
				dest = filepath.Join(alias, "checkout")
			case "source symlink":
				alias := filepath.Join(parent, "alias")
				if err := os.Symlink(source, alias); err != nil {
					t.Fatal(err)
				}
				source = alias
				dest = filepath.Join(parent, "source", "checkout")
			}
			_, err := BootstrapSource(t.Context(), ingestionOptions(source, dest, filepath.Join(t.TempDir(), "remote.git")))
			if err == nil {
				t.Fatal("accepted overlapping paths")
			}
			requireFile(t, marker, "keep source")
			if relation == "inside source" || relation == "parent symlink" || relation == "source symlink" {
				if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("destination appeared: %v", err)
				}
			}
		})
	}
}

func TestBootstrapIngestionFailedClonePreservesEmptyDestination(t *testing.T) {
	for _, cancelRequest := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelRequest), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if cancelRequest {
					cancel()
				}
				w.WriteHeader(503)
			}))
			defer server.Close()
			dest := filepath.Join(t.TempDir(), "checkout")
			if err := os.Mkdir(dest, 0700); err != nil {
				t.Fatal(err)
			}
			remote := filepath.Join(t.TempDir(), "remote.git")
			_, err := BootstrapSource(ctx, ingestionOptions(server.URL+"/source.git", dest, remote))
			if err == nil {
				t.Fatal("failed clone was accepted")
			}
			if cancelRequest && !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation lost: %v", err)
			}
			entries, readErr := os.ReadDir(dest)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("destination changed: %v %v", entries, readErr)
			}
			info, _ := os.Stat(dest)
			if info.Mode().Perm() != 0700 {
				t.Fatal("destination permissions changed")
			}
			if _, err := os.Stat(remote); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("remote created: %v", err)
			}
			requireNoStaging(t, dest)
		})
	}
}

func TestBootstrapIngestionResolvesRequestedRefInsteadOfDefault(t *testing.T) {
	source := bootstrapTestRepo(t)
	mainHead, _ := gitOut(t.Context(), source, "rev-parse", "HEAD")
	if err := runGit(t.Context(), source, "checkout", "-b", "requested"); err != nil {
		t.Fatal(err)
	}
	selected := bootstrapTestCommit(t, source, "requested contents")
	if err := runGit(t.Context(), source, "tag", "release-ref"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(t.Context(), source, "checkout", "-b", "develop", "main"); err != nil {
		t.Fatal(err)
	}
	defaultHead := bootstrapTestCommit(t, source, "different default branch")
	for _, test := range []struct{ ref, head string }{{"main", strings.TrimSpace(mainHead)}, {"requested", selected}, {"release-ref", selected}, {"refs/heads/requested", selected}, {"refs/tags/release-ref", selected}, {strings.TrimSpace(mainHead), strings.TrimSpace(mainHead)}, {"HEAD", defaultHead}} {
		t.Run(test.ref, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "checkout")
			remote := filepath.Join(t.TempDir(), "remote.git")
			options := ingestionOptions(source, dest, remote)
			options.Ref = test.ref
			result, err := BootstrapSource(t.Context(), options)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := gitOut(t.Context(), dest, "rev-parse", "HEAD")
			if err != nil || strings.TrimSpace(actual) != test.head || result.BaseCommit != test.head || result.HeadCommit != test.head {
				t.Fatalf("ref %s head=%q result=%+v err=%v", test.ref, actual, result, err)
			}
			published, err := gitOut(t.Context(), remote, "rev-parse", "refs/heads/main")
			if err != nil || strings.TrimSpace(published) != test.head {
				t.Fatalf("wrong published head: %s %v", published, err)
			}
			requireNoStaging(t, dest)
		})
	}
	actual, _ := gitOut(t.Context(), source, "rev-parse", "HEAD")
	if strings.TrimSpace(actual) != defaultHead {
		t.Fatal("source repository was changed")
	}
}

func TestBootstrapIngestionMissingRefDoesNotPublishOrPromote(t *testing.T) {
	source := bootstrapTestRepo(t)
	dest := filepath.Join(t.TempDir(), "checkout")
	remote := filepath.Join(t.TempDir(), "remote.git")
	options := ingestionOptions(source, dest, remote)
	options.Ref = "does-not-exist"
	apiCalls := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { apiCalls++; w.WriteHeader(503) }))
	defer api.Close()
	options.LocalOnly = false
	options.CreateGitHubRepo = true
	options.GitHubToken = "fixture-token"
	options.GitHubAPIBaseURL = api.URL
	options.GitHubRepo = "owner/new-repo"

	_, err := BootstrapSource(t.Context(), options)
	if !errors.Is(err, domain.ErrNotFound) || apiCalls != 0 {
		t.Fatalf("missing ref=%v API calls=%d", err, apiCalls)
	}
	for _, path := range []string{dest, remote} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("created %s: %v", path, err)
		}
	}
	requireNoStaging(t, dest)
}

func TestBootstrapIngestionPreservesCompetingDestinationWriter(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "app.txt"), []byte("source"), 0600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "checkout")
	if err := os.Mkdir(dest, 0700); err != nil {
		t.Fatal(err)
	}
	hooks := t.TempDir()
	hook := `#!/bin/sh
printf '%s' 'competing work' > "$CONDUIT_TEST_INGEST_DEST/sentinel"
`
	if err := os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte(hook), 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(config, []byte(fmt.Sprintf("[core]\n hooksPath = %q\n[commit]\n gpgsign = false\n", hooks)), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("CONDUIT_TEST_INGEST_DEST", dest)
	remote := filepath.Join(t.TempDir(), "remote.git")
	_, err := BootstrapSource(t.Context(), ingestionOptions(source, dest, remote))
	if !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("competing writer=%v", err)
	}
	requireFile(t, filepath.Join(dest, "sentinel"), "competing work")
	if _, err := os.Stat(remote); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("remote created after conflict: %v", err)
	}
	requireNoStaging(t, dest)
}

func TestBootstrapPlainDirectoryKeepsExecutablesAndRejectsUnknownRef(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "run.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "checkout")
	remote := filepath.Join(t.TempDir(), "remote.git")
	_, err := BootstrapSource(t.Context(), ingestionOptions(source, dest, remote))
	if err != nil {
		t.Fatal(err)
	}
	tree, err := gitOut(t.Context(), dest, "ls-tree", "HEAD", "run.sh")
	if err != nil || !strings.HasPrefix(tree, "100755 ") {
		t.Fatalf("executable mode lost: %q %v", tree, err)
	}
	options := ingestionOptions(source, filepath.Join(t.TempDir(), "checkout"), filepath.Join(t.TempDir(), "remote.git"))
	options.Ref = "no-directory-history"
	if _, err := BootstrapSource(t.Context(), options); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("invented directory ref: %v", err)
	}
}

func TestBootstrapRejectsInvalidIdentityBeforeFilesystemMutation(t *testing.T) {
	source := t.TempDir()
	dest := filepath.Join(t.TempDir(), "checkout")
	options := ingestionOptions(source, dest, filepath.Join(t.TempDir(), "remote.git"))
	options.WorkspaceID = "../outside"
	if _, err := BootstrapSource(t.Context(), options); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("unsafe workspace ID: %v", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination created: %v", err)
	}
}

func TestBootstrapEmptyDirectoryGetsAnInitialCommitWithoutLosingSource(t *testing.T) {
	source := t.TempDir()
	if err := os.Mkdir(filepath.Join(source, "empty-subdirectory"), 0700); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "checkout")
	result, err := BootstrapSource(t.Context(), ingestionOptions(source, dest, filepath.Join(t.TempDir(), "remote.git")))
	if err != nil || result.BaseCommit == "" {
		t.Fatalf("empty source: %+v %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(source, "empty-subdirectory")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "README.md")); err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapIngestionPreCancelledRequestDoesNotTouchDestination(t *testing.T) {
	source := t.TempDir()
	dest := filepath.Join(t.TempDir(), "checkout")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := BootstrapSource(ctx, ingestionOptions(source, dest, filepath.Join(t.TempDir(), "remote.git"))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if _, err := os.Stat(dest); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("destination created: %v", err)
	}
}
