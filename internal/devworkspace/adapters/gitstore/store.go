// Package gitstore is the durable Git-backed developer workspace adapter.
// It persists the workspace catalog on disk and stores each workspace's /repo
// tree on an isolated Git branch. Tree, file, and diff reads inspect committed
// Git objects only so uncommitted working-tree edits and files outside /repo
// never leak into the source viewer or ChangeRequest diff.
package gitstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
	"github.com/jonathanagustin/conduit/internal/devworkspace/ports"
)

// Options configures a Git-backed workspace store.
type Options struct {
	RootDir         string
	UpstreamDir     string
	RetentionWindow time.Duration
}

// Store persists workspace records and manages per-workspace Git repositories.
type Store struct {
	mu              sync.Mutex
	rootDir         string
	catalogPath     string
	upstreamDir     string
	retentionWindow time.Duration
	rows            map[string]domain.Workspace
	runtimes        map[string]domain.Runtime
	pods            map[string]struct{}
}

// Open opens or initializes a durable Git-backed workspace store at opts.RootDir.
func Open(opts Options) (*Store, error) {
	root := strings.TrimSpace(opts.RootDir)
	if root == "" {
		return nil, fmt.Errorf("gitstore root directory is required")
	}
	if err := os.MkdirAll(filepath.Join(root, "workspaces"), 0o755); err != nil {
		return nil, err
	}
	retention := opts.RetentionWindow
	if retention <= 0 {
		retention = domain.DefaultRetentionWindow
	}
	store := &Store{
		rootDir:         root,
		catalogPath:     filepath.Join(root, "workspaces.json"),
		upstreamDir:     strings.TrimSpace(opts.UpstreamDir),
		retentionWindow: retention,
		rows:            map[string]domain.Workspace{},
		runtimes:        map[string]domain.Runtime{},
		pods:            map[string]struct{}{},
	}
	if err := store.loadCatalog(); err != nil {
		return nil, err
	}
	return store, nil
}

// RetentionWindow returns the configured volume preservation window.
func (store *Store) RetentionWindow() time.Duration {
	return store.retentionWindow
}

// WorkspaceDir returns the outer workspace volume directory (<root>/workspaces/<id>).
func (store *Store) WorkspaceDir(id string) string {
	return filepath.Join(store.rootDir, "workspaces", id)
}

// RepoDir returns the /repo Git checkout directory (<root>/workspaces/<id>/repo).
func (store *Store) RepoDir(id string) string {
	return filepath.Join(store.WorkspaceDir(id), "repo")
}

// Files returns the ports.Files view of the store.
func (store *Store) Files() ports.Files {
	return treeAdapter{store: store}
}

// Pods returns the ports.Provisioner view of the store.
func (store *Store) Pods() ports.Provisioner {
	return &podAdapter{store: store}
}

func (store *Store) loadCatalog() error {
	raw, err := os.ReadFile(store.catalogPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var list []domain.Workspace
	if err := json.Unmarshal(raw, &list); err != nil {
		return err
	}
	for _, ws := range list {
		store.rows[ws.ID] = ws
	}
	return nil
}

func (store *Store) saveCatalogLocked() error {
	rows := make([]domain.Workspace, 0, len(store.rows))
	for _, ws := range store.rows {
		rows = append(rows, ws)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	tmp := store.catalogPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, store.catalogPath)
}

// List returns all stored workspaces ordered by ID.
func (store *Store) List(context.Context) ([]domain.Workspace, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	rows := make([]domain.Workspace, 0, len(store.rows))
	for _, row := range store.rows {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	return rows, nil
}

// Get returns one workspace by ID.
func (store *Store) Get(_ context.Context, id string) (domain.Workspace, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	row, ok := store.rows[id]
	if !ok {
		return domain.Workspace{}, domain.ErrNotFound
	}
	return row, nil
}

// Save persists a workspace record and ensures its isolated /repo Git repository exists.
func (store *Store) Save(ctx context.Context, workspace domain.Workspace) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if strings.TrimSpace(workspace.Branch) == "" {
		owner := strings.TrimSpace(workspace.Owner)
		if owner == "" {
			owner = "operator"
		}
		workspace.Branch = fmt.Sprintf("workspace/%s/%s", sanitizeBranchSegment(owner), workspace.ID)
	}
	repoDir := store.RepoDir(workspace.ID)
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		baseHead, initErr := store.initWorkspaceRepoLocked(ctx, workspace, repoDir)
		if initErr != nil {
			return initErr
		}
		if workspace.BaseCommit == "" {
			workspace.BaseCommit = baseHead
		}
		if workspace.HeadCommit == "" {
			workspace.HeadCommit = baseHead
		}
		if workspace.SourceDigest == "" && baseHead != "" {
			workspace.SourceDigest = "git:" + baseHead
		}
		if (workspace.ProposalRevision == "" || workspace.ProposalRevision == "rev-1") && baseHead != "" {
			workspace.ProposalRevision = baseHead
		}
	}
	store.rows[workspace.ID] = workspace
	return store.saveCatalogLocked()
}

