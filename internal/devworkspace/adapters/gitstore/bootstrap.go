package gitstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BootstrapOptions configures how a workspace initializes its /repo checkout
// from an upstream source (online Git URL or local filesystem path) and
// provisions a new durable GitHub repository (or local bare remote fallback).
type BootstrapOptions struct {
	WorkspaceID      string
	SourceRepo       string
	Ref              string
	Branch           string
	DestDir          string
	LocalRemoteDir   string
	CreateGitHubRepo bool
	LocalOnly        bool
	GitHubRepo       string
	GitHubPrivate    bool
	GitHubToken      string
	GitHubAPIBaseURL string
	Description      string
}

// BootstrapResult records the initialized checkout and durable remote coordinates.
type BootstrapResult struct {
	UpstreamRepo   string `json:"upstreamRepo"`
	UpstreamRef    string `json:"upstreamRef"`
	RepoURL        string `json:"repoUrl"`
	RemoteURL      string `json:"remoteUrl"`
	Branch         string `json:"branch"`
	BaseCommit     string `json:"baseCommit"`
	HeadCommit     string `json:"headCommit"`
	CreatedGitHub  bool   `json:"createdGitHub"`
	GitHubFullName string `json:"githubFullName,omitempty"`
	RepoDir        string `json:"repoDir"`
}

var skipBootstrapDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".next":        true,
	"dist":         true,
	"build":        true,
	".cache":       true,
}

