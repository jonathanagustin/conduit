package application

import (
	"context"
	"strings"
	"sync"
)

// Memory is the process-local generation store. Its results are lost when
// the process stops. The PostgreSQL adapter keeps them.
type Memory struct {
	mu    sync.Mutex
	order []string
	items map[string]Result
}

var _ Store = (*Memory)(nil)

func (store *Memory) Save(_ context.Context, result Result) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.items == nil {
		store.items = map[string]Result{}
	}
	existing, ok := store.items[result.ID]
	if ok && existing.Owner != result.Owner {
		return ErrOwnerConflict
	}
	if !ok {
		store.order = append(store.order, result.ID)
	}
	store.items[result.ID] = result
	return nil
}

func (store *Memory) Get(_ context.Context, id string) (Result, bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	result, ok := store.items[id]
	return result, ok, nil
}

func (store *Memory) List(_ context.Context, owner string) ([]Result, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	out := []Result{}
	if strings.TrimSpace(owner) == "" {
		return out, nil
	}
	for _, id := range store.order {
		if item := store.items[id]; item.Owner == owner {
			out = append(out, item)
		}
	}
	return out, nil
}