func (store *Store) initWorkspaceRepoLocked(ctx context.Context, workspace domain.Workspace, repoDir string) (string, error) {
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return "", err
	}
	remoteURL := strings.TrimSpace(workspace.RepoURL)
	if remoteURL == "" && store.upstreamDir != "" {
		remoteURL = store.upstreamDir
	}
	cloned := false
	if remoteURL != "" && isCloneableRepo(remoteURL) {
		if err := runGit(ctx, "", "clone", "--no-recurse-submodules", remoteURL, repoDir); err == nil {
			cloned = true
		}
	}
	if !cloned {
		if err := runGit(ctx, repoDir, "init"); err != nil {
			return "", err
		}
		if remoteURL != "" {
			_ = runGit(ctx, repoDir, "remote", "add", "origin", remoteURL)
		}
	}
	_ = runGit(ctx, repoDir, "config", "user.name", "workspace")
	_ = runGit(ctx, repoDir, "config", "user.email", "workspace@local")
	if head, err := gitOut(ctx, repoDir, "rev-parse", "--verify", "HEAD"); err == nil && strings.TrimSpace(head) != "" {
		_ = runGit(ctx, repoDir, "checkout", "-B", workspace.Branch)
		return strings.TrimSpace(head), nil
	}
	_ = runGit(ctx, repoDir, "symbolic-ref", "HEAD", "refs/heads/"+workspace.Branch)
	readme := filepath.Join(repoDir, "README.md")
	if _, err := os.Stat(readme); errors.Is(err, os.ErrNotExist) {
		title := workspace.Title
		if title == "" {
			title = workspace.ID
		}
		if err := os.WriteFile(readme, []byte("# "+title+"\n"), 0o644); err != nil {
			return "", err
		}
	}
	if err := runGit(ctx, repoDir, "add", "--", "README.md"); err != nil {
		return "", err
	}
	if err := runGit(ctx, repoDir, "-c", "user.name=workspace", "-c", "user.email=workspace@local", "commit", "-m", "initialize workspace branch"); err != nil {
		return "", err
	}
	head, err := gitOut(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(head), nil
}

// Delete removes a workspace record and its disk directory.
func (store *Store) Delete(_ context.Context, id string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.rows[id]; !ok {
		return domain.ErrNotFound
	}
	delete(store.rows, id)
	delete(store.runtimes, id)
	delete(store.pods, id)
	_ = os.RemoveAll(store.WorkspaceDir(id))
	return store.saveCatalogLocked()
}

// WriteAndCommit writes the given relative paths inside /repo and commits them
// on the workspace's Git branch. Paths outside /repo or matching secret file
// patterns are rejected.
func (store *Store) WriteAndCommit(ctx context.Context, id string, files map[string]string, message string) (domain.Workspace, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	ws, ok := store.rows[id]
	if !ok {
		return domain.Workspace{}, domain.ErrNotFound
	}
	repoDir := store.RepoDir(id)
	paths := make([]string, 0, len(files))
	for rel, content := range files {
		confined, err := domain.Confine(rel)
		if err != nil {
			return domain.Workspace{}, err
		}
		if isSecretPath(confined) {
			return domain.Workspace{}, fmt.Errorf("%w: refusing secret path %s", domain.ErrInvalid, confined)
		}
		full := filepath.Join(repoDir, filepath.FromSlash(confined))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return domain.Workspace{}, err
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return domain.Workspace{}, err
		}
		paths = append(paths, confined)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := runGit(ctx, repoDir, "add", "--", p); err != nil {
			return domain.Workspace{}, err
		}
	}
	if strings.TrimSpace(message) == "" {
		message = "workspace commit"
	}
	commitArgs := append([]string{"-c", "user.name=workspace", "-c", "user.email=workspace@local", "commit", "-m", message, "--"}, paths...)
	if err := runGit(ctx, repoDir, commitArgs...); err != nil {
		return domain.Workspace{}, err
	}
	head, err := gitOut(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return domain.Workspace{}, err
	}
	ws.HeadCommit = strings.TrimSpace(head)
	ws.ProposalRevision = ws.HeadCommit
	ws.UpdatedAt = time.Now().UTC()
	store.rows[id] = ws
	if err := store.saveCatalogLocked(); err != nil {
		return domain.Workspace{}, err
	}
	return ws, nil
}

