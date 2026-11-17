package repoclone

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// cacheKey identifies one reusable checkout. The commit is part of it, so a
// branch that moved is a different entry and never the old files. The
// connection is part of it, so a checkout fetched with one credential is not
// handed to a clone that would have used another, or none.
type cacheKey struct {
	url        string
	sha        string
	connection string
}

type cacheEntry struct {
	key  cacheKey
	dir  string
	refs int
	dead bool
}

// cache holds a bounded number of pristine checkouts, least recently used
// first out. Callers never receive an entry's directory, only a copy of it.
type cache struct {
	mu      sync.Mutex
	max     int
	entries map[cacheKey]*cacheEntry
	// order is least recently used first.
	order []cacheKey
}

func newCache(max int) *cache {
	return &cache{max: max, entries: map[cacheKey]*cacheEntry{}}
}

// acquire returns the entry for key and holds it against removal until
// release. It returns nil when there is none.
func (store *cache) acquire(key cacheKey) *cacheEntry {
	store.mu.Lock()
	defer store.mu.Unlock()
	entry := store.entries[key]
	if entry == nil {
		return nil
	}
	entry.refs++
	store.touch(key)
	return entry
}

func (store *cache) release(entry *cacheEntry) {
	store.mu.Lock()
	entry.refs--
	remove := entry.dead && entry.refs == 0
	store.mu.Unlock()
	if remove {
		os.RemoveAll(entry.dir)
	}
}

// expand finds a cached commit of the same repository and connection that
// starts with an abbreviated commit. It returns "" unless exactly one does.
func (store *cache) expand(key cacheKey, prefix string) string {
	store.mu.Lock()
	defer store.mu.Unlock()
	found := ""
	for item := range store.entries {
		if item.url == key.url && item.connection == key.connection && strings.HasPrefix(item.sha, prefix) {
			if found != "" {
				return ""
			}
			found = item.sha
		}
	}
	return found
}

// add takes ownership of dir as the entry for key and evicts the least
// recently used entries beyond the bound. A key already present keeps its
// entry, and dir is removed.
func (store *cache) add(key cacheKey, dir string) {
	var remove []string
	store.mu.Lock()
	if _, ok := store.entries[key]; ok {
		store.touch(key)
		remove = append(remove, dir)
	} else {
		store.entries[key] = &cacheEntry{key: key, dir: dir}
		store.order = append(store.order, key)
		for len(store.order) > store.max {
			oldest := store.order[0]
			store.order = store.order[1:]
			entry := store.entries[oldest]
			delete(store.entries, oldest)
			entry.dead = true
			if entry.refs == 0 {
				remove = append(remove, entry.dir)
			}
		}
	}
	store.mu.Unlock()
	for _, path := range remove {
		os.RemoveAll(path)
	}
}

func (store *cache) touch(key cacheKey) {
	for index, item := range store.order {
		if item == key {
			store.order = append(append(store.order[:index:index], store.order[index+1:]...), key)
			return
		}
	}
}

func (store *cache) close() {
	store.mu.Lock()
	var remove []string
	for key, entry := range store.entries {
		entry.dead = true
		if entry.refs == 0 {
			remove = append(remove, entry.dir)
		}
		delete(store.entries, key)
	}
	store.order = nil
	store.mu.Unlock()
	for _, path := range remove {
		os.RemoveAll(path)
	}
}

// copyOut copies a checkout into a new temporary directory. Symbolic links
// are copied as links and are not followed.
func copyOut(source string) (string, error) {
	target, err := os.MkdirTemp("", "conduit-planning-")
	if err != nil {
		return "", err
	}
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case entry.IsDir():
			if relative == "." {
				return nil
			}
			return os.Mkdir(destination, info.Mode().Perm()|0o700)
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, destination)
		case info.Mode().IsRegular():
			return copyFile(path, destination, info.Mode().Perm())
		default:
			return nil
		}
	})
	if err != nil {
		os.RemoveAll(target)
		return "", err
	}
	return target, nil
}

func copyFile(source, destination string, mode fs.FileMode) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// dirSize is the bytes of the regular files under dir. A file that vanishes
// while git is writing is skipped.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func overLimit(dir string, max int64) bool {
	return max > 0 && dirSize(dir) > max
}

// watchSize stops the clone, by cancelling its context with ErrTooLarge, as
// soon as a sample finds dir over max. The returned function ends the watch.
func watchSize(ctx context.Context, dir string, max int64, cancel context.CancelCauseFunc) func() {
	if max <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if overLimit(dir, max) {
					cancel(ErrTooLarge)
					return
				}
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() { close(done) })
		<-finished
	}
}