// BootstrapSource ingests SourceRepo (either an online http/https Git repository
// or a local filesystem Git repository / directory) into DestDir at Ref, and
// provisions a new durable GitHub repository (or local bare Git remote when
// LocalOnly is set or no GitHub token is configured) as origin.
func BootstrapSource(ctx context.Context, opts BootstrapOptions) (BootstrapResult, error) {
	source := strings.TrimSpace(opts.SourceRepo)
	if source == "" {
		return BootstrapResult{}, fmt.Errorf("source repository URL or path is required")
	}
	destDir := strings.TrimSpace(opts.DestDir)
	if destDir == "" {
		return BootstrapResult{}, fmt.Errorf("destination directory is required")
	}
	ref := strings.TrimSpace(opts.Ref)
	if ref == "" {
		ref = "main"
	}
	branch := strings.TrimSpace(opts.Branch)
	if branch == "" {
		branch = "workspace/operator/default"
	}

	if err := ingestSourceToGitDir(ctx, source, ref, destDir); err != nil {
		return BootstrapResult{}, err
	}
	_ = runGit(ctx, destDir, "config", "user.name", "workspace")
	_ = runGit(ctx, destDir, "config", "user.email", "workspace@local")

	baseCommit, err := gitOut(ctx, destDir, "rev-parse", "HEAD")
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("resolve base commit: %w", err)
	}
	baseCommit = strings.TrimSpace(baseCommit)

	// Ensure both 'main' and the workspace branch point to baseCommit.
	_ = runGit(ctx, destDir, "branch", "-f", "main", baseCommit)
	if err := runGit(ctx, destDir, "checkout", "-B", branch, baseCommit); err != nil {
		return BootstrapResult{}, fmt.Errorf("checkout branch %s: %w", branch, err)
	}

	token := resolveGitHubToken(opts.GitHubToken)
	wantGitHub := !opts.LocalOnly && (opts.CreateGitHubRepo || strings.TrimSpace(opts.GitHubRepo) != "" || token != "")

	res := BootstrapResult{
		UpstreamRepo: source,
		UpstreamRef:  ref,
		Branch:       branch,
		BaseCommit:   baseCommit,
		HeadCommit:   baseCommit,
		RepoDir:      destDir,
	}

	if wantGitHub && token != "" {
		repoSpec := strings.TrimSpace(opts.GitHubRepo)
		if repoSpec == "" {
			repoSpec = defaultGitHubRepoName(source, opts.WorkspaceID)
		}
		desc := strings.TrimSpace(opts.Description)
		if desc == "" {
			desc = fmt.Sprintf("Conduit durable workspace repository for %s", filepath.Base(strings.TrimSuffix(source, ".git")))
		}
		ghRepo, err := createOrGetGitHubRepo(ctx, opts.GitHubAPIBaseURL, token, repoSpec, desc, opts.GitHubPrivate)
		if err != nil {
			return BootstrapResult{}, fmt.Errorf("create GitHub repository %q: %w", repoSpec, err)
		}
		_ = runGit(ctx, destDir, "remote", "remove", "origin")
		if err := runGit(ctx, destDir, "remote", "add", "origin", ghRepo.HTMLURL); err != nil {
			return BootstrapResult{}, fmt.Errorf("add GitHub remote: %w", err)
		}
		if err := pushBranchToGitHubOrOrigin(ctx, destDir, "main", opts.GitHubAPIBaseURL, token, ghRepo.Owner, ghRepo.Name); err != nil {
			return BootstrapResult{}, fmt.Errorf("push base branch to GitHub repo %s: %w", ghRepo.FullName, err)
		}
		if branch != "main" {
			if err := pushBranchToGitHubOrOrigin(ctx, destDir, branch, opts.GitHubAPIBaseURL, token, ghRepo.Owner, ghRepo.Name); err != nil {
				return BootstrapResult{}, fmt.Errorf("push workspace branch to GitHub repo %s: %w", ghRepo.FullName, err)
			}
		}
		res.RepoURL = ghRepo.HTMLURL
		res.RemoteURL = ghRepo.HTMLURL
		res.CreatedGitHub = true
		res.GitHubFullName = ghRepo.FullName
		return res, nil
	}

	if opts.CreateGitHubRepo && token == "" {
		return BootstrapResult{}, fmt.Errorf("GH_TOKEN or GITHUB_TOKEN is required to create a new GitHub repository")
	}

	// Local bare repository fallback for offline / unit-test execution.
	remoteDir := strings.TrimSpace(opts.LocalRemoteDir)
	if remoteDir == "" {
		remoteDir = filepath.Join(filepath.Dir(destDir), "remote.git")
	}
	if _, err := os.Stat(filepath.Join(remoteDir, "HEAD")); err != nil {
		if err := os.MkdirAll(remoteDir, 0o755); err != nil {
			return BootstrapResult{}, err
		}
		if err := runGit(ctx, remoteDir, "init", "--bare"); err != nil {
			return BootstrapResult{}, fmt.Errorf("init bare remote: %w", err)
		}
	}
	_ = runGit(ctx, destDir, "remote", "remove", "origin")
	if err := runGit(ctx, destDir, "remote", "add", "origin", remoteDir); err != nil {
		return BootstrapResult{}, fmt.Errorf("add local bare remote: %w", err)
	}
	if err := pushGitRef(ctx, destDir, "", "HEAD:refs/heads/main"); err != nil {
		return BootstrapResult{}, fmt.Errorf("push main to local remote: %w", err)
	}
	if branch != "main" {
		if err := pushGitRef(ctx, destDir, "", "HEAD:refs/heads/"+branch); err != nil {
			return BootstrapResult{}, fmt.Errorf("push %s to local remote: %w", branch, err)
		}
	}
	res.RepoURL = canonicalRepoURL(source)
	res.RemoteURL = remoteDir
	return res, nil
}

func ingestSourceToGitDir(ctx context.Context, source, ref, destDir string) error {
	if err := os.RemoveAll(destDir); err != nil {
		return err
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}

	// Check if source is a local filesystem path or file:// URL.
	if localPath, ok := resolveLocalSourcePath(source); ok {
		if isGitDirectory(localPath) {
			if err := runGit(ctx, "", "clone", "--no-recurse-submodules", localPath, destDir); err != nil {
				return fmt.Errorf("clone local git repo %s: %w", localPath, err)
			}
			checkoutRequestedRef(ctx, destDir, ref)
			return ensureAtLeastOneCommit(ctx, destDir, filepath.Base(localPath))
		}
		// Plain filesystem directory: copy non-secret files and initialize git repository.
		if err := copyDirectoryFiltered(localPath, destDir); err != nil {
			return fmt.Errorf("copy local source directory %s: %w", localPath, err)
		}
		if err := runGit(ctx, destDir, "init"); err != nil {
			return err
		}
		_ = runGit(ctx, destDir, "symbolic-ref", "HEAD", "refs/heads/main")
		if err := runGit(ctx, destDir, "add", "-A"); err != nil {
			return err
		}
		_ = runGit(ctx, destDir, "-c", "user.name=workspace", "-c", "user.email=workspace@local", "commit", "-m", "chore(workspace): import local source directory")
		return ensureAtLeastOneCommit(ctx, destDir, filepath.Base(localPath))
	}

	// Online http:// or https:// Git repository.
	if isHTTPURL(source) {
		if err := runGit(ctx, "", "clone", "--depth", "1", "--no-recurse-submodules", source, destDir); err != nil {
			return fmt.Errorf("clone online repository %s: %w", source, err)
		}
		checkoutRequestedRef(ctx, destDir, ref)
		return ensureAtLeastOneCommit(ctx, destDir, filepath.Base(strings.TrimSuffix(source, ".git")))
	}

	return fmt.Errorf("source %q is neither a readable local path nor an http/https Git repository URL", source)
}

