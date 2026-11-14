package workspacegit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitWritesOnlyGivenFilesOnABranch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "secret.pem"), []byte("nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := Prepare(context.Background(), dir, "", "", "workspace/demo")
	if err != nil {
		t.Fatal(err)
	}
	// empty repo needs an initial commit before some git versions allow checkout -B after init
	if snap.Branch != "workspace/demo" {
		t.Fatalf("branch = %q", snap.Branch)
	}
	repo := Repo{Dir: dir, Branch: snap.Branch}
	sha, err := repo.Commit(context.Background(), map[string]string{
		"Dockerfile":       "FROM scratch\nUSER 65532\n",
		"zarf.yaml":        "apiVersion: zarf.dev/v1alpha1\n",
		"uds-package.yaml": "apiVersion: uds.dev/v1alpha1\nkind: Package\n",
		".dockerignore":    ".git\n",
	}, "add package files")
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 40 {
		t.Fatalf("sha = %q", sha)
	}
	out, err := gitOut(context.Background(), dir, "show", "--name-only", "--pretty=format:", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Fields(out)
	joined := strings.Join(names, ",")
	for _, want := range []string{"Dockerfile", "zarf.yaml", "uds-package.yaml", ".dockerignore"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("commit files = %q", out)
		}
	}
	if strings.Contains(joined, "secret.pem") || strings.Contains(joined, "README.md") {
		t.Fatalf("committed extra files: %q", out)
	}
	branch, err := gitOut(context.Background(), dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || strings.TrimSpace(branch) != "workspace/demo" {
		t.Fatalf("head branch = %q %v", branch, err)
	}
	if _, err := repo.Commit(context.Background(), map[string]string{"id_rsa": "secret"}, "no"); err == nil {
		t.Fatal("expected secret path to be rejected")
	}
}

func TestPrepareRejectsPathEscapeOnCommit(t *testing.T) {
	dir := t.TempDir()
	if _, err := Prepare(context.Background(), dir, "", "", "workspace/demo"); err != nil {
		t.Fatal(err)
	}
	repo := Repo{Dir: dir, Branch: "workspace/demo"}
	if _, err := repo.Commit(context.Background(), map[string]string{"../outside": "x"}, "no"); err == nil {
		t.Fatal("expected path escape to fail")
	}
}
