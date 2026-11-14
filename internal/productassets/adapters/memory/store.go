// Package memory supplies immutable in-memory download metadata for tests and
// explicitly ephemeral composition. Production uses file or PostgreSQL storage.
package memory

import (
	"context"
	"github.com/jonathanagustin/conduit/internal/productassets/domain"
	"github.com/jonathanagustin/conduit/internal/productassets/ports"
	"sync"
)

type Store struct {
	mu      sync.Mutex
	records map[string]domain.Prepared
}

var _ ports.Store = (*Store)(nil)

func New() *Store { return &Store{records: map[string]domain.Prepared{}} }
func (store *Store) Prepare(ctx context.Context, record domain.Prepared) (domain.Prepared, error) {
	if err := ctx.Err(); err != nil {
		return domain.Prepared{}, err
	}
	if err := record.Validate(); err != nil {
		return domain.Prepared{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	key := record.Manifest.Coordinate()
	if existing, ok := store.records[key]; ok {
		if existing.Digest != record.Digest {
			return domain.Prepared{}, domain.ErrConflict
		}
		return domain.Clone(existing), nil
	}
	store.records[key] = domain.Clone(record)
	return domain.Clone(record), nil
}
func (store *Store) Get(ctx context.Context, coordinate string) (domain.Prepared, error) {
	if err := ctx.Err(); err != nil {
		return domain.Prepared{}, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	record, ok := store.records[coordinate]
	if !ok {
		return domain.Prepared{}, domain.ErrNotFound
	}
	return domain.Clone(record), nil
}
