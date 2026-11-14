package source

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureGit(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git fixture: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	run("init", "--quiet")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM example.invalid/app:1\nEXPOSE 8080\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(strings.Repeat("x", MaxFileBytes+1)), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "core.hooksPath="+t.TempDir(), "commit", "--quiet", "-m", "fixture")
	return dir, run("rev-parse", "HEAD")
}
func TestReadCommitIgnoresWorktreeAndEnvironment(t *testing.T) {
	dir, revision := fixtureGit(t)
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("changed uncommitted"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(dir, "missing"))
	s, err := ReadCommit(context.Background(), dir, revision, []string{"Dockerfile"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Revision != revision || len(s.Files) != 1 || !strings.Contains(s.Files[0].Text, "EXPOSE 8080") || !objectID.MatchString(s.Files[0].Blob) || len(s.Files[0].SHA256) != 64 {
		t.Fatalf("wrong evidence: %#v", s)
	}
}
func TestReadCommitRejectsInvalidInputs(t *testing.T) {
	dir, revision := fixtureGit(t)
	for _, names := range [][]string{nil, {"missing"}, {"../Dockerfile"}, {"*"}, {"Dockerfile", "Dockerfile"}, {"large.txt"}, {":"}, {"."}} {
		if _, err := ReadCommit(context.Background(), dir, revision, names); err == nil {
			t.Fatalf("accepted %v", names)
		}
	}
	for _, rev := range []string{"HEAD", revision[:8], strings.Repeat("a", 40), "--help"} {
		if _, err := ReadCommit(context.Background(), dir, rev, []string{"Dockerfile"}); err == nil {
			t.Fatalf("accepted revision %s", rev)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadCommit(ctx, dir, revision, []string{"Dockerfile"}); err == nil {
		t.Fatal("ignored cancellation")
	}
}

func TestReadCommitRejectsNonregularTreeEntries(t *testing.T) {
	dir, revision := fixtureGit(t)
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		b, e := c.CombinedOutput()
		if e != nil {
			t.Fatalf("git: %v %s", e, b)
		}
		return strings.TrimSpace(string(b))
	}
	blob := run("rev-parse", revision+":Dockerfile")
	run("update-index", "--add", "--cacheinfo", "120000,"+blob+",link")
	run("update-index", "--add", "--cacheinfo", "160000,"+revision+",module")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+t.TempDir(), "commit", "--quiet", "-m", "nonregular")
	rev := run("rev-parse", "HEAD")
	for _, p := range []string{"link", "module"} {
		if _, e := ReadCommit(context.Background(), dir, rev, []string{p}); e == nil {
			t.Fatalf("accepted %s", p)
		}
	}
}
func TestBoundedBufferDoesNotBypassLimit(t *testing.T) {
	b := &boundedBuffer{limit: 3}
	if _, e := io.Copy(b, strings.NewReader("1234")); e == nil {
		t.Fatal("unbounded copy")
	}
	if b.buffer.Len() > 3 {
		t.Fatal("exceeded limit")
	}
}
