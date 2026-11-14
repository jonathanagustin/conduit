package repoclone

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestClonePinsCommitAndAllowsOnlyTheGitHost(t *testing.T) {
	t.Parallel()
	fixture, sha := makeFixture(t)
	server := serveGit(t, fixture)
	otherHits := atomic.Int32{}
	other := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		otherHits.Add(1)
		http.Error(writer, "other host", http.StatusForbidden)
	}))
	t.Cleanup(other.Close)

	got, cleanup, err := Clone(context.Background(), server.URL+"/src.git", "main")
	defer cleanup()
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA != sha {
		t.Fatalf("sha = %q, want %q", got.SHA, sha)
	}
	body, err := os.ReadFile(filepath.Join(got.Dir, "README.md"))
	if err != nil || strings.TrimSpace(string(body)) != "fixture readme" {
		t.Fatalf("worktree = %q %v", body, err)
	}

	_, blockedCleanup, err := Clone(context.Background(), other.URL+"/src.git", "main")
	blockedCleanup()
	if err == nil {
		t.Fatal("clone of a host with no repository succeeded")
	}

	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, other.URL+request.URL.RequestURI(), http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	before := otherHits.Load()
	_, redirectCleanup, err := Clone(context.Background(), redirect.URL+"/src.git", "main")
	redirectCleanup()
	if err == nil {
		t.Fatal("clone followed a redirect to another host")
	}
	if otherHits.Load() != before {
		t.Fatalf("other host was contacted %d times", otherHits.Load()-before)
	}

	bySHA, shaCleanup, err := Clone(context.Background(), server.URL+"/src.git", sha)
	defer shaCleanup()
	if err != nil {
		t.Fatal(err)
	}
	if bySHA.SHA != sha {
		t.Fatalf("sha ref = %q, want %q", bySHA.SHA, sha)
	}
}

func TestCloneRefusesCredentialsAndNonHTTP(t *testing.T) {
	t.Parallel()
	_, cleanup, err := Clone(context.Background(), "https://user:pass@example.com/repo", "main")
	cleanup()
	if err == nil {
		t.Fatal("accepted credentials in the URL")
	}
	_, cleanup, err = Clone(context.Background(), "file:///tmp/repo", "main")
	cleanup()
	if err == nil {
		t.Fatal("accepted a file URL")
	}
}

func makeFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("README.md", "fixture readme\n")
	write("package.json", `{"name":"fixture","scripts":{"start":"node server.js"}}`+"\n")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = src
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run("init", "--quiet", "-b", "main")
	run("config", "user.name", "fixture")
	run("config", "user.email", "fixture@local")
	run("add", ".")
	run("commit", "--quiet", "-m", "fixture")
	sha := strings.TrimSpace(run("rev-parse", "HEAD"))
	bare := filepath.Join(dir, "src.git")
	cmd := exec.Command("git", "clone", "--quiet", "--bare", src, bare)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bare clone: %v\n%s", err, out)
	}
	if err := exec.Command("git", "--git-dir", bare, "update-server-info").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "--git-dir", bare, "config", "http.uploadpack", "true").Run(); err != nil {
		t.Fatal(err)
	}
	return dir, sha
}

func serveGit(t *testing.T, projectRoot string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body bytes.Buffer
		_, _ = io.Copy(&body, request.Body)
		cmd := exec.Command("git", "http-backend")
		cmd.Env = append(os.Environ(),
			"GIT_PROJECT_ROOT="+projectRoot,
			"GIT_HTTP_EXPORT_ALL=1",
			"PATH_INFO="+request.URL.Path,
			"PATH_TRANSLATED="+filepath.Join(projectRoot, request.URL.Path),
			"QUERY_STRING="+request.URL.RawQuery,
			"REQUEST_METHOD="+request.Method,
			"CONTENT_TYPE="+request.Header.Get("Content-Type"),
			"CONTENT_LENGTH="+strconv.Itoa(body.Len()),
		)
		cmd.Stdin = &body
		var stdout bytes.Buffer
		var stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			http.Error(writer, stderr.String(), http.StatusInternalServerError)
			return
		}
		out := stdout.Bytes()
		header, rest, found := bytes.Cut(out, []byte("\r\n\r\n"))
		if !found {
			header, rest, found = bytes.Cut(out, []byte("\n\n"))
		}
		if !found {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(out)
			return
		}
		status := http.StatusOK
		for _, line := range strings.Split(string(header), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			key, value, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if strings.EqualFold(key, "Status") {
				if code, _, _ := strings.Cut(value, " "); code != "" {
					if parsed, err := strconv.Atoi(code); err == nil {
						status = parsed
					}
				}
				continue
			}
			writer.Header().Add(key, value)
		}
		writer.WriteHeader(status)
		_, _ = writer.Write(rest)
	}))
	t.Cleanup(server.Close)
	return server
}