func checkoutRequestedRef(ctx context.Context, repoDir, ref string) {
	ref = strings.TrimSpace(ref)
	if ref == "" || ref == "HEAD" || ref == "main" {
		return
	}
	if err := runGit(ctx, repoDir, " fetch", "--depth", "1", "--no-recurse-submodules", "origin", ref); err == nil {
		if runGit(ctx, repoDir, "checkout", "--detach", "FETCH_HEAD") == nil {
			return
		}
	}
	_ = runGit(ctx, repoDir, "fetch", "--depth", "1", "--no-recurse-submodules", "origin", ref)
	if runGit(ctx, repoDir, "checkout", "--detach", "FETCH_HEAD") == nil {
		return
	}
	_ = runGit(ctx, repoDir, "checkout", ref)
}

func ensureAtLeastOneCommit(ctx context.Context, repoDir, title string) error {
	if head, err := gitOut(ctx, repoDir, "rev-parse", "--verify", "HEAD"); err == nil && strings.TrimSpace(head) != "" {
		return nil
	}
	readme := filepath.Join(repoDir, "README.md")
	if _, err := os.Stat(readme); errors.Is(err, os.ErrNotExist) {
		if title == "" {
			title = "workspace"
		}
		if err := os.WriteFile(readme, []byte("# "+title+"\n"), 0o644); err != nil {
			return err
		}
	}
	if err := runGit(ctx, repoDir, "add", "-A"); err != nil {
		return err
	}
	return runGit(ctx, repoDir, "-c", "user.name=workspace", "-c", "user.email=workspace@local", "commit", "-m", "chore(workspace): initialize repository")
}

func resolveLocalSourcePath(source string) (string, bool) {
	if strings.HasPrefix(source, "file://") {
		parsed, err := url.Parse(source)
		if err == nil && parsed.Path != "" {
			if info, statErr := os.Stat(parsed.Path); statErr == nil && info.IsDir() {
				return parsed.Path, true
			}
		}
	}
	if isHTTPURL(source) {
		return "", false
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return "", false
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return "", false
	}
	return abs, true
}

func isGitDirectory(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		if _, objErr := os.Stat(filepath.Join(dir, "objects")); objErr == nil {
			return true
		}
	}
	return false
}

func isHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func canonicalRepoURL(source string) string {
	if isHTTPURL(source) {
		return strings.TrimSuffix(strings.TrimSpace(source), ".git")
	}
	base := filepath.Base(strings.TrimRight(source, "/"))
	if base == "" || base == "." {
		base = "local-app"
	}
	return "https://github.com/local/" + sanitizeBranchSegment(base)
}

func copyDirectoryFiltered(srcDir, dstDir string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil || rel == "." {
			return err
		}
		if d.IsDir() {
			if skipBootstrapDirs[d.Name()] {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dstDir, rel), 0o755)
		}
		if isSecretPath(rel) || !d.Type().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		target := filepath.Join(dstDir, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

type githubRepoInfo struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
	CloneURL string `json:"clone_url"`
	Owner    string
}

func resolveGitHubToken(explicit string) string {
	for _, candidate := range []string{
		strings.TrimSpace(explicit),
		strings.TrimSpace(os.Getenv("CONDUIT_GITHUB_TOKEN")),
		strings.TrimSpace(os.Getenv("GH_TOKEN")),
		strings.TrimSpace(os.Getenv("GITHUB_TOKEN")),
	} {
		if candidate != "" && !strings.HasPrefix(candidate, "agy-h-") {
			return candidate
		}
	}
	return ""
}

