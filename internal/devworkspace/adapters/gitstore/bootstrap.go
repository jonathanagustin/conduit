package gitstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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
	if err := runGit(ctx, destDir, "push", "-f", "origin", "HEAD:refs/heads/main"); err != nil {
		return BootstrapResult{}, fmt.Errorf("push main to local remote: %w", err)
	}
	if branch != "main" {
		if err := runGit(ctx, destDir, "push", "-f", "origin", "HEAD:refs/heads/"+branch); err != nil {
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
		_ = runGit(ctx, destDir, "-c", "user.name=workspace", "-c", "user.email=workspace@local", "commit", "-m", "import local source directory")
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
	return runGit(ctx, repoDir, "-c", "user.name=workspace", "-c", "user.email=workspace@local", "commit", "-m", "initialize workspace repository")
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
	if token != "" {
		basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		header := "AUTHORIZATION: basic " + basic
		if err := runGit(ctx, repoDir, "-c", "http.extraHeader="+header, "push", "-f", "origin", refSpec); err == nil {
			return nil
		}
		if owner != "" && repo != "" {
			if apiErr := syncBranchViaGitHubDataAPI(ctx, repoDir, branch, apiBase, token, owner, repo); apiErr == nil {
				return nil
			} else {
				return apiErr
			}
		}
	}
	return runGit(ctx, repoDir, "push", "-f", "origin", refSpec)
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

// syncBranchViaGitHubDataAPI replicates the tracked files at branch (or HEAD)
// to GitHub's Git Data REST API (/git/blobs, /git/trees, /git/commits, /git/refs).
func syncBranchViaGitHubDataAPI(ctx context.Context, repoDir, branch, apiBase, token, owner, repo string) error {
	if strings.TrimSpace(apiBase) == "" {
		apiBase = "https://api.github.com"
	}
	apiBase = strings.TrimRight(apiBase, "/")

	rev := "refs/heads/" + branch
	if _, err := gitOut(ctx, repoDir, "rev-parse", "--verify", rev); err != nil {
		rev = "HEAD"
	}
	files, err := filesAtCommit(ctx, repoDir, rev)
	if err != nil {
		return err
	}
	msg, err := gitOut(ctx, repoDir, "log", "-1", "--format=%s", rev)
	if err != nil || strings.TrimSpace(msg) == "" {
		msg = "Update workspace branch " + branch
	}
	msg = strings.TrimSpace(msg)

	type treeEntry struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
		Type string `json:"type"`
		SHA  string `json:"sha"`
	}
	entries := make([]treeEntry, 0, len(files))
	for path, content := range files {
		var blobResp struct {
			SHA string `json:"sha"`
		}
		blobURL := fmt.Sprintf("%s/repos/%s/%s/git/blobs", apiBase, owner, repo)
		status, body, err := githubJSONRequest(ctx, http.MethodPost, blobURL, token, map[string]any{
			"content":  base64.StdEncoding.EncodeToString(content),
			"encoding": "base64",
		}, &blobResp)
		if err != nil {
			return err
		}
		if status != http.StatusCreated && status != http.StatusOK {
			return fmt.Errorf("create github blob %s HTTP %d: %s", path, status, string(body))
		}
		entries = append(entries, treeEntry{
			Path: path,
			Mode: "100644",
			Type: "blob",
			SHA:  blobResp.SHA,
		})
	}

	var treeResp struct {
		SHA string `json:"sha"`
	}
	treeURL := fmt.Sprintf("%s/repos/%s/%s/git/trees", apiBase, owner, repo)
	status, body, err := githubJSONRequest(ctx, http.MethodPost, treeURL, token, map[string]any{
		"tree": entries,
	}, &treeResp)
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("create github tree HTTP %d: %s", status, string(body))
	}

	// Check if branch ref already exists so we can parent the commit cleanly.
	var parents []string
	var refLookup struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	refGetURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/%s", apiBase, owner, repo, branch)
	if refStatus, _, refErr := githubJSONRequest(ctx, http.MethodGet, refGetURL, token, nil, &refLookup); refErr == nil && refStatus == http.StatusOK && refLookup.Object.SHA != "" {
		parents = []string{refLookup.Object.SHA}
	} else if branch != "main" {
		mainGetURL := fmt.Sprintf("%s/repos/%s/%s/git/ref/heads/main", apiBase, owner, repo)
		if mStatus, _, mErr := githubJSONRequest(ctx, http.MethodGet, mainGetURL, token, nil, &refLookup); mErr == nil && mStatus == http.StatusOK && refLookup.Object.SHA != "" {
			parents = []string{refLookup.Object.SHA}
		}
	}

	var commitResp struct {
		SHA string `json:"sha"`
	}
	commitURL := fmt.Sprintf("%s/repos/%s/%s/git/commits", apiBase, owner, repo)
	commitPayload := map[string]any{
		"message": msg,
		"tree":    treeResp.SHA,
	}
	if len(parents) > 0 {
		commitPayload["parents"] = parents
	}
	status, body, err = githubJSONRequest(ctx, http.MethodPost, commitURL, token, commitPayload, &commitResp)
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("create github commit HTTP %d: %s", status, string(body))
	}

	// Update existing ref or create new ref.
	patchURL := fmt.Sprintf("%s/repos/%s/%s/git/refs/heads/%s", apiBase, owner, repo, branch)
	status, body, err = githubJSONRequest(ctx, http.MethodPatch, patchURL, token, map[string]any{
		"sha":   commitResp.SHA,
		"force": true,
	}, nil)
	if err == nil && (status == http.StatusOK || status == http.StatusCreated) {
		return nil
	}
	createRefURL := fmt.Sprintf("%s/repos/%s/%s/git/refs", apiBase, owner, repo)
	status, body, err = githubJSONRequest(ctx, http.MethodPost, createRefURL, token, map[string]any{
		"ref": "refs/heads/" + branch,
		"sha": commitResp.SHA,
	}, nil)
	if err != nil {
		return err
	}
	if status != http.StatusCreated && status != http.StatusOK {
		return fmt.Errorf("create github ref refs/heads/%s HTTP %d: %s", branch, status, string(body))
	}
	return nil
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

func filesAtCommit(ctx context.Context, repoDir, ref string) (map[string][]byte, error) {
	out, err := gitOut(ctx, repoDir, "ls-tree", "-r", "-z", "--full-tree", ref)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, entry := range strings.Split(out, "\x00") {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		meta, filePath, ok := strings.Cut(entry, "\t")
		if !ok || filePath == "" {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) < 3 || fields[1] != "blob" {
			continue
		}
		content, err := gitRawOut(ctx, repoDir, "cat-file", "blob", fields[2])
		if err != nil {
			return nil, err
		}
		files[filePath] = content
	}
	return files, nil
}
