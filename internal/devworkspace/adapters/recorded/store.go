// Package recorded is the no-cluster workspace adapter. Tests and the optional
// seed use it when no cluster is present.
package recorded

import (
	"context"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/jonathanagustin/conduit/internal/devworkspace/domain"
)

// Store is an in-memory catalog, tree, runtime, and pod record.
type Store struct {
	mu       sync.Mutex
	rows     map[string]domain.Workspace
	current  map[string]map[string][]byte
	base     map[string]map[string][]byte
	runtime  map[string]domain.Runtime
	logs     map[string][]string
	closed   map[string]bool
	pods     map[string]struct{}
	starts   int
	failNext bool
}

// New returns an empty store.
func New() *Store {
	return &Store{
		rows: map[string]domain.Workspace{}, current: map[string]map[string][]byte{},
		base: map[string]map[string][]byte{}, runtime: map[string]domain.Runtime{},
		logs: map[string][]string{}, closed: map[string]bool{}, pods: map[string]struct{}{},
	}
}

// PutFile sets the current bytes of one path.
func (store *Store) PutFile(id, path string, content []byte) error {
	confined, err := domain.Confine(path)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.rows[id]; !ok && store.current[id] == nil {
		return domain.ErrNotFound
	}
	if store.current[id] == nil {
		store.current[id] = map[string][]byte{}
	}
	store.current[id][confined] = append([]byte(nil), content...)
	return nil
}

// PutBase sets the comparison base for one path.
func (store *Store) PutBase(id, path string, content []byte) error {
	confined, err := domain.Confine(path)
	if err != nil {
		return err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.base[id] == nil {
		store.base[id] = map[string][]byte{}
	}
	store.base[id][confined] = append([]byte(nil), content...)
	return nil
}

// FailNextStart makes the following Start report a failure.
func (store *Store) FailNextStart() {
	store.mu.Lock()
	store.failNext = true
	store.mu.Unlock()
}

// Starts is how many times Start launched a new runtime.
func (store *Store) Starts() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return store.starts
}

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

func (store *Store) Get(_ context.Context, id string) (domain.Workspace, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	row, ok := store.rows[id]
	if !ok {
		return domain.Workspace{}, domain.ErrNotFound
	}
	return row, nil
}

func (store *Store) Save(_ context.Context, workspace domain.Workspace) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.rows[workspace.ID] = workspace
	if store.current[workspace.ID] == nil {
		store.current[workspace.ID] = map[string][]byte{}
	}
	return nil
}

func (store *Store) Delete(_ context.Context, id string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.rows, id)
	delete(store.current, id)
	delete(store.base, id)
	return nil
}

// Files adapts the store to the file port.
func (store *Store) Files() *Tree { return &Tree{store: store} }

// Tree is the file port over a store.
type Tree struct{ store *Store }

func (tree *Tree) List(ctx context.Context, id, prefix, cursor string, limit int) (domain.TreePage, error) {
	return tree.store.listDir(ctx, id, prefix, cursor, limit)
}

func (tree *Tree) Read(ctx context.Context, id, path, revision string) (domain.FileView, error) {
	return tree.store.Read(ctx, id, path, revision)
}

func (tree *Tree) Write(ctx context.Context, id, path string, content []byte) (domain.FileView, error) {
	return tree.store.Write(ctx, id, path, content)
}

func (tree *Tree) Snapshot(ctx context.Context, id string) (map[string][]byte, error) {
	return tree.store.Snapshot(ctx, id)
}

func (tree *Tree) Diff(ctx context.Context, id string) (domain.Diff, error) {
	return tree.store.Diff(ctx, id)
}

func (tree *Tree) Copy(ctx context.Context, fromID, toID string) error {
	return tree.store.Copy(ctx, fromID, toID)
}

