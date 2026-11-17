// Package recorded is the process-local attachment store. Tests and the
// default Hub use it; bytes are kept by lowercase sha256 hex.
package recorded

import (
	"context"
	"strings"
	"sync"

	"github.com/jonathanagustin/conduit/internal/attachments/domain"
)

// Store keeps attachments in memory, in the order they were first stored.
type Store struct {
	mu    sync.Mutex
	order []string
	items map[string]domain.Stored
}

// New returns an empty store.
func New() *Store { return &Store{items: map[string]domain.Stored{}} }

func (store *Store) Put(_ context.Context, item domain.Stored) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.items == nil {
		store.items = map[string]domain.Stored{}
	}
	if existing, ok := store.items[item.SHA256]; ok {
		existing.Owners = mergeOwners(existing.Owners, item.Owners)
		store.items[item.SHA256] = existing
		return false, nil
	}
	item.Owners = mergeOwners(nil, item.Owners)
	store.items[item.SHA256] = item
	store.order = append(store.order, item.SHA256)
	return true, nil
}

func mergeOwners(current, extra []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(current)+len(extra))
	for _, owner := range append(append([]string{}, current...), extra...) {
		owner = strings.TrimSpace(owner)
		if owner == "" || seen[owner] {
			continue
		}
		seen[owner] = true
		out = append(out, owner)
	}
	return out
}

func (store *Store) Get(_ context.Context, digest string) (domain.Stored, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	item, ok := store.items[digest]
	return item, ok, nil
}

func (store *Store) List(_ context.Context) ([]domain.Attachment, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	rows := make([]domain.Attachment, 0, len(store.order))
	for _, digest := range store.order {
		rows = append(rows, store.items[digest].Attachment)
	}
	return rows, nil
}
