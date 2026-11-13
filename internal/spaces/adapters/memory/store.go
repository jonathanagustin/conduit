package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/jonathanagustin/conduit/internal/spaces/domain"
	"github.com/jonathanagustin/conduit/internal/spaces/ports"
)

type Store struct {
	mu       sync.Mutex
	values   map[string]domain.Space
	bindings map[domain.Resource]domain.Binding
}

var _ ports.Store = (*Store)(nil)

func New() *Store {
	return &Store{values: map[string]domain.Space{}, bindings: map[domain.Resource]domain.Binding{}}
}
func (store *Store) CreateSpace(ctx context.Context, value domain.Space) (domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return domain.Space{}, err
	}
	if err := value.Validate(); err != nil {
		return domain.Space{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.values[value.ID]; exists {
		return domain.Space{}, domain.ErrConflict
	}
	store.values[value.ID] = value
	return value, nil
}
func (store *Store) GetSpace(ctx context.Context, id string) (domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return domain.Space{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, exists := store.values[id]
	if !exists {
		return domain.Space{}, domain.ErrNotFound
	}
	return value, nil
}
func (store *Store) ListSpaces(ctx context.Context) ([]domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	values := make([]domain.Space, 0, len(store.values))
	for _, value := range store.values {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values, nil
}

func (store *Store) BindResource(ctx context.Context, value domain.Binding) (domain.Binding, error) {
	if err := ctx.Err(); err != nil {
		return domain.Binding{}, err
	}
	if err := value.Validate(); err != nil {
		return domain.Binding{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.values[value.SpaceID]; !exists {
		return domain.Binding{}, domain.ErrNotFound
	}
	if existing, exists := store.bindings[value.Resource]; exists {
		if existing.SpaceID != value.SpaceID {
			return domain.Binding{}, domain.ErrConflict
		}
		return existing, nil
	}
	store.bindings[value.Resource] = value
	return value, nil
}
func (store *Store) GetBinding(ctx context.Context, resource domain.Resource) (domain.Binding, error) {
	if err := ctx.Err(); err != nil {
		return domain.Binding{}, err
	}
	if err := resource.Validate(); err != nil {
		return domain.Binding{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	value, exists := store.bindings[resource]
	if !exists {
		return domain.Binding{}, domain.ErrNotFound
	}
	return value, nil
}
func (store *Store) ListBindings(ctx context.Context, spaceID string) ([]domain.Binding, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, exists := store.values[spaceID]; !exists {
		return nil, domain.ErrNotFound
	}
	values := make([]domain.Binding, 0)
	for _, value := range store.bindings {
		if value.SpaceID == spaceID {
			values = append(values, value)
		}
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Resource.Kind == values[j].Resource.Kind {
			return values[i].Resource.ID < values[j].Resource.ID
		}
		return values[i].Resource.Kind < values[j].Resource.Kind
	})
	return values, nil
}
