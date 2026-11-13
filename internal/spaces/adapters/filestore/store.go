// Package filestore persists native Space identities and grouping intent in
// private files installed atomically without replacing existing assignments.
package filestore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jonathanagustin/conduit/internal/spaces/domain"
	"github.com/jonathanagustin/conduit/internal/spaces/ports"
)

type Store struct{ root string }

var _ ports.Store = (*Store)(nil)

func New(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}
func (store *Store) CreateSpace(ctx context.Context, value domain.Space) (domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return domain.Space{}, err
	}
	if err := value.Validate(); err != nil {
		return domain.Space{}, err
	}
	document, err := json.Marshal(value)
	if err != nil {
		return domain.Space{}, err
	}
	if err := install(ctx, store.root, value.ID+".json", document); err != nil {
		return domain.Space{}, err
	}
	return value, nil
}

func install(ctx context.Context, directory, key string, document []byte) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".space-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(append(document, '\n')); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// Link installs a complete file without replacing another process's record.
	if err := os.Link(temporary.Name(), filepath.Join(directory, key)); err != nil {
		if errors.Is(err, os.ErrExist) {
			return domain.ErrConflict
		}
		return err
	}
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

func (store *Store) GetSpace(ctx context.Context, id string) (domain.Space, error) {
	if err := ctx.Err(); err != nil {
		return domain.Space{}, err
	}
	if !domain.ValidID(id) {
		return domain.Space{}, domain.ErrNotFound
	}
	document, err := os.ReadFile(filepath.Join(store.root, id+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return domain.Space{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Space{}, err
	}
	var value domain.Space
	if err := json.Unmarshal(document, &value); err != nil {
		return domain.Space{}, fmt.Errorf("decode Space record: %w", err)
	}
	if value.ID != id {
		return domain.Space{}, errors.New("Space record identity does not match its storage key")
	}
	if err := value.Validate(); err != nil {
		return domain.Space{}, err
	}
	return value, nil
}
func (store *Store) ListSpaces(ctx context.Context) ([]domain.Space, error) {
	entries, err := os.ReadDir(store.root)
	if err != nil {
		return nil, err
	}
	values := make([]domain.Space, 0)
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".json")
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") || !domain.ValidID(id) {
			continue
		}
		value, err := store.GetSpace(ctx, id)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func bindingKey(resource domain.Resource) string {
	document, _ := json.Marshal(resource)
	digest := sha256.Sum256(document)
	return hex.EncodeToString(digest[:]) + ".json"
}
func (store *Store) BindResource(ctx context.Context, value domain.Binding) (domain.Binding, error) {
	if err := value.Validate(); err != nil {
		return domain.Binding{}, err
	}
	if _, err := store.GetSpace(ctx, value.SpaceID); err != nil {
		return domain.Binding{}, err
	}
	document, err := json.Marshal(value)
	if err != nil {
		return domain.Binding{}, err
	}
	err = install(ctx, filepath.Join(store.root, "bindings"), bindingKey(value.Resource), document)
	if err != nil && !errors.Is(err, domain.ErrConflict) {
		return domain.Binding{}, err
	}
	existing, err := store.GetBinding(ctx, value.Resource)
	if err != nil {
		return domain.Binding{}, err
	}
	if existing.SpaceID != value.SpaceID {
		return domain.Binding{}, domain.ErrConflict
	}
	return existing, nil
}
func (store *Store) GetBinding(ctx context.Context, resource domain.Resource) (domain.Binding, error) {
	if err := ctx.Err(); err != nil {
		return domain.Binding{}, err
	}
	if err := resource.Validate(); err != nil {
		return domain.Binding{}, err
	}
	value, err := readBinding(filepath.Join(store.root, "bindings", bindingKey(resource)))
	if err != nil {
		return domain.Binding{}, err
	}
	if value.Resource != resource {
		return domain.Binding{}, errors.New("Space binding does not match its resource key")
	}
	return value, nil
}
func (store *Store) ListBindings(ctx context.Context, spaceID string) ([]domain.Binding, error) {
	if _, err := store.GetSpace(ctx, spaceID); err != nil {
		return nil, err
	}
	directory := filepath.Join(store.root, "bindings")
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return []domain.Binding{}, nil
	}
	if err != nil {
		return nil, err
	}
	values := make([]domain.Binding, 0)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := readBinding(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if bindingKey(value.Resource) != entry.Name() {
			return nil, errors.New("Space binding does not match its storage key")
		}
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
func readBinding(path string) (domain.Binding, error) {
	document, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return domain.Binding{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Binding{}, err
	}
	var value domain.Binding
	if err := json.Unmarshal(document, &value); err != nil {
		return domain.Binding{}, fmt.Errorf("decode Space binding: %w", err)
	}
	if err := value.Validate(); err != nil {
		return domain.Binding{}, err
	}
	return value, nil
}