func defaultGitHubRepoName(source, workspaceID string) string {
	cleaned := strings.TrimSuffix(strings.TrimRight(source, "/"), ".git")
	base := filepath.Base(cleaned)
	if base == "" || base == "." {
		base = "app"
	}
	id := sanitizeBranchSegment(workspaceID)
	if id == "" {
		id = "ws"
	}
	return fmt.Sprintf("conduit-%s-%s", strings.ToLower(sanitizeBranchSegment(base)), strings.ToLower(id))
}

func createOrGetGitHubRepo(ctx context.Context, apiBase, token, repoSpec, description string, private bool) (githubRepoInfo, error) {
	if strings.TrimSpace(apiBase) == "" {
		apiBase = "https://api.github.com"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	login, err := githubAuthenticatedUser(ctx, apiBase, token)
	if err != nil {
		return githubRepoInfo{}, err
	}

	owner := login
	name := strings.TrimSpace(repoSpec)
	if parts := strings.SplitN(name, "/", 2); len(parts) == 2 {
		if strings.TrimSpace(parts[0]) != "" {
			owner = strings.TrimSpace(parts[0])
		}
		name = strings.TrimSpace(parts[1])
	}
	name = sanitizeBranchSegment(name)
	if name == "" {
		name = "conduit-workspace"
	}

	createURL := apiBase + "/user/repos"
	if !strings.EqualFold(owner, login) {
		createURL = fmt.Sprintf("%s/orgs/%s/repos", apiBase, owner)
	}

	payload := map[string]any{
		"name":        name,
		"description": description,
		"private":     private,
		"auto_init":   false,
	}
	var created githubRepoInfo
	status, respBody, err := githubJSONRequest(ctx, http.MethodPost, createURL, token, payload, &created)
	if err != nil {
		return githubRepoInfo{}, err
	}
	if status == http.StatusCreated || status == http.StatusOK {
		created.Owner = owner
		if created.FullName == "" {
			created.FullName = owner + "/" + name
		}
		if created.HTMLURL == "" {
			created.HTMLURL = "https://github.com/" + created.FullName
		}
		return created, nil
	}
	if status == http.StatusUnprocessableEntity {
		getURL := fmt.Sprintf("%s/repos/%s/%s", apiBase, owner, name)
		var existing githubRepoInfo
		getStatus, getBody, getErr := githubJSONRequest(ctx, http.MethodGet, getURL, token, nil, &existing)
		if getErr != nil {
			return githubRepoInfo{}, getErr
		}
		if getStatus == http.StatusOK {
			existing.Owner = owner
			if existing.FullName == "" {
				existing.FullName = owner + "/" + name
			}
			if existing.HTMLURL == "" {
				existing.HTMLURL = "https://github.com/" + existing.FullName
			}
			return existing, nil
		}
		return githubRepoInfo{}, fmt.Errorf("github get existing repo %s/%s HTTP %d: %s", owner, name, getStatus, string(getBody))
	}
	return githubRepoInfo{}, fmt.Errorf("github create repo %s/%s HTTP %d: %s", owner, name, status, string(respBody))
}

func githubAuthenticatedUser(ctx context.Context, apiBase, token string) (string, error) {
	var user struct {
		Login string `json:"login"`
	}
	status, body, err := githubJSONRequest(ctx, http.MethodGet, apiBase+"/user", token, nil, &user)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || strings.TrimSpace(user.Login) == "" {
		return "", fmt.Errorf("github /user HTTP %d: %s", status, string(body))
	}
	return strings.TrimSpace(user.Login), nil
}

// pushBranchToGitHubOrOrigin pushes a local branch to origin using git push
// (with token extraheader when pushing to GitHub), and falls back to the
// GitHub Git Data REST API if smart-HTTP git-receive-pack is blocked by a
// sandbox proxy or when running against an API-only test server.
func pushBranchToGitHubOrOrigin(ctx context.Context, repoDir, branch, apiBase, token, owner, repo string) error {
	refSpec := fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)
	if _, err := gitOut(ctx, repoDir, "rev-parse", "--verify", "refs/heads/"+branch); err != nil {
		refSpec = fmt.Sprintf("HEAD:refs/heads/%s", branch)
	}
	err := pushGitRef(ctx, repoDir, token, refSpec)
	if err == nil {
		return nil
	}
	if !errors.Is(err, errGitPushTransportUnavailable) {
		return err
	}
	if token != "" && owner != "" && repo != "" {
		return syncBranchViaGitHubDataAPI(ctx, repoDir, branch, apiBase, token, owner, repo)
	}
	return err
}

