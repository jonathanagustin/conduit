package composition

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jonathanagustin/conduit/internal/controlplane"
	"github.com/jonathanagustin/conduit/internal/devworkspace/adapters/gitstore"
	devdomain "github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/planner"
	"github.com/jonathanagustin/conduit/internal/planning/adapters/repoclone"
	planningdomain "github.com/jonathanagustin/conduit/internal/planning/domain"
	"github.com/jonathanagustin/conduit/internal/testsupport/modelhttpfixture"
)

const legacyPlaceholderSHA = "3a317b550123456789abcdef0123456789abcdef"

type testGitRemote struct {
	workDir    string
	bareDir    string
	server     *httptest.Server
	repoURL    string
	defaultSHA string
}

func newTestGitRemote(t *testing.T, repoName string, files map[string]string) *testGitRemote {
	t.Helper()
	root := t.TempDir()
	workDir := filepath.Join(root, "work")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		full := filepath.Join(workDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitIn(t, workDir, "init", "--quiet", "-b", "main")
	runGitIn(t, workDir, "config", "user.name", "fixture")
	runGitIn(t, workDir, "config", "user.email", "fixture@local")
	runGitIn(t, workDir, "add", "-A")
	runGitIn(t, workDir, "commit", "--quiet", "-m", "test: initialize source fixture")
	defaultSHA := strings.TrimSpace(runGitIn(t, workDir, "rev-parse", "HEAD"))

	projectRoot := filepath.Join(root, "git-http")
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	bareDir := filepath.Join(projectRoot, repoName+".git")
	runGitIn(t, root, "clone", "--quiet", "--bare", workDir, bareDir)
	runGitIn(t, workDir, "remote", "add", "origin", bareDir)
	refreshBareServer(t, bareDir)

	server := serveGitHTTPBackend(t, projectRoot)
	return &testGitRemote{
		workDir:    workDir,
		bareDir:    bareDir,
		server:     server,
		repoURL:    server.URL + "/" + repoName + ".git",
		defaultSHA: defaultSHA,
	}
}

func (r *testGitRemote) commitBranch(t *testing.T, branch, baseRef string, files map[string]string, remove []string, message string) string {
	t.Helper()
	runGitIn(t, r.workDir, "checkout", "--quiet", "-B", branch, baseRef)
	for _, rel := range remove {
		_ = os.Remove(filepath.Join(r.workDir, rel))
	}
	for rel, content := range files {
		full := filepath.Join(r.workDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runGitIn(t, r.workDir, "add", "-A")
	runGitIn(t, r.workDir, "commit", "--quiet", "-m", "test: "+message)
	sha := strings.TrimSpace(runGitIn(t, r.workDir, "rev-parse", "HEAD"))
	runGitIn(t, r.workDir, "push", "--quiet", "-f", "origin", "refs/heads/"+branch+":refs/heads/"+branch)
	refreshBareServer(t, r.bareDir)
	return sha
}

func refreshBareServer(t *testing.T, bareDir string) {
	t.Helper()
	runGitIn(t, bareDir, "update-server-info")
	runGitIn(t, bareDir, "config", "http.uploadpack", "true")
}

func runGitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

func serveGitHTTPBackend(t *testing.T, projectRoot string) *httptest.Server {
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
		var stdout, stderr bytes.Buffer
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

func TestPlanningCloneResolvesRequestedRefInIsolatedCheckoutAndIgnoresWorkspaces(t *testing.T) {
	remote := newTestGitRemote(t, "excalidraw", map[string]string{
		"README.md":    "# main branch\n",
		"package.json": `{"name":"excalidraw-main","version":"0.17.0","scripts":{"start":"yarn start"}}`,
		"yarn.lock":    "# yarn lockfile v1\n",
		"main-only.js": "console.log('main');\n",
	})
	mainSHA := remote.defaultSHA
	releaseSHA := remote.commitBranch(t, "release", "main", map[string]string{
		"README.md":       "# release branch\n",
		"package.json":    `{"name":"excalidraw-release","version":"0.18.0","scripts":{"start":"yarn start"}}`,
		"release-only.js": "console.log('release');\n",
	}, []string{"main-only.js"}, "prepare release branch")
	if releaseSHA == mainSHA {
		t.Fatalf("expected distinct commits, got %s", releaseSHA)
	}

	// Negative control 1: populate a gitstore workspace for the same RepoURL
	// checked out on main with dirty uncommitted files and an unindexed/short BaseCommit.
	gitRoot := t.TempDir()
	store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	ws := devdomain.Touch(devdomain.Workspace{
		ID:            "wsp_cached_main",
		ProfileID:     "onboard",
		EnvironmentID: "dev",
		Owner:         "bob",
		Title:         "Bob workspace on main",
		Status:        devdomain.StatusRunning,
		UpstreamRepo:  remote.repoURL,
		RepoURL:       remote.repoURL,
		Branch:        "workspace/bob/wsp_cached_main",
		BaseCommit:    "short",
		HeadCommit:    mainSHA,
	}, now)
	if err := store.Save(t.Context(), ws); err != nil {
		t.Fatal(err)
	}
	wsRepoDir := store.RepoDir("wsp_cached_main")
	dirtyFile := filepath.Join(wsRepoDir, "dirty-uncommitted.txt")
	if err := os.WriteFile(dirtyFile, []byte("uncommitted workspace secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	wsReadme := filepath.Join(wsRepoDir, "README.md")
	if err := os.WriteFile(wsReadme, []byte("dirty modified readme\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wsHeadBefore := strings.TrimSpace(runGitIn(t, wsRepoDir, "rev-parse", "HEAD"))
	wsBranchBefore := strings.TrimSpace(runGitIn(t, wsRepoDir, "rev-parse", "--abbrev-ref", "HEAD"))

	// Negative control 2: set CONDUIT_DEMO_SOURCE_DIR to a plain non-Git directory
	// that previously triggered the 3a317b55... placeholder SHA fallback.
	demoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(demoDir, "package.json"), []byte(`{"name":"demo-override"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONDUIT_DEMO_SOURCE_DIR", demoDir)
	t.Setenv("DEVWORKSPACE_GIT_ROOT", gitRoot)

	cloneFn := loopbackPlanningClone(t)
	checkout, cleanup, err := cloneFn(t.Context(), remote.repoURL, "release")
	if err != nil {
		t.Fatalf("hubPlanningClone(release): %v", err)
	}
	if checkout.SHA != releaseSHA {
		cleanup()
		t.Fatalf("resolved SHA = %q, want release %q (main=%q)", checkout.SHA, releaseSHA, mainSHA)
	}
	if checkout.SHA == legacyPlaceholderSHA {
		cleanup()
		t.Fatal("hubPlanningClone returned legacy placeholder SHA")
	}
	if checkout.Dir == wsRepoDir || checkout.Dir == demoDir {
		cleanup()
		t.Fatalf("checkout reused non-isolated directory %q", checkout.Dir)
	}

	pkgBytes, err := os.ReadFile(filepath.Join(checkout.Dir, "package.json"))
	if err != nil || !strings.Contains(string(pkgBytes), "excalidraw-release") {
		cleanup()
		t.Fatalf("expected release package.json in isolated checkout, got %q (err=%v)", string(pkgBytes), err)
	}
	if _, err := os.Stat(filepath.Join(checkout.Dir, "release-only.js")); err != nil {
		cleanup()
		t.Fatalf("expected release-only.js in checkout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkout.Dir, "main-only.js")); !os.IsNotExist(err) {
		cleanup()
		t.Fatalf("unexpected main-only.js in release checkout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(checkout.Dir, "dirty-uncommitted.txt")); !os.IsNotExist(err) {
		cleanup()
		t.Fatalf("dirty workspace file leaked into planning checkout: %v", err)
	}

	isolatedDir := checkout.Dir
	cleanup()
	if _, err := os.Stat(isolatedDir); !os.IsNotExist(err) {
		t.Fatalf("cleanup did not remove isolated checkout %s: %v", isolatedDir, err)
	}

	// Verify the user's existing workspace was not reset, deleted, or overwritten.
	wsHeadAfter := strings.TrimSpace(runGitIn(t, wsRepoDir, "rev-parse", "HEAD"))
	wsBranchAfter := strings.TrimSpace(runGitIn(t, wsRepoDir, "rev-parse", "--abbrev-ref", "HEAD"))
	if wsHeadAfter != wsHeadBefore || wsBranchAfter != wsBranchBefore {
		t.Fatalf("existing workspace git state changed: head %s->%s branch %s->%s", wsHeadBefore, wsHeadAfter, wsBranchBefore, wsBranchAfter)
	}
	if raw, err := os.ReadFile(dirtyFile); err != nil || string(raw) != "uncommitted workspace secret" {
		t.Fatalf("existing workspace dirty file lost: %q %v", raw, err)
	}
	if raw, err := os.ReadFile(wsReadme); err != nil || string(raw) != "dirty modified readme\n" {
		t.Fatalf("existing workspace modified tracked file lost: %q %v", raw, err)
	}
}

func TestPlanningCloneFollowsMovedHeadAndRejectsInvalidRefs(t *testing.T) {
	remote := newTestGitRemote(t, "app", map[string]string{
		"README.md":    "v1\n",
		"package.json": `{"name":"app","version":"1.0.0","scripts":{"start":"node index.js"}}`,
	})
	v1SHA := remote.defaultSHA

	gitRoot := t.TempDir()
	store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), devdomain.Touch(devdomain.Workspace{
		ID:            "wsp_stale",
		ProfileID:     "onboard",
		EnvironmentID: "dev",
		Owner:         "alice",
		Status:        devdomain.StatusRunning,
		UpstreamRepo:  remote.repoURL,
		RepoURL:       remote.repoURL,
		BaseCommit:    v1SHA,
		HeadCommit:    v1SHA,
	}, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	// Remove .git in the workspace repoDir to simulate a broken/non-git directory
	// that previously caused rev-parse HEAD to fail and fall back to 3a317b55...
	brokenWs := devdomain.Touch(devdomain.Workspace{
		ID:            "wsp_broken",
		ProfileID:     "onboard",
		EnvironmentID: "dev",
		Owner:         "alice",
		Status:        devdomain.StatusRunning,
		UpstreamRepo:  remote.repoURL,
		RepoURL:       remote.repoURL,
		BaseCommit:    "",
	}, time.Now().UTC())
	if err := store.Save(t.Context(), brokenWs); err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(filepath.Join(store.RepoDir("wsp_broken"), ".git"))

	demoDir := t.TempDir()
	t.Setenv("CONDUIT_DEMO_SOURCE_DIR", demoDir)
	t.Setenv("DEVWORKSPACE_GIT_ROOT", gitRoot)

	cloneFn := loopbackPlanningClone(t)

	// 1. Initial clone resolves v1SHA.
	c1, clean1, err := cloneFn(t.Context(), remote.repoURL, "main")
	if err != nil {
		t.Fatalf("clone v1: %v", err)
	}
	if c1.SHA != v1SHA {
		clean1()
		t.Fatalf("c1.SHA = %q, want %q", c1.SHA, v1SHA)
	}
	clean1()

	// 2. Move main head to v2SHA and verify subsequent clone resolves v2SHA (not stale wsp_stale BaseCommit).
	v2SHA := remote.commitBranch(t, "main", "main", map[string]string{
		"README.md":    "v2\n",
		"package.json": `{"name":"app","version":"2.0.0","scripts":{"start":"node index.js"}}`,
	}, nil, "bump to v2")
	if v2SHA == v1SHA {
		t.Fatal("expected main head to advance")
	}

	c2, clean2, err := cloneFn(t.Context(), remote.repoURL, "main")
	if err != nil {
		t.Fatalf("clone v2: %v", err)
	}
	if c2.SHA != v2SHA {
		clean2()
		t.Fatalf("c2.SHA = %q, want moved head %q (stale=%q)", c2.SHA, v2SHA, v1SHA)
	}
	readme, err := os.ReadFile(filepath.Join(c2.Dir, "README.md"))
	clean2()
	if err != nil || string(readme) != "v2\n" {
		t.Fatalf("expected v2 README.md, got %q (err=%v)", string(readme), err)
	}

	// 3. Invalid and missing refs must fail rather than falling back to HEAD or placeholder SHA.
	for _, badRef := range []string{
		"does-not-exist",
		"refs/heads/missing",
		"refs/tags/missing",
		"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"--upload-pack=evil",
		"main:other",
	} {
		got, cleanBad, err := cloneFn(t.Context(), remote.repoURL, badRef)
		cleanBad()
		if err == nil {
			t.Fatalf("clone with invalid ref %q succeeded with SHA=%q Dir=%q", badRef, got.SHA, got.Dir)
		}
		if got.SHA == legacyPlaceholderSHA {
			t.Fatalf("invalid ref %q returned legacy placeholder SHA", badRef)
		}
	}
}

func TestPlanningSessionEndToEndHonorsRequestedBranchAndRejectsMovedOrInvalidRef(t *testing.T) {
	tmpDir := t.TempDir()
	modelhttpfixture.Configure(t, nil)

	remote := newTestGitRemote(t, "excalidraw", map[string]string{
		"package.json":  `{"name":"excalidraw","version":"0.17.0","scripts":{"start":"npm start"}}`,
		"README.md":     "# Excalidraw on main\n",
		"main-file.txt": "main branch only\n",
	})
	mainSHA := remote.defaultSHA
	releaseSHA := remote.commitBranch(t, "release", "main", map[string]string{
		"package.json":     `{"name":"excalidraw","version":"0.18.0","scripts":{"start":"yarn start"}}`,
		"yarn.lock":        "# yarn lockfile v1\n",
		"tsconfig.json":    `{"compilerOptions":{"strict":true}}`,
		"release-file.txt": "release branch only\n",
	}, []string{"main-file.txt"}, "release branch with yarn+ts")

	// Seed a workspace on main with dirty files and a non-git CONDUIT_DEMO_SOURCE_DIR.
	gitRoot := filepath.Join(tmpDir, "devworkspaces")
	store, err := gitstore.Open(gitstore.Options{RootDir: gitRoot})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), devdomain.Touch(devdomain.Workspace{
		ID:            "wsp_other_user",
		ProfileID:     "onboard",
		EnvironmentID: "dev",
		Owner:         "bob",
		Status:        devdomain.StatusRunning,
		UpstreamRepo:  remote.repoURL,
		RepoURL:       remote.repoURL,
		BaseCommit:    mainSHA,
		HeadCommit:    mainSHA,
	}, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.RepoDir("wsp_other_user"), "dirty.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}

	plainDemo := filepath.Join(tmpDir, "plain-demo")
	if err := os.MkdirAll(plainDemo, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONDUIT_DEMO_SOURCE_DIR", plainDemo)
	t.Setenv("DEVWORKSPACE_GIT_ROOT", gitRoot)
	// The fixture source is a loopback server, which the default policy refuses.
	t.Setenv("CONDUIT_SOURCE_ALLOWED_HOSTS", "127.0.0.1")

	repository := controlplane.NewMemoryRepository()
	opts := controlplane.HandlerOptions{
		AllowInsecureOperatorIdentityHeaders: true,
		BootstrapAdministrators:              []string{"alice"},
	}
	handler, err := mountPlanning(http.NotFoundHandler(), repository, opts)
	if err != nil {
		t.Fatalf("mountPlanning: %v", err)
	}

	// 1. Requesting an invalid ref fails on turn 1 and never records placeholder SHA.
	badResp := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions", `{"repoUrl":"`+remote.repoURL+`","ref":"nonexistent-branch"}`)
	var badSession planningdomain.Session
	if badResp.Code != http.StatusCreated || json.Unmarshal(badResp.Body.Bytes(), &badSession) != nil {
		t.Fatalf("create bad ref session: %d %s", badResp.Code, badResp.Body.String())
	}
	badTurn := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+badSession.ID+"/turns", `{"message":"Inspect"}`)
	if badTurn.Code == http.StatusOK {
		t.Fatalf("expected turn on nonexistent ref to fail, got 200: %s", badTurn.Body.String())
	}
	badGet := planningRequest(t, handler, "alice", "GET", "/v1/planning-sessions/"+badSession.ID, "")
	_ = json.Unmarshal(badGet.Body.Bytes(), &badSession)
	if badSession.ResolvedCommit != "" {
		t.Fatalf("expected empty ResolvedCommit on failed ref, got %q", badSession.ResolvedCommit)
	}

	// 2. Requesting branch "release" inspects releaseSHA (typescript/yarn + release-file.txt), not mainSHA or dirty workspace.
	createBody, _ := json.Marshal(map[string]string{
		"repoUrl": remote.repoURL,
		"ref":     "release",
	})
	resp := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions", string(createBody))
	var created planningdomain.Session
	if resp.Code != http.StatusCreated || json.Unmarshal(resp.Body.Bytes(), &created) != nil {
		t.Fatalf("create release session: %d %s", resp.Code, resp.Body.String())
	}
	turn1 := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+created.ID+"/turns", `{"message":"Inspect release branch"}`)
	if turn1.Code != http.StatusOK || json.Unmarshal(turn1.Body.Bytes(), &created) != nil {
		t.Fatalf("turn 1: %d %s", turn1.Code, turn1.Body.String())
	}
	if created.ResolvedCommit != releaseSHA {
		t.Fatalf("session ResolvedCommit = %q, want releaseSHA %q (mainSHA=%q)", created.ResolvedCommit, releaseSHA, mainSHA)
	}
	if created.Inspection == nil || created.Inspection.Language != "typescript" || created.Inspection.PackageManager != "yarn" {
		t.Fatalf("expected typescript/yarn from release branch, got %+v", created.Inspection)
	}
	treeJoined := strings.Join(created.Inspection.Tree, ",")
	if !strings.Contains(treeJoined, "release-file.txt") || strings.Contains(treeJoined, "main-file.txt") || strings.Contains(treeJoined, "dirty.txt") {
		t.Fatalf("unexpected tree for release branch: %v", created.Inspection.Tree)
	}

	// 3. Answer NeedsInput, then move "release" branch head before Turn 2: Turn 2 must detect the ref movement and refuse.
	ansResp := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+created.ID+"/answers", `{"answers":[{"id":"node_version","value":"20"}]}`)
	if ansResp.Code != http.StatusOK {
		t.Fatalf("answers: %d %s", ansResp.Code, ansResp.Body.String())
	}
	movedReleaseSHA := remote.commitBranch(t, "release", "release", map[string]string{
		"release-file.txt": "moved release head\n",
	}, nil, "advance release branch")
	if movedReleaseSHA == releaseSHA {
		t.Fatal("expected moved release SHA")
	}
	turn2Moved := planningRequest(t, handler, "alice", "POST", "/v1/planning-sessions/"+created.ID+"/turns", `{"message":"Write plan"}`)
	if turn2Moved.Code == http.StatusOK {
		t.Fatalf("expected turn 2 after branch movement to fail, got 200: %s", turn2Moved.Body.String())
	}
}

// loopbackPlanningClone builds the Hub clone function with the fixture host
// on the operator allowlist. Without that entry the default policy refuses a
// loopback server.
func loopbackPlanningClone(t *testing.T) planner.CloneFunc {
	t.Helper()
	cloneFn, err := hubPlanningClone(func(name string) string {
		if name == repoclone.EnvAllowedHosts {
			return "127.0.0.1"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	return cloneFn
}

func TestHubPlanningCloneRefusesLoopbackUnlessAllowlistedAndRejectsBadPolicy(t *testing.T) {
	remote := newTestGitRemote(t, "app", map[string]string{"README.md": "v1\n"})
	cloneFn, err := hubPlanningClone(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := cloneFn(t.Context(), remote.repoURL, "main")
	cleanup()
	if !errors.Is(err, repoclone.ErrAddressRefused) {
		t.Fatalf("default policy clone of a loopback host: err = %v, want ErrAddressRefused", err)
	}
	if _, err := hubPlanningClone(func(name string) string {
		if name == repoclone.EnvConnections {
			return `[{"id":"corp","host":"example.test","token":"inline-secret"}]`
		}
		return ""
	}); err == nil || strings.Contains(err.Error(), "inline-secret") {
		t.Fatalf("inline secret in the connection list: err = %v, want a refusal that does not repeat it", err)
	}
}