func (store *Store) listDir(_ context.Context, id, prefix, cursor string, limit int) (domain.TreePage, error) {
	confined := "."
	var err error
	if prefix != "" && prefix != "." {
		confined, err = domain.Confine(prefix)
		if err != nil {
			return domain.TreePage{}, err
		}
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.rows[id]; !ok {
		return domain.TreePage{}, domain.ErrNotFound
	}
	if limit <= 0 || limit > domain.MaxTreePage {
		limit = domain.MaxTreePage
	}
	entries := children(store.current[id], confined)
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

func (store *Store) Read(_ context.Context, id, requestPath, revision string) (domain.FileView, error) {
	confined, err := domain.Confine(requestPath)
	if err != nil {
		return domain.FileView{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	files := store.current[id]
	if files == nil {
		if _, ok := store.rows[id]; !ok {
			return domain.FileView{}, domain.ErrNotFound
		}
		return domain.FileView{}, domain.ErrMissing
	}
	content, ok := files[confined]
	if !ok {
		return domain.FileView{}, domain.ErrMissing
	}
	digest := domain.Digest(content)
	view := domain.FileView{
		Path: confined, Revision: digest, Digest: digest, ValueTier: domain.ValueTier,
		DownloadPath: "/v1/dev-workspaces/" + id + "/download?path=" + confined,
	}
	if revision != "" && revision != digest {
		return domain.FileView{}, domain.ErrStale
	}
	if len(content) > domain.MaxFileBytes {
		view.TooLarge = true
		return view, nil
	}
	if !utf8.Valid(content) || bytesContain(content, 0) {
		view.Binary = true
		return view, nil
	}
	text, redacted := domain.RedactText(string(content))
	view.Content = text
	view.Redacted = redacted
	return view, nil
}

func (store *Store) Write(_ context.Context, id, requestPath string, content []byte) (domain.FileView, error) {
	confined, err := domain.Confine(requestPath)
	if err != nil {
		return domain.FileView{}, err
	}
	if len(content) > domain.MaxFileBytes {
		return domain.FileView{}, domain.ErrInvalid
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.rows[id]; !ok {
		return domain.FileView{}, domain.ErrNotFound
	}
	if store.current[id] == nil {
		store.current[id] = map[string][]byte{}
	}
	copied := append([]byte(nil), content...)
	store.current[id][confined] = copied
	digest := domain.Digest(copied)
	view := domain.FileView{
		Path: confined, Revision: digest, Digest: digest, ValueTier: domain.ValueTier,
		DownloadPath: "/v1/dev-workspaces/" + id + "/download?path=" + confined,
	}
	if !utf8.Valid(copied) || bytesContain(copied, 0) {
		view.Binary = true
		return view, nil
	}
	text, redacted := domain.RedactText(string(copied))
	view.Content = text
	view.Redacted = redacted
	return view, nil
}

func (store *Store) Snapshot(_ context.Context, id string) (map[string][]byte, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.rows[id]; !ok {
		return nil, domain.ErrNotFound
	}
	return cloneFiles(store.current[id]), nil
}

func (store *Store) Diff(_ context.Context, id string) (domain.Diff, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.rows[id]; !ok {
		return domain.Diff{}, domain.ErrNotFound
	}
	files := domain.DiffSnapshots(store.base[id], store.current[id])
	if files == nil {
		files = []domain.DiffFile{}
	}
	return domain.Diff{Files: files, Current: true, ValueTier: domain.ValueTier}, nil
}

func (store *Store) Copy(_ context.Context, fromID, toID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, ok := store.rows[fromID]; !ok && store.current[fromID] == nil {
		return domain.ErrNotFound
	}
	store.current[toID] = cloneFiles(store.current[fromID])
	store.base[toID] = cloneFiles(store.base[fromID])
	return nil
}

func (store *Store) Start(_ context.Context, workspace domain.Workspace) (domain.Runtime, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if current, ok := store.runtime[workspace.ID]; ok && (current.Status == domain.StatusRunning || current.Status == domain.StatusStarting) {
		return current, nil
	}
	store.starts++
	if store.failNext {
		store.failNext = false
		store.runtime[workspace.ID] = domain.Runtime{
			ID: "rt_" + workspace.ID, WorkspaceID: workspace.ID, Owner: workspace.Owner,
			Status: domain.StatusFailed, Failure: "start failed", Origin: domain.OriginLocalRuntime,
		}
		return store.runtime[workspace.ID], domain.ErrInvalid
	}
	store.closed[workspace.ID] = false
	store.logs[workspace.ID] = []string{}
	store.runtime[workspace.ID] = domain.Runtime{
		ID: "rt_" + workspace.ID, WorkspaceID: workspace.ID, Owner: workspace.Owner,
		Status: domain.StatusRunning, Origin: domain.OriginLocalRuntime,
	}
	return store.runtime[workspace.ID], nil
}

func (store *Store) Stop(_ context.Context, workspaceID string) (domain.Runtime, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	runtime, ok := store.runtime[workspaceID]
	if !ok {
		runtime = domain.Runtime{ID: "rt_" + workspaceID, WorkspaceID: workspaceID, Origin: domain.OriginLocalRuntime}
	}
	runtime.Status = domain.StatusStopped
	store.runtime[workspaceID] = runtime
	store.closed[workspaceID] = true
	return runtime, nil
}

func (store *Store) Status(_ context.Context, workspaceID string) (domain.Runtime, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	runtime, ok := store.runtime[workspaceID]
	if !ok {
		return domain.Runtime{}, domain.ErrNotFound
	}
	return runtime, nil
}

func (store *Store) Logs(_ context.Context, workspaceID, cursor string, limit int) (domain.LogPage, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	lines := store.logs[workspaceID]
	start := 0
	if cursor != "" {
		for index, line := range lines {
			if line == cursor {
				start = index + 1
				break
			}
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	page := lines[start:]
	next := ""
	if len(page) > limit {
		page = page[:limit]
		next = page[len(page)-1]
	}
	status := domain.StatusStopped
	if runtime, ok := store.runtime[workspaceID]; ok {
		status = runtime.Status
	}
	return domain.LogPage{Lines: append([]string{}, page...), NextCursor: next, Origin: domain.OriginLocalRuntime, Status: status, ValueTier: domain.ValueTier}, nil
}

func (store *Store) Clear(_ context.Context, workspaceID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.logs[workspaceID] = []string{}
	return nil
}

// AppendLog adds one line unless collection has stopped.
func (store *Store) AppendLog(workspaceID, line string) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.closed[workspaceID] {
		return
	}
	text, _ := domain.RedactLine(line)
	total := 0
	for _, existing := range store.logs[workspaceID] {
		total += len(existing)
	}
	if total >= domain.MaxLogBytes {
		if len(store.logs[workspaceID]) == 0 || store.logs[workspaceID][len(store.logs[workspaceID])-1] != "[truncated]" {
			store.logs[workspaceID] = append(store.logs[workspaceID], "[truncated]")
		}
		return
	}
	store.logs[workspaceID] = append(store.logs[workspaceID], text)
}

func (store *Store) Create(_ context.Context, workspace domain.Workspace) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.pods[workspace.ID] = struct{}{}
	return nil
}

func (store *Store) DeletePod(_ context.Context, workspaceID string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.pods, workspaceID)
	return nil
}

// Pods adapts the store to the provisioner port. The catalog already owns Delete.
func (store *Store) Pods() *podPort { return &podPort{store: store} }

type podPort struct{ store *Store }

func (port *podPort) Create(ctx context.Context, workspace domain.Workspace) error {
	return port.store.Create(ctx, workspace)
}

func (port *podPort) Delete(ctx context.Context, workspaceID string) error {
	return port.store.DeletePod(ctx, workspaceID)
}

func children(files map[string][]byte, prefix string) []domain.TreeEntry {
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
		if _, isDir := seen[path]; isDir && seen[path].Kind == "dir" {
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

func cloneFiles(files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for path, content := range files {
		out[path] = append([]byte(nil), content...)
	}
	return out
}

func bytesContain(content []byte, value byte) bool {
	for _, item := range content {
		if item == value {
			return true
		}
	}
	return false
}