// Sync stages and commits working-tree changes inside /repo onto the workspace
// Git branch. Secret-like files in /repo are rejected and files outside /repo
// are ignored.
func (store *Store) Sync(ctx context.Context, id, message string) (domain.Workspace, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	ws, ok := store.rows[id]
	if !ok {
		return domain.Workspace{}, domain.ErrNotFound
	}
	head, changed, err := store.commitWorkingTreeLocked(ctx, ws, message, false)
	if err != nil {
		return domain.Workspace{}, err
	}
	if changed {
		ws.HeadCommit = head
		ws.ProposalRevision = head
		ws.UpdatedAt = time.Now().UTC()
		store.rows[id] = ws
		if err := store.saveCatalogLocked(); err != nil {
			return domain.Workspace{}, err
		}
	}
	return ws, nil
}

func (store *Store) commitWorkingTreeLocked(ctx context.Context, ws domain.Workspace, message string, skipSecrets bool) (string, bool, error) {
	repoDir := store.RepoDir(ws.ID)
	statusOut, err := gitOut(ctx, repoDir, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil {
		return "", false, err
	}
	if statusOut == "" {
		head, err := gitOut(ctx, repoDir, "rev-parse", "HEAD")
		return strings.TrimSpace(head), false, err
	}
	entries := strings.Split(statusOut, "\x00")
	var toStage []string
	for _, entry := range entries {
		if len(entry) < 4 {
			continue
		}
		rel := entry[3:]
		if isSecretPath(rel) {
			if skipSecrets {
				continue
			}
			return "", false, fmt.Errorf("%w: refusing secret path %s", domain.ErrInvalid, rel)
		}
		toStage = append(toStage, rel)
	}
	if len(toStage) == 0 {
		head, err := gitOut(ctx, repoDir, "rev-parse", "HEAD")
		return strings.TrimSpace(head), false, err
	}
	for _, rel := range toStage {
		if err := runGit(ctx, repoDir, "add", "-A", "--", rel); err != nil {
			return "", false, err
		}
	}
	if strings.TrimSpace(message) == "" {
		message = "workspace sync"
	}
	if err := runGit(ctx, repoDir, "-c", "user.name=workspace", "-c", "user.email=workspace@local", "commit", "-m", message); err != nil {
		return "", false, err
	}
	head, err := gitOut(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(head), true, nil
}

// PreserveOnIdle transitions the workspace through STOPPING, commits any dirty
// /repo files (skipping secret files) to a preservation commit on the workspace
// branch, and pushes the branch to origin when configured. If the push fails,
// the workspace volume is marked preserved until now + retentionWindow.
func (store *Store) PreserveOnIdle(ctx context.Context, workspace domain.Workspace, now time.Time) (domain.Workspace, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	row, ok := store.rows[workspace.ID]
	if !ok {
		row = workspace
	}
	row.Status = domain.StatusStopping
	row.UpdatedAt = now
	head, changed, err := store.commitWorkingTreeLocked(ctx, row, "workspace idle preservation commit", true)
	if err == nil && head != "" {
		row.HeadCommit = head
		if changed || row.ProposalRevision == "" || row.ProposalRevision == "rev-1" {
			row.ProposalRevision = head
		}
	}
	pushErr := err
	if pushErr == nil {
		pushErr = store.pushBranchLocked(ctx, row)
	}
	if pushErr != nil {
		row.VolumePreserved = true
		row.PreservedUntil = now.Add(store.retentionWindow)
		store.rows[row.ID] = row
		_ = store.saveCatalogLocked()
		return row, pushErr
	}
	row.VolumePreserved = false
	row.PreservedUntil = time.Time{}
	store.rows[row.ID] = row
	_ = store.saveCatalogLocked()
	return row, nil
}

func (store *Store) pushBranchLocked(ctx context.Context, ws domain.Workspace) error {
	repoDir := store.RepoDir(ws.ID)
	remotes, err := gitOut(ctx, repoDir, "remote")
	if err != nil || !strings.Contains(remotes, "origin") {
		return nil
	}
	return runGit(ctx, repoDir, "push", "-u", "origin", "HEAD:refs/heads/"+ws.Branch)
}

// ExpirePreservedVolumes removes preserved workspace volumes whose retention
// window has elapsed and updates their catalog records.
func (store *Store) ExpirePreservedVolumes(_ context.Context, now time.Time) ([]domain.Workspace, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	expired := []domain.Workspace{}
	for id, row := range store.rows {
		if !row.VolumePreserved || row.PreservedUntil.IsZero() || now.Before(row.PreservedUntil) {
			continue
		}
		_ = os.RemoveAll(store.WorkspaceDir(id))
		row.VolumePreserved = false
		row.PreservedUntil = time.Time{}
		row.UpdatedAt = now
		store.rows[id] = row
		expired = append(expired, row)
	}
	if len(expired) > 0 {
		sort.Slice(expired, func(i, j int) bool { return expired[i].ID < expired[j].ID })
		if err := store.saveCatalogLocked(); err != nil {
			return nil, err
		}
	}
	return expired, nil
}

// ListDir implements ports.Files over committed Git state at HEAD.
func (store *Store) ListDir(ctx context.Context, id, prefix, cursor string, limit int) (domain.TreePage, error) {
	confined := "."
	var err error
	if prefix != "" && prefix != "." {
		confined, err = domain.Confine(prefix)
		if err != nil {
			return domain.TreePage{}, err
		}
	}
	store.mu.Lock()
	ws, ok := store.rows[id]
	store.mu.Unlock()
	if !ok {
		return domain.TreePage{}, domain.ErrNotFound
	}
	ref := ws.HeadCommit
	if ref == "" {
		ref = "HEAD"
	}
	committed, err := store.readCommittedSnapshot(ctx, store.RepoDir(id), ref)
	if err != nil {
		return domain.TreePage{}, err
	}
	if limit <= 0 || limit > domain.MaxTreePage {
		limit = domain.MaxTreePage
	}
	entries := childrenFromFiles(committed, confined)
	if cursor != "" {
		filtered := []domain.TreeEntry{}
		for _, entry := range entries {
			if entry.Path > cursor {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	page := domain.TreePage{Entries: []domain.TreeEntry{}, ValueTier: domain.ValueTier}
	if len(entries) > limit {
		page.Entries = entries[:limit]
		page.NextCursor = page.Entries[len(page.Entries)-1].Path
		return page, nil
	}
	page.Entries = entries
	return page, nil
}

// Read implements ports.Files over committed Git state at HEAD (or a specific commit/digest).
func (store *Store) Read(ctx context.Context, id, requestPath, revision string) (domain.FileView, error) {
	confined, err := domain.Confine(requestPath)
	if err != nil {
		return domain.FileView{}, err
	}
	store.mu.Lock()
	ws, ok := store.rows[id]
	store.mu.Unlock()
	if !ok {
		return domain.FileView{}, domain.ErrNotFound
	}
	ref := ws.HeadCommit
	if ref == "" {
		ref = "HEAD"
	}
	committed, err := store.readCommittedSnapshot(ctx, store.RepoDir(id), ref)
	if err != nil {
		return domain.FileView{}, err
	}
	content, exists := committed[confined]
	if !exists {
		return domain.FileView{}, domain.ErrMissing
	}
	digest := domain.Digest(content)
	if revision != "" && revision != digest && revision != ws.ProposalRevision && revision != ws.HeadCommit {
		return domain.FileView{}, domain.ErrStale
	}
	view := domain.FileView{
		Path:         confined,
		Revision:     digest,
		Digest:       digest,
		ValueTier:    domain.ValueTier,
		DownloadPath: "/v1/dev-workspaces/" + id + "/download?path=" + confined,
	}
	if len(content) > domain.MaxFileBytes {
		view.TooLarge = true
		return view, nil
	}
	if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
		view.Binary = true
		return view, nil
	}
	text, redacted := domain.RedactText(string(content))
	view.Content = text
	view.Redacted = redacted
	return view, nil
}

// Diff implements ports.Files by comparing BaseCommit against HeadCommit in Git.
// Uncommitted working-tree edits and files outside /repo are never included.
func (store *Store) Diff(ctx context.Context, id string) (domain.Diff, error) {
	store.mu.Lock()
	ws, ok := store.rows[id]
	store.mu.Unlock()
	if !ok {
		return domain.Diff{}, domain.ErrNotFound
	}
	repoDir := store.RepoDir(id)
	baseRef := ws.BaseCommit
	headRef := ws.HeadCommit
	if headRef == "" {
		headRef = "HEAD"
	}
	baseFiles := map[string][]byte{}
	if baseRef != "" {
		if snap, err := store.readCommittedSnapshot(ctx, repoDir, baseRef); err == nil {
			baseFiles = snap
		}
	}
	currentFiles, err := store.readCommittedSnapshot(ctx, repoDir, headRef)
	if err != nil {
		return domain.Diff{}, err
	}
	files := domain.DiffSnapshots(baseFiles, currentFiles)
	if files == nil {
		files = []domain.DiffFile{}
	}
	return domain.Diff{
		WorkspaceID:      ws.ID,
		SourceDigest:     ws.SourceDigest,
		ProposalRevision: ws.ProposalRevision,
		ArtifactDigest:   ws.ArtifactDigest,
		Current:          true,
		Files:            files,
		ValueTier:        domain.ValueTier,
	}, nil
}

// Copy clones a workspace's committed Git state into a new workspace checkout.
func (store *Store) Copy(ctx context.Context, fromID, toID string) error {
	store.mu.Lock()
	fromWS, ok := store.rows[fromID]
	store.mu.Unlock()
	if !ok {
		return domain.ErrNotFound
	}
	fromRepo := store.RepoDir(fromID)
	toRepo := store.RepoDir(toID)
	if err := os.MkdirAll(filepath.Dir(toRepo), 0o755); err != nil {
		return err
	}
	if err := runGit(ctx, "", "clone", "--no-recurse-submodules", fromRepo, toRepo); err != nil {
		return err
	}
	branch := fmt.Sprintf("workspace/%s/%s", sanitizeBranchSegment(fromWS.Owner), toID)
	return runGit(ctx, toRepo, "checkout", "-B", branch)
}

// treeAdapter disambiguates Catalog.List from Files.List when Store is used as ports.Files.
type treeAdapter struct {
	store *Store
}

func (t treeAdapter) List(ctx context.Context, id, prefix, cursor string, limit int) (domain.TreePage, error) {
	return t.store.ListDir(ctx, id, prefix, cursor, limit)
}

func (t treeAdapter) Read(ctx context.Context, id, path, revision string) (domain.FileView, error) {
	return t.store.Read(ctx, id, path, revision)
}

func (t treeAdapter) Diff(ctx context.Context, id string) (domain.Diff, error) {
	return t.store.Diff(ctx, id)
}

func (t treeAdapter) Copy(ctx context.Context, fromID, toID string) error {
	return t.store.Copy(ctx, fromID, toID)
}

func (t treeAdapter) PreserveOnIdle(ctx context.Context, workspace domain.Workspace, now time.Time) (domain.Workspace, error) {
	return t.store.PreserveOnIdle(ctx, workspace, now)
}

func (t treeAdapter) ExpirePreservedVolumes(ctx context.Context, now time.Time) ([]domain.Workspace, error) {
	return t.store.ExpirePreservedVolumes(ctx, now)
}

// Tree returns a ports.Files adapter over the store's committed Git state.
func (store *Store) Tree() ports.Files {
	return treeAdapter{store: store}
}

// Start implements ports.Runtime for local preview processes.
func (store *Store) Start(_ context.Context, workspace domain.Workspace) (domain.Runtime, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	rt := domain.Runtime{
		ID:               "rt_" + workspace.ID,
		WorkspaceID:      workspace.ID,
		Owner:            workspace.Owner,
		Status:           domain.StatusRunning,
		ProposalRevision: workspace.ProposalRevision,
		ArtifactDigest:   workspace.ArtifactDigest,
		Origin:           domain.OriginLocalRuntime,
	}
	store.runtimes[workspace.ID] = rt
	return rt, nil
}

// Stop implements ports.Runtime.
func (store *Store) Stop(_ context.Context, workspaceID string) (domain.Runtime, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	rt, ok := store.runtimes[workspaceID]
	if !ok {
		rt = domain.Runtime{ID: "rt_" + workspaceID, WorkspaceID: workspaceID, Origin: domain.OriginLocalRuntime}
	}
	rt.Status = domain.StatusStopped
	store.runtimes[workspaceID] = rt
	return rt, nil
}

// Status implements ports.Runtime.
func (store *Store) Status(_ context.Context, workspaceID string) (domain.Runtime, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	rt, ok := store.runtimes[workspaceID]
	if !ok {
		return domain.Runtime{}, domain.ErrNotFound
	}
	return rt, nil
}

// Logs implements ports.Runtime.
func (store *Store) Logs(_ context.Context, workspaceID, _ string, _ int) (domain.LogPage, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	status := domain.StatusStopped
	if rt, ok := store.runtimes[workspaceID]; ok {
		status = rt.Status
	}
	return domain.LogPage{Lines: []string{}, Origin: domain.OriginLocalRuntime, Status: status, ValueTier: domain.ValueTier}, nil
}

// Clear implements ports.Runtime.
func (store *Store) Clear(_ context.Context, _ string) error {
	return nil
}

type podAdapter struct {
	store *Store
}

func (p *podAdapter) Create(_ context.Context, workspace domain.Workspace) error {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	p.store.pods[workspace.ID] = struct{}{}
	return nil
}

func (p *podAdapter) Delete(_ context.Context, workspaceID string) error {
	p.store.mu.Lock()
	defer p.store.mu.Unlock()
	delete(p.store.pods, workspaceID)
	return nil
}

func (store *Store) readCommittedSnapshot(ctx context.Context, repoDir, ref string) (map[string][]byte, error) {
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

func childrenFromFiles(files map[string][]byte, prefix string) []domain.TreeEntry {
	seen := map[string]domain.TreeEntry{}
	root := prefix == "."
	for name, content := range files {
		if !root && name != prefix && !strings.HasPrefix(name, prefix+"/") {
			continue
		}
		rest := name
		if !root {
			if name == prefix {
				continue
			}
			rest = strings.TrimPrefix(name, prefix+"/")
		}
		head, _, nested := strings.Cut(rest, "/")
		path := head
		if !root {
			path = prefix + "/" + head
		}
		if nested {
			seen[path] = domain.TreeEntry{Path: path, Kind: "dir"}
			continue
		}
		if existing, isDir := seen[path]; isDir && existing.Kind == "dir" {
			continue
		}
		seen[path] = domain.TreeEntry{Path: path, Kind: "file", Digest: domain.Digest(content), Size: int64(len(content))}
	}
	entries := make([]domain.TreeEntry, 0, len(seen))
	for _, entry := range seen {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

func isSecretPath(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch {
	case base == ".env" || strings.HasPrefix(base, ".env."):
		return true
	case base == "id_rsa" || strings.HasPrefix(base, "id_"):
		return true
	case strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") || strings.HasSuffix(base, ".p12"):
		return true
	case base == "credentials" || strings.Contains(base, "secret"):
		return true
	default:
		return false
	}
}

func sanitizeBranchSegment(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "operator"
	}
	return out
}

func isCloneableRepo(remoteURL string) bool {
	if strings.HasPrefix(remoteURL, "file://") {
		return true
	}
	if info, err := os.Stat(remoteURL); err == nil && info.IsDir() {
		return true
	}
	return false
}

func runGit(ctx context.Context, dir string, args ...string) error {
	_, err := gitOut(ctx, dir, args...)
	return err
}

func gitOut(ctx context.Context, dir string, args ...string) (string, error) {
	raw, err := gitRawOut(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func gitRawOut(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

var _ ports.Catalog = (*Store)(nil)
var _ ports.BranchPreserver = (*Store)(nil)
var _ ports.Runtime = (*Store)(nil)