var errGitPushTransportUnavailable = errors.New("git push transport is unavailable")

func pushGitRef(ctx context.Context, repoDir, token, refSpec string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "git", "push", "--porcelain", "origin", refSpec)
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=0")
	if token != "" {
		header := "AUTHORIZATION: basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
		cmd.Env = append(cmd.Env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0="+header)
	}
	output, err := cmd.Output()
	if err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return fmt.Errorf("execute git push: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "!\t") {
			return fmt.Errorf("git rejected branch publication: %w", domain.ErrConflict)
		}
	}
	failure := strings.ToLower(string(exit.Stderr))
	for _, message := range []string{"authentication failed", "could not read username", "invalid username or password", "permission denied", "returned error: 401"} {
		if strings.Contains(failure, message) {
			return fmt.Errorf("git push authentication failed: %w", domain.ErrForbidden)
		}
	}
	return errGitPushTransportUnavailable
}

func parseGitHubOwnerRepo(remoteURL string) (string, string, bool) {
	remoteURL = strings.TrimSpace(strings.TrimSuffix(remoteURL, ".git"))
	parsed, err := url.Parse(remoteURL)
	if err != nil {
		return "", "", false
	}
	if !strings.EqualFold(parsed.Host, "github.com") {
		return "", "", false
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// syncBranchViaGitHubDataAPI publishes a snapshot only to an absent branch.
// to GitHub's Git Data REST API (/git/blobs, /git/trees, /git/commits, /git/refs).
func syncBranchViaGitHubDataAPI(ctx context.Context, repoDir, branch, apiBase, token, owner, repo string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := runGit(ctx, repoDir, "check-ref-format", "refs/heads/"+branch); err != nil {
		return domain.ErrInvalid
	}
	if strings.TrimSpace(apiBase) == "" {
		apiBase = "https://api.github.com"
	}
	apiBase = strings.TrimRight(apiBase, "/")
	rev := "refs/heads/" + branch
	if _, err := gitOut(ctx, repoDir, "rev-parse", "--verify", rev); err != nil {
		rev = "HEAD"
	}
	localHead, err := gitOut(ctx, repoDir, "rev-parse", rev)
	if err != nil {
		return err
	}
	localHead = strings.TrimSpace(localHead)
	if !validGitHubObjectID(localHead) {
		return domain.ErrInvalid
	}
	endpoint := fmt.Sprintf("%s/repos/%s/%s/git", apiBase, owner, repo)
	remoteHead, absent, err := lookupBootstrapRef(ctx, endpoint, token, branch)
	if err != nil {
		return err
	}
	if !absent {
		if remoteHead == localHead {
			return nil
		}
		return fmt.Errorf("API fallback cannot replace an existing branch; use a fast-forward Git push: %w", domain.ErrConflict)
	}
	var parents []string
	if branch != "main" {
		mainHead, mainAbsent, err := lookupBootstrapRef(ctx, endpoint, token, "main")
		if err != nil {
			return err
		}
		if !mainAbsent {
			parents = []string{mainHead}
		}
	}
	files, err := filesAtCommit(ctx, repoDir, rev)
	if err != nil {
		return err
	}
	type treeEntry struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	entries := make([]treeEntry, 0, len(files))
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		file := files[path]
		var blob struct {
			SHA string `json:"sha"`
		}
		status, _, err := githubJSONRequest(ctx, http.MethodPost, endpoint+"/blobs", token, map[string]any{"content": base64.StdEncoding.EncodeToString(file.Content), "encoding": "base64"}, &blob)
		if err != nil {
			return err
		}
		if (status != http.StatusCreated && status != http.StatusOK) || !validGitHubObjectID(blob.SHA) {
			return fmt.Errorf("create GitHub blob failed (HTTP %d)", status)
		}
		entries = append(entries, treeEntry{Path: path, Mode: file.Mode, Type: "blob", SHA: blob.SHA})
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	status, _, err := githubJSONRequest(ctx, http.MethodPost, endpoint+"/trees", token, map[string]any{"tree": entries}, &tree)
	if err != nil {
		return err
	}
	if (status != http.StatusCreated && status != http.StatusOK) || !validGitHubObjectID(tree.SHA) {
		return fmt.Errorf("create GitHub tree failed (HTTP %d)", status)
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	payload := map[string]any{"message": "chore(workspace): bootstrap source snapshot", "tree": tree.SHA}
	if len(parents) > 0 {
		payload["parents"] = parents
	}
	status, _, err = githubJSONRequest(ctx, http.MethodPost, endpoint+"/commits", token, payload, &commit)
	if err != nil {
		return err
	}
	if (status != http.StatusCreated && status != http.StatusOK) || !validGitHubObjectID(commit.SHA) {
		return fmt.Errorf("create GitHub commit failed (HTTP %d)", status)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Creating a ref is atomic with respect to absence. Never PATCH or force an
	// existing ref, including one created by another caller after our lookup.
	var createdRef struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	status, _, err = githubJSONRequest(ctx, http.MethodPost, endpoint+"/refs", token, map[string]any{"ref": "refs/heads/" + branch, "sha": commit.SHA}, &createdRef)
	if err != nil {
		return err
	}
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		return fmt.Errorf("branch creation conflicted: %w", domain.ErrConflict)
	}
	if status != http.StatusCreated {
		return fmt.Errorf("create GitHub ref failed (HTTP %d)", status)
	}
	if createdRef.Ref != "refs/heads/"+branch || createdRef.Object.SHA != commit.SHA || createdRef.Object.Type != "commit" {
		return fmt.Errorf("created GitHub ref response is invalid")
	}
	return nil
}

func validGitHubObjectID(value string) bool {
	if len(value) != 40 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func lookupBootstrapRef(ctx context.Context, endpoint, token, branch string) (string, bool, error) {
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"object"`
	}
	status, body, err := githubJSONRequest(ctx, http.MethodGet, endpoint+"/ref/heads/"+branch, token, nil, &ref)
	if err != nil {
		return "", false, err
	}
	if status == http.StatusOK {
		if ref.Ref != "refs/heads/"+branch || ref.Object.Type != "commit" || !validGitHubObjectID(ref.Object.SHA) {
			return "", false, fmt.Errorf("GitHub ref response is invalid")
		}
		return ref.Object.SHA, false, nil
	}
	if status == http.StatusNotFound {
		return "", true, nil
	}
	// GitHub reports a newly created, empty repository with this specific 409.
	// Other conflicts do not establish absence and must not authorize creation.
	if status == http.StatusConflict {
		var failure struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &failure) == nil && failure.Message == "Git Repository is empty." {
			return "", true, nil
		}
	}
	return "", false, fmt.Errorf("lookup GitHub ref failed (HTTP %d)", status)
}

func githubJSONRequest(ctx context.Context, method, reqURL, token string, payload any, out any) (int, []byte, error) {
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		bodyReader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "conduit-gitstore")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if out != nil && len(respBytes) > 0 && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_ = json.Unmarshal(respBytes, out)
	}
	return resp.StatusCode, respBytes, nil
}

type bootstrapFile struct {
	Content []byte
	Mode    string
}

func filesAtCommit(ctx context.Context, repoDir, ref string) (map[string]bootstrapFile, error) {
	out, err := gitOut(ctx, repoDir, "ls-tree", "-r", "-z", "--full-tree", ref)
	if err != nil {
		return nil, err
	}
	files := map[string]bootstrapFile{}
	for _, entry := range strings.Split(out, "\x00") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		meta, filePath, ok := strings.Cut(entry, "\t")
		if !ok || filePath == "" {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 || fields[1] != "blob" || (fields[0] != "100644" && fields[0] != "100755" && fields[0] != "120000") {
			return nil, fmt.Errorf("GitHub snapshot fallback does not support this tree entry")
		}
		content, err := gitRawOut(ctx, repoDir, "cat-file", "blob", fields[2])
		if err != nil {
			return nil, err
		}
		files[filePath] = bootstrapFile{Content: content, Mode: fields[0]}
	}
	return files, nil
}
