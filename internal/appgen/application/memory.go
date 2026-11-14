package application

import (
	"context"
	"sync"
)

// Memory is the process-local generation store.
type Memory struct {
	mu    sync.Mutex
	order []string
	items map[string]Result
}

func (store *Memory) Save(_ context.Context, result Result) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.items == nil {
		store.items = map[string]Result{}
	}
	if _, ok := store.items[result.ID]; !ok {
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

func (store *Memory) List(_ context.Context) ([]Result, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	out := make([]Result, 0, len(store.order))
	for _, id := range store.order {
		out = append(out, store.items[id])
	}
	return out, nil
}
